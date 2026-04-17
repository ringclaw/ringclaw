package oob

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Client is the narrow interface that the OOB manager needs from the
// RingCentral client. Defined locally so messaging/oob does not import
// the full ringcentral package (avoids a cycle when ringcentral grows
// helpers that want to surface OOB state).
type Client interface {
	CreateAdaptiveCard(ctx context.Context, chatID string, card json.RawMessage) (Card, error)
	SendText(ctx context.Context, chatID, text string) error
}

// Card is the minimum response we need back from CreateAdaptiveCard
// (mostly the ID so we can correlate with future updates if needed).
type Card interface {
	GetID() string
}

// Authorizer is the top-level entry point used by callers (handler,
// actions). It composes Issue/Wait with the Adaptive Card UX so each
// caller is a single function call. A nil *Manager value is acceptable
// for Authorize-via-helper (see AuthorizeOptional below).
type Authorizer interface {
	Authorize(ctx context.Context, opts AuthorizeOptions) (bool, error)
}

// AuthorizeOptions parameterizes a single approval round-trip.
type AuthorizeOptions struct {
	// RequesterID is the RingCentral user ID that triggered the action.
	// Used as the cache key and as the "Requester" field on the card.
	RequesterID string
	// Intent is a human-readable description of what is being requested
	// (e.g. "MESSAGE cross-chat to chat:12345"). Should be stable for
	// the same logical action so the approval cache can hit.
	Intent string
	// OriginChatID is the chat that triggered the action (for audit).
	OriginChatID string
	// OwnerDMChat is the chat ID of the bot DM with the trusted owner.
	// The approval card is posted here. Required.
	OwnerDMChat string
	// Client posts the cards / text. Required.
	Client Client
	// TTL overrides DefaultChallengeTTL when non-zero.
	TTL time.Duration
	// SkipCache forces a fresh challenge even if the (requester, intent)
	// pair is currently cached as approved. Use sparingly (e.g. for
	// /full-access where each unlock should be explicit).
	SkipCache bool
}

// Authorize issues a challenge, posts the Adaptive Card to OwnerDMChat,
// and blocks until it resolves. Returns (true, nil) on approval and
// (false, err) for any failure path.
//
// On success the (requester, intent) pair is cached for
// DefaultApprovalCacheTTL so repeated identical actions in a short
// window do not re-prompt.
func (m *Manager) Authorize(ctx context.Context, opts AuthorizeOptions) (bool, error) {
	if opts.Client == nil {
		return false, fmt.Errorf("oob: Authorize requires Client")
	}
	if strings.TrimSpace(opts.OwnerDMChat) == "" {
		return false, fmt.Errorf("oob: Authorize requires OwnerDMChat")
	}
	if !opts.SkipCache && m.CachedApproval(opts.RequesterID, opts.Intent) {
		slog.Info("oob: cached approval hit",
			"component", "oob",
			"requesterID", opts.RequesterID,
			"intent", opts.Intent,
		)
		return true, nil
	}
	c, err := m.Issue(opts.RequesterID, opts.Intent, opts.OriginChatID, opts.OwnerDMChat, IssueOptions{TTL: opts.TTL})
	if err != nil {
		return false, err
	}
	cardJSON := BuildChallengeCard(c)
	if _, postErr := opts.Client.CreateAdaptiveCard(ctx, opts.OwnerDMChat, cardJSON); postErr != nil {
		// Fall back to a plain text message so the operator is not left
		// in the dark when the Adaptive Card POST fails (e.g. RC rate
		// limit). The text contains the same information.
		slog.Warn("oob: failed to post challenge card; falling back to text",
			"component", "oob",
			"challengeID", c.ID,
			"error", postErr,
		)
		fallback := fmt.Sprintf("RingClaw approval required for %q.\nReply `%s <PIN>` to approve, `/deny %s` to refuse. Expires %s.",
			truncate(c.Intent, 200), c.ID, c.ID, c.ExpiresAt.Format(time.RFC3339))
		if textErr := opts.Client.SendText(ctx, opts.OwnerDMChat, fallback); textErr != nil {
			m.removeChallenge(c.ID)
			return false, fmt.Errorf("oob: deliver challenge: %w", textErr)
		}
	}
	approved, waitErr := c.Wait(ctx, m)
	switch {
	case waitErr == ErrChallengeExpired:
		_, _ = opts.Client.CreateAdaptiveCard(ctx, opts.OwnerDMChat, BuildResolutionCard(c, CardKindExpired, "Challenge expired without a reply."))
	case approved:
		_, _ = opts.Client.CreateAdaptiveCard(ctx, opts.OwnerDMChat, BuildResolutionCard(c, CardKindApproved, "Action will proceed."))
	case waitErr == nil:
		_, _ = opts.Client.CreateAdaptiveCard(ctx, opts.OwnerDMChat, BuildResolutionCard(c, CardKindDenied, "Operator denied the request."))
	}
	return approved, waitErr
}

// HandleApprovalReply tries to interpret a DM message as an approval
// reply and, if recognized, applies the result to the matching pending
// challenge. Returns true when the message was consumed (callers must
// then short-circuit normal agent dispatch). Replies that do not match
// the documented syntax are ignored — the message falls through.
//
// senderID is used both for the bare-PIN disambiguation (only acts when
// the sender has exactly one outstanding challenge) and to scope `/deny`
// so a teammate cannot cancel another user's challenge.
func (m *Manager) HandleApprovalReply(ctx context.Context, client Client, dmChatID, senderID, text string) bool {
	reply := ParseApprovalReply(text)
	if reply.Kind == ReplyNone {
		return false
	}
	switch reply.Kind {
	case ReplyApprove:
		return m.handleApproveReply(ctx, client, dmChatID, senderID, reply)
	case ReplyDeny:
		return m.handleDenyReply(ctx, client, dmChatID, senderID, reply)
	}
	return false
}

func (m *Manager) handleApproveReply(ctx context.Context, client Client, dmChatID, senderID string, reply ApprovalReply) bool {
	challengeID := reply.ChallengeID
	if challengeID == "" {
		pending := m.PendingFor(senderID)
		if len(pending) != 1 {
			// Bare PIN with zero or multiple pending challenges: do not
			// guess; let the message fall through and the operator can
			// re-issue with an explicit ID.
			if len(pending) > 1 {
				_ = client.SendText(ctx, dmChatID, "Multiple pending challenges. Reply with `<id> <PIN>` instead.")
				return true
			}
			return false
		}
		challengeID = pending[0].ID
	}
	approved, err := m.Approve(challengeID, reply.PIN)
	if err != nil {
		switch err {
		case ErrChallengeNotFound:
			_ = client.SendText(ctx, dmChatID, fmt.Sprintf("Challenge `%s` is not pending.", challengeID))
		case ErrChallengeExpired:
			_ = client.SendText(ctx, dmChatID, fmt.Sprintf("Challenge `%s` expired.", challengeID))
		case ErrInvalidPIN:
			_ = client.SendText(ctx, dmChatID, "Incorrect PIN.")
		default:
			slog.Error("oob: approve failed", "component", "oob", "error", err)
			_ = client.SendText(ctx, dmChatID, "Approval failed: "+err.Error())
		}
		return true
	}
	if !approved {
		_ = client.SendText(ctx, dmChatID, "Approval rejected.")
	}
	return true
}

func (m *Manager) handleDenyReply(ctx context.Context, client Client, dmChatID, senderID string, reply ApprovalReply) bool {
	c, ok := m.lookupChallenge(reply.ChallengeID)
	if !ok {
		_ = client.SendText(ctx, dmChatID, fmt.Sprintf("Challenge `%s` is not pending.", reply.ChallengeID))
		return true
	}
	if c.RequesterID != senderID {
		// Defense-in-depth: only the original requester can /deny.
		// Without this check, a teammate sharing the bot DM could
		// cancel a pending challenge issued for the owner.
		slog.Warn("oob: deny refused for non-requester",
			"component", "oob",
			"challengeID", reply.ChallengeID,
			"senderID", senderID,
			"requesterID", c.RequesterID,
		)
		_ = client.SendText(ctx, dmChatID, fmt.Sprintf("Challenge `%s` was issued for a different user.", reply.ChallengeID))
		return true
	}
	if !m.Deny(reply.ChallengeID) {
		_ = client.SendText(ctx, dmChatID, fmt.Sprintf("Challenge `%s` is not pending.", reply.ChallengeID))
		return true
	}
	_ = client.SendText(ctx, dmChatID, fmt.Sprintf("Challenge `%s` denied.", reply.ChallengeID))
	return true
}
