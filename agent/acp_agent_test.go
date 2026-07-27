package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// newMockACPAgentForChat creates an ACPAgent with pipes and a real cmd process
// for tests that exercise Chat (which accesses a.cmd.Process.Pid).
func newMockACPAgentForChat(t *testing.T, sessions map[string]string) (a *ACPAgent, stdinReader *io.PipeReader, stdoutWriter *io.PipeWriter) {
	t.Helper()

	stdinReader2, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter2 := io.Pipe()

	// Start a real process so a.cmd.Process.Pid is valid
	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})

	a = &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		cmd:      cmd,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: sessions,
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()

	return a, stdinReader2, stdoutWriter2
}

func TestExtractChunkText(t *testing.T) {
	update := &sessionUpdate{Text: "hello world"}
	got := extractChunkText(update)
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

func TestExtractChunkText_FromContent(t *testing.T) {
	content, _ := json.Marshal(map[string]string{"type": "text", "text": "from content"})
	update := &sessionUpdate{Content: json.RawMessage(content)}
	got := extractChunkText(update)
	if got != "from content" {
		t.Errorf("expected 'from content', got %q", got)
	}
}

func TestExtractChunkText_Empty(t *testing.T) {
	update := &sessionUpdate{}
	got := extractChunkText(update)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExtractPromptResultText(t *testing.T) {
	result, _ := json.Marshal(map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": "part1"},
			{"type": "text", "text": "part2"},
		},
	})
	got := extractPromptResultText(json.RawMessage(result))
	if got != "part1part2" {
		t.Errorf("expected 'part1part2', got %q", got)
	}
}

func TestExtractPromptResultText_FlatText(t *testing.T) {
	result, _ := json.Marshal(map[string]string{"text": "flat response"})
	got := extractPromptResultText(json.RawMessage(result))
	if got != "flat response" {
		t.Errorf("expected 'flat response', got %q", got)
	}
}

func TestExtractPromptResultText_Nil(t *testing.T) {
	got := extractPromptResultText(nil)
	if got != "" {
		t.Errorf("expected empty for nil, got %q", got)
	}
}

func TestACPAgent_SessionReuse(t *testing.T) {
	a := &ACPAgent{
		sessions: make(map[string]string),
	}

	// Simulate session creation
	a.sessions["conv-1"] = "session-abc"

	a.mu.Lock()
	sid, exists := a.sessions["conv-1"]
	a.mu.Unlock()

	if !exists || sid != "session-abc" {
		t.Errorf("expected session reuse, got exists=%v, sid=%q", exists, sid)
	}

	// Different conversation should not have a session
	a.mu.Lock()
	_, exists = a.sessions["conv-2"]
	a.mu.Unlock()
	if exists {
		t.Error("expected no session for conv-2")
	}
}

func TestNewACPAgent_Defaults(t *testing.T) {
	a := NewACPAgent(ACPAgentConfig{})
	if a.command != "claude-agent-acp" {
		t.Errorf("expected default command 'claude-agent-acp', got %q", a.command)
	}
	if a.cwd == "" {
		t.Error("expected non-empty default cwd")
	}
	if a.sessions == nil {
		t.Error("expected non-nil sessions map")
	}
	if a.pending == nil {
		t.Error("expected non-nil pending map")
	}
	if a.notifyCh == nil {
		t.Error("expected non-nil notifyCh map")
	}
	if a.termMgr == nil {
		t.Error("expected non-nil termMgr")
	}
}

func TestNewACPAgent_CustomConfig(t *testing.T) {
	a := NewACPAgent(ACPAgentConfig{
		Command:      "/custom/agent",
		Args:         []string{"--verbose"},
		Model:        "sonnet",
		SystemPrompt: "be helpful",
		Cwd:          "/custom/workspace",
		Env:          map[string]string{"KEY": "VALUE"},
		AllowWrite:   true,
	})
	if a.command != "/custom/agent" {
		t.Errorf("expected command '/custom/agent', got %q", a.command)
	}
	if a.model != "sonnet" {
		t.Errorf("expected model 'sonnet', got %q", a.model)
	}
	if a.systemPrompt != "be helpful" {
		t.Errorf("expected systemPrompt 'be helpful', got %q", a.systemPrompt)
	}
	if a.cwd != "/custom/workspace" {
		t.Errorf("expected cwd '/custom/workspace', got %q", a.cwd)
	}
	if !a.allowWrite {
		t.Error("expected allowWrite true")
	}
	if len(a.args) != 1 || a.args[0] != "--verbose" {
		t.Errorf("expected args ['--verbose'], got %v", a.args)
	}
}

func TestACPAgent_Info(t *testing.T) {
	a := NewACPAgent(ACPAgentConfig{
		Command: "/usr/bin/agent",
		Model:   "sonnet",
	})
	info := a.Info()
	if info.Name != "/usr/bin/agent" {
		t.Errorf("expected name '/usr/bin/agent', got %q", info.Name)
	}
	if info.Type != "acp" {
		t.Errorf("expected type 'acp', got %q", info.Type)
	}
	if info.Model != "sonnet" {
		t.Errorf("expected model 'sonnet', got %q", info.Model)
	}
	if info.Command != "/usr/bin/agent" {
		t.Errorf("expected command '/usr/bin/agent', got %q", info.Command)
	}
	if info.PID != 0 {
		t.Errorf("expected PID 0 when not started, got %d", info.PID)
	}
}

func TestACPAgent_SetCwd(t *testing.T) {
	a := NewACPAgent(ACPAgentConfig{Cwd: "/original"})
	a.SetCwd("/new/path")
	if a.cwd != "/new/path" {
		t.Errorf("expected cwd '/new/path', got %q", a.cwd)
	}
}

func TestACPAgent_Stop_NotStarted(t *testing.T) {
	a := NewACPAgent(ACPAgentConfig{})
	// Should not panic when not started
	a.Stop()
}

func TestACPAgent_Start_BadCommand(t *testing.T) {
	a := NewACPAgent(ACPAgentConfig{
		Command: "nonexistent_acp_agent_binary",
		Cwd:     t.TempDir(),
	})
	err := a.Start(context.Background())
	if err == nil {
		t.Fatal("expected error for bad command")
	}
}

func TestACPAgent_Start_AlreadyStarted(t *testing.T) {
	a := NewACPAgent(ACPAgentConfig{})
	a.mu.Lock()
	a.started = true
	a.mu.Unlock()

	err := a.Start(context.Background())
	if err != nil {
		t.Fatalf("expected nil error when already started, got %v", err)
	}
}

func TestACPAgent_GetOrCreateSession_ExistingReuse(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: map[string]string{"conv-1": "existing-session"},
		termMgr:  newTerminalManager(t.TempDir()),
	}

	// Should reuse without any RPC call
	sid, isNew, err := a.getOrCreateSession(context.Background(), "conv-1", Origin{IsOwner: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isNew {
		t.Error("expected isNew=false for existing session")
	}
	if sid != "existing-session" {
		t.Errorf("expected sid 'existing-session', got %q", sid)
	}

	stdinReader.Close()
	stdinWriter.Close()
	stdoutReader.Close()
	stdoutWriter.Close()
}

func TestACPAgent_GetOrCreateSession_NewSession(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:        stdinWriter,
		scanner:      bufio.NewScanner(stdoutReader),
		started:      true,
		pending:      make(map[int64]chan *rpcResponse),
		notifyCh:     make(map[string]chan *sessionUpdate),
		sessions:     make(map[string]string),
		cwd:          t.TempDir(),
		model:        "test-model",
		systemPrompt: "test prompt",
		termMgr:      newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()

	// Mock: handle session/new and session/set_mode calls
	callCount := 0
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		for scanner.Scan() {
			line := scanner.Text()
			var req rpcRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				continue
			}
			callCount++
			switch req.Method {
			case "session/new":
				resp := rpcResponseOut{
					JSONRPC: "2.0",
					ID:      mustMarshal(req.ID),
					Result:  map[string]string{"sessionId": "new-session-123"},
				}
				data, _ := json.Marshal(resp)
				fmt.Fprintf(stdoutWriter, "%s\n", data)
			case "session/set_mode":
				resp := rpcResponseOut{
					JSONRPC: "2.0",
					ID:      mustMarshal(req.ID),
					Result:  map[string]string{},
				}
				data, _ := json.Marshal(resp)
				fmt.Fprintf(stdoutWriter, "%s\n", data)
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sid, isNew, err := a.getOrCreateSession(ctx, "new-conv", Origin{IsOwner: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isNew {
		t.Error("expected isNew=true for new session")
	}
	if sid != "new-session-123" {
		t.Errorf("expected sid 'new-session-123', got %q", sid)
	}

	// Verify session is stored
	a.mu.Lock()
	stored := a.sessions["new-conv"]
	a.mu.Unlock()
	if stored != "new-session-123" {
		t.Errorf("expected stored session 'new-session-123', got %q", stored)
	}

	stdinWriter.Close()
	stdoutWriter.Close()
}

func TestACPAgent_Chat_EmptyResponse(t *testing.T) {
	a, stdinReader, stdoutWriter := newMockACPAgentForChat(t, map[string]string{"conv-1": "sess-1"})

	// Mock: respond to session/prompt with empty result
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		for scanner.Scan() {
			line := scanner.Text()
			var req rpcRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				continue
			}
			resp := rpcResponseOut{
				JSONRPC: "2.0",
				ID:      mustMarshal(req.ID),
				Result:  map[string]string{"stopReason": "end"},
			}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(stdoutWriter, "%s\n", data)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := a.Chat(ctx, "conv-1", "hello")
	if err == nil {
		t.Fatal("expected Empty error for empty response")
	}
	var ae *AgentError
	if ok := isAgentError(err, &ae); ok {
		if ae.Code != ErrCodeEmpty {
			t.Errorf("expected ErrCodeEmpty, got %v", ae.Code)
		}
	}

	stdinReader.Close()
	stdoutWriter.Close()
}

func TestACPAgent_Chat_Timeout(t *testing.T) {
	a, stdinReader, stdoutWriter := newMockACPAgentForChat(t, map[string]string{"conv-1": "sess-1"})

	// Don't respond — let the context timeout
	go io.Copy(io.Discard, stdinReader)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := a.Chat(ctx, "conv-1", "hello")
	if err == nil {
		t.Fatal("expected timeout error")
	}

	stdinReader.Close()
	stdoutWriter.Close()
}

func TestACPAgent_Chat_WithStreamingChunks(t *testing.T) {
	a, stdinReader, stdoutWriter := newMockACPAgentForChat(t, map[string]string{"conv-1": "sess-1"})

	// Mock: send streaming chunks then prompt result
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		for scanner.Scan() {
			line := scanner.Text()
			var req rpcRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				continue
			}

			if req.Method == "session/prompt" {
				// Send streaming chunks before the response
				for _, text := range []string{"Hello", " ", "World"} {
					chunk := map[string]interface{}{
						"jsonrpc": "2.0",
						"method":  "session/update",
						"params": map[string]interface{}{
							"sessionId": "sess-1",
							"update": map[string]interface{}{
								"sessionUpdate": "agent_message_chunk",
								"text":          text,
							},
						},
					}
					data, _ := json.Marshal(chunk)
					fmt.Fprintf(stdoutWriter, "%s\n", data)
				}

				// Then send the prompt result
				time.Sleep(50 * time.Millisecond)
				resp := rpcResponseOut{
					JSONRPC: "2.0",
					ID:      mustMarshal(req.ID),
					Result:  map[string]string{"stopReason": "end"},
				}
				data, _ := json.Marshal(resp)
				fmt.Fprintf(stdoutWriter, "%s\n", data)
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := a.Chat(ctx, "conv-1", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", result)
	}

	stdinReader.Close()
	stdoutWriter.Close()
}

func TestACPAgent_Chat_CrashResponse(t *testing.T) {
	a, stdinReader, stdoutWriter := newMockACPAgentForChat(t, map[string]string{"conv-1": "sess-1"})

	// Mock: respond with RPC error
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		for scanner.Scan() {
			line := scanner.Text()
			var req rpcRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				continue
			}
			resp := rpcResponseOut{
				JSONRPC: "2.0",
				ID:      mustMarshal(req.ID),
				Error:   &rpcError{Code: -32000, Message: "agent crashed"},
			}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(stdoutWriter, "%s\n", data)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := a.Chat(ctx, "conv-1", "hello")
	if err == nil {
		t.Fatal("expected crash error")
	}
	// Chat wraps call errors as Crash
	var ae *AgentError
	if ok := isAgentError(err, &ae); ok {
		if ae.Code != ErrCodeCrash {
			t.Errorf("expected ErrCodeCrash, got %v", ae.Code)
		}
	}

	stdinReader.Close()
	stdoutWriter.Close()
}

func TestACPAgent_Chat_AutoRestart(t *testing.T) {
	// When started=false, Chat should attempt to Start (which will fail with bad command)
	a := NewACPAgent(ACPAgentConfig{
		Command: "nonexistent_acp_agent_binary",
		Cwd:     t.TempDir(),
	})
	a.started = false

	_, err := a.Chat(context.Background(), "conv-1", "hello")
	if err == nil {
		t.Fatal("expected error (bad binary on auto-restart)")
	}
}

func TestAgentInfo_String(t *testing.T) {
	info := AgentInfo{
		Name:    "test-agent",
		Type:    "acp",
		Model:   "sonnet",
		Command: "/usr/bin/agent",
	}
	s := info.String()
	if s == "" {
		t.Error("expected non-empty string")
	}

	infoWithPID := AgentInfo{
		Name:    "test-agent",
		Type:    "acp",
		Model:   "sonnet",
		Command: "/usr/bin/agent",
		PID:     12345,
	}
	s = infoWithPID.String()
	if s == "" {
		t.Error("expected non-empty string")
	}
}

func TestStart_NpxCacheRetry(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "_npx", "abc123")
	os.MkdirAll(filepath.Join(cacheDir, "node_modules"), 0o755)

	// Create a fake "npx" script that emits ENOTEMPTY to stderr and exits.
	npxScript := filepath.Join(tmpDir, "npx")
	os.WriteFile(npxScript, []byte(fmt.Sprintf(
		"#!/bin/sh\necho \"ENOTEMPTY: directory not empty, rename '%s/node_modules/pkg' -> '%s/node_modules/.tmp'\" >&2\nexit 1\n",
		cacheDir, cacheDir,
	)), 0o755)

	a := NewACPAgent(ACPAgentConfig{
		Command: npxScript,
		Args:    []string{"-y", "@agentclientprotocol/claude-agent-acp"},
		Cwd:     tmpDir,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := a.Start(ctx)
	// Both attempts will fail (fake script always exits 1), but the
	// important assertion is that the cache dir was cleaned.
	if err == nil {
		t.Fatal("expected error from fake npx")
	}
	if _, statErr := os.Stat(cacheDir); !os.IsNotExist(statErr) {
		t.Errorf("expected cache dir %q to be removed, but it still exists", cacheDir)
	}
}

func TestStart_NonNpx_NoRetry(t *testing.T) {
	tmpDir := t.TempDir()

	// A non-npx command that fails — Start should NOT retry.
	script := filepath.Join(tmpDir, "agent")
	os.WriteFile(script, []byte(
		"#!/bin/sh\necho 'ENOTEMPTY: _npx/abc' >&2\nexit 1\n",
	), 0o755)

	a := NewACPAgent(ACPAgentConfig{
		Command: script,
		Args:    nil,
		Cwd:     tmpDir,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := a.Start(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	// isNpxCommand() is false, so no retry path taken
	if a.isNpxCommand() {
		t.Error("expected isNpxCommand=false for non-npx command")
	}
}

func TestIsNpxCommand(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{"/usr/local/bin/npx", true},
		{"/Users/x/.nvm/versions/node/v22.21.1/bin/npx", true},
		{"npx", true},
		{"C:/Program Files/nodejs/npx.cmd", true},
		{"npx.cmd", true},
		{"npx.exe", true},
		{"/usr/bin/claude-agent-acp", false},
		{"claude-agent-acp", false},
		{"/usr/bin/codex-acp", false},
		{"", false},
	}
	for _, tt := range tests {
		a := &ACPAgent{command: tt.command}
		if got := a.isNpxCommand(); got != tt.want {
			t.Errorf("isNpxCommand(%q) = %v, want %v", tt.command, got, tt.want)
		}
	}
}
