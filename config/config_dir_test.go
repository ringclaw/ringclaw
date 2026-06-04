package config

import (
	"path/filepath"
	"testing"
)

// TestConfigPath_Default verifies that an empty dir argument resolves
// to ~/.ringclaw/config.json using the current user's home directory.
func TestConfigPath_Default(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	got, err := ConfigPath("")
	if err != nil {
		t.Fatalf("ConfigPath(\"\") unexpected error: %v", err)
	}
	want := filepath.Join(tmpHome, ".ringclaw", "config.json")
	if got != want {
		t.Errorf("ConfigPath(\"\") = %q, want %q", got, want)
	}
}

// TestConfigPath_Custom verifies that a non-empty dir argument is used
// directly as the home directory, producing <dir>/config.json.
func TestConfigPath_Custom(t *testing.T) {
	dir := "/tmp/bot1"
	got, err := ConfigPath(dir)
	if err != nil {
		t.Fatalf("ConfigPath(%q) unexpected error: %v", dir, err)
	}
	want := "/tmp/bot1/config.json"
	if got != want {
		t.Errorf("ConfigPath(%q) = %q, want %q", dir, got, want)
	}
}
