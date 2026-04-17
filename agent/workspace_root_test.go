package agent

import (
	"path/filepath"
	"testing"
)

func TestEnsurePathInWorkspace_Disabled(t *testing.T) {
	defer SetWorkspaceRoots(nil)
	SetWorkspaceRoots(nil)

	if err := EnsurePathInWorkspace("/anywhere/at/all"); err != nil {
		t.Errorf("expected nil when workspace allowlist is empty, got %v", err)
	}
}

func TestEnsurePathInWorkspace_Allows(t *testing.T) {
	root := t.TempDir()
	defer SetWorkspaceRoots(nil)
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
	defer SetWorkspaceRoots(nil)
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
	defer SetWorkspaceRoots(nil)
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

// TestEnsurePathInWorkspace_MultipleRoots verifies that a path is
// allowed when it lives under ANY of the configured roots, and rejected
// when it lives under none.
func TestEnsurePathInWorkspace_MultipleRoots(t *testing.T) {
	defer SetWorkspaceRoots(nil)
	rootA := t.TempDir()
	rootB := t.TempDir()
	other := t.TempDir()
	SetWorkspaceRoots([]string{rootA, rootB})

	allowed := []string{
		rootA,
		filepath.Join(rootA, "x"),
		rootB,
		filepath.Join(rootB, "deep", "nested"),
	}
	for _, p := range allowed {
		if err := EnsurePathInWorkspace(p); err != nil {
			t.Errorf("expected %q allowed (under one of the roots), got %v", p, err)
		}
	}

	denied := []string{
		other,
		filepath.Join(other, "y"),
		"/etc/passwd",
	}
	for _, p := range denied {
		if err := EnsurePathInWorkspace(p); err == nil {
			t.Errorf("expected %q rejected (outside every root), got nil", p)
		}
	}
}

// TestSetWorkspaceRoots_DedupesAndCanonicalizes verifies that empty
// strings are dropped and equivalent paths are deduplicated after
// symlink resolution.
func TestSetWorkspaceRoots_DedupesAndCanonicalizes(t *testing.T) {
	defer SetWorkspaceRoots(nil)
	root := t.TempDir()
	SetWorkspaceRoots([]string{root, "", "  ", root, root + string(filepath.Separator)})
	got := WorkspaceRoots()
	if len(got) != 1 {
		t.Fatalf("expected dedupe to a single root, got %v", got)
	}
}
