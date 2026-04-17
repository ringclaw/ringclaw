package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// AgentInfo holds metadata about an agent for logging/debugging.
type AgentInfo struct {
	Name    string // e.g. "claude-acp", "claude", "gpt-4o"
	Type    string // e.g. "acp", "cli", "http"
	Model   string // e.g. "sonnet", "gpt-4o-mini"
	Command string // binary path, e.g. "/usr/local/bin/claude-agent-acp"
	PID     int    // subprocess PID (0 if not applicable, e.g. http agent)
}

// String returns a human-readable summary for logging.
func (i AgentInfo) String() string {
	s := fmt.Sprintf("name=%s, type=%s, model=%s, command=%s", i.Name, i.Type, i.Model, i.Command)
	if i.PID > 0 {
		s += fmt.Sprintf(", pid=%d", i.PID)
	}
	return s
}

// workspaceRoot is the configured allowlist root for /cwd and Agent.SetCwd.
// When non-empty, every requested cwd must resolve to a path inside this
// directory. When empty (the test default), the allowlist check is disabled
// for backward compatibility.
var (
	workspaceRootMu sync.RWMutex
	workspaceRoot   string
)

// SetWorkspaceRoot configures the allowlist root for cwd changes. Production
// code should call this once at startup with cfg.AgentWorkspace (or the
// default ~/.ringclaw/workspace). The path is resolved to its absolute,
// symlink-cleaned form so containment checks are stable.
func SetWorkspaceRoot(root string) {
	root = strings.TrimSpace(root)
	if root == "" {
		workspaceRootMu.Lock()
		workspaceRoot = ""
		workspaceRootMu.Unlock()
		return
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		slog.Warn("workspace root: filepath.Abs failed, using raw value", "component", "agent", "root", root, "error", err)
		abs = root
	}
	if cleaned, err := filepath.EvalSymlinks(abs); err == nil {
		abs = cleaned
	}
	workspaceRootMu.Lock()
	workspaceRoot = abs
	workspaceRootMu.Unlock()
}

// WorkspaceRoot returns the configured allowlist root, or "" if unset.
func WorkspaceRoot() string {
	workspaceRootMu.RLock()
	defer workspaceRootMu.RUnlock()
	return workspaceRoot
}

// EnsurePathInWorkspace verifies that absPath is inside the configured
// workspace root. Returns nil when no root is configured (allowlist
// disabled), when absPath equals the root, or when filepath.Rel reports a
// non-escaping relative path. Symlinks are resolved for any existing
// ancestor of the path so the check survives operator paths under
// /var/folders -> /private/var/folders style indirection.
func EnsurePathInWorkspace(absPath string) error {
	root := WorkspaceRoot()
	if root == "" {
		return nil
	}
	abs, err := filepath.Abs(absPath)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", absPath, err)
	}
	abs = resolveExistingSymlinks(abs)
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return fmt.Errorf("path %q is not under workspace root %q", absPath, root)
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("path %q escapes workspace root %q", absPath, root)
	}
	return nil
}

// resolveExistingSymlinks walks up the path until an existing ancestor is
// found, runs filepath.EvalSymlinks on that ancestor, and re-joins the
// trailing components. This lets us evaluate paths that don't yet exist on
// disk while still following symlinks on the parts that do.
func resolveExistingSymlinks(abs string) string {
	cur := abs
	var trailing []string
	for {
		if _, err := os.Stat(cur); err == nil {
			if cleaned, err := filepath.EvalSymlinks(cur); err == nil {
				if len(trailing) == 0 {
					return cleaned
				}
				parts := append([]string{cleaned}, trailing...)
				return filepath.Join(parts...)
			}
			return abs
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		trailing = append([]string{filepath.Base(cur)}, trailing...)
		cur = parent
	}
}

// defaultWorkspace returns ~/.ringclaw/workspace as the default working directory.
// Ensures it is a git repo so CLI agents like codex don't complain.
func defaultWorkspace() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	dir := filepath.Join(home, ".ringclaw", "workspace")
	os.MkdirAll(dir, 0o755)
	// Initialize as git repo if not already one
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		exec.Command("git", "init", dir).Run()
	}
	return dir
}

// mergeEnv merges extra environment variables into the base environment.
func mergeEnv(base []string, extra map[string]string) ([]string, error) {
	if len(extra) == 0 {
		return base, nil
	}

	merged := append([]string(nil), base...)
	indexByKey := make(map[string]int, len(base))
	for i, entry := range merged {
		key, _, found := strings.Cut(entry, "=")
		if !found || key == "" {
			continue
		}
		indexByKey[key] = i
	}

	newKeys := make([]string, 0, len(extra))
	for key, value := range extra {
		if key == "" || strings.Contains(key, "=") {
			return nil, fmt.Errorf("invalid env key %q", key)
		}
		entry := key + "=" + value
		if idx, ok := indexByKey[key]; ok {
			merged[idx] = entry
			continue
		}
		newKeys = append(newKeys, key)
	}

	sort.Strings(newKeys)
	for _, key := range newKeys {
		merged = append(merged, key+"="+extra[key])
	}

	return merged, nil
}

// ImageAttachment holds a downloaded image for multi-modal input.
type ImageAttachment struct {
	Data      []byte
	MediaType string // e.g. "image/png"
	Name      string
}

// Agent is the interface for AI chat agents.
type Agent interface {
	// Chat sends a message to the agent and returns the response.
	// conversationID is used to maintain conversation history per user.
	Chat(ctx context.Context, conversationID string, message string) (string, error)

	// ResetSession clears the existing session for the given conversationID and
	// starts a new one. Returns the new session ID if immediately available
	// (ACP mode), or an empty string if the ID will be assigned on next Chat
	// (CLI mode) or is not applicable (HTTP mode).
	ResetSession(ctx context.Context, conversationID string) (string, error)

	// SetCwd changes the working directory for subsequent operations.
	SetCwd(cwd string)

	// Info returns metadata about this agent.
	Info() AgentInfo
}

// ImageSupporter is an optional interface for agents that support image input.
type ImageSupporter interface {
	ChatWithImages(ctx context.Context, conversationID string, message string, images []ImageAttachment) (string, error)
}
