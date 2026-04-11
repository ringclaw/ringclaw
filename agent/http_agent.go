package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ringclaw/ringclaw/config"
)

func init() {
	Register("http", func(_ context.Context, name string, cfg config.AgentConfig, cwd string) (Agent, error) {
		if cfg.Endpoint == "" {
			return nil, fmt.Errorf("HTTP agent %q has no endpoint", name)
		}
		var timeout time.Duration
		if cfg.Timeout > 0 {
			timeout = time.Duration(cfg.Timeout) * time.Second
		}
		return NewHTTPAgent(HTTPAgentConfig{
			Name:         name,
			Endpoint:     cfg.Endpoint,
			APIKey:       cfg.APIKey,
			Headers:      cfg.Headers,
			Model:        cfg.Model,
			SystemPrompt: cfg.SystemPrompt,
			MaxHistory:   cfg.MaxHistory,
			Format:       cfg.Format,
			Cwd:          cwd,
			Sender:       cfg.Sender,
			ContextMode:  cfg.ContextMode,
			GroupJID:     cfg.GroupJID,
			Timeout:      timeout,
		}), nil
	})
}

// ChatMessage represents a single message in a conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// httpFormat abstracts the request/response protocol for different HTTP APIs.
type httpFormat interface {
	buildRequest(conversationID, message string, opts formatOpts) ([]byte, error)
	parseResponse(body []byte) (string, error)
	managesHistory() bool
	supportsCwd() bool
}

type formatOpts struct {
	Model        string
	SystemPrompt string
	Cwd          string
	History      []ChatMessage
	Sender       string
	ContextMode  string
	GroupJID     string
}

// HTTPAgent is an HTTP-based chat agent supporting multiple API formats.
type HTTPAgent struct {
	name         string
	endpoint     string
	apiKey       string
	headers      map[string]string
	model        string
	systemPrompt string
	httpClient   *http.Client
	mu           sync.Mutex
	format       httpFormat
	cwd          string
	history      map[string][]ChatMessage
	maxHistory   int
	sender       string
	contextMode  string
	groupJID     string
}

// HTTPAgentConfig holds configuration for the HTTP agent.
type HTTPAgentConfig struct {
	Name         string
	Endpoint     string
	APIKey       string
	Headers      map[string]string
	Model        string
	SystemPrompt string
	MaxHistory   int
	Format       string
	Cwd          string
	Sender       string
	ContextMode  string
	GroupJID     string
	Timeout      time.Duration
}

// NewHTTPAgent creates a new HTTP agent with the specified format.
func NewHTTPAgent(cfg HTTPAgentConfig) *HTTPAgent {
	if cfg.MaxHistory == 0 {
		cfg.MaxHistory = 20
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	cwd := cfg.Cwd
	if cwd == "" {
		cwd = defaultWorkspace()
	}

	var f httpFormat
	switch strings.ToLower(cfg.Format) {
	case "nanoclaw":
		f = &nanoclawFormat{}
	case "dify":
		f = newDifyFormat(cfg.Endpoint, cfg.APIKey, &http.Client{Timeout: timeout})
	default:
		f = &openaiFormat{}
	}

	return &HTTPAgent{
		name:         cfg.Name,
		endpoint:     cfg.Endpoint,
		apiKey:       cfg.APIKey,
		headers:      cloneHeaders(cfg.Headers),
		model:        cfg.Model,
		systemPrompt: cfg.SystemPrompt,
		httpClient:   &http.Client{Timeout: timeout},
		history:      make(map[string][]ChatMessage),
		maxHistory:   cfg.MaxHistory,
		format:       f,
		cwd:          cwd,
		sender:       strings.TrimSpace(cfg.Sender),
		contextMode:  strings.TrimSpace(cfg.ContextMode),
		groupJID:     strings.TrimSpace(cfg.GroupJID),
	}
}

func (a *HTTPAgent) Info() AgentInfo {
	name := a.name
	if name == "" {
		name = "http"
	}
	return AgentInfo{
		Name:    name,
		Type:    "http",
		Model:   a.model,
		Command: a.endpoint,
	}
}

func (a *HTTPAgent) SetCwd(cwd string) {
	if !a.format.supportsCwd() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cwd = cwd
}

func (a *HTTPAgent) ResetSession(ctx context.Context, conversationID string) (string, error) {
	if !a.format.managesHistory() {
		a.mu.Lock()
		delete(a.history, conversationID)
		a.mu.Unlock()
	}
	// For formats that manage sessions server-side (e.g. dify), clear their mapping too.
	type sessionResetter interface {
		resetConversation(ctx context.Context, conversationID string)
	}
	if r, ok := a.format.(sessionResetter); ok {
		r.resetConversation(ctx, conversationID)
	}
	return "", nil
}

func (a *HTTPAgent) Chat(ctx context.Context, conversationID string, message string) (string, error) {
	a.mu.Lock()
	opts := formatOpts{
		Model:        a.model,
		SystemPrompt: a.systemPrompt,
		Cwd:          a.cwd,
		Sender:       a.sender,
		ContextMode:  a.contextMode,
		GroupJID:     a.groupJID,
	}
	if !a.format.managesHistory() {
		opts.History = a.history[conversationID]
	}
	a.mu.Unlock()

	body, err := a.format.buildRequest(conversationID, message, opts)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	respBody, err := a.doHTTP(ctx, body)
	if err != nil {
		return "", err
	}

	reply, err := a.format.parseResponse(respBody)
	if err != nil {
		return "", err
	}

	if !a.format.managesHistory() {
		a.mu.Lock()
		a.history[conversationID] = append(a.history[conversationID],
			ChatMessage{Role: "user", Content: message},
			ChatMessage{Role: "assistant", Content: reply},
		)
		if len(a.history[conversationID]) > a.maxHistory*2 {
			a.history[conversationID] = a.history[conversationID][len(a.history[conversationID])-a.maxHistory*2:]
		}
		a.mu.Unlock()
	}

	return reply, nil
}

func (a *HTTPAgent) doHTTP(ctx context.Context, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// --- OpenAI format ---

type openaiFormat struct{}

func (f *openaiFormat) managesHistory() bool { return false }
func (f *openaiFormat) supportsCwd() bool    { return false }

func (f *openaiFormat) buildRequest(_, message string, opts formatOpts) ([]byte, error) {
	var messages []ChatMessage
	if opts.SystemPrompt != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: opts.SystemPrompt})
	}
	messages = append(messages, opts.History...)
	messages = append(messages, ChatMessage{Role: "user", Content: message})

	return json.Marshal(map[string]interface{}{
		"model":    opts.Model,
		"messages": messages,
	})
}

func (f *openaiFormat) parseResponse(body []byte) (string, error) {
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return result.Choices[0].Message.Content, nil
}

// --- NanoClaw format ---

type nanoclawFormat struct{}

func (f *nanoclawFormat) managesHistory() bool { return true }
func (f *nanoclawFormat) supportsCwd() bool    { return true }

func (f *nanoclawFormat) buildRequest(conversationID, message string, opts formatOpts) ([]byte, error) {
	payload := struct {
		ConversationID string `json:"conversation_id"`
		Message        string `json:"message"`
		GroupJID       string `json:"group_jid,omitempty"`
		Sender         string `json:"sender,omitempty"`
		ContextMode    string `json:"context_mode,omitempty"`
		Cwd            string `json:"cwd,omitempty"`
		SystemPrompt   string `json:"system_prompt,omitempty"`
	}{
		ConversationID: conversationID,
		Message:        message,
		GroupJID:       opts.GroupJID,
		Sender:         opts.Sender,
		ContextMode:    opts.ContextMode,
		Cwd:            opts.Cwd,
		SystemPrompt:   opts.SystemPrompt,
	}
	return json.Marshal(payload)
}

func (f *nanoclawFormat) parseResponse(body []byte) (string, error) {
	var parsed struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && strings.TrimSpace(parsed.Reply) != "" {
		return strings.TrimSpace(parsed.Reply), nil
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "", fmt.Errorf("empty response")
	}
	return trimmed, nil
}

// --- Dify format ---

const difySessionMaxAge = 24 * time.Hour

// difySession holds Dify-side state for one RingClaw conversation.
type difySession struct {
	convID     string    // Dify conversation_id
	user       string    // user identifier sent to Dify
	lastAccess time.Time // for stale session eviction
}

// difyFormat implements the Dify chatflow API.
// Dify manages conversation history server-side, identified by its own conversation_id.
// We map each RingClaw conversationID to the corresponding Dify session.
type difyFormat struct {
	baseURL       string
	apiKey        string
	httpClient    *http.Client
	mu            sync.Mutex
	sessions      map[string]difySession // ringclawConvID -> dify session
	pendingConvID string                 // set by buildRequest, read by parseResponse (same Chat call)
}

func newDifyFormat(endpoint, apiKey string, client *http.Client) *difyFormat {
	// Derive baseURL: strip from "/v1/" onward so DELETE paths work correctly
	// e.g. "https://api.dify.ai/v1/chat-messages" → "https://api.dify.ai"
	baseURL := endpoint
	if u, err := url.Parse(endpoint); err == nil {
		path := u.Path
		if idx := strings.Index(path, "/v1/"); idx >= 0 {
			u.Path = path[:idx]
		} else {
			u.Path = ""
		}
		u.RawQuery = ""
		u.Fragment = ""
		baseURL = u.String()
	}
	if strings.HasPrefix(endpoint, "http://") {
		slog.Warn("dify endpoint uses HTTP; consider HTTPS to avoid 301 redirect (POST→GET downgrade)", "endpoint", endpoint)
	}
	return &difyFormat{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: client,
		sessions:   make(map[string]difySession),
	}
}

func (f *difyFormat) managesHistory() bool { return true }
func (f *difyFormat) supportsCwd() bool    { return false }

func (f *difyFormat) buildRequest(conversationID, message string, opts formatOpts) ([]byte, error) {
	user := opts.Sender
	if user == "" {
		user = conversationID
	}

	now := time.Now()
	f.mu.Lock()
	s := f.sessions[conversationID]
	s.user = user
	s.lastAccess = now
	f.sessions[conversationID] = s
	difyConvID := s.convID
	f.pendingConvID = conversationID
	// Evict stale sessions
	for k, v := range f.sessions {
		if k != conversationID && now.Sub(v.lastAccess) > difySessionMaxAge {
			delete(f.sessions, k)
		}
	}
	f.mu.Unlock()

	return json.Marshal(map[string]interface{}{
		"inputs":          map[string]interface{}{},
		"query":           message,
		"response_mode":   "blocking",
		"conversation_id": difyConvID,
		"user":            user,
	})
}

func (f *difyFormat) parseResponse(body []byte) (string, error) {
	var result struct {
		Answer         string `json:"answer"`
		ConversationID string `json:"conversation_id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse dify response: %w", err)
	}
	if strings.TrimSpace(result.Answer) == "" {
		return "", fmt.Errorf("empty answer in dify response")
	}
	// Store the Dify conversation_id using the pendingConvID set by buildRequest
	f.mu.Lock()
	convID := f.pendingConvID
	if result.ConversationID != "" && convID != "" {
		s := f.sessions[convID]
		s.convID = result.ConversationID
		f.sessions[convID] = s
	}
	f.mu.Unlock()
	return strings.TrimSpace(result.Answer), nil
}

// resetConversation clears the local session mapping and asynchronously deletes
// the conversation on the Dify server so history is fully wiped on both sides.
func (f *difyFormat) resetConversation(_ context.Context, conversationID string) {
	f.mu.Lock()
	s := f.sessions[conversationID]
	delete(f.sessions, conversationID)
	f.mu.Unlock()

	if s.convID == "" {
		return
	}
	// Fire-and-forget with Background context so it survives handler return.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := f.deleteConversation(ctx, s.convID, s.user); err != nil {
			slog.Warn("dify: delete conversation failed", "convID", s.convID, "error", err)
		}
	}()
}

// deleteConversation calls DELETE /v1/conversations/{id} on the Dify server.
func (f *difyFormat) deleteConversation(ctx context.Context, convID, user string) error {
	body, err := json.Marshal(map[string]string{"user": user})
	if err != nil {
		return err
	}
	endpoint := f.baseURL + "/v1/conversations/" + url.PathEscape(convID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if f.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+f.apiKey)
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// --- Helpers ---

func cloneHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// buildMessages is kept for test compatibility.
func (a *HTTPAgent) buildMessages(conversationID string, message string) []ChatMessage {
	var messages []ChatMessage
	if a.systemPrompt != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: a.systemPrompt})
	}
	if hist, ok := a.history[conversationID]; ok {
		messages = append(messages, hist...)
	}
	messages = append(messages, ChatMessage{Role: "user", Content: message})
	return messages
}
