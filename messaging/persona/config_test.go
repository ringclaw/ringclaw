package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_IsEnabled_DefaultsToTrue(t *testing.T) {
	// nil pointer means "not set" → defaults to enabled so a fresh
	// install gets the persona machinery without any toggle.
	if !(Config{}).IsEnabled() {
		t.Error("zero Config.IsEnabled() should be true")
	}
}

func TestConfig_IsEnabled_RespectsExplicitFalse(t *testing.T) {
	off := false
	c := Config{Enabled: &off}
	if c.IsEnabled() {
		t.Error("Enabled=&false should report disabled")
	}

	on := true
	c2 := Config{Enabled: &on}
	if !c2.IsEnabled() {
		t.Error("Enabled=&true should report enabled")
	}
}

func TestConfig_Resolved_FillsDefaults(t *testing.T) {
	r := Config{}.Resolved()
	if !r.Enabled {
		t.Error("zero Config should resolve to Enabled=true")
	}
	if r.MaxSoulChars != DefaultMaxSoulChars {
		t.Errorf("MaxSoulChars=%d, want default %d", r.MaxSoulChars, DefaultMaxSoulChars)
	}
	if r.MaxChatMemoryChars != DefaultMaxChatMemoryChars {
		t.Errorf("MaxChatMemoryChars=%d, want default %d", r.MaxChatMemoryChars, DefaultMaxChatMemoryChars)
	}
	if r.MaxUserMemoryChars != DefaultMaxUserMemoryChars {
		t.Errorf("MaxUserMemoryChars=%d, want default %d", r.MaxUserMemoryChars, DefaultMaxUserMemoryChars)
	}
	if r.MaxGlobalMemoryChars != DefaultMaxGlobalMemoryChars {
		t.Errorf("MaxGlobalMemoryChars=%d, want default %d", r.MaxGlobalMemoryChars, DefaultMaxGlobalMemoryChars)
	}
	if r.SoulFile == "" || r.MemoryDir == "" {
		t.Errorf("expected non-empty paths, got SoulFile=%q MemoryDir=%q", r.SoulFile, r.MemoryDir)
	}
	// Defaults end with .ringclaw/SOUL.md and .ringclaw/memory.
	if !strings.HasSuffix(r.SoulFile, filepath.Join(".ringclaw", "SOUL.md")) {
		t.Errorf("SoulFile=%q does not end in default .ringclaw/SOUL.md", r.SoulFile)
	}
	if !strings.HasSuffix(r.MemoryDir, filepath.Join(".ringclaw", "memory")) {
		t.Errorf("MemoryDir=%q does not end in default .ringclaw/memory", r.MemoryDir)
	}
}

func TestConfig_Resolved_HonoursOverrides(t *testing.T) {
	off := false
	c := Config{
		Enabled:              &off,
		SoulFile:             "/tmp/soul.md",
		MemoryDir:            "/tmp/mem",
		MaxSoulChars:         123,
		MaxChatMemoryChars:   456,
		MaxUserMemoryChars:   789,
		MaxGlobalMemoryChars: 1011,
	}
	r := c.Resolved()
	if r.Enabled {
		t.Error("explicit Enabled=&false should disable")
	}
	if r.SoulFile != "/tmp/soul.md" || r.MemoryDir != "/tmp/mem" {
		t.Errorf("absolute paths should pass through, got %+v", r)
	}
	if r.MaxSoulChars != 123 || r.MaxChatMemoryChars != 456 ||
		r.MaxUserMemoryChars != 789 || r.MaxGlobalMemoryChars != 1011 {
		t.Errorf("explicit caps should pass through, got %+v", r)
	}
}

func TestConfig_Resolved_ExpandsHomeTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir available")
	}
	c := Config{
		SoulFile:  "~/persona/SOUL.md",
		MemoryDir: "~/persona/memory",
	}
	r := c.Resolved()
	if !strings.HasPrefix(r.SoulFile, home) {
		t.Errorf("SoulFile=%q should start with home %q", r.SoulFile, home)
	}
	if !strings.HasPrefix(r.MemoryDir, home) {
		t.Errorf("MemoryDir=%q should start with home %q", r.MemoryDir, home)
	}
}

func TestExpandHome_Behaviors(t *testing.T) {
	cases := []struct {
		name string
		path string
		home string
		want string
	}{
		{"empty path", "", "/h", ""},
		{"empty home", "/abs", "", "/abs"},
		{"bare tilde", "~", "/h", "/h"},
		{"tilde-slash", "~/x", "/h", filepath.Join("/h", "x")},
		{"absolute path unchanged", "/abs/path", "/h", "/abs/path"},
		{"non-tilde relative unchanged", "rel/x", "/h", "rel/x"},
		{"mid-string tilde unchanged", "/foo/~/bar", "/h", "/foo/~/bar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := expandHome(tc.path, tc.home)
			if got != tc.want {
				t.Errorf("expandHome(%q,%q) = %q, want %q", tc.path, tc.home, got, tc.want)
			}
		})
	}
}

func TestPickInt_PrefersPositiveOverFallback(t *testing.T) {
	cases := []struct {
		got, fallback, want int
	}{
		{0, 100, 100},
		{-5, 100, 100},
		{50, 100, 50},
		{1, 100, 1},
	}
	for _, tc := range cases {
		got := pickInt(tc.got, tc.fallback)
		if got != tc.want {
			t.Errorf("pickInt(%d,%d) = %d, want %d", tc.got, tc.fallback, got, tc.want)
		}
	}
}

func TestStore_MemoryFilePath_PerScope(t *testing.T) {
	s, dir := newTestStore(t, 500)
	memDir := filepath.Join(dir, "memory")

	gpath, err := s.MemoryFilePath(ScopeGlobal, "ignored")
	if err != nil {
		t.Fatalf("global: %v", err)
	}
	if gpath != filepath.Join(memDir, "global.md") {
		t.Errorf("global path = %q, want %q", gpath, filepath.Join(memDir, "global.md"))
	}

	upath, err := s.MemoryFilePath(ScopeUser, "alice")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	if !strings.HasPrefix(upath, filepath.Join(memDir, "user")+string(os.PathSeparator)) {
		t.Errorf("user path %q should live under %s", upath, filepath.Join(memDir, "user"))
	}

	cpath, err := s.MemoryFilePath(ScopeChat, "12345")
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if !strings.HasPrefix(cpath, filepath.Join(memDir, "chat")+string(os.PathSeparator)) {
		t.Errorf("chat path %q should live under %s", cpath, filepath.Join(memDir, "chat"))
	}

	// Unknown scope must surface an error so callers do not silently
	// write to MemoryDir root.
	if _, err := s.MemoryFilePath(Scope("bogus"), "x"); err == nil {
		t.Error("MemoryFilePath(bogus) should fail")
	}
}

func TestStore_MemoryFilePath_RequiresMemoryDir(t *testing.T) {
	s := NewStore(ResolvedConfig{}) // MemoryDir empty
	if _, err := s.MemoryFilePath(ScopeGlobal, ""); err == nil {
		t.Error("empty MemoryDir should yield an error")
	}
}
