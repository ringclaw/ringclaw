package persona

import (
	"context"
	"strings"
	"testing"
)

func newTestLoader(t *testing.T) (*Loader, *Store) {
	t.Helper()
	store, _ := newTestStore(t, 500)
	return NewLoader(store), store
}

func TestLoader_NilIsDisabled(t *testing.T) {
	// A nil *Loader must be safe — callers can pass it without a
	// guard. Every public method treats nil as "disabled".
	var l *Loader
	if l.Enabled() {
		t.Error("nil loader reported Enabled=true")
	}
	if got := l.Build(context.Background(), "c", "u", false); got != "" {
		t.Errorf("nil loader Build returned %q, want empty", got)
	}
	if l.Store() != nil {
		t.Error("nil loader Store() should return nil")
	}
}

func TestLoader_Disabled_ReturnsEmpty(t *testing.T) {
	// Even with a backing store, an Enabled=false config should
	// produce no banner so operators can turn persona off with one
	// flag.
	store, _ := newTestStore(t, 500)
	store.cfg.Enabled = false
	l := NewLoader(store)

	if l.Enabled() {
		t.Error("Enabled=false config reported Enabled=true")
	}
	if got := l.Build(context.Background(), "c", "u", false); got != "" {
		t.Errorf("disabled loader Build = %q, want empty", got)
	}
}

func TestLoader_AllEmpty_NoBanner(t *testing.T) {
	// With no SOUL and no memory files, Build must return the empty
	// string — this is the "fresh install" case where agent
	// behaviour should be unchanged from pre-persona baseline.
	l, _ := newTestLoader(t)
	if got := l.Build(context.Background(), "12345", "67890", true); got != "" {
		t.Errorf("empty store Build = %q, want empty", got)
	}
}

func TestLoader_WithSoulOnly(t *testing.T) {
	l, store := newTestLoader(t)
	if err := store.EnsureSoulTemplate(); err != nil {
		t.Fatalf("EnsureSoulTemplate: %v", err)
	}
	got := l.Build(context.Background(), "12345", "67890", true)
	if !strings.Contains(got, "<context type=\"persona\">") {
		t.Errorf("banner missing persona section: %q", got)
	}
	if strings.Contains(got, "<context type=\"memory\"") {
		t.Errorf("banner should not have memory section when no memory files exist: %q", got)
	}
}

func TestLoader_WithAllScopes(t *testing.T) {
	l, store := newTestLoader(t)
	_ = store.EnsureSoulTemplate()

	if err := store.AppendMemory(ScopeGlobal, "", "globally-relevant fact"); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMemory(ScopeUser, "user-7", "user prefers terse replies"); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMemory(ScopeChat, "chat-42", "sprint goal: ship v2"); err != nil {
		t.Fatal(err)
	}

	got := l.Build(context.Background(), "chat-42", "user-7", false)

	// Each tagged section must appear.
	wantSubs := []string{
		`<context type="persona">`,
		`<context type="memory" scope="global">`,
		`<context type="memory" scope="user">`,
		`<context type="memory" scope="chat" chat_type="Group">`,
		"globally-relevant fact",
		"user prefers terse replies",
		"sprint goal: ship v2",
	}
	for _, sub := range wantSubs {
		if !strings.Contains(got, sub) {
			t.Errorf("banner missing %q\n--- banner ---\n%s", sub, got)
		}
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Errorf("banner must end with a blank-line separator so the caller's user message starts cleanly; got trailing %q", tail(got, 6))
	}
}

func TestLoader_ChatTypeFlipsOnDM(t *testing.T) {
	l, store := newTestLoader(t)
	if err := store.AppendMemory(ScopeChat, "chat-1", "dm fact"); err != nil {
		t.Fatal(err)
	}

	dm := l.Build(context.Background(), "chat-1", "u", true)
	grp := l.Build(context.Background(), "chat-1", "u", false)

	if !strings.Contains(dm, `chat_type="DM"`) {
		t.Errorf("DM banner missing chat_type=\"DM\": %s", dm)
	}
	if !strings.Contains(grp, `chat_type="Group"`) {
		t.Errorf("group banner missing chat_type=\"Group\": %s", grp)
	}
}

func TestLoader_CrossChat_DoesNotLeak(t *testing.T) {
	// Writing memory for chat A must not surface in chat B's banner
	// — this is the invariant that makes per-chat memory safe to
	// use in shared accounts.
	l, store := newTestLoader(t)
	if err := store.AppendMemory(ScopeChat, "chat-A", "secret-fact-A"); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMemory(ScopeChat, "chat-B", "fact-B"); err != nil {
		t.Fatal(err)
	}

	gotB := l.Build(context.Background(), "chat-B", "u", false)
	if strings.Contains(gotB, "secret-fact-A") {
		t.Fatalf("chat B banner leaked chat A memory:\n%s", gotB)
	}
	if !strings.Contains(gotB, "fact-B") {
		t.Errorf("chat B banner missing its own memory:\n%s", gotB)
	}
}

func TestLoader_CrossUser_DoesNotLeak(t *testing.T) {
	l, store := newTestLoader(t)
	if err := store.AppendMemory(ScopeUser, "alice", "alice-private"); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMemory(ScopeUser, "bob", "bob-private"); err != nil {
		t.Fatal(err)
	}

	gotForBob := l.Build(context.Background(), "chat-1", "bob", false)
	if strings.Contains(gotForBob, "alice-private") {
		t.Fatalf("user bob banner leaked alice memory:\n%s", gotForBob)
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
