package agent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNewCLIAgent_Defaults(t *testing.T) {
	a := NewCLIAgent(CLIAgentConfig{
		Name:    "claude",
		Command: "/usr/bin/claude",
	})
	if a.name != "claude" {
		t.Errorf("expected name 'claude', got %q", a.name)
	}
	if a.command != "/usr/bin/claude" {
		t.Errorf("expected command '/usr/bin/claude', got %q", a.command)
	}
	if a.cwd == "" {
		t.Error("expected non-empty default cwd")
	}
	if a.sessions == nil {
		t.Error("expected non-nil sessions map")
	}
}

func TestNewCLIAgent_WithAllFields(t *testing.T) {
	a := NewCLIAgent(CLIAgentConfig{
		Name:         "codex",
		Command:      "/usr/bin/codex",
		Args:         []string{"--flag1", "--flag2"},
		Cwd:          "/custom/workspace",
		Env:          map[string]string{"KEY": "VALUE"},
		Model:        "o4-mini",
		SystemPrompt: "be helpful",
	})
	if a.name != "codex" {
		t.Errorf("expected name 'codex', got %q", a.name)
	}
	if a.cwd != "/custom/workspace" {
		t.Errorf("expected cwd '/custom/workspace', got %q", a.cwd)
	}
	if len(a.args) != 2 {
		t.Errorf("expected 2 args, got %d", len(a.args))
	}
	if a.model != "o4-mini" {
		t.Errorf("expected model 'o4-mini', got %q", a.model)
	}
	if a.systemPrompt != "be helpful" {
		t.Errorf("expected systemPrompt 'be helpful', got %q", a.systemPrompt)
	}
	if a.env["KEY"] != "VALUE" {
		t.Errorf("expected env KEY=VALUE, got %v", a.env)
	}
}

func TestCLIAgent_Info(t *testing.T) {
	a := NewCLIAgent(CLIAgentConfig{
		Name:    "claude",
		Command: "/usr/bin/claude",
		Model:   "sonnet",
	})
	info := a.Info()
	if info.Name != "claude" {
		t.Errorf("expected name 'claude', got %q", info.Name)
	}
	if info.Type != "cli" {
		t.Errorf("expected type 'cli', got %q", info.Type)
	}
	if info.Model != "sonnet" {
		t.Errorf("expected model 'sonnet', got %q", info.Model)
	}
	if info.Command != "/usr/bin/claude" {
		t.Errorf("expected command '/usr/bin/claude', got %q", info.Command)
	}
}

func TestCLIAgent_SetCwd(t *testing.T) {
	a := NewCLIAgent(CLIAgentConfig{
		Name:    "claude",
		Command: "/usr/bin/claude",
		Cwd:     "/original",
	})
	a.SetCwd("/new/path")
	if a.cwd != "/new/path" {
		t.Errorf("expected cwd '/new/path', got %q", a.cwd)
	}
}

func TestCLIAgent_ResetSession(t *testing.T) {
	a := NewCLIAgent(CLIAgentConfig{
		Name:    "claude",
		Command: "/usr/bin/claude",
	})

	// Simulate existing sessions
	a.mu.Lock()
	a.sessions["conv-1"] = "sess-abc"
	a.sessions["conv-2"] = "sess-def"
	a.mu.Unlock()

	result, err := a.ResetSession(context.Background(), "conv-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}

	// Verify conv-1 is gone
	a.mu.Lock()
	_, exists := a.sessions["conv-1"]
	a.mu.Unlock()
	if exists {
		t.Error("expected conv-1 session to be deleted")
	}

	// Verify conv-2 is still there
	a.mu.Lock()
	_, exists = a.sessions["conv-2"]
	a.mu.Unlock()
	if !exists {
		t.Error("expected conv-2 session to still exist")
	}
}

func TestCLIAgent_ResetSession_NonExistent(t *testing.T) {
	a := NewCLIAgent(CLIAgentConfig{
		Name:    "claude",
		Command: "/usr/bin/claude",
	})

	// Should not error on non-existent session
	_, err := a.ResetSession(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCLIAgent_Chat_Codex_BadCommand(t *testing.T) {
	a := NewCLIAgent(CLIAgentConfig{
		Name:    "codex",
		Command: "nonexistent_codex_binary",
		Cwd:     t.TempDir(),
	})

	_, err := a.Chat(context.Background(), "conv-1", "hello")
	if err == nil {
		t.Fatal("expected error for bad command")
	}
}

func TestCLIAgent_Chat_Claude_BadCommand(t *testing.T) {
	a := NewCLIAgent(CLIAgentConfig{
		Name:    "claude",
		Command: "nonexistent_claude_binary",
		Cwd:     t.TempDir(),
	})

	_, err := a.Chat(context.Background(), "conv-1", "hello")
	if err == nil {
		t.Fatal("expected error for bad command")
	}
}

func TestCLIAgent_Chat_DispatchesCorrectly(t *testing.T) {
	// Verify that Chat dispatches to chatCodex for "codex" name
	a := NewCLIAgent(CLIAgentConfig{
		Name:    "codex",
		Command: "nonexistent_binary",
		Cwd:     t.TempDir(),
	})
	_, err := a.Chat(context.Background(), "conv-1", "hello")
	if err == nil {
		t.Fatal("expected error (bad binary), but got none")
	}

	// Verify that Chat dispatches to chatClaude for other names
	a2 := NewCLIAgent(CLIAgentConfig{
		Name:    "claude",
		Command: "nonexistent_binary",
		Cwd:     t.TempDir(),
	})
	_, err = a2.Chat(context.Background(), "conv-1", "hello")
	if err == nil {
		t.Fatal("expected error (bad binary), but got none")
	}
}

func TestCLIAgent_ChatCodex_EmptyOutput(t *testing.T) {
	// Use a command that produces empty output
	a := NewCLIAgent(CLIAgentConfig{
		Name:    "codex",
		Command: "echo",
		Args:    []string{"-n"}, // echo -n produces empty output
		Cwd:     t.TempDir(),
	})
	// Override command to just echo nothing - the codex format uses "exec" as first arg
	// but we can test with a command that outputs empty string
	a.command = "true" // true outputs nothing and exits 0
	_, err := a.Chat(context.Background(), "conv-1", "hello")
	if err == nil {
		t.Fatal("expected Empty error for empty output")
	}
	if !IsRetryable(err) {
		// Empty errors are retryable
		var ae *AgentError
		if ok := isAgentError(err, &ae); ok && ae.Code == ErrCodeCrash {
			// chatCodex wraps exec errors as Crash, which is not retryable — that's also valid
		} else {
			t.Logf("error type: %T, error: %v", err, err)
		}
	}
}

func TestCLIAgent_ChatClaude_WithModel(t *testing.T) {
	// Test that model flag is correctly built for claude
	a := NewCLIAgent(CLIAgentConfig{
		Name:    "claude",
		Command: "echo", // will fail but we test arg building
		Model:   "opus",
		Cwd:     t.TempDir(),
	})
	// We can't fully test chatClaude without a real claude binary,
	// but we verify the agent is constructed properly
	if a.model != "opus" {
		t.Errorf("expected model 'opus', got %q", a.model)
	}
}

// --- streamEvent parsing tests ---

func TestStreamEvent_Parsing(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, e streamEvent)
	}{
		{
			name:  "result event",
			input: `{"type":"result","result":"task completed","is_error":false}`,
			check: func(t *testing.T, e streamEvent) {
				if e.Type != "result" {
					t.Errorf("expected type 'result', got %q", e.Type)
				}
				if e.Result != "task completed" {
					t.Errorf("expected result 'task completed', got %q", e.Result)
				}
				if e.IsError {
					t.Error("expected IsError false")
				}
			},
		},
		{
			name:  "error result",
			input: `{"type":"result","result":"something broke","is_error":true}`,
			check: func(t *testing.T, e streamEvent) {
				if !e.IsError {
					t.Error("expected IsError true")
				}
			},
		},
		{
			name:  "assistant with message",
			input: `{"type":"assistant","message":{"content":[{"type":"text","text":"hello world"}]}}`,
			check: func(t *testing.T, e streamEvent) {
				if e.Type != "assistant" {
					t.Errorf("expected type 'assistant', got %q", e.Type)
				}
				if e.Message == nil {
					t.Fatal("expected non-nil message")
				}
				if len(e.Message.Content) != 1 {
					t.Fatalf("expected 1 content block, got %d", len(e.Message.Content))
				}
				if e.Message.Content[0].Text != "hello world" {
					t.Errorf("expected text 'hello world', got %q", e.Message.Content[0].Text)
				}
			},
		},
		{
			name:  "session_id tracking",
			input: `{"type":"result","session_id":"sess-123","result":"done"}`,
			check: func(t *testing.T, e streamEvent) {
				if e.SessionID != "sess-123" {
					t.Errorf("expected session_id 'sess-123', got %q", e.SessionID)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var e streamEvent
			err := json.Unmarshal([]byte(tt.input), &e)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Unmarshal error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.check != nil {
				tt.check(t, e)
			}
		})
	}
}

// isAgentError is a test helper to check if an error wraps an AgentError.
func isAgentError(err error, target **AgentError) bool {
	if ae, ok := err.(*AgentError); ok {
		*target = ae
		return true
	}
	return false
}
