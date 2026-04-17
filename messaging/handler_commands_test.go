package messaging

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ringclaw/ringclaw/agent"
)

func TestValidateCwdPath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		path    string
		wantErr bool
	}{
		{filepath.Join(home, "workspace"), false},
		{"/tmp/project", false},
		{filepath.Join(home, ".ssh"), true},
		{filepath.Join(home, ".ssh", "keys"), true},
		{filepath.Join(home, ".gnupg"), true},
		{filepath.Join(home, ".ringclaw"), true},
		{filepath.Join(home, ".ringclaw", "config"), true},
		{filepath.Join(home, ".aws"), true},
		{filepath.Join(home, ".aws", "credentials"), true},
		{filepath.Join(home, ".kube"), true},
		{filepath.Join(home, ".config", "gcloud"), true},
	}

	// On Windows, paths use backslashes but validateCwdPath normalizes
	if runtime.GOOS == "windows" {
		tests = append(tests, struct {
			path    string
			wantErr bool
		}{home + `\.ssh\keys`, true})
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			err := validateCwdPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCwdPath(%q) error=%v, wantErr=%v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestHandleCwd_AllowsInsideWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "project")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	defer agent.SetWorkspaceRoot("")
	agent.SetWorkspaceRoot(root)

	h := newTestHandler()
	got := h.handleCwd("/cwd " + sub)
	if !strings.HasPrefix(got, "cwd: ") {
		t.Errorf("expected success reply, got %q", got)
	}
}

func TestHandleCwd_RejectsOutsideWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // a sibling temp dir, definitely outside `root`
	defer agent.SetWorkspaceRoot("")
	agent.SetWorkspaceRoot(root)

	h := newTestHandler()
	got := h.handleCwd("/cwd " + outside)
	if !strings.HasPrefix(got, "Denied:") {
		t.Errorf("expected denial reply, got %q", got)
	}
	if !strings.Contains(got, "escapes workspace root") {
		t.Errorf("expected escape message, got %q", got)
	}
}
