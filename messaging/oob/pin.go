// Package oob implements out-of-band PIN approval for high-risk RingClaw
// actions (Phase 2 of the Remote Control hardening plan).
//
// Phase 1 closed the trivial unauthenticated-RCE surface by enforcing a
// trusted-sender allowlist and locking non-owner ACTION blocks to the
// origin chat. Phase 2 raises the bar for the remaining high-risk paths
// that even a trusted owner should not be able to trigger purely from a
// chat message: cross-chat MESSAGE / CARD dispatches and ACP full-access
// unlocks.
//
// The trust assumption is that an attacker who hijacks the owner's
// RingCentral session (account compromise, prompt injection inside a
// bot DM, malicious teammate impersonating the owner) does NOT
// simultaneously have shell access to the host running RingClaw. A
// 6-digit PIN that is generated locally and never leaves the host's
// disk therefore acts as a second factor: every high-risk action must
// be acknowledged with the PIN typed back into the bot DM before the
// action executes.
//
// On-disk format: ~/.ringclaw/oob_pin (mode 0o600, JSON document with a
// bcrypt hash of the plaintext PIN). The plaintext is shown to the
// operator exactly once at startup, on the local terminal, and is never
// transmitted. Operators can rotate by deleting the file and restarting
// the bot.
package oob

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// PinFileName is the on-disk name (under ~/.ringclaw) of the bcrypt-hashed
// approval PIN.
const PinFileName = "oob_pin"

// pinDigits is the length of the generated decimal PIN. 6 digits balances
// human typability against a 1-in-1e6 brute-force probability per attempt;
// the rate limiter on Manager.VerifyPIN tightens this further.
const pinDigits = 6

// pinFile is the JSON shape persisted to disk.
type pinFile struct {
	Version   int       `json:"version"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"createdAt"`
}

// Manager is the central OOB primitive: it owns the on-disk PIN hash and
// the in-memory state for outstanding challenges, the approval cache, and
// the full-access TTL token.
//
// A nil *Manager is NOT a valid receiver; callers should treat the OOB
// manager as optional via a separate "is configured" boolean to keep
// Phase 1 behavior in code paths that have not been wired through.
type Manager struct {
	dir       string
	hash      []byte
	createdAt time.Time

	mu              sync.Mutex
	challenges      map[string]*Challenge
	approvals       map[string]time.Time // key = approvalCacheKey(requesterID, intent)
	verifyFailures  []time.Time
	fullAccessUntil time.Time
}

// LoadOptions tweaks Manager construction; the zero value is fine for
// production callers (they get the default ~/.ringclaw directory).
type LoadOptions struct {
	// Dir overrides the directory that holds the PIN file. Defaults to
	// ~/.ringclaw.  Tests use this to avoid touching the real home dir.
	Dir string
}

// Load returns a Manager backed by the on-disk PIN file. If the file
// does not exist, a fresh 6-digit PIN is generated, hashed, and written
// with mode 0o600; the plaintext is returned via newPIN so the caller
// can print it on the local terminal exactly once. For an existing file
// newPIN is empty.
//
// Returning the plaintext (instead of, say, logging it inside Load)
// keeps the secret out of structured logs and lets the caller decide
// whether to skip printing it (e.g. in unit tests).
func Load(opts LoadOptions) (m *Manager, newPIN string, err error) {
	dir := opts.Dir
	if dir == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return nil, "", fmt.Errorf("oob: resolve home dir: %w", herr)
		}
		dir = filepath.Join(home, ".ringclaw")
	}
	if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
		return nil, "", fmt.Errorf("oob: create dir %s: %w", dir, mkErr)
	}
	path := filepath.Join(dir, PinFileName)

	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		var pf pinFile
		if jErr := json.Unmarshal(data, &pf); jErr != nil {
			return nil, "", fmt.Errorf("oob: parse %s: %w", path, jErr)
		}
		if pf.Hash == "" {
			return nil, "", fmt.Errorf("oob: %s has empty hash", path)
		}
		return &Manager{
			dir:        dir,
			hash:       []byte(pf.Hash),
			createdAt:  pf.CreatedAt,
			challenges: map[string]*Challenge{},
			approvals:  map[string]time.Time{},
		}, "", nil
	case errors.Is(readErr, fs.ErrNotExist):
		return generateAndStore(dir, path)
	default:
		return nil, "", fmt.Errorf("oob: read %s: %w", path, readErr)
	}
}

func generateAndStore(dir, path string) (*Manager, string, error) {
	pin, err := generatePIN(pinDigits)
	if err != nil {
		return nil, "", fmt.Errorf("oob: generate pin: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", fmt.Errorf("oob: hash pin: %w", err)
	}
	pf := pinFile{
		Version:   1,
		Hash:      string(hash),
		CreatedAt: time.Now().UTC(),
	}
	body, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("oob: marshal pin file: %w", err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		return nil, "", fmt.Errorf("oob: write %s: %w", path, err)
	}
	slog.Info("oob: generated new approval PIN", "component", "oob", "path", path)
	return &Manager{
		dir:        dir,
		hash:       hash,
		createdAt:  pf.CreatedAt,
		challenges: map[string]*Challenge{},
		approvals:  map[string]time.Time{},
	}, pin, nil
}

// Path returns the absolute path to the on-disk PIN file.
func (m *Manager) Path() string { return filepath.Join(m.dir, PinFileName) }

// CreatedAt returns when the PIN was generated. Mostly useful for the
// /info status card so operators know how stale the PIN is.
func (m *Manager) CreatedAt() time.Time { return m.createdAt }

// VerifyPIN compares pin against the stored bcrypt hash. It also enforces
// a sliding-window rate limit (max 5 verification attempts per minute)
// to defeat trivial 6-digit brute force attempts even by callers with a
// foothold inside the bot.
func (m *Manager) VerifyPIN(pin string) bool {
	pin = strings.TrimSpace(pin)
	if pin == "" {
		return false
	}
	if !m.takeVerifyToken() {
		slog.Warn("oob: PIN verify rate limit exceeded", "component", "oob")
		return false
	}
	if err := bcrypt.CompareHashAndPassword(m.hash, []byte(pin)); err != nil {
		return false
	}
	return true
}

// takeVerifyToken implements a 5-attempt-per-minute sliding window.
func (m *Manager) takeVerifyToken() bool {
	const window = time.Minute
	const maxAttempts = 5
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := now.Add(-window)
	kept := m.verifyFailures[:0]
	for _, t := range m.verifyFailures {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	m.verifyFailures = kept
	if len(m.verifyFailures) >= maxAttempts {
		return false
	}
	m.verifyFailures = append(m.verifyFailures, now)
	return true
}

// generatePIN returns a zero-padded decimal string of n digits drawn
// uniformly from crypto/rand.
func generatePIN(n int) (string, error) {
	if n <= 0 || n > 18 {
		return "", fmt.Errorf("invalid pin length %d", n)
	}
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
	v, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	format := fmt.Sprintf("%%0%dd", n)
	return fmt.Sprintf(format, v), nil
}
