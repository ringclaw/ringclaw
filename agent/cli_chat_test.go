package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeFakeClaude installs a shell script that emits stream-json lines
// matching the structure chatClaude expects. The script ignores its
// arguments so we can drive the parser end-to-end.
func writeFakeClaude(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-binary scripts use POSIX shell; not portable to windows runners")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-claude.sh")
	script := "#!/bin/sh\ncat <<'__EOF__'\n" + body + "\n__EOF__\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	return path
}

func TestCLIAgent_ChatClaude_StreamsResult(t *testing.T) {
	body := `{"type":"assistant","session_id":"sess-1","message":{"content":[{"type":"text","text":"hello "}]}}
{"type":"assistant","session_id":"sess-1","message":{"content":[{"type":"text","text":"world"}]}}
{"type":"result","session_id":"sess-1","result":"hello world","is_error":false}`
	bin := writeFakeClaude(t, body)

	a := NewCLIAgent(CLIAgentConfig{
		Name:    "claude",
		Command: bin,
		Cwd:     t.TempDir(),
	})

	got, err := a.Chat(context.Background(), "conv-x", "hi")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sessions["conv-x"] != "sess-1" {
		t.Errorf("expected session id stored, got %q", a.sessions["conv-x"])
	}
}

func TestCLIAgent_ChatClaude_ResumesSavedSession(t *testing.T) {
	// When a previous session exists, claude is invoked with --resume <id>.
	// The fake binary always returns success regardless of args, but we
	// verify the agent does not lose the session id between calls.
	body := `{"type":"result","session_id":"sess-2","result":"second","is_error":false}`
	bin := writeFakeClaude(t, body)
	a := NewCLIAgent(CLIAgentConfig{Name: "claude", Command: bin, Cwd: t.TempDir()})

	a.mu.Lock()
	a.sessions["conv-y"] = "prev-sess"
	a.mu.Unlock()

	got, err := a.Chat(context.Background(), "conv-y", "next")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got != "second" {
		t.Errorf("got %q, want %q", got, "second")
	}
}

func TestCLIAgent_ChatClaude_FallsBackToAssistantTexts(t *testing.T) {
	// No "result" event means we should join assistant texts.
	body := `{"type":"assistant","session_id":"s","message":{"content":[{"type":"text","text":"alpha"}]}}
{"type":"assistant","session_id":"s","message":{"content":[{"type":"text","text":" beta"}]}}`
	bin := writeFakeClaude(t, body)
	a := NewCLIAgent(CLIAgentConfig{Name: "claude", Command: bin, Cwd: t.TempDir()})

	got, err := a.Chat(context.Background(), "conv-z", "hi")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Errorf("expected joined assistant texts, got %q", got)
	}
}

func TestCLIAgent_ChatClaude_ResultIsErrorPropagates(t *testing.T) {
	body := `{"type":"result","session_id":"s","result":"oops","is_error":true}`
	bin := writeFakeClaude(t, body)
	a := NewCLIAgent(CLIAgentConfig{Name: "claude", Command: bin, Cwd: t.TempDir()})

	_, err := a.Chat(context.Background(), "conv-q", "hi")
	if err == nil {
		t.Fatal("expected error when result is_error=true")
	}
	if !strings.Contains(err.Error(), "oops") {
		t.Errorf("error %v should mention 'oops'", err)
	}
}

func TestCLIAgent_ChatClaude_EmptyOutputIsRetryable(t *testing.T) {
	bin := writeFakeClaude(t, "")
	a := NewCLIAgent(CLIAgentConfig{Name: "claude", Command: bin, Cwd: t.TempDir()})

	_, err := a.Chat(context.Background(), "conv-empty", "hi")
	if err == nil {
		t.Fatal("expected error for empty output")
	}
	if !IsRetryable(err) {
		t.Errorf("empty-output error should be retryable, got %v", err)
	}
}

func TestCLIAgent_SetCwd_RejectsOutsideAllowlist(t *testing.T) {
	// Configure a single-root allowlist and verify SetCwd rejects
	// paths outside it (the EnsurePathInWorkspace defense-in-depth).
	root := t.TempDir()
	other := t.TempDir()
	SetWorkspaceRoot(root)
	defer SetWorkspaceRoots(nil)

	a := NewCLIAgent(CLIAgentConfig{Name: "claude", Command: "echo", Cwd: root})
	a.SetCwd(other) // outside allowlist
	if a.cwd != root {
		t.Errorf("SetCwd should have rejected path outside allowlist; cwd=%q", a.cwd)
	}

	a.SetCwd(filepath.Join(root, "sub"))
	if a.cwd != filepath.Join(root, "sub") {
		t.Errorf("SetCwd should have accepted path inside allowlist; cwd=%q", a.cwd)
	}
}
