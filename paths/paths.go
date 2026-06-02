package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AppHome returns RingClaw's writable state root.
//
// Precedence:
// 1. RINGCLAW_HOME
// 2. $HOME/.ringclaw
func AppHome() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("RINGCLAW_HOME")); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ringclaw"), nil
}

// AppPath returns a path rooted under AppHome.
func AppPath(parts ...string) (string, error) {
	dir, err := AppHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{dir}, parts...)...), nil
}

// MustAppPath returns a path rooted under AppHome, falling back to os.TempDir
// when the home lookup fails.
func MustAppPath(parts ...string) string {
	path, err := AppPath(parts...)
	if err == nil {
		return path
	}
	fallback := append([]string{os.TempDir(), "ringclaw"}, parts...)
	return filepath.Join(fallback...)
}

// ConfigFile returns the config path, honoring RINGCLAW_CONFIG first.
func ConfigFile() (string, error) {
	if path := strings.TrimSpace(os.Getenv("RINGCLAW_CONFIG")); path != "" {
		return path, nil
	}
	return AppPath("config.json")
}

// ResolveEnvOrDefault returns envVar when set, otherwise the default path
// rooted under AppHome.
func ResolveEnvOrDefault(envVar string, defaultParts ...string) (string, error) {
	if path := strings.TrimSpace(os.Getenv(envVar)); path != "" {
		return path, nil
	}
	if len(defaultParts) == 0 {
		return "", fmt.Errorf("defaultParts must not be empty")
	}
	return AppPath(defaultParts...)
}
