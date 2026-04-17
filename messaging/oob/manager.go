// Package oob implements the minimal out-of-band approval primitives
// used by RingClaw's Phase 2b hardening. The previous iteration (Phase
// 2) required a locally-generated 6-digit PIN (bcrypt-hashed on disk,
// rate-limited) that the operator typed into the bot DM to approve
// high-risk actions. Investigation showed that RingCentral's WebSocket
// subscription does not deliver Adaptive-Card Action.Submit events, so
// the PIN machinery could only ever be driven via plain-text replies
// in the DM anyway — a slash command in the owner's DM turned out to
// provide equivalent practical security with much less ceremony.
//
// Phase 2b therefore drops PIN/bcrypt/rate-limit and keeps only:
//
//   - Challenge lifecycle (Issue / Wait / Approve / Deny) so a specific
//     /full-access grant request can be bound to a short-lived ID.
//   - A TTL-based full-access state (GrantFullAccess / FullAccessActive /
//     FullAccessExpiresAt) consumed by the ACP agent layer to flip new
//     sessions into set_mode "full-access".
//
// Cross-chat ACTION dispatches are no longer gated here; callers
// instead post a best-effort notification to the owner DM (see
// messaging/actions.go). The Manager lives entirely in memory — there
// is no longer a file under ~/.ringclaw for this subsystem.
package oob

import (
	"log/slog"
	"sync"
	"time"
)

// Manager owns the in-memory challenge table and the full-access TTL
// state. A nil *Manager is not a valid receiver; callers treat OOB as
// optional via a separate "is configured" boolean so library entry
// points keep working without forcing every call site through OOB.
type Manager struct {
	mu              sync.Mutex
	challenges      map[string]*Challenge
	fullAccessUntil time.Time
}

// Options tweaks Manager construction. The zero value is fine for
// production callers.
type Options struct {
	// Reserved for future use (e.g. injectable clock for tests). Kept
	// as a struct so callers do not have to change signature if we add
	// knobs later.
	_ struct{}
}

// New returns a fresh Manager with empty state. Unlike the Phase 2
// oob.Load, this does not touch the filesystem: there is no PIN file,
// no bcrypt hash, no on-disk state. The manager is recreated on every
// restart, which is intentional — any previous /full-access grant is
// discarded so a crash-restart re-locks the bot until the operator
// explicitly re-grants.
func New(_ Options) *Manager {
	return &Manager{
		challenges: make(map[string]*Challenge),
	}
}

// GrantFullAccess unlocks ACP full-access mode for the given duration.
// The agent layer polls FullAccessActive on every new ACP session so
// expired grants naturally drop back to the default guarded mode.
//
// ttl <= 0 is a no-op so callers can pass a parsed-but-negative value
// without an extra guard; validation happens at the command layer.
func (m *Manager) GrantFullAccess(ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fullAccessUntil = time.Now().Add(ttl)
	slog.Warn("oob: ACP full-access granted",
		"component", "oob",
		"ttl", ttl.String(),
		"expiresAt", m.fullAccessUntil.Format(time.RFC3339),
	)
}

// RevokeFullAccess clears any active full-access grant immediately.
// Idempotent: revoking when nothing is granted is a silent no-op.
func (m *Manager) RevokeFullAccess() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.fullAccessUntil.IsZero() {
		slog.Warn("oob: ACP full-access revoked", "component", "oob")
	}
	m.fullAccessUntil = time.Time{}
}

// FullAccessActive reports whether a full-access grant is currently in
// effect. Lazily clears expired grants on read so callers do not need
// a separate sweep goroutine.
func (m *Manager) FullAccessActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fullAccessUntil.IsZero() {
		return false
	}
	if time.Now().After(m.fullAccessUntil) {
		m.fullAccessUntil = time.Time{}
		return false
	}
	return true
}

// FullAccessExpiresAt returns the expiration time of the active grant
// (zero if none). Used by /info and /full-access status replies.
func (m *Manager) FullAccessExpiresAt() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.fullAccessUntil.IsZero() && time.Now().After(m.fullAccessUntil) {
		m.fullAccessUntil = time.Time{}
	}
	return m.fullAccessUntil
}
