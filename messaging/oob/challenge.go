package oob

import (
	"context"
	"crypto/rand"
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

// challengeIDBytes controls challenge ID entropy; 4 bytes -> 8 hex
// chars, which is plenty given each ID is single-use and short-lived.
const challengeIDBytes = 4

// Errors returned by the challenge primitives.
var (
	ErrChallengeNotFound = errors.New("oob: challenge not found or already resolved")
	ErrChallengeExpired  = errors.New("oob: challenge expired")
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

	// OwnerID, when non-empty, names a user who may approve this
	// challenge even when they are not the RequesterID. Set to the
	// machine owner's ID for challenges issued on behalf of non-owner
	// senders (e.g. cross-chat ACTION flows). Empty means only the
	// requester can approve (the /full-access self-approval path).
	OwnerID string

	resultCh chan challengeResult
	once     sync.Once
}

type challengeResult struct {
	approved bool
	err      error
}

// IssueOptions tunes the lifetime of a single challenge. The zero
// value uses DefaultChallengeTTL.
type IssueOptions struct {
	TTL     time.Duration
	OwnerID string // optional: a user who can approve besides the requester
}

// Issue creates a new pending challenge in the manager and returns
// it. The caller is responsible for delivering the challenge prompt
// to the operator (via PostChallengePrompt or an equivalent text post
// in the owner DM) and for invoking Wait to block on the resolution.
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
		OwnerID:      opts.OwnerID,
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
// On any return path (success or failure) the challenge is removed
// from the manager so its ID becomes invalid for further Approve or
// Deny calls.
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

// resolve sends a final result on the challenge's channel exactly
// once. Subsequent calls are silently dropped so racing
// Approve/Deny/expire paths do not panic.
func (c *Challenge) resolve(approved bool, err error) {
	c.once.Do(func() {
		c.resultCh <- challengeResult{approved: approved, err: err}
	})
}

// Approve marks the named challenge as approved. Returns ErrChallengeNotFound
// when the ID is unknown and ErrChallengeExpired when the challenge
// has already timed out. Phase 2b no longer consults a PIN — the caller
// has already validated the operator identity (owner DM + slash-command
// prefix).
func (m *Manager) Approve(challengeID string) (bool, error) {
	c, ok := m.lookupChallenge(challengeID)
	if !ok {
		return false, ErrChallengeNotFound
	}
	if time.Now().After(c.ExpiresAt) {
		c.resolve(false, ErrChallengeExpired)
		return false, ErrChallengeExpired
	}
	c.resolve(true, nil)
	slog.Info("oob: challenge approved",
		"component", "oob",
		"challengeID", challengeID,
		"requesterID", c.RequesterID,
		"intent", c.Intent,
	)
	return true, nil
}

// Deny resolves the challenge as not approved. Useful for an explicit
// `/approval deny <id>` reply in the bot DM.
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

// PendingFor returns the currently outstanding (non-expired)
// challenges issued to the given requester.
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

// Pending returns all non-expired challenges regardless of requester.
func (m *Manager) Pending() []*Challenge {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	var out []*Challenge
	for _, c := range m.challenges {
		if now.After(c.ExpiresAt) {
			continue
		}
		out = append(out, c)
	}
	return out
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
