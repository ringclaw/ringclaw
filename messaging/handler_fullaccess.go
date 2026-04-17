package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/messaging/oob"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// FullAccessCommandPrefix is the slash prefix that toggles ACP
// full-access via the OOB PIN flow. The command is intentionally only
// honored from the bot's DM with the trusted owner so the PIN approval
// stays on the same secured channel as the OOB challenges themselves.
const FullAccessCommandPrefix = "/full-access"

// fullAccessDefaultGrant is the TTL applied when the operator runs
// `/full-access grant` without an explicit duration. Five minutes is
// short enough to limit blast radius if the operator forgets to
// /full-access revoke afterwards but long enough for a typical
// interactive task.
const fullAccessDefaultGrant = 5 * time.Minute

// fullAccessMaxGrant caps any explicit duration the operator passes to
// /full-access grant. We refuse longer windows so a single approval
// cannot leave the bot unguarded for an entire workday.
const fullAccessMaxGrant = 4 * time.Hour

// IsFullAccessCommand reports whether text begins with the
// /full-access slash command (with optional subcommand arguments).
func IsFullAccessCommand(text string) bool {
	t := strings.TrimSpace(text)
	if t == FullAccessCommandPrefix {
		return true
	}
	return strings.HasPrefix(t, FullAccessCommandPrefix+" ")
}

// handleFullAccess routes a /full-access command. Subcommands:
//
//	/full-access                 → status (same as `status`)
//	/full-access status          → show current grant state
//	/full-access grant [dur]     → request a TTL unlock; PIN approval
//	                               is performed via the standard OOB
//	                               challenge in this same DM
//	/full-access revoke          → clear any active grant immediately
//
// The function only acts when invoked from the bot's DM with the
// trusted owner — the same chat that receives OOB challenge cards.
// Group-chat invocations are refused with an explanatory message so
// operators are not surprised when the command appears to do nothing.
func (h *Handler) handleFullAccess(ctx context.Context, client *ringcentral.Client, chatID, requesterID, text string) {
	mgr := h.OOBManager()
	if mgr == nil {
		logSendError(SendTextReply(ctx, client, chatID, "OOB approval is not configured; /full-access is disabled."))
		return
	}
	ownerDM := h.OwnerDMChatID()
	if ownerDM == "" || chatID != ownerDM {
		logSendError(SendTextReply(ctx, client, chatID,
			"`/full-access` is only available in the bot's DM with the owner."))
		return
	}

	args := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), FullAccessCommandPrefix))
	sub, rest := splitFirstWord(args)
	switch strings.ToLower(sub) {
	case "", "status":
		logSendError(SendTextReply(ctx, client, chatID, formatFullAccessStatus(mgr)))
	case "revoke", "off", "lock":
		mgr.RevokeFullAccess()
		logSendError(SendTextReply(ctx, client, chatID, "Full-access revoked."))
	case "grant", "on", "unlock":
		dur, err := parseGrantDuration(rest)
		if err != nil {
			logSendError(SendTextReply(ctx, client, chatID, "Invalid duration: "+err.Error()))
			return
		}
		// Async OOB round-trip: the operator must reply with the PIN
		// (in this same DM) for the grant to take effect. Run on a
		// background goroutine so the slash-command handler returns
		// quickly and the WebSocket loop can keep dispatching.
		go h.runFullAccessGrant(ctx, client, chatID, requesterID, dur)
	default:
		logSendError(SendTextReply(ctx, client, chatID,
			"Usage: `/full-access status` | `/full-access grant [duration]` | `/full-access revoke`"))
	}
}

// runFullAccessGrant drives the OOB challenge → PIN approval round-trip
// and, on success, calls Manager.GrantFullAccess for the requested TTL.
// All errors are reported back to the same DM so the operator gets a
// clear acknowledgement either way.
func (h *Handler) runFullAccessGrant(ctx context.Context, client *ringcentral.Client, chatID, requesterID string, dur time.Duration) {
	mgr := h.OOBManager()
	if mgr == nil {
		logSendError(SendTextReply(ctx, client, chatID, "OOB manager went away before approval; aborted."))
		return
	}
	intent := fmt.Sprintf("grant ACP full-access for %s", dur)
	approved, err := mgr.Authorize(ctx, oob.AuthorizeOptions{
		RequesterID:  requesterID,
		Intent:       intent,
		OriginChatID: chatID,
		OwnerDMChat:  chatID,
		Client:       newOOBClient(client),
		// SkipCache keeps every /full-access invocation explicit:
		// a recently-approved grant must not bypass the PIN prompt
		// because the "intent" includes a fresh duration each time.
		SkipCache: true,
	})
	if err != nil {
		slog.Warn("full-access OOB authorize failed",
			"component", "handler",
			"requesterID", requesterID,
			"error", err,
		)
		logSendError(SendTextReply(ctx, client, chatID,
			fmt.Sprintf("Full-access grant aborted: %v", err)))
		return
	}
	if !approved {
		logSendError(SendTextReply(ctx, client, chatID, "Full-access grant denied."))
		return
	}
	mgr.GrantFullAccess(dur)
	expiry := mgr.FullAccessExpiresAt()
	logSendError(SendTextReply(ctx, client, chatID,
		fmt.Sprintf("Full-access granted for %s (until %s).",
			dur, expiry.Format(time.RFC3339))))
}

func formatFullAccessStatus(mgr *oob.Manager) string {
	if !mgr.FullAccessActive() {
		return fmt.Sprintf("Full-access: **off**.\nUse `/full-access grant [duration]` (default %s, max %s) to request a TTL-bounded unlock.",
			fullAccessDefaultGrant, fullAccessMaxGrant)
	}
	exp := mgr.FullAccessExpiresAt()
	remaining := time.Until(exp).Round(time.Second)
	return fmt.Sprintf("Full-access: **on** (expires %s, ~%s remaining). Use `/full-access revoke` to lock immediately.",
		exp.Format(time.RFC3339), remaining)
}

// parseGrantDuration parses an optional duration argument. Empty input
// yields fullAccessDefaultGrant; oversized inputs are clamped at
// fullAccessMaxGrant and returned with no error so operators do not
// have to guess the cap.
func parseGrantDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return fullAccessDefaultGrant, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}
	if d > fullAccessMaxGrant {
		return fullAccessMaxGrant, nil
	}
	return d, nil
}

// splitFirstWord splits s on the first run of whitespace, returning
// (firstWord, restTrimmed). Both halves are empty when s is empty.
func splitFirstWord(s string) (string, string) {
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
