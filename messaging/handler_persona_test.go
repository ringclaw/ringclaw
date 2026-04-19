package messaging

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ringclaw/ringclaw/messaging/persona"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// newPersonaTestHandler wires a Handler with a persona loader rooted
// in a temp dir. Returns the handler and the persona store so tests
// can arrange SOUL / memory fixtures directly.
func newPersonaTestHandler(t *testing.T) (*Handler, *persona.Store, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := persona.ResolvedConfig{
		Enabled:              true,
		SoulFile:             filepath.Join(dir, "SOUL.md"),
		MemoryDir:            filepath.Join(dir, "memory"),
		MaxSoulChars:         500,
		MaxChatMemoryChars:   500,
		MaxUserMemoryChars:   500,
		MaxGlobalMemoryChars: 500,
	}
	store := persona.NewStore(cfg)
	h := newTestHandler()
	h.SetPersonaLoader(persona.NewLoader(store))
	return h, store, dir
}

// TestBuildPersonaBanner_EmptyWhenLoaderNil confirms the injection
// short-circuit: a handler without a loader must produce an empty
// banner. This is what existing agent tests rely on to keep passing.
func TestBuildPersonaBanner_EmptyWhenLoaderNil(t *testing.T) {
	h := newTestHandler() // no SetPersonaLoader call
	bot := ringcentral.NewBotClient("http://example.com", "token")
	bot.SetOwnerID("bot-1")

	got := h.buildPersonaBanner(context.Background(), bot, ringcentral.Post{GroupID: "c1", CreatorID: "u1"})
	if got != "" {
		t.Errorf("nil loader must yield empty banner, got %q", got)
	}
}

// TestBuildPersonaBanner_IncludesSoulAndMemory is the happy-path
// integration assertion: SOUL + chat memory present → both land in
// the banner.
func TestBuildPersonaBanner_IncludesSoulAndMemory(t *testing.T) {
	h, store, _ := newPersonaTestHandler(t)
	if err := store.EnsureSoulTemplate(); err != nil {
		t.Fatalf("EnsureSoulTemplate: %v", err)
	}
	if err := store.AppendMemory(persona.ScopeChat, "c1", "project uses Go 1.25"); err != nil {
		t.Fatalf("AppendMemory: %v", err)
	}

	bot := ringcentral.NewBotClient("http://example.com", "token")
	bot.SetOwnerID("bot-1")

	got := h.buildPersonaBanner(context.Background(), bot, ringcentral.Post{GroupID: "c1", CreatorID: "u1"})
	if !strings.Contains(got, `<context type="persona">`) {
		t.Errorf("banner missing persona section:\n%s", got)
	}
	if !strings.Contains(got, "project uses Go 1.25") {
		t.Errorf("banner missing chat memory:\n%s", got)
	}
}

// TestBuildPersonaBanner_DMvsGroupAttribute confirms the chat_type
// attribute flips based on IsBotDM. Exercised through a real
// ringcentral.Client whose DM chat ID matches the post.
func TestBuildPersonaBanner_DMvsGroupAttribute(t *testing.T) {
	h, store, _ := newPersonaTestHandler(t)
	if err := store.AppendMemory(persona.ScopeChat, "dm-1", "dm memory"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := store.AppendMemory(persona.ScopeChat, "group-1", "group memory"); err != nil {
		t.Fatalf("append: %v", err)
	}

	bot := ringcentral.NewBotClient("http://example.com", "token")
	bot.SetOwnerID("bot-1")
	bot.SetDMChatID("dm-1")

	// Post from the DM chat → DM.
	dm := h.buildPersonaBanner(context.Background(), bot, ringcentral.Post{GroupID: "dm-1", CreatorID: "u1"})
	if !strings.Contains(dm, `chat_type="DM"`) {
		t.Errorf("DM banner missing chat_type DM:\n%s", dm)
	}

	// Post from a different chat → Group.
	grp := h.buildPersonaBanner(context.Background(), bot, ringcentral.Post{GroupID: "group-1", CreatorID: "u1"})
	if !strings.Contains(grp, `chat_type="Group"`) {
		t.Errorf("group banner missing chat_type Group:\n%s", grp)
	}
}

// TestBuildPersonaBanner_CrossChatNoLeak confirms a critical
// invariant: chat A's memory must not show up in chat B's banner,
// even when the handler is the same instance. This is what makes
// per-chat memory safe in shared bot accounts.
func TestBuildPersonaBanner_CrossChatNoLeak(t *testing.T) {
	h, store, _ := newPersonaTestHandler(t)
	if err := store.AppendMemory(persona.ScopeChat, "chat-A", "secret-fact-A"); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMemory(persona.ScopeChat, "chat-B", "fact-B"); err != nil {
		t.Fatal(err)
	}

	bot := ringcentral.NewBotClient("http://example.com", "token")
	bot.SetOwnerID("bot-1")

	bannerB := h.buildPersonaBanner(context.Background(), bot, ringcentral.Post{GroupID: "chat-B", CreatorID: "u1"})
	if strings.Contains(bannerB, "secret-fact-A") {
		t.Fatalf("chat B banner leaked chat A memory:\n%s", bannerB)
	}
	if !strings.Contains(bannerB, "fact-B") {
		t.Errorf("chat B banner missing its own memory:\n%s", bannerB)
	}
}

// --- /remember, /recall, /forget, /persona slash-command tests ---

func TestHandleRemember_AppendsToChatByDefault(t *testing.T) {
	h, store, _ := newPersonaTestHandler(t)

	reply := h.handlePersonaCommand("/remember prefers TypeScript", "chat-1", "user-1", false)
	if !strings.Contains(reply, "Remembered") {
		t.Errorf("expected success reply, got %q", reply)
	}

	mem, _ := store.LoadMemory(persona.ScopeChat, "chat-1")
	if !strings.Contains(mem, "prefers TypeScript") {
		t.Errorf("chat memory missing entry: %q", mem)
	}
}

func TestHandleRemember_ExplicitScope(t *testing.T) {
	h, store, _ := newPersonaTestHandler(t)

	h.handlePersonaCommand("/remember user uses terse Chinese", "chat-1", "user-42", false)
	h.handlePersonaCommand("/remember global engineering team uses Go+TypeScript", "chat-1", "user-42", false)

	userMem, _ := store.LoadMemory(persona.ScopeUser, "user-42")
	if !strings.Contains(userMem, "terse Chinese") {
		t.Errorf("user memory missing: %q", userMem)
	}
	globalMem, _ := store.LoadMemory(persona.ScopeGlobal, "")
	if !strings.Contains(globalMem, "Go+TypeScript") {
		t.Errorf("global memory missing: %q", globalMem)
	}
}

func TestHandleRemember_UsageWhenNoArgs(t *testing.T) {
	h, _, _ := newPersonaTestHandler(t)
	reply := h.handlePersonaCommand("/remember", "c", "u", false)
	if !strings.Contains(reply, "Usage") {
		t.Errorf("expected usage hint, got %q", reply)
	}
}

func TestHandleRecall_ReturnsStoredMemory(t *testing.T) {
	h, store, _ := newPersonaTestHandler(t)
	_ = store.AppendMemory(persona.ScopeChat, "c1", "remember this")

	reply := h.handlePersonaCommand("/recall", "c1", "u", false)
	if !strings.Contains(reply, "remember this") {
		t.Errorf("recall missing stored memory: %q", reply)
	}
}

func TestHandleRecall_EmptyScope(t *testing.T) {
	h, _, _ := newPersonaTestHandler(t)
	reply := h.handlePersonaCommand("/recall user", "c", "u", false)
	if !strings.Contains(reply, "no user memory") {
		t.Errorf("expected 'no user memory', got %q", reply)
	}
}

func TestHandleForget_RequiresConfirmation(t *testing.T) {
	h, store, _ := newPersonaTestHandler(t)
	_ = store.AppendMemory(persona.ScopeChat, "c1", "will be forgotten")

	// First call: returns confirmation prompt, memory untouched.
	reply := h.handlePersonaCommand("/forget", "c1", "u", false)
	if !strings.Contains(reply, "Re-send") {
		t.Errorf("expected confirmation prompt, got %q", reply)
	}
	mem, _ := store.LoadMemory(persona.ScopeChat, "c1")
	if !strings.Contains(mem, "will be forgotten") {
		t.Errorf("first /forget should not clear, got %q", mem)
	}

	// Second call with `confirm`: actually clears.
	reply = h.handlePersonaCommand("/forget confirm", "c1", "u", false)
	if !strings.Contains(reply, "Forgot") {
		t.Errorf("expected success, got %q", reply)
	}
	mem, _ = store.LoadMemory(persona.ScopeChat, "c1")
	if mem != "" {
		t.Errorf("memory should be empty after confirm, got %q", mem)
	}
}

func TestHandlePersona_ShowsSoulWhenPresent(t *testing.T) {
	h, store, _ := newPersonaTestHandler(t)
	_ = store.EnsureSoulTemplate()

	reply := h.handlePersonaCommand("/persona", "c", "u", false)
	if !strings.Contains(reply, "SOUL.md") {
		t.Errorf("expected SOUL.md reference, got %q", reply)
	}
}

func TestHandlePersona_ShowsPathWhenEmpty(t *testing.T) {
	h, _, _ := newPersonaTestHandler(t)
	reply := h.handlePersonaCommand("/persona", "c", "u", false)
	if !strings.Contains(reply, "No SOUL configured") {
		t.Errorf("expected 'No SOUL configured' hint, got %q", reply)
	}
}

func TestHandlePersonaCommand_DisabledReturnsDiagnostic(t *testing.T) {
	// A handler without a persona loader must return the
	// feature-disabled message for every persona command, not panic.
	h := newTestHandler()
	for _, cmd := range []string{"/remember hello", "/recall", "/forget", "/persona"} {
		reply := h.handlePersonaCommand(cmd, "c", "u", false)
		if !strings.Contains(reply, "disabled") {
			t.Errorf("cmd %q with nil loader should mention 'disabled', got %q", cmd, reply)
		}
	}
}

func TestIsPersonaCommand(t *testing.T) {
	cases := map[string]bool{
		"/remember hello":  true,
		"/recall":          true,
		"/recall user":     true,
		"/forget confirm":  true,
		"/persona":         true,
		"hello":            false,
		"/cron list":       false,
		"/rememberxyz":     false, // strict prefix
	}
	for in, want := range cases {
		if got := IsPersonaCommand(in); got != want {
			t.Errorf("IsPersonaCommand(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestIsPrivilegedCommand_IncludesRememberForget confirms the Layer 1
// gate now covers the two memory-mutating persona commands. Read-only
// /recall and /persona must remain out of the set.
func TestIsPrivilegedCommand_IncludesRememberForget(t *testing.T) {
	privileged := []string{"/remember hello", "/remember user foo", "/forget", "/forget chat confirm"}
	for _, cmd := range privileged {
		if !isPrivilegedCommand(cmd) {
			t.Errorf("isPrivilegedCommand(%q) = false, want true", cmd)
		}
	}
	unprivileged := []string{"/recall", "/recall user", "/persona"}
	for _, cmd := range unprivileged {
		if isPrivilegedCommand(cmd) {
			t.Errorf("isPrivilegedCommand(%q) = true, want false", cmd)
		}
	}
}
