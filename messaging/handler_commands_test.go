package messaging

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
