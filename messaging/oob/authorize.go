package oob

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Client is the narrow interface the OOB manager needs from the
// RingCentral client: sending a plain text message to the owner's
// bot DM. Defined locally so messaging/oob does not import
// ringcentral (which would create a cycle when ringcentral grows
// helpers that surface OOB state).
type Client interface {
	SendText(ctx context.Context, chatID, text string) error
}

// PostChallengePrompt posts a plain-text prompt to the owner DM
// describing the pending challenge. Phase 2b intentionally avoids
// Adaptive Cards here: RingCentral's WebSocket subscription does not
// deliver Action.Submit events, so an interactive card would be a
// one-way display only. The text contains the challenge ID and the
// two recognised reply shapes.
//
// requestedTTL is a human-readable label (e.g. "24h") shown so the
// operator knows what they are approving; it is not interpreted here.
func PostChallengePrompt(ctx context.Context, client Client, c *Challenge, requestedTTL string) error {
	if client == nil {
		return fmt.Errorf("oob: PostChallengePrompt requires Client")
	}
	if c == nil {
		return fmt.Errorf("oob: PostChallengePrompt requires Challenge")
	}
	ttlLabel := strings.TrimSpace(requestedTTL)
	if ttlLabel == "" {
		ttlLabel = "n/a"
	}
	expiresIn := time.Until(c.ExpiresAt).Round(time.Second)
	if expiresIn < 0 {
		expiresIn = 0
	}
	msg := fmt.Sprintf(
		"Pending approval: reply `/approval %s` to confirm or `/approval deny %s` to reject. Expires in %s. Requested TTL: %s.",
		c.ID, c.ID, expiresIn, ttlLabel,
	)
	if err := client.SendText(ctx, c.OwnerDMChat, msg); err != nil {
		return fmt.Errorf("oob: deliver challenge prompt: %w", err)
	}
	return nil
}

// ApprovalReplyKind enumerates the recognised DM reply patterns.
type ApprovalReplyKind int

const (
	// ReplyNone means the text did not match any recognised approval
	// syntax. The caller should fall through to normal message
	// dispatch.
	ReplyNone ApprovalReplyKind = iota
	// ReplyApprove means `/approval <id>`.
	ReplyApprove
	// ReplyDeny means `/approval deny <id>`.
	ReplyDeny
)

// ApprovalReply is the parsed shape of a `/approval ...` reply. Kind
// == ReplyNone when the text did not match.
type ApprovalReply struct {
	Kind        ApprovalReplyKind
	ChallengeID string
}

// ParseApprovalReply interprets a DM text body as an approval reply.
// Returns ReplyNone when the text does not look like one.
//
// Recognised syntaxes (case-insensitive command token; challenge ID
// must be 8 hex characters):
//
//	/approval <id>          -> ReplyApprove
//	/approval deny <id>     -> ReplyDeny
//
// Anything else (including the former PIN / bare-PIN shapes) returns
// ReplyNone so the message falls through to the agent.
func ParseApprovalReply(text string) ApprovalReply {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ApprovalReply{}
	}
	head, rest := splitFirstField(trimmed)
	if !strings.EqualFold(head, "/approval") {
		return ApprovalReply{}
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return ApprovalReply{}
	}
	sub, remainder := splitFirstField(rest)
	if strings.EqualFold(sub, "deny") {
		remainder = strings.TrimSpace(remainder)
		if !isChallengeID(remainder) {
			return ApprovalReply{}
		}
		return ApprovalReply{Kind: ReplyDeny, ChallengeID: strings.ToLower(remainder)}
	}
	// Otherwise `sub` is the challenge ID. Anything trailing is
	// rejected to keep the parser strict.
	if remainder != "" {
		return ApprovalReply{}
	}
	if !isChallengeID(sub) {
		return ApprovalReply{}
	}
	return ApprovalReply{Kind: ReplyApprove, ChallengeID: strings.ToLower(sub)}
}

// HandleApprovalReply tries to interpret a DM message as an approval
// reply and, if recognised, resolves the matching pending challenge.
// Returns true when the message was consumed (callers must then
// short-circuit normal agent dispatch so the slash command does not
// reach the AI).
//
// senderID scopes `/approval deny` so a teammate sharing the bot DM
// cannot cancel another user's pending challenge.
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
	c, ok := m.lookupChallenge(reply.ChallengeID)
	if !ok {
		_ = client.SendText(ctx, dmChatID, fmt.Sprintf("Challenge `%s` is not pending.", reply.ChallengeID))
		return true
	}
	if c.RequesterID != senderID && c.OwnerID != senderID {
		slog.Warn("oob: approval refused for non-requester, non-owner",
			"component", "oob",
			"challengeID", reply.ChallengeID,
			"senderID", senderID,
			"requesterID", c.RequesterID,
			"ownerID", c.OwnerID,
		)
		_ = client.SendText(ctx, dmChatID, fmt.Sprintf("Challenge `%s` was issued for a different user.", reply.ChallengeID))
		return true
	}
	if _, err := m.Approve(reply.ChallengeID); err != nil {
		switch err {
		case ErrChallengeNotFound:
			_ = client.SendText(ctx, dmChatID, fmt.Sprintf("Challenge `%s` is not pending.", reply.ChallengeID))
		case ErrChallengeExpired:
			_ = client.SendText(ctx, dmChatID, fmt.Sprintf("Challenge `%s` expired.", reply.ChallengeID))
		default:
			slog.Error("oob: approve failed", "component", "oob", "error", err)
			_ = client.SendText(ctx, dmChatID, "Approval failed: "+err.Error())
		}
		return true
	}
	return true
}

func (m *Manager) handleDenyReply(ctx context.Context, client Client, dmChatID, senderID string, reply ApprovalReply) bool {
	c, ok := m.lookupChallenge(reply.ChallengeID)
	if !ok {
		_ = client.SendText(ctx, dmChatID, fmt.Sprintf("Challenge `%s` is not pending.", reply.ChallengeID))
		return true
	}
	if c.RequesterID != senderID && c.OwnerID != senderID {
		slog.Warn("oob: deny refused for non-requester, non-owner",
			"component", "oob",
			"challengeID", reply.ChallengeID,
			"senderID", senderID,
			"requesterID", c.RequesterID,
			"ownerID", c.OwnerID,
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

func isChallengeID(s string) bool {
	if len(s) != challengeIDBytes*2 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// splitFirstField returns (first whitespace-delimited field, rest).
// Both halves are already trimmed of the whitespace that separated
// them; an input that has no whitespace returns ("", s) only when s
// is empty, otherwise (s, "").
func splitFirstField(s string) (string, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	for i, r := range s {
		if r == ' ' || r == '\t' {
			return s[:i], strings.TrimSpace(s[i:])
		}
	}
	return s, ""
}
