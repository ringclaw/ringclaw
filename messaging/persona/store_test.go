package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestStore returns a Store rooted in a fresh temp dir. Uses
// aggressive caps so truncation tests hit the boundary without huge
// fixtures.
func newTestStore(t *testing.T, chatCap int) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := ResolvedConfig{
		Enabled:              true,
		SoulFile:             filepath.Join(dir, "SOUL.md"),
		MemoryDir:            filepath.Join(dir, "memory"),
		MaxSoulChars:         500,
		MaxChatMemoryChars:   chatCap,
		MaxUserMemoryChars:   500,
		MaxGlobalMemoryChars: 500,
	}
	return NewStore(cfg), dir
}

func TestStore_LoadSoul_MissingFile(t *testing.T) {
	// No SOUL.md on disk must return ("", nil) — callers treat that
	// as "persona not yet configured" rather than a hard failure.
	s, _ := newTestStore(t, 500)
	got, err := s.LoadSoul()
	if err != nil {
		t.Fatalf("LoadSoul: %v", err)
	}
	if got != "" {
		t.Errorf("missing soul should yield empty, got %q", got)
	}
}

func TestStore_EnsureSoulTemplate_WritesDefault(t *testing.T) {
	s, dir := newTestStore(t, 500)
	if err := s.EnsureSoulTemplate(); err != nil {
		t.Fatalf("EnsureSoulTemplate: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "SOUL.md"))
	if err != nil {
		t.Fatalf("read soul: %v", err)
	}
	if !strings.Contains(string(data), "# SOUL") {
		t.Errorf("template missing '# SOUL' heading: %q", data)
	}

	// Second call must not overwrite an edited file — operators'
	// custom persona text is sacred.
	customized := "# SOUL\n\nCustom persona.\n"
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte(customized), 0o600); err != nil {
		t.Fatalf("write custom: %v", err)
	}
	if err := s.EnsureSoulTemplate(); err != nil {
		t.Fatalf("second EnsureSoulTemplate: %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "SOUL.md"))
	if string(data) != customized {
		t.Errorf("EnsureSoulTemplate overwrote custom content:\n%s", data)
	}
}

func TestStore_AppendAndLoadMemory_RoundTrip(t *testing.T) {
	s, _ := newTestStore(t, 500)

	if err := s.AppendMemory(ScopeChat, "12345", "user prefers terse replies"); err != nil {
		t.Fatalf("AppendMemory: %v", err)
	}

	got, err := s.LoadMemory(ScopeChat, "12345")
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if !strings.Contains(got, "user prefers terse replies") {
		t.Errorf("LoadMemory missing appended entry: %q", got)
	}
	if !strings.HasPrefix(got, "- [") {
		t.Errorf("expected timestamped bullet, got %q", got[:min(40, len(got))])
	}
}

func TestStore_AppendMemory_RejectsEmpty(t *testing.T) {
	// Empty entries are almost certainly a bug at the call site
	// (e.g. "/remember" with no text). Refuse them here so a
	// corrupted empty line never lands in memory.
	s, _ := newTestStore(t, 500)
	if err := s.AppendMemory(ScopeChat, "12345", ""); err == nil {
		t.Error("AppendMemory(empty) should fail")
	}
	if err := s.AppendMemory(ScopeChat, "12345", "   \t\n"); err == nil {
		t.Error("AppendMemory(whitespace-only) should fail")
	}
}

func TestStore_AppendMemory_TruncatesAtCap(t *testing.T) {
	// After each append, the file must not exceed the scope cap. A
	// small cap makes the boundary easy to hit.
	s, _ := newTestStore(t, 120)
	for i := 0; i < 30; i++ {
		if err := s.AppendMemory(ScopeChat, "c1", strings.Repeat("x", 20)); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	got, _ := s.LoadMemory(ScopeChat, "c1")
	if len(got) > 120 {
		t.Errorf("memory len %d exceeds cap 120", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected truncation marker, got %q", got)
	}
}

func TestStore_ClearMemory_RemovesFile(t *testing.T) {
	s, _ := newTestStore(t, 500)
	if err := s.AppendMemory(ScopeChat, "c1", "hello"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := s.ClearMemory(ScopeChat, "c1"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ := s.LoadMemory(ScopeChat, "c1")
	if got != "" {
		t.Errorf("after clear expected empty, got %q", got)
	}
	// Clearing an already-absent file must be a no-op, not an error.
	if err := s.ClearMemory(ScopeChat, "c1"); err != nil {
		t.Errorf("second clear: %v", err)
	}
}

func TestStore_PathTraversal_ContainedInMemoryDir(t *testing.T) {
	// Hostile chat / user IDs must not escape MemoryDir. After
	// AppendMemory completes, the only files written must live
	// inside MemoryDir. We assert this by rooting the test in a
	// tempdir and checking that no file outside it was created.
	s, dir := newTestStore(t, 500)
	memDir := filepath.Join(dir, "memory")

	attacks := []string{
		"../etc/passwd",
		"..",
		"../../root",
		"/absolute/path",
		"chat\x00../etc",
	}
	for _, raw := range attacks {
		if err := s.AppendMemory(ScopeChat, raw, "attack attempt"); err != nil {
			t.Errorf("AppendMemory(%q) unexpected error: %v", raw, err)
		}
	}

	// Walk the filesystem rooted at dir — every file we find must
	// live under memDir/chat/ (plus the memDir itself).
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasPrefix(path, memDir+string(os.PathSeparator)) {
			t.Errorf("attack wrote outside memDir: %s", path)
		}
		return nil
	})
}

func TestStore_SoulTruncation(t *testing.T) {
	s, dir := newTestStore(t, 500)

	// Write a SOUL longer than the cap, load, expect truncation.
	long := strings.Repeat("A", 600)
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, "SOUL.md")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte(long), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := s.LoadSoul()
	if err != nil {
		t.Fatalf("LoadSoul: %v", err)
	}
	if len(got) > 500 {
		t.Errorf("soul len %d exceeds cap 500", len(got))
	}
}

func TestStore_GlobalScope_IgnoresID(t *testing.T) {
	// ScopeGlobal is singleton — different IDs must hit the same
	// file so global facts really are shared.
	s, _ := newTestStore(t, 500)
	if err := s.AppendMemory(ScopeGlobal, "ignored-1", "one"); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMemory(ScopeGlobal, "ignored-2", "two"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.LoadMemory(ScopeGlobal, "whatever")
	if !strings.Contains(got, "one") || !strings.Contains(got, "two") {
		t.Errorf("global scope should share file, got %q", got)
	}
}

func TestTruncateTail_Behaviors(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		max       int
		wantLen   int
		wantBody  string // substring that must be present
	}{
		{"under cap unchanged", "hello", 100, 5, "hello"},
		{"zero cap disables", strings.Repeat("x", 200), 0, 200, "x"},
		{"exact cap unchanged", strings.Repeat("x", 50), 50, 50, "x"},
		{"over cap truncated", strings.Repeat("x", 500), 100, 100, "truncated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateTail(tc.in, tc.max)
			if len(got) != tc.wantLen {
				t.Errorf("len=%d want %d", len(got), tc.wantLen)
			}
			if !strings.Contains(got, tc.wantBody) {
				t.Errorf("got %q does not contain %q", got, tc.wantBody)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
