package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Scope identifies which memory bucket an operation targets. Keep
// this in sync with the on-disk layout:
//
//	<MemoryDir>/global.md
//	<MemoryDir>/user/<sanitized userID>.md
//	<MemoryDir>/chat/<sanitized chatID>.md
//
// SOUL lives at ResolvedConfig.SoulFile and has no scope — callers
// use the dedicated LoadSoul / EnsureSoulTemplate helpers.
type Scope string

const (
	ScopeGlobal Scope = "global"
	ScopeUser   Scope = "user"
	ScopeChat   Scope = "chat"
)

// truncationMarker is prefixed to truncated content so agents reading
// the banner know the file was clipped, not mysteriously short.
const truncationMarker = "\n...[older entries truncated]...\n\n"

// Store reads and writes SOUL.md + the memory files under MemoryDir.
// It is the only code path in the persona package that touches the
// filesystem for SOUL / memory content; Loader delegates here so the
// sandbox invariants (path containment, size caps, 0o600 perms) live
// in exactly one place.
type Store struct {
	cfg ResolvedConfig
}

// NewStore returns a Store bound to the given resolved config.
func NewStore(cfg ResolvedConfig) *Store { return &Store{cfg: cfg} }

// Config returns the resolved config the store was built with.
func (s *Store) Config() ResolvedConfig { return s.cfg }

// memoryFilePath returns the absolute on-disk path for (scope, id).
// id is ignored for ScopeGlobal. Both scope and id are contained
// within MemoryDir so a hostile chatID / userID cannot escape.
func (s *Store) memoryFilePath(scope Scope, id string) (string, error) {
	memDir := s.cfg.MemoryDir
	if memDir == "" {
		return "", fmt.Errorf("persona: MemoryDir not configured")
	}
	switch scope {
	case ScopeGlobal:
		return filepath.Join(memDir, "global.md"), nil
	case ScopeUser:
		return filepath.Join(memDir, "user", SanitizeID(id)+".md"), nil
	case ScopeChat:
		return filepath.Join(memDir, "chat", SanitizeID(id)+".md"), nil
	default:
		return "", fmt.Errorf("persona: unknown scope %q", scope)
	}
}

// cap returns the character budget for the given scope from the
// resolved config.
func (s *Store) cap(scope Scope) int {
	switch scope {
	case ScopeGlobal:
		return s.cfg.MaxGlobalMemoryChars
	case ScopeUser:
		return s.cfg.MaxUserMemoryChars
	case ScopeChat:
		return s.cfg.MaxChatMemoryChars
	}
	return 0
}

// LoadSoul returns the current SOUL.md contents, truncated to the
// configured cap. An absent file returns ("", nil) — callers should
// treat it as "no persona configured yet" rather than an error.
func (s *Store) LoadSoul() (string, error) {
	data, err := os.ReadFile(s.cfg.SoulFile)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("persona: read soul: %w", err)
	}
	return truncateTail(string(data), s.cfg.MaxSoulChars), nil
}

// LoadMemory returns the contents of the memory file for (scope, id),
// truncated to the scope's cap. Missing files return ("", nil).
func (s *Store) LoadMemory(scope Scope, id string) (string, error) {
	path, err := s.memoryFilePath(scope, id)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("persona: read memory %s: %w", path, err)
	}
	return truncateTail(string(data), s.cap(scope)), nil
}

// AppendMemory adds an entry to the memory file for (scope, id). The
// entry is prefixed with an RFC3339 timestamp so the file acts as a
// timestamped log, and the whole file is re-truncated to the scope's
// cap after writing so an append-heavy user cannot grow the banner
// unboundedly.
//
// The write is best-effort atomic: we rewrite the full file (not
// open-append) because the truncation pass needs the final bytes
// anyway. 0o600 perms match ~/.ringclaw/api_token.
func (s *Store) AppendMemory(scope Scope, id, entry string) error {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return fmt.Errorf("persona: refusing to append empty memory entry")
	}

	path, err := s.memoryFilePath(scope, id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("persona: create memory dir: %w", err)
	}

	existing, _ := os.ReadFile(path) // missing is fine, yields nil
	timestamp := time.Now().UTC().Format(time.RFC3339)
	newLine := fmt.Sprintf("- [%s] %s\n", timestamp, entry)

	combined := string(existing) + newLine
	combined = truncateTail(combined, s.cap(scope))

	return os.WriteFile(path, []byte(combined), 0o600)
}

// ClearMemory removes the memory file for (scope, id). Missing files
// are not an error — the postcondition is "no memory present" either
// way.
func (s *Store) ClearMemory(scope Scope, id string) error {
	path, err := s.memoryFilePath(scope, id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("persona: clear memory %s: %w", path, err)
	}
	return nil
}

// EnsureSoulTemplate writes a minimal SOUL.md at cfg.SoulFile if no
// file is present there yet. Existing files are never overwritten so
// an operator's handcrafted persona survives restarts and upgrades.
//
// Called from cmd/start on every boot; idempotent.
func (s *Store) EnsureSoulTemplate() error {
	if _, err := os.Stat(s.cfg.SoulFile); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("persona: stat soul: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.cfg.SoulFile), 0o700); err != nil {
		return fmt.Errorf("persona: create soul dir: %w", err)
	}
	return os.WriteFile(s.cfg.SoulFile, []byte(defaultSoulTemplate), 0o600)
}

// defaultSoulTemplate is the stock persona written on first run. It
// is intentionally short and neutral — the operator should edit it.
const defaultSoulTemplate = `# SOUL

You are an assistant connected to RingCentral via RingClaw. Be helpful, concise, and direct.

## Personality
- Professional but friendly
- Honest about limitations — ask for clarification rather than guess
- Proactive about flagging potential problems

## Principles
- Prefer simple solutions over complex ones
- Always confirm before destructive actions
- Respect user privacy

> Edit this file to shape the assistant's persona. All agents (Claude,
> Codex, Gemini, …) share it, so switching agents mid-conversation
> keeps the voice consistent.
`

// truncateTail keeps the last max characters of s, prepending a
// marker when content was dropped. The marker's length is counted
// against the cap so the final string length is always ≤ max.
//
// max ≤ 0 disables truncation (returns s as-is). This makes the
// function safe to call with caps from a misconfigured ResolvedConfig
// — the result is merely "untruncated", which is worse for token
// budget but never crashes.
func truncateTail(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if len(truncationMarker) >= max {
		// Degenerate: cap is smaller than the marker itself. Give up
		// on the marker and just return the tail.
		return s[len(s)-max:]
	}
	return truncationMarker + s[len(s)-(max-len(truncationMarker)):]
}
