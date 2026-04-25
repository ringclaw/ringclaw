package oob

import (
	"context"
	"fmt"
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
// one-way display only. The text instructs the operator to use the
// terminal CLI for approval.
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
		"Pending approval (challenge `%s`). Run on the host machine:\n  ringclaw approval %s\n  ringclaw approval deny %s\nExpires in %s. Requested TTL: %s.",
		c.ID, c.ID, c.ID, expiresIn, ttlLabel,
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

// HandleApprovalReply intercepts `/approval` messages in the bot DM.
// Chat-based approval is disabled; the function informs the sender to
// use the terminal CLI instead and returns true to consume the message.
func (m *Manager) HandleApprovalReply(ctx context.Context, client Client, dmChatID, senderID, text string) bool {
	reply := ParseApprovalReply(text)
	if reply.Kind == ReplyNone {
		return false
	}
	_ = client.SendText(ctx, dmChatID,
		"Approval via chat is disabled for security. Run on the host machine:\n"+
			"  ringclaw approval <id>       (approve)\n"+
			"  ringclaw approval deny <id>  (deny)\n"+
			"  ringclaw approval list       (list pending)")
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
