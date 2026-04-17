package oob

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// DefaultChallengeTTL is how long a freshly issued challenge stays
// valid before it expires unanswered. Five minutes balances usability
// (operator can step away briefly) against blast radius (a leaked
// challenge ID stays exploitable only briefly).
const DefaultChallengeTTL = 5 * time.Minute

// DefaultApprovalCacheTTL is how long a successful approval is cached
// for the same (requester, intent) pair so the operator is not prompted
// repeatedly when the AI emits a burst of similar actions.
const DefaultApprovalCacheTTL = 5 * time.Minute

// challengeIDBytes controls challenge ID entropy; 4 bytes -> 8 hex chars,
// which is plenty given each ID is single-use and short-lived.
const challengeIDBytes = 4

// Errors returned by the challenge primitives.
var (
	ErrChallengeNotFound = errors.New("oob: challenge not found or already resolved")
	ErrChallengeExpired  = errors.New("oob: challenge expired")
	ErrInvalidPIN        = errors.New("oob: invalid PIN")
	ErrCanceled          = errors.New("oob: challenge canceled")
)

// Challenge represents a single outstanding approval request. Each
// challenge resolves exactly once (approved, denied, expired, or
// canceled) and then is removed from the manager.
type Challenge struct {
	ID           string
	Intent       string
	RequesterID  string
	OriginChatID string
	OwnerDMChat  string
	IssuedAt     time.Time
	ExpiresAt    time.Time

	resultCh chan challengeResult
	once     sync.Once
}

type challengeResult struct {
	approved bool
	err      error
}

// IssueOptions tunes the lifetime of a single challenge. The zero value
// uses DefaultChallengeTTL.
type IssueOptions struct {
	TTL time.Duration
}

// Issue creates a new pending challenge in the manager and returns it.
// The caller is responsible for delivering the challenge to the operator
// (e.g. via an Adaptive Card posted to OwnerDMChat) and for invoking
// Wait to block on the resolution.
//
// Issue does not consult the approval cache — call CachedApproval first
// if the caller wants the cache fast-path.
func (m *Manager) Issue(requesterID, intent, originChatID, ownerDMChat string, opts IssueOptions) (*Challenge, error) {
	if strings.TrimSpace(requesterID) == "" {
		return nil, errors.New("oob: requesterID required")
	}
	if strings.TrimSpace(intent) == "" {
		return nil, errors.New("oob: intent required")
	}
	id, err := newChallengeID()
	if err != nil {
		return nil, fmt.Errorf("oob: generate challenge id: %w", err)
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultChallengeTTL
	}
	now := time.Now()
	c := &Challenge{
		ID:           id,
		Intent:       intent,
		RequesterID:  requesterID,
		OriginChatID: originChatID,
		OwnerDMChat:  ownerDMChat,
		IssuedAt:     now,
		ExpiresAt:    now.Add(ttl),
		resultCh:     make(chan challengeResult, 1),
	}
	m.mu.Lock()
	m.challenges[id] = c
	m.mu.Unlock()
	slog.Info("oob: challenge issued",
		"component", "oob",
		"challengeID", id,
		"requesterID", requesterID,
		"intent", intent,
		"ttl", ttl.String(),
	)
	return c, nil
}

// Wait blocks until the challenge resolves. The returned bool reports
// whether the challenge was approved; err is non-nil for any failure
// mode (timeout, cancel, internal error).
//
// On any return path (success or failure) the challenge is removed from
// the manager so its ID becomes invalid for further Approve/Deny calls.
func (c *Challenge) Wait(ctx context.Context, m *Manager) (bool, error) {
	defer m.removeChallenge(c.ID)
	select {
	case <-ctx.Done():
		c.resolve(false, ErrCanceled)
		return false, ctx.Err()
	case <-time.After(time.Until(c.ExpiresAt)):
		c.resolve(false, ErrChallengeExpired)
		return false, ErrChallengeExpired
	case res := <-c.resultCh:
		return res.approved, res.err
	}
}

// resolve sends a final result on the challenge's channel exactly once.
// Subsequent calls are silently dropped so racing Approve/Deny/expire
// paths do not panic.
func (c *Challenge) resolve(approved bool, err error) {
	c.once.Do(func() {
		c.resultCh <- challengeResult{approved: approved, err: err}
	})
}

// Approve marks the named challenge as approved iff the supplied PIN
// matches the on-disk hash. The boolean return tells the caller whether
// the challenge was found and the PIN was correct; an err is returned
// only for unexpected internal failures.
//
// The PIN is verified via Manager.VerifyPIN, which is rate limited.
func (m *Manager) Approve(challengeID, pin string) (bool, error) {
	c, ok := m.lookupChallenge(challengeID)
	if !ok {
		return false, ErrChallengeNotFound
	}
	if time.Now().After(c.ExpiresAt) {
		c.resolve(false, ErrChallengeExpired)
		return false, ErrChallengeExpired
	}
	if !m.VerifyPIN(pin) {
		slog.Warn("oob: PIN verification failed",
			"component", "oob",
			"challengeID", challengeID,
			"requesterID", c.RequesterID,
		)
		return false, ErrInvalidPIN
	}
	m.cacheApproval(c.RequesterID, c.Intent, DefaultApprovalCacheTTL)
	c.resolve(true, nil)
	slog.Info("oob: challenge approved",
		"component", "oob",
		"challengeID", challengeID,
		"requesterID", c.RequesterID,
		"intent", c.Intent,
	)
	return true, nil
}

// Deny resolves the challenge as not approved without consuming a PIN.
// Useful for an explicit "/deny <id>" reply in the bot DM.
func (m *Manager) Deny(challengeID string) bool {
	c, ok := m.lookupChallenge(challengeID)
	if !ok {
		return false
	}
	c.resolve(false, nil)
	slog.Info("oob: challenge denied",
		"component", "oob",
		"challengeID", challengeID,
		"requesterID", c.RequesterID,
		"intent", c.Intent,
	)
	return true
}

// PendingFor returns the IDs of currently outstanding challenges issued
// to the given requester. Used by the bare-PIN reply heuristic so the
// operator can omit the challenge ID when only one is pending.
func (m *Manager) PendingFor(requesterID string) []*Challenge {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	var out []*Challenge
	for _, c := range m.challenges {
		if c.RequesterID != requesterID {
			continue
		}
		if now.After(c.ExpiresAt) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// CachedApproval reports whether (requesterID, intent) has been
// approved within the cache TTL. Callers should consult it before
// issuing a fresh challenge to avoid prompt fatigue when the AI emits
// a burst of similar actions.
func (m *Manager) CachedApproval(requesterID, intent string) bool {
	key := approvalCacheKey(requesterID, intent)
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.approvals[key]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(m.approvals, key)
		return false
	}
	return true
}

// cacheApproval records (requesterID, intent) as approved until now+ttl.
func (m *Manager) cacheApproval(requesterID, intent string, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	key := approvalCacheKey(requesterID, intent)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approvals[key] = time.Now().Add(ttl)
}

// approvalCacheKey hashes (requesterID, intent) so callers cannot
// accidentally collide cache slots via cleverly-formatted intents.
func approvalCacheKey(requesterID, intent string) string {
	h := sha256.New()
	h.Write([]byte(requesterID))
	h.Write([]byte{0})
	h.Write([]byte(intent))
	return hex.EncodeToString(h.Sum(nil))
}

func (m *Manager) lookupChallenge(id string) (*Challenge, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.challenges[id]
	return c, ok
}

func (m *Manager) removeChallenge(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.challenges, id)
}

func newChallengeID() (string, error) {
	buf := make([]byte, challengeIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// GrantFullAccess unlocks ACP full-access mode for the given duration.
// The Manager exposes FullAccessActive so the agent layer can poll the
// state without owning a separate token store.
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
func (m *Manager) RevokeFullAccess() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.fullAccessUntil.IsZero() {
		slog.Warn("oob: ACP full-access revoked", "component", "oob")
	}
	m.fullAccessUntil = time.Time{}
}

// FullAccessActive reports whether a full-access grant is currently in
// effect. Lazily clears expired grants on read.
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
