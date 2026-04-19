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

	dm := h.buildPersonaBanner(context.Background(), bot, ringcentral.Post{GroupID: "dm-1", CreatorID: "u1"})
	if !strings.Contains(dm, `chat_type="DM"`) {
		t.Errorf("DM banner missing chat_type DM:\n%s", dm)
	}

	grp := h.buildPersonaBanner(context.Background(), bot, ringcentral.Post{GroupID: "group-1", CreatorID: "u1"})
	if !strings.Contains(grp, `chat_type="Group"`) {
		t.Errorf("group banner missing chat_type Group:\n%s", grp)
	}
}

// TestBuildPersonaBanner_CrossChatNoLeak confirms a critical
// invariant: chat A's memory must not show up in chat B's banner,
// even when the handler is the same instance.
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

// --- /mem add | show | del slash-command tests ---

func TestHandleMemAdd_AppendsToChatByDefault(t *testing.T) {
	h, store, _ := newPersonaTestHandler(t)

	reply := h.handleMemCommand("/mem add prefers TypeScript", "chat-1", "user-1", false)
	if !strings.Contains(reply, "Remembered") {
		t.Errorf("expected success reply, got %q", reply)
	}

	mem, _ := store.LoadMemory(persona.ScopeChat, "chat-1")
	if !strings.Contains(mem, "prefers TypeScript") {
		t.Errorf("chat memory missing entry: %q", mem)
	}
}

func TestHandleMemAdd_ExplicitScope(t *testing.T) {
	h, store, _ := newPersonaTestHandler(t)

	h.handleMemCommand("/mem add user uses terse Chinese", "chat-1", "user-42", false)
	h.handleMemCommand("/mem add global engineering team uses Go+TypeScript", "chat-1", "user-42", false)

	userMem, _ := store.LoadMemory(persona.ScopeUser, "user-42")
	if !strings.Contains(userMem, "terse Chinese") {
		t.Errorf("user memory missing: %q", userMem)
	}
	globalMem, _ := store.LoadMemory(persona.ScopeGlobal, "")
	if !strings.Contains(globalMem, "Go+TypeScript") {
		t.Errorf("global memory missing: %q", globalMem)
	}
}

func TestHandleMemAdd_UsageWhenNoArgs(t *testing.T) {
	h, _, _ := newPersonaTestHandler(t)
	reply := h.handleMemCommand("/mem add", "c", "u", false)
	if !strings.Contains(reply, "Usage") {
		t.Errorf("expected usage hint, got %q", reply)
	}
}

func TestHandleMemShow_ReturnsStoredMemory(t *testing.T) {
	h, store, _ := newPersonaTestHandler(t)
	_ = store.AppendMemory(persona.ScopeChat, "c1", "remember this")

	reply := h.handleMemCommand("/mem show", "c1", "u", false)
	if !strings.Contains(reply, "remember this") {
		t.Errorf("show missing stored memory: %q", reply)
	}
}

func TestHandleMemShow_EmptyScope(t *testing.T) {
	h, _, _ := newPersonaTestHandler(t)
	reply := h.handleMemCommand("/mem show user", "c", "u", false)
	if !strings.Contains(reply, "no user memory") {
		t.Errorf("expected 'no user memory', got %q", reply)
	}
}

func TestHandleMemDel_RequiresConfirmation(t *testing.T) {
	h, store, _ := newPersonaTestHandler(t)
	_ = store.AppendMemory(persona.ScopeChat, "c1", "will be forgotten")

	// First call: returns confirmation prompt, memory untouched.
	reply := h.handleMemCommand("/mem del", "c1", "u", false)

	// The first-phase warning must surface the irreversibility, the
	// resolved path, the /new hint, the tail preview, and the exact
	// command to re-send. Each assertion encodes one operator-facing
	// guarantee — losing any of them silently would defeat the whole
	// point of the two-phase confirm.
	checks := map[string]string{
		"WARNING header":         "WARNING",
		"irreversibility note":   "cannot be undone",
		"/new hint":              "/new",
		"explicit re-send line":  "/mem del chat confirm",
		"tail preview of entry":  "will be forgotten",
		"resolved memory dir":    store.Config().MemoryDir,
	}
	for label, needle := range checks {
		if !strings.Contains(reply, needle) {
			t.Errorf("first-phase reply missing %s (%q):\n%s", label, needle, reply)
		}
	}

	mem, _ := store.LoadMemory(persona.ScopeChat, "c1")
	if !strings.Contains(mem, "will be forgotten") {
		t.Errorf("first /mem del should not clear, got %q", mem)
	}

	// Second call with `confirm`: actually clears. Success message
	// stays the simple one-liner (no path / preview noise on the
	// destructive path itself).
	reply = h.handleMemCommand("/mem del confirm", "c1", "u", false)
	if !strings.Contains(reply, "Forgot") {
		t.Errorf("expected success, got %q", reply)
	}
	mem, _ = store.LoadMemory(persona.ScopeChat, "c1")
	if mem != "" {
		t.Errorf("memory should be empty after confirm, got %q", mem)
	}
}

// TestHandleMemDel_EmptyScope_NoWarning confirms the empty case
// short-circuits to a one-liner instead of printing a multi-line
// "WARNING ... 0 chars" wall — there is nothing to warn about, and
// the confirm step would no-op anyway.
func TestHandleMemDel_EmptyScope_NoWarning(t *testing.T) {
	h, _, _ := newPersonaTestHandler(t)

	reply := h.handleMemCommand("/mem del", "c1", "u", false)
	if !strings.Contains(reply, "nothing to delete") {
		t.Errorf("empty-scope first phase should be a one-liner, got %q", reply)
	}
	if strings.Contains(reply, "WARNING") {
		t.Errorf("empty-scope first phase must not emit the WARNING wall, got %q", reply)
	}
	if strings.Contains(reply, "/new") {
		t.Errorf("empty-scope first phase should not nudge /new, got %q", reply)
	}
}

func TestHandleMem_UnknownSubcommand(t *testing.T) {
	h, store, _ := newPersonaTestHandler(t)
	_ = store.AppendMemory(persona.ScopeChat, "c1", "untouched")

	reply := h.handleMemCommand("/mem foo", "c1", "u", false)
	if !strings.Contains(reply, "Usage") {
		t.Errorf("unknown subcommand should print usage, got %q", reply)
	}

	mem, _ := store.LoadMemory(persona.ScopeChat, "c1")
	if !strings.Contains(mem, "untouched") {
		t.Errorf("memory must not be mutated by unknown subcommand: %q", mem)
	}
}

func TestHandleMem_BareUsage(t *testing.T) {
	h, _, _ := newPersonaTestHandler(t)
	reply := h.handleMemCommand("/mem", "c", "u", false)
	if !strings.Contains(reply, "Usage") {
		t.Errorf("/mem alone should print usage, got %q", reply)
	}
}

// TestHandlePersona_ShowsSoulWhenPresent and _ShowsPathWhenEmpty are
// the SOUL-side counterparts; /persona is independent of /mem and
// must keep working unchanged.
func TestHandlePersona_ShowsSoulWhenPresent(t *testing.T) {
	h, store, _ := newPersonaTestHandler(t)
	_ = store.EnsureSoulTemplate()

	reply := h.handlePersonaCommand()
	if !strings.Contains(reply, "SOUL.md") {
		t.Errorf("expected SOUL.md reference, got %q", reply)
	}
}

func TestHandlePersona_ShowsPathWhenEmpty(t *testing.T) {
	h, _, _ := newPersonaTestHandler(t)
	reply := h.handlePersonaCommand()
	if !strings.Contains(reply, "No SOUL configured") {
		t.Errorf("expected 'No SOUL configured' hint, got %q", reply)
	}
}

func TestHandleMemCommand_DisabledReturnsDiagnostic(t *testing.T) {
	// A handler without a persona loader must return the
	// feature-disabled message for every /mem command and /persona,
	// not panic.
	h := newTestHandler()
	for _, cmd := range []string{"/mem add hello", "/mem show", "/mem del"} {
		reply := h.handleMemCommand(cmd, "c", "u", false)
		if !strings.Contains(reply, "disabled") {
			t.Errorf("cmd %q with nil loader should mention 'disabled', got %q", cmd, reply)
		}
	}
	if reply := h.handlePersonaCommand(); !strings.Contains(reply, "disabled") {
		t.Errorf("/persona with nil loader should mention 'disabled', got %q", reply)
	}
}

func TestIsMemCommand(t *testing.T) {
	cases := map[string]bool{
		"/mem":             true,
		"/mem add hi":      true,
		"/mem show":        true,
		"/mem del confirm": true,
		"/MEM ADD x":       true, // case-insensitive prefix
		"hello":            false,
		"/memo":            false, // strict prefix word
		"/memx":            false,
		"/persona":         false,
		"/cron list":       false,
	}
	for in, want := range cases {
		if got := IsMemCommand(in); got != want {
			t.Errorf("IsMemCommand(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsPersonaCommand(t *testing.T) {
	cases := map[string]bool{
		"/persona":           true,
		"/persona ":          true,
		"/persona ignored":   true,
		"/PERSONA":           true,
		"/mem":               false,
		"/mem add x":         false,
		"hello":              false,
		"/personax":          false,
	}
	for in, want := range cases {
		if got := IsPersonaCommand(in); got != want {
			t.Errorf("IsPersonaCommand(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestMemPrivilegedGate confirms the Layer 1 gate covers the two
// memory-mutating /mem subcommands but leaves /mem show, /persona
// and bare /mem out (so the user gets a usage hint instead of a
// permission denial).
func TestMemPrivilegedGate(t *testing.T) {
	privileged := []string{
		"/mem add hello",
		"/mem add user foo",
		"/mem del",
		"/mem del chat confirm",
	}
	for _, cmd := range privileged {
		if !isPrivilegedCommand(cmd) {
			t.Errorf("isPrivilegedCommand(%q) = false, want true", cmd)
		}
	}
	unprivileged := []string{
		"/mem",
		"/mem show",
		"/mem show user",
		"/mem foo",
		"/persona",
	}
	for _, cmd := range unprivileged {
		if isPrivilegedCommand(cmd) {
			t.Errorf("isPrivilegedCommand(%q) = true, want false", cmd)
		}
	}
}

// TestParseMemCommand exercises argument parsing in isolation so
// future additions (extra flags, scopes) don't have to thread
// through the full Handler stack to be tested.
func TestParseMemCommand(t *testing.T) {
	cases := []struct {
		in        string
		wantSub   string
		wantScope persona.Scope
		wantBody  string
		wantConf  bool
		wantUsage bool
	}{
		{"/mem", "", "", "", false, true},
		{"/mem foo", "foo", persona.ScopeChat, "", false, true},
		{"/mem add", "add", persona.ScopeChat, "", false, true},
		{"/mem add hello", "add", persona.ScopeChat, "hello", false, false},
		{"/mem add chat hello world", "add", persona.ScopeChat, "hello world", false, false},
		{"/mem add user terse Chinese", "add", persona.ScopeUser, "terse Chinese", false, false},
		{"/mem add global engineering uses Go", "add", persona.ScopeGlobal, "engineering uses Go", false, false},
		// Single-token body that happens to look like a scope name → still treated as body.
		// (The "scope only" arm requires len(rest) >= 2, so "user" alone falls through.)
		{"/mem add user", "add", persona.ScopeChat, "user", false, false},
		{"/mem show", "show", persona.ScopeChat, "", false, false},
		{"/mem show chat", "show", persona.ScopeChat, "", false, false},
		{"/mem show user", "show", persona.ScopeUser, "", false, false},
		{"/mem show global", "show", persona.ScopeGlobal, "", false, false},
		{"/mem show bogus", "show", persona.ScopeChat, "", false, true},
		{"/mem del", "del", persona.ScopeChat, "", false, false},
		{"/mem del confirm", "del", persona.ScopeChat, "", true, false},
		{"/mem del global", "del", persona.ScopeGlobal, "", false, false},
		{"/mem del global confirm", "del", persona.ScopeGlobal, "", true, false},
		{"/mem del confirm global", "del", persona.ScopeGlobal, "", true, false},
		{"/mem del bogus", "del", persona.ScopeChat, "", false, true},
		// Mixed case for the subcommand verb.
		{"/MEM Add hello", "add", persona.ScopeChat, "hello", false, false},
	}
	for _, c := range cases {
		got := parseMemCommand(c.in)
		if got.sub != c.wantSub {
			t.Errorf("%q sub = %q, want %q", c.in, got.sub, c.wantSub)
		}
		if c.wantSub != "" && c.wantSub != "foo" && got.scope != c.wantScope {
			t.Errorf("%q scope = %q, want %q", c.in, got.scope, c.wantScope)
		}
		if got.body != c.wantBody {
			t.Errorf("%q body = %q, want %q", c.in, got.body, c.wantBody)
		}
		if got.confirmed != c.wantConf {
			t.Errorf("%q confirmed = %v, want %v", c.in, got.confirmed, c.wantConf)
		}
		if (got.usage != "") != c.wantUsage {
			t.Errorf("%q usage = %q, wantUsage = %v", c.in, got.usage, c.wantUsage)
		}
	}
}
