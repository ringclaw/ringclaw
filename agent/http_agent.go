package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ChatMessage represents a single message in a conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// httpFormat abstracts the request/response protocol for different HTTP APIs.
type httpFormat interface {
	buildRequest(conversationID, message string, opts formatOpts) ([]byte, error)
	parseResponse(body []byte, conversationID string) (string, error)
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
		f = newDifyFormat()
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

func (a *HTTPAgent) ResetSession(_ context.Context, conversationID string) (string, error) {
	if !a.format.managesHistory() {
		a.mu.Lock()
		delete(a.history, conversationID)
		a.mu.Unlock()
	}
	// For formats that manage sessions server-side (e.g. dify), clear their mapping too.
	type sessionResetter interface {
		resetConversation(string)
	}
	if r, ok := a.format.(sessionResetter); ok {
		r.resetConversation(conversationID)
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

	reply, err := a.format.parseResponse(respBody, conversationID)
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

func (f *openaiFormat) parseResponse(body []byte, _ string) (string, error) {
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

func (f *nanoclawFormat) parseResponse(body []byte, _ string) (string, error) {
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

// difyFormat implements the Dify chatflow API.
// Dify manages conversation history server-side, identified by its own conversation_id.
// We map each RingClaw conversationID to the corresponding Dify conversation_id.
type difyFormat struct {
	mu      sync.Mutex
	convIDs map[string]string // ringclawConvID -> difyConvID
}

func newDifyFormat() *difyFormat {
	return &difyFormat{convIDs: make(map[string]string)}
}

func (f *difyFormat) managesHistory() bool { return true }
func (f *difyFormat) supportsCwd() bool    { return false }

func (f *difyFormat) buildRequest(conversationID, message string, opts formatOpts) ([]byte, error) {
	f.mu.Lock()
	difyConvID := f.convIDs[conversationID]
	f.mu.Unlock()

	user := opts.Sender
	if user == "" {
		user = conversationID
	}

	return json.Marshal(map[string]interface{}{
		"inputs":          map[string]interface{}{},
		"query":           message,
		"response_mode":   "blocking",
		"conversation_id": difyConvID,
		"user":            user,
	})
}

func (f *difyFormat) parseResponse(body []byte, conversationID string) (string, error) {
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
	if result.ConversationID != "" && conversationID != "" {
		f.mu.Lock()
		f.convIDs[conversationID] = result.ConversationID
		f.mu.Unlock()
	}
	return strings.TrimSpace(result.Answer), nil
}

// resetConversation clears the Dify conversation_id for the given conversationID so
// the next message starts a fresh Dify conversation.
func (f *difyFormat) resetConversation(conversationID string) {
	f.mu.Lock()
	delete(f.convIDs, conversationID)
	f.mu.Unlock()
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
