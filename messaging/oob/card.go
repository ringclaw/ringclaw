package oob

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CardKind tags the visual purpose of an OOB Adaptive Card so the
// Adaptive Card style can change (color scheme, leading emoji) without
// the caller assembling JSON itself.
type CardKind int

const (
	CardKindChallenge CardKind = iota
	CardKindApproved
	CardKindDenied
	CardKindExpired
)

// BuildChallengeCard returns Adaptive Card JSON describing a pending
// approval request. The card is intentionally read-only (no Input.Text
// or Action.Submit) because the current RingCentral monitor only
// subscribes to /team-messaging/v1/posts and would not deliver a card
// submit event. Operators reply with the PIN as a normal text message
// in the bot DM; ParseApprovalReply does the routing.
//
// Each card is rendered with the challenge ID and the truncated intent
// so the operator can disambiguate concurrent requests.
func BuildChallengeCard(c *Challenge) json.RawMessage {
	body := []map[string]any{
		{
			"type":   "TextBlock",
			"text":   "🔐 RingClaw approval required",
			"weight": "Bolder",
			"size":   "Medium",
			"color":  "Attention",
		},
		{
			"type":  "TextBlock",
			"text":  fmt.Sprintf("**Action:** %s", truncate(c.Intent, 200)),
			"wrap":  true,
			"color": "Default",
		},
		{
			"type": "FactSet",
			"facts": []map[string]any{
				{"title": "Challenge", "value": "`" + c.ID + "`"},
				{"title": "Requester", "value": c.RequesterID},
				{"title": "Expires", "value": c.ExpiresAt.Format(time.RFC3339)},
			},
		},
		{
			"type": "TextBlock",
			"text": "Reply in this DM with `" + c.ID + " <PIN>` to approve, or `/deny " + c.ID + "` to refuse. " +
				"If this is the only outstanding challenge you may reply with the PIN alone.",
			"wrap":     true,
			"isSubtle": true,
			"size":     "Small",
		},
	}
	card := map[string]any{
		"type":    "AdaptiveCard",
		"version": "1.3",
		"body":    body,
	}
	out, _ := json.Marshal(card)
	return out
}

// BuildResolutionCard returns a confirmation card for an approved /
// denied / expired challenge. Used so the operator gets visual feedback
// instead of a bare text string.
func BuildResolutionCard(c *Challenge, kind CardKind, detail string) json.RawMessage {
	var (
		title string
		color string
	)
	switch kind {
	case CardKindApproved:
		title = "✅ Approved"
		color = "Good"
	case CardKindDenied:
		title = "🚫 Denied"
		color = "Warning"
	case CardKindExpired:
		title = "⌛ Expired"
		color = "Attention"
	default:
		title = "OOB result"
		color = "Default"
	}
	body := []map[string]any{
		{
			"type":   "TextBlock",
			"text":   title,
			"weight": "Bolder",
			"size":   "Medium",
			"color":  color,
		},
		{
			"type": "FactSet",
			"facts": []map[string]any{
				{"title": "Challenge", "value": "`" + c.ID + "`"},
				{"title": "Action", "value": truncate(c.Intent, 200)},
			},
		},
	}
	if detail != "" {
		body = append(body, map[string]any{
			"type": "TextBlock",
			"text": detail,
			"wrap": true,
		})
	}
	card := map[string]any{
		"type":    "AdaptiveCard",
		"version": "1.3",
		"body":    body,
	}
	out, _ := json.Marshal(card)
	return out
}

// ApprovalReply describes the parsed shape of a PIN reply typed into
// the bot DM. Exactly one of Approve/Deny is set when Kind != ReplyNone.
type ApprovalReply struct {
	Kind        ApprovalReplyKind
	ChallengeID string // may be empty for a bare PIN
	PIN         string
}

// ApprovalReplyKind enumerates the recognized DM reply patterns.
type ApprovalReplyKind int

const (
	ReplyNone ApprovalReplyKind = iota
	ReplyApprove
	ReplyDeny
)

// ParseApprovalReply best-effort interprets a DM text body as an
// approval-related reply. Returns ReplyNone when the text does not look
// like one (so the caller can fall through to normal agent dispatch).
//
// Recognized syntaxes:
//
//	/approve <id> <pin>     -> ReplyApprove with both fields set
//	<id> <pin>              -> ReplyApprove with both fields set
//	<pin>                   -> ReplyApprove with empty ChallengeID;
//	                           caller must resolve via Manager.PendingFor
//	/deny <id>              -> ReplyDeny
//
// All matches require <id> to be 8 hex characters and <pin> to be
// pinDigits decimal characters so a casual chat message ("hi 123456")
// is not interpreted as an approval — the bare-PIN case is intentionally
// permissive but Manager.HandleApprovalReply only acts on it when there
// is exactly one pending challenge for the requester.
func ParseApprovalReply(text string) ApprovalReply {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ApprovalReply{}
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "/approve"):
		fields := strings.Fields(trimmed[len("/approve"):])
		if len(fields) == 2 && isChallengeID(fields[0]) && isPIN(fields[1]) {
			return ApprovalReply{Kind: ReplyApprove, ChallengeID: strings.ToLower(fields[0]), PIN: fields[1]}
		}
		return ApprovalReply{}
	case strings.HasPrefix(lower, "/deny"):
		fields := strings.Fields(trimmed[len("/deny"):])
		if len(fields) == 1 && isChallengeID(fields[0]) {
			return ApprovalReply{Kind: ReplyDeny, ChallengeID: strings.ToLower(fields[0])}
		}
		return ApprovalReply{}
	}
	fields := strings.Fields(trimmed)
	switch len(fields) {
	case 2:
		if isChallengeID(fields[0]) && isPIN(fields[1]) {
			return ApprovalReply{Kind: ReplyApprove, ChallengeID: strings.ToLower(fields[0]), PIN: fields[1]}
		}
	case 1:
		if isPIN(fields[0]) {
			return ApprovalReply{Kind: ReplyApprove, PIN: fields[0]}
		}
	}
	return ApprovalReply{}
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

func isPIN(s string) bool {
	if len(s) != pinDigits {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func truncate(s string, max int) string {
	if max <= 3 || len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
