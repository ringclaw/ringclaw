package paths

import (
	"path/filepath"
	"testing"
)

func TestAppHomeUsesHomeWhenItAlreadyPointsAtRingClawRoot(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".ringclaw")
	t.Setenv("RINGCLAW_HOME", "")
	t.Setenv("HOME", home)

	got, err := AppHome()
	if err != nil {
		t.Fatalf("AppHome returned error: %v", err)
	}
	if got != home {
		t.Fatalf("AppHome() = %q, want %q", got, home)
	}
}

func TestResolveEnvOrDefaultUsesRingClawRootHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".ringclaw")
	t.Setenv("RINGCLAW_HOME", "")
	t.Setenv("RINGCLAW_MEMORY_DIR", "")
	t.Setenv("HOME", home)

	got, err := ResolveEnvOrDefault("RINGCLAW_MEMORY_DIR", "workspace", "memory")
	if err != nil {
		t.Fatalf("ResolveEnvOrDefault returned error: %v", err)
	}
	want := filepath.Join(home, "workspace", "memory")
	if got != want {
		t.Fatalf("ResolveEnvOrDefault() = %q, want %q", got, want)
	}
}
