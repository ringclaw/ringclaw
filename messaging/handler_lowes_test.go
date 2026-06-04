package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ringclaw/ringclaw/messaging/persona"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// newLowesTestClient creates a test HTTP server that records SendPost calls and
// a bot client pointed at it.
func newLowesTestClient(t *testing.T) (*ringcentral.Client, *[]string, *httptest.Server) {
	t.Helper()
	var sentTexts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/posts") {
			var req struct {
				Text string `json:"text"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			sentTexts = append(sentTexts, req.Text)
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	t.Cleanup(func() { srv.Close() })
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")
	return client, &sentTexts, srv
}

// newLowesHandler creates a Handler backed by a temp-dir persona store.
func newLowesHandler(t *testing.T) (*Handler, *persona.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := persona.ResolvedConfig{
		Enabled:              true,
		SoulFile:             filepath.Join(dir, "SOUL.md"),
		MemoryDir:            filepath.Join(dir, "memory"),
		MaxSoulChars:         2000,
		MaxChatMemoryChars:   8000,
		MaxUserMemoryChars:   2000,
		MaxGlobalMemoryChars: 2000,
	}
	store := persona.NewStore(cfg)
	h := newTestHandler()
	h.SetPersonaLoader(persona.NewLoader(store))
	return h, store
}

// TestHandleLowesBatch_NoDate verifies that a missing date argument
// returns a usage error.
func TestHandleLowesBatch_NoDate(t *testing.T) {
	h, _ := newLowesHandler(t)
	got := h.handleLowesBatch(context.Background(), nil, "chat-1", "/lowes-batch send")
	if !strings.Contains(got, "Usage") {
		t.Errorf("expected usage error for missing date, got %q", got)
	}
}

// TestHandleLowesBatch_NoPending verifies that when no PENDING entries
// exist for the given date the command returns a clear "no pending" message.
func TestHandleLowesBatch_NoPending(t *testing.T) {
	h, store := newLowesHandler(t)

	// Seed chat memory with a PENDING entry for a DIFFERENT date.
	if err := store.AppendMemory(persona.ScopeChat, "chat-1",
		"PENDING|A9999|Atlanta|+19195550199|2026-01-01"); err != nil {
		t.Fatal(err)
	}

	got := h.handleLowesBatch(context.Background(), nil, "chat-1", "/lowes-batch send 2026-06-03")
	if !strings.Contains(got, "No pending notifications for 2026-06-03") {
		t.Errorf("expected no-pending message, got %q", got)
	}
}

// TestHandleLowesBatch_SendsBatch verifies the happy path:
// 3 PENDING entries → 3 SMS posts sent, NOTE appended to memory, summary returned.
func TestHandleLowesBatch_SendsBatch(t *testing.T) {
	h, store := newLowesHandler(t)
	client, sentTexts, _ := newLowesTestClient(t)

	// Seed chat memory with 3 PENDING entries for the target date.
	entries := []string{
		"PENDING|A8809|Atlanta|+19195550100|2026-06-03",
		"PENDING|A8815|Atlanta|+19195550101|2026-06-03",
		"PENDING|A8820|Charlotte|+17045550202|2026-06-03",
	}
	for _, e := range entries {
		if err := store.AppendMemory(persona.ScopeChat, "chat-lw", e); err != nil {
			t.Fatalf("seed memory: %v", err)
		}
	}

	got := h.handleLowesBatch(context.Background(), client, "chat-lw", "/lowes-batch send 2026-06-03")

	// 3 SMS posts should have been sent.
	if len(*sentTexts) != 3 {
		t.Errorf("expected 3 SMS posts sent, got %d: %v", len(*sentTexts), *sentTexts)
	}
	// Each sent text should contain ACTION:SMS.
	for _, txt := range *sentTexts {
		if !strings.Contains(txt, "ACTION:SMS") {
			t.Errorf("expected ACTION:SMS in post, got %q", txt)
		}
	}

	// Summary should mention 3 sent.
	if !strings.Contains(got, "3 sent") {
		t.Errorf("expected '3 sent' in summary, got %q", got)
	}
	// Date should appear in summary.
	if !strings.Contains(got, "2026-06-03") {
		t.Errorf("expected date in summary, got %q", got)
	}

	// Memory should now contain SENT entries (not PENDING) and a BATCH_SENT note.
	mem, err := store.LoadMemory(persona.ScopeChat, "chat-lw")
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if strings.Contains(mem, "PENDING|A8809") {
		t.Errorf("expected PENDING|A8809 to be updated to SENT in memory, got:\n%s", mem)
	}
	if !strings.Contains(mem, "SENT|A8809") {
		t.Errorf("expected SENT|A8809 in memory after batch, got:\n%s", mem)
	}
	if !strings.Contains(mem, "BATCH_SENT") {
		t.Errorf("expected BATCH_SENT note appended to memory, got:\n%s", mem)
	}
}

// TestHandleLowesBatch_InvalidCommand verifies that unknown subcommands
// return a usage error.
func TestHandleLowesBatch_InvalidCommand(t *testing.T) {
	h, _ := newLowesHandler(t)

	cases := []struct {
		input string
		want  string
	}{
		{"/lowes-batch foo", "Usage"},
		{"/lowes-batch", "Usage"},
		{"/lowes-batch send notadate", "Invalid date"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := h.handleLowesBatch(context.Background(), nil, "chat-1", tc.input)
			if !strings.Contains(got, tc.want) {
				t.Errorf("handleLowesBatch(%q) = %q, want contains %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestHandleLowesBatch_NilPersonaLoader verifies that when persona is not
// configured the command returns a graceful error.
func TestHandleLowesBatch_NilPersonaLoader(t *testing.T) {
	h := newTestHandler() // no persona loader
	got := h.handleLowesBatch(context.Background(), nil, "chat-1", "/lowes-batch send 2026-06-03")
	if !strings.Contains(got, "disabled") {
		t.Errorf("expected 'disabled' error when loader is nil, got %q", got)
	}
}

// TestParsePendingEntries_Basic exercises the low-level parser directly.
func TestParsePendingEntries_Basic(t *testing.T) {
	memory := strings.Join([]string{
		"PENDING|A001|Atlanta|+11234567890|2026-06-03",
		"PENDING|A002|Charlotte|+10987654321|2026-06-03",
		"PENDING|A003|Raleigh|+15551112222|2026-07-01", // different date
		"SENT|A000|Atlanta|+10000000000|2026-05-01",    // already sent
	}, "\n")

	entries, remaining := parsePendingEntries(memory, "2026-06-03")
	if len(entries) != 2 {
		t.Errorf("expected 2 PENDING entries for 2026-06-03, got %d", len(entries))
	}
	if entries[0].OrderID != "A001" {
		t.Errorf("expected first entry OrderID=A001, got %q", entries[0].OrderID)
	}
	if entries[1].Phone != "+10987654321" {
		t.Errorf("expected second entry Phone=+10987654321, got %q", entries[1].Phone)
	}
	// Remaining should contain the non-matching lines.
	if !strings.Contains(remaining, "A003") {
		t.Errorf("expected non-target-date PENDING in remaining, got %q", remaining)
	}
	if !strings.Contains(remaining, "SENT|A000") {
		t.Errorf("expected already-SENT entry in remaining, got %q", remaining)
	}
	// Target-date entries should NOT be in remaining (they're returned as entries).
	if strings.Contains(remaining, "A001") {
		t.Errorf("target-date entry A001 should not appear in remaining, got %q", remaining)
	}
}

// TestIsValidDate exercises the date format validator.
func TestIsValidDate(t *testing.T) {
	valid := []string{"2026-06-03", "2000-01-01", "9999-12-31"}
	for _, d := range valid {
		if !isValidDate(d) {
			t.Errorf("isValidDate(%q) = false, want true", d)
		}
	}
	invalid := []string{"", "notadate", "26-6-3", "2026/06/03", "2026-6-3", "20260603"}
	for _, d := range invalid {
		if isValidDate(d) {
			t.Errorf("isValidDate(%q) = true, want false", d)
		}
	}
}

// TestIsPrivilegedCommand_LowesBatch verifies that /lowes-batch is treated
// as a privileged command (owner-only in group chats).
func TestIsPrivilegedCommand_LowesBatch(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"/lowes-batch send 2026-06-03", true},
		{"/lowes-batch", true},
		{"/lowes-batch foo", true},
	}
	for _, tc := range cases {
		got := isPrivilegedCommand(tc.text)
		if got != tc.want {
			t.Errorf("isPrivilegedCommand(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}
