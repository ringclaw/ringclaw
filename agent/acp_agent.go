package agent

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ACPAgent communicates with ACP-compatible agents via stdio JSON-RPC 2.0.
type ACPAgent struct {
	command      string
	args         []string
	model        string
	systemPrompt string
	cwd          string
	env          map[string]string
	allowWrite   bool

	mu       sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	scanner  *bufio.Scanner
	started  bool
	nextID   atomic.Int64
	sessions map[string]string // conversationID -> sessionID

	// pending tracks in-flight JSON-RPC requests
	pendingMu sync.Mutex
	pending   map[int64]chan *rpcResponse

	// notifications channel for session/update events
	notifyMu sync.Mutex
	notifyCh map[string]chan *sessionUpdate // sessionID -> channel

	stderr         *acpStderrWriter
	droppedUpdates atomic.Int64
	loggedMethods  sync.Map
	termMgr        *terminalManager
}

// ACPAgentConfig holds configuration for the ACP agent.
type ACPAgentConfig struct {
	Command      string
	Args         []string
	Model        string
	SystemPrompt string
	Cwd          string
	Env          map[string]string
	AllowWrite   bool
}

// --- ACP protocol types ---

type initParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities clientCapabilities `json:"clientCapabilities"`
}

type clientCapabilities struct {
	FS       *fsCapabilities `json:"fs,omitempty"`
	Terminal bool            `json:"terminal,omitempty"`
}

type fsCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

type newSessionParams struct {
	Cwd          string        `json:"cwd"`
	McpServers   []interface{} `json:"mcpServers"`
	SystemPrompt string        `json:"systemPrompt,omitempty"`
	Model        string        `json:"model,omitempty"`
}

type newSessionResult struct {
	SessionID string `json:"sessionId"`
}

type promptParams struct {
	SessionID string        `json:"sessionId"`
	Prompt    []promptEntry `json:"prompt"`
}

type promptEntry struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type promptResult struct {
	StopReason string `json:"stopReason"`
}

type sessionUpdateParams struct {
	SessionID string        `json:"sessionId"`
	Update    sessionUpdate `json:"update"`
}

type sessionUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	Content       json.RawMessage `json:"content,omitempty"`
	Type          string          `json:"type,omitempty"`
	Text          string          `json:"text,omitempty"`
	ToolCallID    string          `json:"toolCallId,omitempty"`
	Title         string          `json:"title,omitempty"`
	Status        string          `json:"status,omitempty"`
	Kind          string          `json:"kind,omitempty"`
	RawInput      json.RawMessage `json:"rawInput,omitempty"`
	RawOutput     json.RawMessage `json:"rawOutput,omitempty"`
}

type permissionRequestParams struct {
	ToolCall json.RawMessage    `json:"toolCall"`
	Options  []permissionOption `json:"options"`
}

type permissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// NewACPAgent creates a new ACP agent.
func NewACPAgent(cfg ACPAgentConfig) *ACPAgent {
	if cfg.Command == "" {
		cfg.Command = "claude-agent-acp"
	}
	if cfg.Cwd == "" {
		cfg.Cwd = defaultWorkspace()
	}
	return &ACPAgent{
		command:      cfg.Command,
		args:         cfg.Args,
		model:        cfg.Model,
		systemPrompt: cfg.SystemPrompt,
		cwd:          cfg.Cwd,
		env:          cfg.Env,
		allowWrite:   cfg.AllowWrite,
		sessions:     make(map[string]string),
		pending:      make(map[int64]chan *rpcResponse),
		notifyCh:     make(map[string]chan *sessionUpdate),
		termMgr:      newTerminalManager(cfg.Cwd),
	}
}

// Start launches the ACP subprocess and initializes the connection.
func (a *ACPAgent) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.started {
		a.mu.Unlock()
		return nil
	}

	a.cmd = exec.CommandContext(ctx, a.command, a.args...)
	a.cmd.Dir = a.cwd
	if len(a.env) > 0 {
		cmdEnv, err := mergeEnv(os.Environ(), a.env)
		if err != nil {
			a.mu.Unlock()
			return fmt.Errorf("build acp env: %w", err)
		}
		a.cmd.Env = cmdEnv
	}
	a.stderr = &acpStderrWriter{prefix: "[acp-stderr]"}
	a.cmd.Stderr = a.stderr

	var err error
	a.stdin, err = a.cmd.StdinPipe()
	if err != nil {
		a.mu.Unlock()
		return fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := a.cmd.StdoutPipe()
	if err != nil {
		a.mu.Unlock()
		return fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := a.cmd.Start(); err != nil {
		a.mu.Unlock()
		return fmt.Errorf("start acp agent %s: %w", a.command, err)
	}

	pid := a.cmd.Process.Pid
	slog.Info("started subprocess", "component", "acp", "command", a.command, "pid", pid)

	a.scanner = bufio.NewScanner(stdout)
	a.scanner.Buffer(make([]byte, 0, 4*1024*1024), 4*1024*1024)
	a.started = true

	go a.readLoop()

	a.mu.Unlock()

	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	slog.Info("sending initialize handshake", "component", "acp", "pid", pid)
	result, err := a.call(initCtx, "initialize", initParams{
		ProtocolVersion: 1,
		ClientCapabilities: clientCapabilities{
			FS:       &fsCapabilities{ReadTextFile: true, WriteTextFile: a.allowWrite},
			Terminal: true,
		},
	})
	if err != nil {
		a.mu.Lock()
		a.started = false
		a.mu.Unlock()
		a.stdin.Close()
		a.cmd.Process.Kill()
		a.cmd.Wait()
		if detail := a.stderr.LastError(); detail != "" {
			return fmt.Errorf("agent startup failed: %s", detail)
		}
		base := strings.ToLower(filepath.Base(a.command))
		if base == "claude" || base == "claude.exe" {
			return fmt.Errorf("agent startup failed (pid=%d): %w\n\nHint: the 'claude' CLI does not support ACP protocol directly.\nSet type to \"cli\" in your config, or install claude-agent-acp and set command to \"claude-agent-acp\".", pid, err)
		}
		return fmt.Errorf("agent startup failed (pid=%d): %w", pid, err)
	}

	slog.Debug("initialized", "component", "acp", "pid", pid, "result", string(result))
	return nil
}

// Stop terminates the subprocess gracefully.
func (a *ACPAgent) Stop() {
	a.mu.Lock()
	if !a.started {
		a.mu.Unlock()
		return
	}
	sessions := make(map[string]string, len(a.sessions))
	for k, v := range a.sessions {
		sessions[k] = v
	}
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, sid := range sessions {
		a.notify("session/end", map[string]string{"sessionId": sid})
	}

	a.mu.Lock()
	a.stdin.Close()
	proc := a.cmd.Process
	a.mu.Unlock()

	done := make(chan struct{})
	go func() {
		a.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		if proc != nil {
			proc.Kill()
		}
		<-done
	}

	a.termMgr.cleanup()

	a.mu.Lock()
	a.started = false
	a.mu.Unlock()
}

// SetCwd changes the working directory for subsequent sessions.
func (a *ACPAgent) SetCwd(cwd string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cwd = cwd
}

// ResetSession clears the existing session and creates a new one.
func (a *ACPAgent) ResetSession(ctx context.Context, conversationID string) (string, error) {
	a.mu.Lock()
	delete(a.sessions, conversationID)
	a.mu.Unlock()
	slog.Info("session reset, creating new session", "component", "acp", "conversation", conversationID)

	sessionID, _, err := a.getOrCreateSession(ctx, conversationID)
	if err != nil {
		return "", fmt.Errorf("create new session: %w", err)
	}
	return sessionID, nil
}

// Chat sends a text message and returns the full response.
func (a *ACPAgent) Chat(ctx context.Context, conversationID string, message string) (string, error) {
	return a.chatWithEntries(ctx, conversationID, []promptEntry{{Type: "text", Text: message}})
}

// ChatWithImages sends a message with image attachments to the agent.
func (a *ACPAgent) ChatWithImages(ctx context.Context, conversationID string, message string, images []ImageAttachment) (string, error) {
	entries := []promptEntry{{Type: "text", Text: message}}
	for _, img := range images {
		entries = append(entries, promptEntry{
			Type:     "image",
			Data:     base64.StdEncoding.EncodeToString(img.Data),
			MimeType: img.MediaType,
		})
	}
	return a.chatWithEntries(ctx, conversationID, entries)
}

func (a *ACPAgent) chatWithEntries(ctx context.Context, conversationID string, entries []promptEntry) (string, error) {
	if !a.started {
		if err := a.Start(ctx); err != nil {
			return "", err
		}
	}

	sessionID, isNew, err := a.getOrCreateSession(ctx, conversationID)
	if err != nil {
		return "", fmt.Errorf("session error: %w", err)
	}

	pid := a.cmd.Process.Pid
	if isNew {
		slog.Info("new session created", "component", "acp", "pid", pid, "session", sessionID, "conversation", conversationID)
	} else {
		slog.Info("reusing session", "component", "acp", "pid", pid, "session", sessionID, "conversation", conversationID)
	}

	notifyCh := make(chan *sessionUpdate, 256)
	a.notifyMu.Lock()
	a.notifyCh[sessionID] = notifyCh
	a.notifyMu.Unlock()

	defer func() {
		a.notifyMu.Lock()
		delete(a.notifyCh, sessionID)
		a.notifyMu.Unlock()
	}()

	type promptDoneMsg struct {
		result json.RawMessage
		err    error
	}
	promptDone := make(chan promptDoneMsg, 1)
	go func() {
		result, err := a.call(ctx, "session/prompt", promptParams{
			SessionID: sessionID,
			Prompt:    entries,
		})
		if result != nil {
			slog.Debug("prompt result", "component", "acp", "session", sessionID, "result", string(result))
		}
		promptDone <- promptDoneMsg{result: result, err: err}
	}()

	var textParts []string

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case update := <-notifyCh:
			if update.SessionUpdate == "agent_message_chunk" {
				text := extractChunkText(update)
				if text != "" {
					textParts = append(textParts, text)
				}
			}
		case done := <-promptDone:
			for {
				select {
				case update := <-notifyCh:
					if update.SessionUpdate == "agent_message_chunk" {
						text := extractChunkText(update)
						if text != "" {
							textParts = append(textParts, text)
						}
					}
				default:
					goto drained
				}
			}
		drained:
			if done.err != nil {
				return "", fmt.Errorf("prompt error: %w", done.err)
			}
			result := strings.TrimSpace(strings.Join(textParts, ""))
			if result == "" {
				result = extractPromptResultText(done.result)
			}
			if result == "" {
				return "", fmt.Errorf("agent returned empty response")
			}
			return result, nil
		}
	}
}

func (a *ACPAgent) getOrCreateSession(ctx context.Context, conversationID string) (string, bool, error) {
	a.mu.Lock()
	sid, exists := a.sessions[conversationID]
	a.mu.Unlock()

	if exists {
		return sid, false, nil
	}

	result, err := a.call(ctx, "session/new", newSessionParams{
		Cwd:          a.cwd,
		McpServers:   []interface{}{},
		SystemPrompt: a.systemPrompt,
		Model:        a.model,
	})
	if err != nil {
		return "", false, err
	}

	var sessionResult newSessionResult
	if err := json.Unmarshal(result, &sessionResult); err != nil {
		return "", false, fmt.Errorf("parse session result: %w", err)
	}

	a.mu.Lock()
	a.sessions[conversationID] = sessionResult.SessionID
	a.mu.Unlock()

	if _, err := a.call(ctx, "session/set_mode", map[string]interface{}{
		"sessionId": sessionResult.SessionID,
		"modeId":    "full-access",
	}); err != nil {
		slog.Warn("set_mode full-access failed, MCP tool calls may be blocked by approval",
			"component", "acp", "session", sessionResult.SessionID, "error", err)
	} else {
		slog.Info("set session mode to full-access", "component", "acp", "session", sessionResult.SessionID)
	}

	return sessionResult.SessionID, true, nil
}

// Info returns metadata about this agent.
func (a *ACPAgent) Info() AgentInfo {
	info := AgentInfo{
		Name:    a.command,
		Type:    "acp",
		Model:   a.model,
		Command: a.command,
	}
	a.mu.Lock()
	if a.cmd != nil && a.cmd.Process != nil {
		info.PID = a.cmd.Process.Pid
	}
	a.mu.Unlock()
	return info
}

func extractChunkText(update *sessionUpdate) string {
	if update.Text != "" {
		return update.Text
	}
	if update.Content != nil {
		var content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(update.Content, &content); err == nil && content.Text != "" {
			return content.Text
		}
	}
	return ""
}

func extractPromptResultText(result json.RawMessage) string {
	if result == nil {
		return ""
	}
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return ""
	}
	if r.Text != "" {
		return r.Text
	}
	var parts []string
	for _, c := range r.Content {
		if c.Type == "text" && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "")
}
