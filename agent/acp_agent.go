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

	"github.com/ringclaw/ringclaw/config"
)

func init() {
	Register("acp", func(ctx context.Context, name string, cfg config.AgentConfig, cwd string) (Agent, error) {
		if cwd == "" {
			cwd = defaultWorkspace()
		}
		ag := NewACPAgent(ACPAgentConfig{
			Command:      cfg.Command,
			Args:         cfg.Args,
			Cwd:          cwd,
			Env:          cfg.Env,
			Model:        cfg.Model,
			SystemPrompt: cfg.SystemPrompt,
			AllowWrite:   cfg.AllowWrite,
			FullAccess:   cfg.FullAccess,
		})
		if err := ag.Start(ctx); err != nil {
			return nil, fmt.Errorf("start ACP agent: %w", err)
		}
		return ag, nil
	})
}

// ACPAgent communicates with ACP-compatible agents via stdio JSON-RPC 2.0.
type ACPAgent struct {
	command      string
	args         []string
	model        string
	systemPrompt string
	cwd          string
	env          map[string]string
	allowWrite   bool
	fullAccess   bool

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
	FullAccess   bool
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

// fullAccessAckEnv must be set to "1" for the full_access config flag to be
// honored when no explicit acknowledgement has been configured via
// SetFullAccessAck. Without one of these, ACP sessions stay in the default
// guarded mode where every MCP tool call requires explicit approval.
const fullAccessAckEnv = "RINGCLAW_FULL_ACCESS_ACK"

var (
	fullAccessAckMu       sync.RWMutex
	fullAccessAckOverride *bool // nil = fall back to env var

	// fullAccessGrantSource, when non-nil, is consulted on every new
	// ACP session to decide whether full-access mode should be enabled
	// for that session. Phase 2 wires it to oob.Manager.FullAccessActive
	// so the /full-access slash command can issue a TTL-bounded unlock
	// without flipping any persistent config field.
	//
	// The startup-time `full_access: true` toggle still takes effect
	// when isFullAccessAcked() is true; this source is additive — a
	// session is granted full-access when EITHER the static config is
	// on OR the dynamic source returns true.
	fullAccessGrantSource func() bool
)

// SetFullAccessAck installs a configured acknowledgement for ACP
// `full_access` mode. When non-nil, this WINS over the
// RINGCLAW_FULL_ACCESS_ACK env var. Pass true to acknowledge, false to
// explicitly refuse (which suppresses any env-var override).
//
// Intended to be called once from cmd/start after config load. Pass a
// fresh value (or call ResetFullAccessAck) in tests to avoid leakage.
func SetFullAccessAck(ack bool) {
	fullAccessAckMu.Lock()
	defer fullAccessAckMu.Unlock()
	fullAccessAckOverride = &ack
}

// ResetFullAccessAck clears any configured acknowledgement so the env
// var becomes the source of truth again. Test-only helper.
func ResetFullAccessAck() {
	fullAccessAckMu.Lock()
	defer fullAccessAckMu.Unlock()
	fullAccessAckOverride = nil
}

// SetFullAccessGrantSource installs a callback that the agent layer
// consults on every new ACP session. When the callback returns true,
// the session is granted full-access mode regardless of the static
// `full_access` config field. Pass nil to clear (test-only).
//
// Phase 2 wires this to oob.Manager.FullAccessActive so the
// /full-access slash command can grant a TTL-bounded unlock.
func SetFullAccessGrantSource(source func() bool) {
	fullAccessAckMu.Lock()
	defer fullAccessAckMu.Unlock()
	fullAccessGrantSource = source
}

// isFullAccessGranted reports whether the dynamic grant source (e.g.
// the OOB /full-access flow) is currently active. Returns false when
// no source has been installed.
func isFullAccessGranted() bool {
	fullAccessAckMu.RLock()
	src := fullAccessGrantSource
	fullAccessAckMu.RUnlock()
	if src == nil {
		return false
	}
	return src()
}

// isFullAccessAcked resolves the effective acknowledgement. Config
// (via SetFullAccessAck) wins over the RINGCLAW_FULL_ACCESS_ACK env var.
func isFullAccessAcked() bool {
	fullAccessAckMu.RLock()
	override := fullAccessAckOverride
	fullAccessAckMu.RUnlock()
	if override != nil {
		return *override
	}
	return os.Getenv(fullAccessAckEnv) == "1"
}

// NewACPAgent creates a new ACP agent.
func NewACPAgent(cfg ACPAgentConfig) *ACPAgent {
	if cfg.Command == "" {
		cfg.Command = "claude-agent-acp"
	}
	if cfg.Cwd == "" {
		cfg.Cwd = defaultWorkspace()
	}
	fullAccess := cfg.FullAccess
	if fullAccess && !isFullAccessAcked() {
		slog.Warn("full_access requested but not acknowledged: refusing to disable MCP guardrails. Set full_access_ack=true in config.json (preferred) or export RINGCLAW_FULL_ACCESS_ACK=1.",
			"component", "acp", "command", cfg.Command, "ack_env", fullAccessAckEnv, "ack_config", "full_access_ack")
		fullAccess = false
	} else if fullAccess {
		slog.Warn("full_access ENABLED: agent will execute MCP tool calls without per-call approval",
			"component", "acp", "command", cfg.Command)
	}
	return &ACPAgent{
		command:      cfg.Command,
		args:         cfg.Args,
		model:        cfg.Model,
		systemPrompt: cfg.SystemPrompt,
		cwd:          cfg.Cwd,
		env:          cfg.Env,
		allowWrite:   cfg.AllowWrite,
		fullAccess:   fullAccess,
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
	registerActiveACPAgent(a)
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

	unregisterActiveACPAgent(a)
}

// SetCwd changes the working directory for subsequent sessions.
// Rejects paths outside the configured workspace root as a defense-in-depth
// against bypasses of the /cwd handler.
func (a *ACPAgent) SetCwd(cwd string) {
	if err := EnsurePathInWorkspace(cwd); err != nil {
		slog.Warn("acp agent cwd rejected", "component", "acp", "cwd", cwd, "error", err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cwd == cwd {
		return
	}
	a.cwd = cwd
	// Clear all sessions so they get recreated with the new cwd.
	// Existing ACP sessions retain the cwd from session/new and cannot be updated.
	for k := range a.sessions {
		delete(a.sessions, k)
	}
	slog.Info("cwd changed, cleared existing sessions", "component", "acp", "cwd", cwd)
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
			return "", Timeout(ctx.Err())
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
				return "", Crash(done.err)
			}
			result := strings.TrimSpace(strings.Join(textParts, ""))
			if result == "" {
				result = extractPromptResultText(done.result)
			}
			if result == "" {
				return "", Empty()
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

	// The static a.fullAccess toggle covers operators who configured
	// `full_access: true` + acknowledged it at startup. The dynamic
	// grant source (Phase 2 OOB /full-access) covers TTL-bounded
	// unlocks. Either one flips the session into full-access mode for
	// THIS conversation; we record which source granted it so audit
	// logs distinguish the two.
	//
	// Race note: we re-read isFullAccessGranted() a second time after
	// the session has been registered in a.sessions. This closes the
	// narrow window where /full-access revoke (or TTL expiry) fires
	// AFTER our first check but BEFORE the new session became visible
	// to the revoke hook — without this double-read the revoke hook's
	// snapshot could miss the session, leaving it in full-access
	// despite the operator seeing "revoked". A third post-call check
	// handles the even narrower window where revoke fires mid-flight;
	// we compensate by issuing an immediate demotion.
	dynGrant := isFullAccessGranted()
	if a.fullAccess || dynGrant {
		// Post-registration re-read: if the dynamic grant has
		// flipped off since the first check, skip set_mode entirely.
		// a.fullAccess (static) cannot change at runtime so it is
		// excluded from the re-check.
		if !a.fullAccess && !isFullAccessGranted() {
			return sessionResult.SessionID, true, nil
		}
		grantSource := "config:full_access"
		if !a.fullAccess && dynGrant {
			grantSource = "oob:/full-access"
		} else if a.fullAccess && dynGrant {
			grantSource = "config:full_access+oob"
		}
		if _, err := a.call(ctx, "session/set_mode", map[string]interface{}{
			"sessionId": sessionResult.SessionID,
			"modeId":    "full-access",
		}); err != nil {
			slog.Warn("set_mode full-access failed, MCP tool calls may be blocked by approval",
				"component", "acp", "session", sessionResult.SessionID, "source", grantSource, "error", err)
		} else {
			slog.Warn("ACP session granted full-access (MCP guardrails disabled for this session)",
				"component", "acp", "command", a.command, "session", sessionResult.SessionID, "conversation", conversationID, "source", grantSource)
			// Post-call re-check: if revoke landed while our
			// set_mode was in flight, demote immediately so the
			// session does not linger in full-access.
			if !a.fullAccess && !isFullAccessGranted() {
				if _, derr := a.call(ctx, "session/set_mode", map[string]interface{}{
					"sessionId": sessionResult.SessionID,
					"modeId":    "default",
				}); derr != nil {
					a.mu.Lock()
					if cur, ok := a.sessions[conversationID]; ok && cur == sessionResult.SessionID {
						delete(a.sessions, conversationID)
					}
					a.mu.Unlock()
					slog.Warn("acp: post-grant revoke compensation failed, session dropped from map",
						"component", "acp", "command", a.command,
						"session", sessionResult.SessionID, "conversation", conversationID, "error", derr)
				} else {
					slog.Warn("acp: post-grant revoke compensation demoted session to default",
						"component", "acp", "command", a.command,
						"session", sessionResult.SessionID, "conversation", conversationID)
				}
			}
		}
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

// activeACPAgents tracks every ACPAgent whose Start() has completed
// successfully and whose Stop() has not yet run. The OOB /full-access
// revoke / TTL-expiry hook walks this set to demote live sessions
// back to the default mode so a grant truly ends when the operator
// (or the TTL) says so, not only on the next brand-new session.
var activeACPAgents sync.Map // map[*ACPAgent]struct{}

func registerActiveACPAgent(a *ACPAgent) {
	if a == nil {
		return
	}
	activeACPAgents.Store(a, struct{}{})
}

func unregisterActiveACPAgent(a *ACPAgent) {
	if a == nil {
		return
	}
	activeACPAgents.Delete(a)
}

// DemoteAllACPFullAccess walks every registered ACPAgent and
// best-effort downgrades every live session back to the default ACP
// mode. Intended to be wired from oob.Manager's revoke hook so
// explicit /full-access revoke and TTL expiry both take effect on
// sessions that were already unlocked, not just on brand-new ones
// created after the revoke.
//
// The ctx is optional — pass context.Background() from a revoke hook
// when no request-scoped context is available; each per-session RPC
// gets its own short timeout internally.
func DemoteAllACPFullAccess(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	activeACPAgents.Range(func(k, _ interface{}) bool {
		a, ok := k.(*ACPAgent)
		if !ok || a == nil {
			return true
		}
		a.demoteFullAccessSessions(ctx)
		return true
	})
}

// acpDemoteRPCTimeout caps the per-session set_mode call issued by
// demoteFullAccessSessions so a single stuck ACP subprocess cannot
// block demotion of the remaining sessions.
const acpDemoteRPCTimeout = 3 * time.Second

// demoteFullAccessSessions iterates every live (conversationID ->
// sessionID) pair and sends session/set_mode modeId="default". On
// success the entry stays in a.sessions so conversation history is
// preserved; on error the entry is dropped so the next prompt for
// that conversation creates a fresh session (which will honor the
// current grant state via getOrCreateSession).
func (a *ACPAgent) demoteFullAccessSessions(ctx context.Context) {
	a.mu.Lock()
	if !a.started {
		a.mu.Unlock()
		return
	}
	snapshot := make(map[string]string, len(a.sessions))
	for k, v := range a.sessions {
		snapshot[k] = v
	}
	a.mu.Unlock()
	if len(snapshot) == 0 {
		return
	}

	for convID, sid := range snapshot {
		callCtx, cancel := context.WithTimeout(ctx, acpDemoteRPCTimeout)
		_, err := a.call(callCtx, "session/set_mode", map[string]interface{}{
			"sessionId": sid,
			"modeId":    "default",
		})
		cancel()
		if err != nil {
			a.mu.Lock()
			if cur, ok := a.sessions[convID]; ok && cur == sid {
				delete(a.sessions, convID)
			}
			a.mu.Unlock()
			slog.Warn("acp demote: set_mode default failed, session dropped from map (next prompt will rebuild)",
				"component", "acp", "command", a.command,
				"session", sid, "conversation", convID, "error", err)
			continue
		}
		slog.Info("acp demote: session returned to default mode",
			"component", "acp", "command", a.command,
			"session", sid, "conversation", convID)
	}
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
