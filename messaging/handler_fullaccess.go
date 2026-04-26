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
// full-access via the `/approval` two-step flow. The command is
// intentionally only honored from the bot's DM with the trusted
// owner so the approval round-trip stays on the secured channel.
const FullAccessCommandPrefix = "/full-access"

// fullAccessDefaultGrant is the TTL applied when the operator runs
// `/full-access grant` without an explicit duration. Twenty-four
// hours covers the typical "I'll be working with the AI today"
// workflow; oversized durations are clamped at fullAccessMaxGrant.
const fullAccessDefaultGrant = 24 * time.Hour

// fullAccessMaxGrant caps any explicit duration the operator passes.
// Thirty days accommodates long-running unattended scenarios (CI,
// persistent scratch boxes) while still forcing a deliberate renewal
// rather than an "effectively forever" toggle.
const fullAccessMaxGrant = 30 * 24 * time.Hour

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
//	/full-access grant [dur]     → issue an `/approval <id>` challenge
//	                               in this DM; the grant activates only
//	                               after the operator replies with
//	                               `/approval <id>`
//	/full-access revoke          → clear any active grant immediately
//
// The function only acts when invoked from the bot's DM with the
// trusted owner — the same chat that receives the `/approval`
// challenge text. Group-chat invocations are refused with an
// explanatory message.
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
		h.startFullAccessGrant(ctx, client, chatID, requesterID, dur)
	default:
		logSendError(SendTextReply(ctx, client, chatID,
			"Usage: `/full-access status` | `/full-access grant [duration]` | `/full-access revoke`"))
	}
}

// startFullAccessGrant issues a fresh OOB challenge, posts the
// `/approval <id>` prompt to the owner DM, and drives the Wait/Approve
// loop asynchronously. The activation itself happens when the
// approval reply resolves the challenge — see awaitFullAccessGrant
// below.
func (h *Handler) startFullAccessGrant(ctx context.Context, client *ringcentral.Client, chatID, requesterID string, dur time.Duration) {
	mgr := h.OOBManager()
	if mgr == nil {
		logSendError(SendTextReply(ctx, client, chatID, "OOB manager went away before approval; aborted."))
		return
	}
	intent := fmt.Sprintf("grant ACP full-access for %s", dur)
	c, err := mgr.Issue(requesterID, intent, chatID, chatID, oob.IssueOptions{TTL: oob.DefaultChallengeTTL})
	if err != nil {
		slog.Error("full-access: issue challenge failed",
			"component", "handler", "requesterID", requesterID, "error", err)
		logSendError(SendTextReply(ctx, client, chatID,
			fmt.Sprintf("Full-access grant aborted: %v", err)))
		return
	}
	if err := oob.PostChallengePrompt(ctx, newOOBClient(client), c, dur.String()); err != nil {
		slog.Error("full-access: post challenge prompt failed",
			"component", "handler", "challengeID", c.ID, "error", err)
		// Best-effort cleanup: resolve the challenge as denied so the
		// awaitFullAccessGrant goroutine does not block for the full
		// TTL on a prompt that never reached the operator.
		mgr.Deny(c.ID)
		logSendError(SendTextReply(ctx, client, chatID,
			fmt.Sprintf("Full-access grant aborted: %v", err)))
		return
	}
	logSendError(SendTextReply(ctx, client, chatID,
		"Full-access grant requested. Confirm via /approval in owner DM."))

	go h.awaitFullAccessGrant(client, chatID, c, dur)
}

// awaitFullAccessGrant blocks on the challenge resolution and, on
// approval, flips the manager into full-access for dur. All outcomes
// (approved, denied, expired) emit a confirmation message to the
// owner DM so the operator has a clear acknowledgement either way.
//
// The background context here is intentional: the originating slash
// command returned as soon as the challenge was posted, so the
// goroutine must survive independently of the caller's request scope.
// We cap the wait with the challenge's own ExpiresAt + a small grace
// window via context.WithTimeout.
func (h *Handler) awaitFullAccessGrant(client *ringcentral.Client, chatID string, c *oob.Challenge, dur time.Duration) {
	mgr := h.OOBManager()
	if mgr == nil {
		return
	}
	timeout := time.Until(c.ExpiresAt) + 5*time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	approved, err := c.Wait(ctx, mgr)
	switch {
	case err != nil && err != oob.ErrChallengeExpired:
		slog.Warn("full-access: challenge wait failed",
			"component", "handler", "challengeID", c.ID, "error", err)
		logSendError(SendTextReply(ctx, client, chatID,
			fmt.Sprintf("Full-access grant aborted: %v", err)))
		return
	case err == oob.ErrChallengeExpired:
		logSendError(SendTextReply(ctx, client, chatID,
			fmt.Sprintf("Full-access grant expired without approval (challenge %s).", c.ID)))
		return
	case !approved:
		logSendError(SendTextReply(ctx, client, chatID, "Full-access grant denied."))
		return
	}
	mgr.GrantFullAccess(dur)
	expiry := mgr.FullAccessExpiresAt()
	logSendError(SendTextReply(ctx, client, chatID,
		fmt.Sprintf("Full-access granted until %s.",
			expiry.Format(time.RFC3339))))
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

// parseGrantDuration parses an optional duration argument. Empty
// input yields fullAccessDefaultGrant; oversized inputs are clamped
// at fullAccessMaxGrant and returned with no error so operators do
// not have to guess the cap.
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
