package heartbeat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/config"
)

// mockAgent implements agent.Agent for testing.
type mockAgent struct {
	reply string
	err   error
}

func (m *mockAgent) Chat(_ context.Context, _ string, _ string) (string, error) {
	return m.reply, m.err
}

func (m *mockAgent) ResetSession(_ context.Context, _ string) (string, error) { return "", nil }
func (m *mockAgent) SetCwd(_ string)                                          {}
func (m *mockAgent) Info() agent.AgentInfo                                    { return agent.AgentInfo{Name: "mock", Type: "test"} }

// --- Start/Stop lifecycle ---

func TestStart_StopsOnCancel(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := config.HeartbeatConfig{Enabled: true, Interval: "100ms"}
	r, err := NewHeartbeatRunner(cfg, nil, "", func() agent.Agent { return nil }, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Start(ctx)
		close(done)
	}()

	// Let it tick once
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Good, Start returned
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not stop after context cancellation")
	}
}

func TestStart_ImmediateCancel(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := config.HeartbeatConfig{Enabled: true, Interval: "1h"}
	r, err := NewHeartbeatRunner(cfg, nil, "", func() agent.Agent { return nil }, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	done := make(chan struct{})
	go func() {
		r.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Start should return quickly
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not stop on immediate cancel")
	}
}

// --- tick() test scenarios ---

func TestTick_OutsideActiveHours(t *testing.T) {
	r := &HeartbeatRunner{
		cfg:         config.HeartbeatConfig{ActiveHours: "00:01-00:02"},
		location:    time.FixedZone("test", 12*3600), // Use a far-off timezone
		activeStart: 1,
		activeEnd:   2,
		recentHash:  make(map[string]time.Time),
	}
	// tick should be a no-op outside active hours (no panic, no error)
	r.tick(context.Background())
}

func TestTick_NoHeartbeatFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	r := &HeartbeatRunner{
		cfg:        config.HeartbeatConfig{},
		location:   time.UTC,
		recentHash: make(map[string]time.Time),
	}
	// No HEARTBEAT.md exists, tick should be a no-op
	r.tick(context.Background())
}

func TestTick_EmptyHeartbeatFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("# Title\n\n"), 0o600)

	r := &HeartbeatRunner{
		cfg:        config.HeartbeatConfig{},
		location:   time.UTC,
		recentHash: make(map[string]time.Time),
	}
	// Empty file (only heading), tick should be a no-op
	r.tick(context.Background())
}

func TestTick_NoAgent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("check server status\n"), 0o600)

	r := &HeartbeatRunner{
		cfg:        config.HeartbeatConfig{},
		location:   time.UTC,
		getAgent:   func() agent.Agent { return nil },
		recentHash: make(map[string]time.Time),
	}
	// No agent available, tick should be a no-op
	r.tick(context.Background())
}

func TestTick_AgentError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("check server status\n"), 0o600)

	ag := &mockAgent{err: errors.New("agent unavailable")}
	r := &HeartbeatRunner{
		cfg:      config.HeartbeatConfig{},
		location: time.UTC,
		getAgent: func() agent.Agent { return ag },
		prompt: func() string {
			return "Check: respond %s if OK.\n%s"
		},
		recentHash: make(map[string]time.Time),
	}
	// Agent error, tick should not panic
	r.tick(context.Background())
}

func TestTick_HeartbeatOK(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("check server\n"), 0o600)

	var sentMsg string
	ag := &mockAgent{reply: "HEARTBEAT_OK"}
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
	}
	r.tick(context.Background())

	// HEARTBEAT_OK means no message sent
	if sentMsg != "" {
		t.Errorf("expected no message sent for HEARTBEAT_OK, got %q", sentMsg)
	}
}

func TestTick_HeartbeatOK_CaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("check server\n"), 0o600)

	var sentMsg string
	ag := &mockAgent{reply: "heartbeat_ok"}
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
	}
	r.tick(context.Background())

	if sentMsg != "" {
		t.Errorf("expected no message for case-insensitive HEARTBEAT_OK, got %q", sentMsg)
	}
}

func TestTick_HeartbeatOK_WithPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("check server\n"), 0o600)

	var sentMsg string
	ag := &mockAgent{reply: "  HEARTBEAT_OK - all systems go"}
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
	}
	r.tick(context.Background())

	if sentMsg != "" {
		t.Errorf("expected no message for HEARTBEAT_OK prefix, got %q", sentMsg)
	}
}

func TestTick_EmptyReply(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("check server\n"), 0o600)

	var sentMsg string
	ag := &mockAgent{reply: ""}
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
	}
	r.tick(context.Background())

	if sentMsg != "" {
		t.Errorf("expected no message for empty reply, got %q", sentMsg)
	}
}

func TestTick_AlertReply_SendsMessage(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("check server\n"), 0o600)

	var sentMsg string
	var sentChatID string
	ag := &mockAgent{reply: "Server CPU at 95%, needs attention!"}
	r := &HeartbeatRunner{
		cfg:      config.HeartbeatConfig{},
		location: time.UTC,
		getAgent: func() agent.Agent { return ag },
		prompt:   func() string { return "Check: respond %s if OK.\n%s" },
		send: func(_ context.Context, chatID, text string) error {
			sentChatID = chatID
			sentMsg = text
			return nil
		},
		chatID:     "alert-chat",
		recentHash: make(map[string]time.Time),
	}
	r.tick(context.Background())

	if sentMsg == "" {
		t.Fatal("expected alert message to be sent")
	}
	if sentChatID != "alert-chat" {
		t.Errorf("expected chat ID alert-chat, got %q", sentChatID)
	}
	if sentMsg != "**[Heartbeat]** Server CPU at 95%, needs attention!" {
		t.Errorf("unexpected message: %q", sentMsg)
	}
}

func TestTick_DuplicateReply_Suppressed(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("check server\n"), 0o600)

	sendCount := 0
	ag := &mockAgent{reply: "Server down!"}
	r := &HeartbeatRunner{
		cfg:      config.HeartbeatConfig{},
		location: time.UTC,
		getAgent: func() agent.Agent { return ag },
		prompt:   func() string { return "Check: respond %s if OK.\n%s" },
		send: func(_ context.Context, _, _ string) error {
			sendCount++
			return nil
		},
		chatID:     "chat",
		recentHash: make(map[string]time.Time),
	}

	// First tick should send
	r.tick(context.Background())
	if sendCount != 1 {
		t.Errorf("expected 1 send, got %d", sendCount)
	}

	// Second tick with same reply should be suppressed
	r.tick(context.Background())
	if sendCount != 1 {
		t.Errorf("expected still 1 send (duplicate suppressed), got %d", sendCount)
	}
}

func TestTick_SendError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("check server\n"), 0o600)

	ag := &mockAgent{reply: "alert!"}
	r := &HeartbeatRunner{
		cfg:      config.HeartbeatConfig{},
		location: time.UTC,
		getAgent: func() agent.Agent { return ag },
		prompt:   func() string { return "Check: respond %s if OK.\n%s" },
		send: func(_ context.Context, _, _ string) error {
			return errors.New("send failed")
		},
		chatID:     "chat",
		recentHash: make(map[string]time.Time),
	}
	// Should not panic on send error
	r.tick(context.Background())
}

// --- isDuplicate with cleanup ---

func TestIsDuplicate_CleansOldEntries(t *testing.T) {
	r := &HeartbeatRunner{recentHash: make(map[string]time.Time)}

	// Add an old entry manually
	r.mu.Lock()
	r.recentHash["old-hash"] = time.Now().Add(-25 * time.Hour)
	r.mu.Unlock()

	// isDuplicate with a new entry should trigger cleanup
	r.isDuplicate("new-content")

	r.mu.Lock()
	_, oldExists := r.recentHash["old-hash"]
	r.mu.Unlock()

	if oldExists {
		t.Error("expected old entry to be cleaned up")
	}
}

// --- readHeartbeatFile ---

func TestReadHeartbeatFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("test content\n"), 0o600)

	r := &HeartbeatRunner{}
	content, err := r.readHeartbeatFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "test content\n" {
		t.Errorf("content = %q, want 'test content\\n'", content)
	}
}

func TestReadHeartbeatFile_Missing(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	r := &HeartbeatRunner{}
	_, err := r.readHeartbeatFile()
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// --- isEffectivelyEmpty edge cases ---

func TestIsEffectivelyEmpty_OnlyComments(t *testing.T) {
	content := "# Heading\n<!-- comment -->\n\n"
	if !isEffectivelyEmpty(content) {
		t.Error("expected effectively empty for headings and comments only")
	}
}

func TestIsEffectivelyEmpty_WithContent(t *testing.T) {
	content := "# Heading\n\n- check this\n"
	if isEffectivelyEmpty(content) {
		t.Error("expected not empty when actual content exists")
	}
}

// --- isActiveTime edge cases ---

func TestIsActiveTime_ExactStart(t *testing.T) {
	now := time.Now()
	mins := now.Hour()*60 + now.Minute()

	r := &HeartbeatRunner{
		cfg:         config.HeartbeatConfig{ActiveHours: "set"},
		location:    time.Local,
		activeStart: mins,
		activeEnd:   mins + 60,
	}
	if !r.isActiveTime() {
		t.Error("should be active at exact start time")
	}
}

func TestIsActiveTime_JustBeforeEnd(t *testing.T) {
	now := time.Now()
	mins := now.Hour()*60 + now.Minute()

	r := &HeartbeatRunner{
		cfg:         config.HeartbeatConfig{ActiveHours: "set"},
		location:    time.Local,
		activeStart: mins - 60,
		activeEnd:   mins + 1,
	}
	if !r.isActiveTime() {
		t.Error("should be active just before end")
	}
}

// --- Concurrent isDuplicate ---

func TestIsDuplicate_Concurrent(t *testing.T) {
	r := &HeartbeatRunner{recentHash: make(map[string]time.Time)}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r.isDuplicate("concurrent-msg")
		}(i)
	}
	wg.Wait()
	// No race condition or panic means success
}

// --- parseActiveHours edge cases ---

func TestParseActiveHours_SameStartEnd(t *testing.T) {
	start, end, err := parseActiveHours("12:00-12:00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if start != 720 || end != 720 {
		t.Errorf("got (%d, %d), want (720, 720)", start, end)
	}
}

func TestParseTimeOfDay_Boundary(t *testing.T) {
	// 23:59 should be valid
	val, err := parseTimeOfDay("23:59")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 1439 {
		t.Errorf("expected 1439, got %d", val)
	}
}

// --- NewHeartbeatRunner full config ---

func TestNewHeartbeatRunner_FullConfig(t *testing.T) {
	cfg := config.HeartbeatConfig{
		Enabled:     true,
		Interval:    "45m",
		ActiveHours: "08:00-20:00",
		Timezone:    "UTC",
	}
	r, err := NewHeartbeatRunner(cfg, nil, "chat-1", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.interval != 45*time.Minute {
		t.Errorf("interval = %v, want 45m", r.interval)
	}
	if r.chatID != "chat-1" {
		t.Errorf("chatID = %q, want chat-1", r.chatID)
	}
	if r.activeStart != 480 || r.activeEnd != 1200 {
		t.Errorf("active hours = (%d, %d), want (480, 1200)", r.activeStart, r.activeEnd)
	}
}

// --- Start with real ticks (short interval) ---

func TestStart_TicksMultipleTimes(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// No HEARTBEAT.md => ticks are no-ops, but they execute
	cfg := config.HeartbeatConfig{Enabled: true, Interval: "50ms"}
	r, err := NewHeartbeatRunner(cfg, nil, "", func() agent.Agent { return nil }, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		r.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Good
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not exit")
	}
}
