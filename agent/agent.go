package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	// ChatWithImages sends a message with image attachments.
	// Agents that don't support images should fall back to text-only.
	ChatWithImages(ctx context.Context, conversationID string, message string, images []ImageAttachment) (string, error)

	// Info returns metadata about this agent.
	Info() AgentInfo
}
