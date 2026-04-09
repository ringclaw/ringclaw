package messaging

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	memoryHeader  = "# RingClaw Shared Memory\n\n"
	memorySection = "## Memories\n"
)

// MemoryStore manages cross-agent shared memory via AGENTS.md.
type MemoryStore struct {
	mu   sync.Mutex
	path string // path to AGENTS.md
}

// NewMemoryStore creates a memory store at the given workspace directory.
// If workspace is empty, falls back to ~/.ringclaw/.
func NewMemoryStore(workspace string) *MemoryStore {
	dir := workspace
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			dir = "."
		} else {
			dir = filepath.Join(home, ".ringclaw")
		}
	}
	_ = os.MkdirAll(dir, 0755)
	return &MemoryStore{path: filepath.Join(dir, "AGENTS.md")}
}

// Path returns the file path of the memory store.
func (m *MemoryStore) Path() string {
	return m.path
}

// Add appends a memory entry with timestamp.
func (m *MemoryStore) Add(content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries := m.readEntries()
	ts := time.Now().Format("2006-01-02")
	entries = append(entries, fmt.Sprintf("[%s] %s", ts, content))
	slog.Info("memory added", "component", "memory", "content", content)
	return m.writeEntries(entries)
}

// Delete removes a memory entry by 1-based index.
func (m *MemoryStore) Delete(index int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries := m.readEntries()
	if index < 1 || index > len(entries) {
		return fmt.Errorf("invalid index %d (have %d entries)", index, len(entries))
	}
	removed := entries[index-1]
	entries = append(entries[:index-1], entries[index:]...)
	slog.Info("memory deleted", "component", "memory", "index", index, "content", removed)
	return m.writeEntries(entries)
}

// List returns all memory entries as a formatted string.
func (m *MemoryStore) List() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries := m.readEntries()
	if len(entries) == 0 {
		return "No memories stored."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Shared Memory** (%d entries)\n", len(entries)))
	for i, e := range entries {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, e))
	}
	sb.WriteString(fmt.Sprintf("\nFile: `%s`", m.path))
	return sb.String()
}

// Count returns the number of stored entries.
func (m *MemoryStore) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.readEntries())
}

func (m *MemoryStore) readEntries() []string {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return nil
	}
	return parseMemoryEntries(string(data))
}

func (m *MemoryStore) writeEntries(entries []string) error {
	var sb strings.Builder
	sb.WriteString(memoryHeader)
	sb.WriteString(memorySection)
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("- %s\n", e))
	}
	return os.WriteFile(m.path, []byte(sb.String()), 0644)
}

func parseMemoryEntries(content string) []string {
	var entries []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			entries = append(entries, trimmed[2:])
		}
	}
	return entries
}

// EnsureBridgeFiles creates CLAUDE.md and GEMINI.md in the workspace
// that reference AGENTS.md, so all agents read the shared memory.
func EnsureBridgeFiles(workspace string) {
	if workspace == "" {
		return
	}

	claudeFile := filepath.Join(workspace, "CLAUDE.md")
	if _, err := os.Stat(claudeFile); os.IsNotExist(err) {
		content := "# Claude Code Instructions\n\n@AGENTS.md\n"
		if err := os.WriteFile(claudeFile, []byte(content), 0644); err != nil {
			slog.Warn("failed to create CLAUDE.md bridge", "component", "memory", "error", err)
		} else {
			slog.Info("created CLAUDE.md bridge file", "component", "memory", "path", claudeFile)
		}
	}

	geminiFile := filepath.Join(workspace, "GEMINI.md")
	if _, err := os.Stat(geminiFile); os.IsNotExist(err) {
		content := "# Gemini CLI Instructions\n\nSee AGENTS.md for shared project memory and conventions.\n"
		if err := os.WriteFile(geminiFile, []byte(content), 0644); err != nil {
			slog.Warn("failed to create GEMINI.md bridge", "component", "memory", "error", err)
		} else {
			slog.Info("created GEMINI.md bridge file", "component", "memory", "path", geminiFile)
		}
	}
}
