package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

// terminalManager tracks running terminal processes for the ACP client interface.
type terminalManager struct {
	mu        sync.Mutex
	terminals map[string]*terminalProc
	nextID    atomic.Int64
	cwd       string // default working directory
}

type terminalProc struct {
	cmd             *exec.Cmd
	output          *terminalBuffer
	done            chan struct{} // closed when process exits
	exitCode        *int
	signal          string
	outputByteLimit int
}

// terminalBuffer is a thread-safe buffer with optional byte limit.
type terminalBuffer struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
}

func (b *terminalBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buf.Write(p)
	if b.limit > 0 && b.buf.Len() > b.limit {
		// Truncate from beginning
		data := b.buf.Bytes()
		keep := data[len(data)-b.limit:]
		b.buf.Reset()
		b.buf.Write(keep)
	}
	return n, err
}

func (b *terminalBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *terminalBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.limit > 0 && b.buf.Len() >= b.limit
}

func newTerminalManager(cwd string) *terminalManager {
	return &terminalManager{
		terminals: make(map[string]*terminalProc),
		cwd:       cwd,
	}
}

// --- ACP terminal request/response types ---

type termCreateRequest struct {
	SessionID       string       `json:"sessionId"`
	Command         string       `json:"command"`
	Args            []string     `json:"args"`
	Cwd             string       `json:"cwd,omitempty"`
	Env             []envVar     `json:"env,omitempty"`
	OutputByteLimit *int         `json:"outputByteLimit,omitempty"`
}

type envVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type termCreateResponse struct {
	TerminalID string `json:"terminalId"`
}

type termOutputRequest struct {
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

type termOutputResponse struct {
	Output     string              `json:"output"`
	Truncated  bool                `json:"truncated"`
	ExitStatus *termExitStatus     `json:"exitStatus,omitempty"`
}

type termExitStatus struct {
	ExitCode *int   `json:"exitCode"`
	Signal   string `json:"signal,omitempty"`
}

type termWaitRequest struct {
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

type termWaitResponse struct {
	ExitCode *int   `json:"exitCode"`
	Signal   string `json:"signal,omitempty"`
}

type termKillRequest struct {
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

type termReleaseRequest struct {
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

func (tm *terminalManager) create(req termCreateRequest) (string, error) {
	id := fmt.Sprintf("term_%d", tm.nextID.Add(1))

	cwd := req.Cwd
	if cwd == "" {
		cwd = tm.cwd
	}

	cmd := exec.Command(req.Command, req.Args...)
	cmd.Dir = cwd

	// Merge environment
	if len(req.Env) > 0 {
		env := os.Environ()
		for _, e := range req.Env {
			env = append(env, fmt.Sprintf("%s=%s", e.Name, e.Value))
		}
		cmd.Env = env
	}

	limit := 0
	if req.OutputByteLimit != nil {
		limit = *req.OutputByteLimit
	}

	buf := &terminalBuffer{limit: limit}
	// Combine stdout and stderr into one buffer
	cmd.Stdout = buf
	cmd.Stderr = buf

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start command %s: %w", req.Command, err)
	}

	proc := &terminalProc{
		cmd:             cmd,
		output:          buf,
		done:            make(chan struct{}),
		outputByteLimit: limit,
	}

	// Wait in background
	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				code = exitErr.ExitCode()
			} else {
				code = -1
			}
		}
		proc.exitCode = &code
		close(proc.done)
	}()

	tm.mu.Lock()
	tm.terminals[id] = proc
	tm.mu.Unlock()

	slog.Info("terminal created", "component", "acp-terminal", "id", id, "command", req.Command, "pid", cmd.Process.Pid)
	return id, nil
}

func (tm *terminalManager) output(id string) (*termOutputResponse, error) {
	tm.mu.Lock()
	proc, ok := tm.terminals[id]
	tm.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("terminal %s not found", id)
	}

	resp := &termOutputResponse{
		Output:    proc.output.String(),
		Truncated: proc.output.Truncated(),
	}

	// Check if exited
	select {
	case <-proc.done:
		resp.ExitStatus = &termExitStatus{
			ExitCode: proc.exitCode,
			Signal:   proc.signal,
		}
	default:
	}

	return resp, nil
}

func (tm *terminalManager) waitForExit(id string) (*termWaitResponse, error) {
	tm.mu.Lock()
	proc, ok := tm.terminals[id]
	tm.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("terminal %s not found", id)
	}

	<-proc.done
	return &termWaitResponse{
		ExitCode: proc.exitCode,
		Signal:   proc.signal,
	}, nil
}

func (tm *terminalManager) kill(id string) error {
	tm.mu.Lock()
	proc, ok := tm.terminals[id]
	tm.mu.Unlock()
	if !ok {
		return fmt.Errorf("terminal %s not found", id)
	}

	select {
	case <-proc.done:
		return nil // already exited
	default:
	}

	if proc.cmd.Process != nil {
		proc.cmd.Process.Kill()
	}
	<-proc.done
	slog.Info("terminal killed", "component", "acp-terminal", "id", id)
	return nil
}

func (tm *terminalManager) release(id string) {
	tm.mu.Lock()
	proc, ok := tm.terminals[id]
	if ok {
		delete(tm.terminals, id)
	}
	tm.mu.Unlock()

	if !ok {
		return
	}

	// Kill if still running
	select {
	case <-proc.done:
	default:
		if proc.cmd.Process != nil {
			proc.cmd.Process.Kill()
		}
		<-proc.done
	}
	slog.Info("terminal released", "component", "acp-terminal", "id", id)
}

// cleanup kills and releases all remaining terminals.
func (tm *terminalManager) cleanup() {
	tm.mu.Lock()
	ids := make([]string, 0, len(tm.terminals))
	for id := range tm.terminals {
		ids = append(ids, id)
	}
	tm.mu.Unlock()

	for _, id := range ids {
		tm.release(id)
	}
}

// --- Handler methods on ACPAgent ---

func (a *ACPAgent) handleTerminalCreate(raw string) {
	id, params, err := parseAgentRequest(raw)
	if err != nil {
		slog.Error("failed to parse terminal/create", "component", "acp", "error", err)
		return
	}

	var req termCreateRequest
	if err := json.Unmarshal(params, &req); err != nil {
		a.sendErrorResponse(id, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}

	termID, err := a.termMgr.create(req)
	if err != nil {
		a.sendErrorResponse(id, -32000, err.Error())
		return
	}

	a.sendResponse(id, termCreateResponse{TerminalID: termID})
}

func (a *ACPAgent) handleTerminalOutput(raw string) {
	id, params, err := parseAgentRequest(raw)
	if err != nil {
		slog.Error("failed to parse terminal/output", "component", "acp", "error", err)
		return
	}

	var req termOutputRequest
	if err := json.Unmarshal(params, &req); err != nil {
		a.sendErrorResponse(id, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}

	resp, err := a.termMgr.output(req.TerminalID)
	if err != nil {
		a.sendErrorResponse(id, -32000, err.Error())
		return
	}

	a.sendResponse(id, resp)
}

func (a *ACPAgent) handleTerminalWaitForExit(raw string) {
	id, params, err := parseAgentRequest(raw)
	if err != nil {
		slog.Error("failed to parse terminal/wait_for_exit", "component", "acp", "error", err)
		return
	}

	var req termWaitRequest
	if err := json.Unmarshal(params, &req); err != nil {
		a.sendErrorResponse(id, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}

	// wait_for_exit blocks until process exits; run in goroutine to not block readLoop
	go func() {
		resp, err := a.termMgr.waitForExit(req.TerminalID)
		if err != nil {
			a.sendErrorResponse(id, -32000, err.Error())
			return
		}
		a.sendResponse(id, resp)
	}()
}

func (a *ACPAgent) handleTerminalKill(raw string) {
	id, params, err := parseAgentRequest(raw)
	if err != nil {
		slog.Error("failed to parse terminal/kill", "component", "acp", "error", err)
		return
	}

	var req termKillRequest
	if err := json.Unmarshal(params, &req); err != nil {
		a.sendErrorResponse(id, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}

	if err := a.termMgr.kill(req.TerminalID); err != nil {
		a.sendErrorResponse(id, -32000, err.Error())
		return
	}

	a.sendResponse(id, struct{}{})
}

func (a *ACPAgent) handleTerminalRelease(raw string) {
	id, params, err := parseAgentRequest(raw)
	if err != nil {
		slog.Error("failed to parse terminal/release", "component", "acp", "error", err)
		return
	}

	var req termReleaseRequest
	if err := json.Unmarshal(params, &req); err != nil {
		a.sendErrorResponse(id, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}

	a.termMgr.release(req.TerminalID)
	a.sendResponse(id, struct{}{})
}

// --- FS handler methods ---

type fsReadRequest struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Line      *int   `json:"line,omitempty"`
	Limit     *int   `json:"limit,omitempty"`
}

type fsReadResponse struct {
	Content string `json:"content"`
}

type fsWriteRequest struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

func (a *ACPAgent) handleFSReadTextFile(raw string) {
	id, params, err := parseAgentRequest(raw)
	if err != nil {
		slog.Error("failed to parse fs/read_text_file", "component", "acp", "error", err)
		return
	}

	var req fsReadRequest
	if err := json.Unmarshal(params, &req); err != nil {
		a.sendErrorResponse(id, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}

	data, err := os.ReadFile(req.Path)
	if err != nil {
		a.sendErrorResponse(id, -32000, fmt.Sprintf("read file: %v", err))
		return
	}

	content := string(data)

	// Apply line/limit if specified
	if req.Line != nil || req.Limit != nil {
		lines := splitLines(content)
		start := 0
		if req.Line != nil && *req.Line > 0 {
			start = *req.Line - 1 // 1-based to 0-based
		}
		if start > len(lines) {
			start = len(lines)
		}
		end := len(lines)
		if req.Limit != nil && *req.Limit > 0 {
			end = start + *req.Limit
		}
		if end > len(lines) {
			end = len(lines)
		}
		content = joinLines(lines[start:end])
	}

	slog.Debug("fs/read_text_file", "component", "acp", "path", req.Path, "size", len(content))
	a.sendResponse(id, fsReadResponse{Content: content})
}

func (a *ACPAgent) handleFSWriteTextFile(raw string) {
	id, params, err := parseAgentRequest(raw)
	if err != nil {
		slog.Error("failed to parse fs/write_text_file", "component", "acp", "error", err)
		return
	}

	if !a.allowWrite {
		a.sendErrorResponse(id, -32000, "write permission denied: allowWrite is false")
		return
	}

	var req fsWriteRequest
	if err := json.Unmarshal(params, &req); err != nil {
		a.sendErrorResponse(id, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}

	if err := os.WriteFile(req.Path, []byte(req.Content), 0o644); err != nil {
		a.sendErrorResponse(id, -32000, fmt.Sprintf("write file: %v", err))
		return
	}

	slog.Info("fs/write_text_file", "component", "acp", "path", req.Path, "size", len(req.Content))
	a.sendResponse(id, struct{}{})
}

// --- Helpers ---

// parseAgentRequest extracts the JSON-RPC id and params from a raw request line.
func parseAgentRequest(raw string) (json.RawMessage, json.RawMessage, error) {
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return nil, nil, err
	}
	return msg.ID, msg.Params, nil
}

// rpcResponseOut is a JSON-RPC response with json.RawMessage ID to avoid
// double-encoding ([]byte → base64) that happens with map[string]interface{}.
type rpcResponseOut struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// sendResponse sends a JSON-RPC success response.
func (a *ACPAgent) sendResponse(id json.RawMessage, result interface{}) {
	data, err := json.Marshal(rpcResponseOut{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
	if err != nil {
		slog.Error("failed to marshal response", "component", "acp", "error", err)
		return
	}
	a.mu.Lock()
	fmt.Fprintf(a.stdin, "%s\n", data)
	a.mu.Unlock()
}

// sendErrorResponse sends a JSON-RPC error response.
func (a *ACPAgent) sendErrorResponse(id json.RawMessage, code int, message string) {
	data, err := json.Marshal(rpcResponseOut{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	})
	if err != nil {
		slog.Error("failed to marshal error response", "component", "acp", "error", err)
		return
	}
	a.mu.Lock()
	fmt.Fprintf(a.stdin, "%s\n", data)
	a.mu.Unlock()
}

func splitLines(s string) []string {
	var lines []string
	reader := bytes.NewReader([]byte(s))
	buf := make([]byte, 0, 4096)
	for {
		b, err := reader.ReadByte()
		if err == io.EOF {
			if len(buf) > 0 {
				lines = append(lines, string(buf))
			}
			break
		}
		buf = append(buf, b)
		if b == '\n' {
			lines = append(lines, string(buf))
			buf = buf[:0]
		}
	}
	return lines
}

func joinLines(lines []string) string {
	var b bytes.Buffer
	for _, l := range lines {
		b.WriteString(l)
	}
	return b.String()
}
