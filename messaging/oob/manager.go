// Package oob implements the minimal out-of-band approval primitives
// used by RingClaw's Phase 2 hardening. The Manager keeps only two
// pieces of state:
//
//   - Challenge lifecycle (Issue / Wait / Approve / Deny) so a specific
//     /full-access grant request can be bound to a short-lived ID.
//   - A TTL-based full-access state (GrantFullAccess / FullAccessActive /
//     FullAccessExpiresAt) consumed by the ACP agent layer to flip new
//     sessions into set_mode "full-access".
//
// Cross-chat ACTION dispatches are not gated here; callers instead
// post a synchronous fail-closed heads-up to the owner DM (see
// messaging/actions.go). The Manager lives entirely in memory — there
// is no file under ~/.ringclaw for this subsystem.
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

	// expiryTimer proactively fires the revoke hook when a live grant
	// reaches its TTL, so callers that never poll FullAccessActive
	// (e.g. the ACP agent layer, which only consults it on new
	// sessions) still observe the expiry in real time. Reset on every
	// GrantFullAccess, cleared on RevokeFullAccess and on fire.
	expiryTimer *time.Timer

	// revokeHook, when non-nil, is invoked every time a previously
	// active full-access grant becomes inactive: explicit revoke,
	// proactive timer expiry, or lazy expiry detected on read. It is
	// intentionally called asynchronously (fresh goroutine) so a slow
	// agent round-trip cannot stall the manager mutex.
	revokeHook func()
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

// SetFullAccessRevokeHook installs a callback that fires every time a
// previously active full-access grant becomes inactive (explicit
// revoke, TTL expiry, or lazy expiry detected on read). Pass nil to
// clear.
//
// The hook is invoked in a fresh goroutine so a slow downstream call
// (e.g. set_mode round-trips to live ACP subprocesses) cannot block
// subsequent Manager operations.
func (m *Manager) SetFullAccessRevokeHook(hook func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revokeHook = hook
}

// GrantFullAccess unlocks ACP full-access mode for the given duration.
// The agent layer polls FullAccessActive on every new ACP session so
// expired grants naturally drop back to the default guarded mode; the
// revoke hook (if installed) provides proactive demotion of live
// sessions.
//
// ttl <= 0 is a no-op so callers can pass a parsed-but-negative value
// without an extra guard; validation happens at the command layer.
func (m *Manager) GrantFullAccess(ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	m.mu.Lock()
	m.fullAccessUntil = time.Now().Add(ttl)
	if m.expiryTimer != nil {
		m.expiryTimer.Stop()
	}
	m.expiryTimer = time.AfterFunc(ttl, m.onExpiryTimer)
	slog.Warn("oob: ACP full-access granted",
		"component", "oob",
		"ttl", ttl.String(),
		"expiresAt", m.fullAccessUntil.Format(time.RFC3339),
	)
	m.mu.Unlock()
}

// RevokeFullAccess clears any active full-access grant immediately.
// Idempotent: revoking when nothing is granted is a silent no-op.
// Fires the revoke hook when (and only when) an active grant was
// actually cleared, so repeat revokes do not trigger spurious
// downstream demotion work.
func (m *Manager) RevokeFullAccess() {
	m.mu.Lock()
	wasActive := !m.fullAccessUntil.IsZero()
	if wasActive {
		slog.Warn("oob: ACP full-access revoked", "component", "oob")
	}
	m.fullAccessUntil = time.Time{}
	if m.expiryTimer != nil {
		m.expiryTimer.Stop()
		m.expiryTimer = nil
	}
	hook := m.revokeHook
	m.mu.Unlock()
	if wasActive && hook != nil {
		go hook()
	}
}

// FullAccessActive reports whether a full-access grant is currently in
// effect. Lazily clears expired grants on read so callers do not need
// a separate sweep goroutine. A lazy expiry detected here also fires
// the revoke hook (if installed) — the proactive timer should normally
// get there first, but the lazy path keeps the invariant true even
// when the timer was lost (e.g. process paused/resumed under a
// debugger).
func (m *Manager) FullAccessActive() bool {
	expired := m.consumeIfExpired()
	if expired {
		m.fireRevokeHook()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.fullAccessUntil.IsZero()
}

// FullAccessExpiresAt returns the expiration time of the active grant
// (zero if none). Used by /info and /full-access status replies.
func (m *Manager) FullAccessExpiresAt() time.Time {
	expired := m.consumeIfExpired()
	if expired {
		m.fireRevokeHook()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fullAccessUntil
}

// consumeIfExpired clears the grant state when TTL has elapsed and
// returns true in that case. Mutex-internal: callers must not hold
// m.mu on entry.
func (m *Manager) consumeIfExpired() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fullAccessUntil.IsZero() {
		return false
	}
	if time.Now().After(m.fullAccessUntil) {
		m.fullAccessUntil = time.Time{}
		if m.expiryTimer != nil {
			m.expiryTimer.Stop()
			m.expiryTimer = nil
		}
		return true
	}
	return false
}

// onExpiryTimer is the time.AfterFunc callback armed by
// GrantFullAccess. It clears the grant and fires the revoke hook. The
// timer may race with an explicit revoke or with consumeIfExpired (on
// a concurrent read); all three paths are idempotent.
func (m *Manager) onExpiryTimer() {
	m.mu.Lock()
	wasActive := !m.fullAccessUntil.IsZero()
	m.fullAccessUntil = time.Time{}
	m.expiryTimer = nil
	hook := m.revokeHook
	m.mu.Unlock()
	if wasActive {
		slog.Warn("oob: ACP full-access expired (TTL reached)", "component", "oob")
		if hook != nil {
			go hook()
		}
	}
}

// fireRevokeHook invokes the installed revoke hook in a fresh
// goroutine. Safe to call when no hook is installed. Safe to call
// concurrently with GrantFullAccess / RevokeFullAccess.
func (m *Manager) fireRevokeHook() {
	m.mu.Lock()
	hook := m.revokeHook
	m.mu.Unlock()
	if hook != nil {
		go hook()
	}
}
