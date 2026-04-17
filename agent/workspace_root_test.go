package agent

import (
	"path/filepath"
	"testing"
)

func TestEnsurePathInWorkspace_Disabled(t *testing.T) {
	defer SetWorkspaceRoot("")
	SetWorkspaceRoot("")

	if err := EnsurePathInWorkspace("/anywhere/at/all"); err != nil {
		t.Errorf("expected nil when workspace root is unset, got %v", err)
	}
}

func TestEnsurePathInWorkspace_Allows(t *testing.T) {
	root := t.TempDir()
	defer SetWorkspaceRoot("")
	SetWorkspaceRoot(root)

	cases := []string{
		root,
		filepath.Join(root, "sub"),
		filepath.Join(root, "deep", "nested", "path"),
	}
	for _, p := range cases {
		if err := EnsurePathInWorkspace(p); err != nil {
			t.Errorf("expected %q allowed, got %v", p, err)
		}
	}
}

func TestEnsurePathInWorkspace_Rejects(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Dir(root)
	defer SetWorkspaceRoot("")
	SetWorkspaceRoot(root)

	cases := []string{
		parent,
		filepath.Join(parent, "sibling"),
		"/etc/passwd",
	}
	for _, p := range cases {
		if err := EnsurePathInWorkspace(p); err == nil {
			t.Errorf("expected %q rejected, got nil", p)
		}
	}
}

func TestSetWorkspaceRoot_TrimsAndAbs(t *testing.T) {
	defer SetWorkspaceRoot("")
	SetWorkspaceRoot("  /tmp  ")
	got := WorkspaceRoot()
	wantAbs, _ := filepath.Abs("/tmp")
	if cleaned, err := filepath.EvalSymlinks(wantAbs); err == nil {
		wantAbs = cleaned
	}
	if got != wantAbs {
		t.Errorf("WorkspaceRoot() = %q, want %q", got, wantAbs)
	}
}
