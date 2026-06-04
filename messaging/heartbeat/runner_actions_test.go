package heartbeat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/config"
	"github.com/ringclaw/ringclaw/messaging"
)

// TestHeartbeatRunner_Action_CARDAllowed verifies that a CARD action in the
// agent reply is passed to executeActions when present.
func TestHeartbeatRunner_Action_CARDAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("check server\n"), 0o600)

	var mu sync.Mutex
	var capturedActions []messaging.AgentAction

	ag := &mockAgent{reply: `ACTION:CARD
{"type":"AdaptiveCard","version":"1.3","body":[{"type":"TextBlock","text":"Status OK"}]}
END_ACTION`}

	r := &HeartbeatRunner{
		cfg:        config.HeartbeatConfig{},
		location:   time.UTC,
		getAgent:   func() agent.Agent { return ag },
		prompt:     func() string { return "Check: respond %s if OK.\n%s" },
		send:       func(_ context.Context, _, _ string) error { return nil },
		chatID:     "test-chat",
		recentHash: make(map[string]time.Time),
		executeActions: func(_ context.Context, chatID string, actions []messaging.AgentAction) {
			mu.Lock()
			capturedActions = append(capturedActions, actions...)
			mu.Unlock()
		},
	}
	r.tick(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if len(capturedActions) != 1 {
		t.Fatalf("expected 1 action fired, got %d", len(capturedActions))
	}
	if capturedActions[0].Type != "CARD" {
		t.Errorf("expected CARD action, got %q", capturedActions[0].Type)
	}
}

// TestHeartbeatRunner_Action_SMSStripped verifies that SMS actions are NOT
// passed to executeActions (heartbeat only allows MESSAGE, NOTE, CARD, TASK).
func TestHeartbeatRunner_Action_SMSStripped(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("check server\n"), 0o600)

	var mu sync.Mutex
	var capturedActions []messaging.AgentAction

	ag := &mockAgent{reply: "ACTION:SMS to=+15551234567\nhello\nEND_ACTION"}

	r := &HeartbeatRunner{
		cfg:        config.HeartbeatConfig{},
		location:   time.UTC,
		getAgent:   func() agent.Agent { return ag },
		prompt:     func() string { return "Check: respond %s if OK.\n%s" },
		send:       func(_ context.Context, _, _ string) error { return nil },
		chatID:     "test-chat",
		recentHash: make(map[string]time.Time),
		executeActions: func(_ context.Context, chatID string, actions []messaging.AgentAction) {
			mu.Lock()
			capturedActions = append(capturedActions, actions...)
			mu.Unlock()
		},
	}
	r.tick(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if len(capturedActions) != 0 {
		t.Errorf("expected SMS action to be stripped, got %d actions: %v", len(capturedActions), capturedActions)
	}
}

// TestHeartbeatRunner_Action_TASKAllowed verifies that TASK actions are passed
// to executeActions and the text part is also sent.
func TestHeartbeatRunner_Action_TASKAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("check server\n"), 0o600)

	var mu sync.Mutex
	var capturedActions []messaging.AgentAction

	ag := &mockAgent{reply: "Disk almost full!\nACTION:TASK subject=Disk cleanup\nEND_ACTION"}

	var sentMsg string
	r := &HeartbeatRunner{
		cfg:      config.HeartbeatConfig{},
		location: time.UTC,
		getAgent: func() agent.Agent { return ag },
		prompt:   func() string { return "Check: respond %s if OK.\n%s" },
		send: func(_ context.Context, _, text string) error {
			sentMsg = text
			return nil
		},
		chatID:     "test-chat",
		recentHash: make(map[string]time.Time),
		executeActions: func(_ context.Context, chatID string, actions []messaging.AgentAction) {
			mu.Lock()
			capturedActions = append(capturedActions, actions...)
			mu.Unlock()
		},
	}
	r.tick(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if len(capturedActions) != 1 {
		t.Fatalf("expected 1 action fired, got %d", len(capturedActions))
	}
	if capturedActions[0].Type != "TASK" {
		t.Errorf("expected TASK action, got %q", capturedActions[0].Type)
	}
	if capturedActions[0].Params["subject"] != "Disk cleanup" {
		t.Errorf("expected subject 'Disk cleanup', got %q", capturedActions[0].Params["subject"])
	}
	// Text part should also be sent
	if !strings.Contains(sentMsg, "Disk almost full!") {
		t.Errorf("expected text message with 'Disk almost full!', got %q", sentMsg)
	}
}
