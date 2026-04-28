package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/internal/util"
	"github.com/ringclaw/ringclaw/messaging/oob"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// authorizeMentionTTLLabel is the human-readable scope shown in the
// owner DM prompt. It is intentionally a plain string (not a
// time.Duration) because the scope here is "this chat only", not a
// time bound — the OOB challenge itself has a 5-minute TTL handled by
// the manager.
const authorizeMentionTTLLabel = "this chat only"

// authorizeCooldownTTL is the silence window after a deny / expire so
// a hostile or noisy non-trusted user cannot keep pushing /approval
// prompts to the owner DM by repeatedly @mentioning the bot. Var (not
// const) so tests can shrink it. Approve never writes the cooldown —
// the approved user becomes trusted via chat_user_allow and Monitor
// Layer 0 admits them before the AuthorizeMention path runs again.
var authorizeCooldownTTL = 24 * time.Hour

// PersistAuthorizeFunc persists a (chat, identifier) authorization
// grant to config.json. identifier is the human-friendly form (email
// preferred; numeric extension ID fallback) so the on-disk
// chat_user_allow list stays auditable.
//
// In-memory state on both Monitor and Handler has already been
// updated by the caller before this is invoked, so a Save failure is
// logged but does not block the user from being trusted in the
// current process. Operators relying on durable persistence should
// monitor the "authorize-mention: persist failed" log line.
type PersistAuthorizeFunc func(chatID, identifier string) error

// authorizeMonitorIface is the narrow surface the authorize-mention
// flow needs from the Monitor: pushing approved (chat, user) grants
// into the WebSocket allowlist. Defined here so messaging does not
// take a hard dependency on *ringcentral.Monitor's full surface.
type authorizeMonitorIface interface {
	AddChatUserAllow(chatID, userID string)
}

// authorizeMeta is the per-challenge metadata captured at prompt-time
// so the approval handler can persist the human-friendly identifier
// (email) without a second directory lookup that may transiently
// fail. Stored under the OOB challenge ID and dropped when the
// challenge resolves.
type authorizeMeta struct {
	Email       string
	DisplayName string
	ChatName    string
}

// SetAuthorizeMention installs the persist callback (used by
// cmd/start.go to write to config.json) and the monitor reference
// (used to push approved grants back into the WebSocket allowlist).
// Pass persist=nil to disable persistence; pass mon=nil to disable
// the runtime allowlist push (for tests). The hook itself
// (Monitor.SetMentionAuthorize) is wired separately in cmd/start.go.
func (h *Handler) SetAuthorizeMention(persist PersistAuthorizeFunc, mon authorizeMonitorIface) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.authorizePersist = persist
	h.authorizeMonitor = mon
}

// AuthorizeMention is the entry point invoked by the Monitor when a
// non-trusted user @mentions the bot in an allowed group chat. It
// validates preconditions, dedupes pending challenges per (chat,
// user), issues a fresh OOB challenge, posts a context-rich prompt
// to the owner DM, and drops the original post. The user must
// @mention again after approval — the original message is not
// replayed (per the v2 spec).
//
// Safe to call when OOB or owner DM is unconfigured; the call becomes
// a strict drop with a single audit log line so operators can see
// that the feature is disabled.
func (h *Handler) AuthorizeMention(ctx context.Context, client, readClient *ringcentral.Client, post ringcentral.Post) {
	mgr := h.OOBManager()
	ownerDM := h.OwnerDMChatID()
	if mgr == nil || ownerDM == "" {
		slog.Warn("authorize-mention: OOB or owner DM unconfigured; dropping",
			"component", "handler", "chatID", post.GroupID, "userID", post.CreatorID)
		return
	}
	if strings.TrimSpace(post.GroupID) == "" || strings.TrimSpace(post.CreatorID) == "" {
		return
	}

	// Defense-in-depth: never issue a self-challenge for the Private
	// App owner. Monitor's Layer 0 sender check already admits the
	// owner (auto-injected to the global trusted set), so reaching
	// this path with post.CreatorID == owner indicates a bug or a
	// hostile caller — fail closed instead of pushing a "user X
	// requesting authorization in chat Y" prompt to X themselves.
	if readClient != nil {
		if owner := strings.TrimSpace(readClient.OwnerID()); owner != "" && owner == strings.TrimSpace(post.CreatorID) {
			slog.Debug("authorize-mention: skipping self-challenge for owner",
				"component", "handler", "chatID", post.GroupID, "userID", post.CreatorID)
			return
		}
	}

	key := authorizePendingKey(post.GroupID, post.CreatorID)

	// Cooldown window from a recent deny / expire — drop silently so
	// a noisy non-trusted user cannot spam the owner DM with new
	// challenges by repeatedly @mentioning the bot. The window
	// resets on next approve (n/a here) or after authorizeCooldownTTL
	// has elapsed.
	if h.inAuthorizeCooldown(key) {
		slog.Debug("authorize-mention: in cooldown after recent deny/expire, dropping",
			"component", "handler",
			"chatID", post.GroupID, "userID", post.CreatorID,
			"cooldown", authorizeCooldownTTL)
		return
	}

	if !h.tryReservePending(key) {
		slog.Debug("authorize-mention: pending challenge already exists, dropping duplicate",
			"component", "handler", "chatID", post.GroupID, "userID", post.CreatorID)
		return
	}

	intent := fmt.Sprintf("authorize user %s in chat %s", post.CreatorID, post.GroupID)
	ownerID := ""
	if readClient != nil {
		ownerID = readClient.OwnerID()
	}
	c, err := mgr.Issue(post.CreatorID, intent, post.GroupID, ownerDM, oob.IssueOptions{
		TTL:     oob.DefaultChallengeTTL,
		OwnerID: ownerID,
	})
	if err != nil {
		slog.Error("authorize-mention: issue challenge failed",
			"component", "handler", "chatID", post.GroupID, "userID", post.CreatorID, "error", err)
		h.releasePending(key)
		return
	}

	meta := h.collectAuthorizeMeta(ctx, readClient, post)
	h.storeAuthorizeMeta(c.ID, meta)

	if err := h.postAuthorizeMentionPrompt(ctx, client, c, post, meta); err != nil {
		slog.Error("authorize-mention: post prompt failed",
			"component", "handler", "challengeID", c.ID, "error", err)
		mgr.Deny(c.ID)
		h.dropAuthorizeMeta(c.ID)
		h.releasePending(key)
		return
	}

	go h.awaitAuthorizeMention(client, c, post.GroupID, post.CreatorID, key)
}

// collectAuthorizeMeta does best-effort directory lookups for the
// chat name, user display name, and user email. Any failure is logged
// at debug level (inside lookupPerson / lookupChat) and replaced with
// the raw ID so the prompt never blocks on flaky network.
// readClient is preferred (Private App credentials yield the email
// field); when nil the prompt falls back to bare IDs.
func (h *Handler) collectAuthorizeMeta(ctx context.Context, readClient *ringcentral.Client, post ringcentral.Post) authorizeMeta {
	if readClient == nil {
		return authorizeMeta{}
	}
	name, email := lookupPerson(ctx, readClient, post.CreatorID)
	return authorizeMeta{
		ChatName:    lookupChat(ctx, readClient, post.GroupID),
		DisplayName: name,
		Email:       email,
	}
}

// postAuthorizeMentionPrompt formats and sends the rich context prompt
// to the owner DM. Failure is reported via the returned error; the
// caller releases the pending entry and denies the challenge in that
// case so a transient post failure does not park the (chat, user)
// pair as pending until TTL.
func (h *Handler) postAuthorizeMentionPrompt(
	ctx context.Context,
	client *ringcentral.Client,
	c *oob.Challenge,
	post ringcentral.Post,
	meta authorizeMeta,
) error {
	ownerDM := h.OwnerDMChatID()
	if ownerDM == "" {
		return fmt.Errorf("owner DM not resolved")
	}

	chatLabel := formatChatLabel(meta.ChatName, post.GroupID)
	userLabel := formatPersonLabel(meta.DisplayName, meta.Email, post.CreatorID)

	mention := util.Truncate(strings.TrimSpace(post.Text), 200)
	if mention == "" {
		mention = "(empty)"
	}
	expiresIn := time.Until(c.ExpiresAt).Round(time.Second)
	if expiresIn < 0 {
		expiresIn = 0
	}

	msg := fmt.Sprintf(
		"Pending authorization (challenge `%s`).\n"+
			"Chat: %s\n"+
			"User: %s\n"+
			"Mention: %s\n\n"+
			"Approve to grant **chat-scoped** access (this chat only, persisted to config.json).\n"+
			"Run on the host:\n"+
			"  ringclaw approval %s        (approve)\n"+
			"  ringclaw approval deny %s   (deny)\n\n"+
			"Expires in %s. Scope: %s. The user's original message is dropped — they must @mention again after approval.",
		c.ID, chatLabel, userLabel, mention, c.ID, c.ID, expiresIn, authorizeMentionTTLLabel,
	)
	return SendTextReply(ctx, client, ownerDM, msg)
}

// awaitAuthorizeMention blocks on the OOB challenge resolution and,
// on approval, applies the grant (in-memory + persistent) and
// notifies the owner DM. The (chat, user) pending lock is released
// on every code path via the deferred releasePending so the next
// @mention from the same user always either issues a new challenge
// (after deny/expire) or is dispatched normally (after approve).
//
// The cached prompt-time metadata is also released centrally via
// defer dropAuthorizeMeta so that any future early-return path (or a
// panic in applyAuthorize) cannot leak entries into h.authorizeMeta.
func (h *Handler) awaitAuthorizeMention(client *ringcentral.Client, c *oob.Challenge, chatID, userID, key string) {
	defer h.releasePending(key)
	defer h.dropAuthorizeMeta(c.ID)

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
	ownerDM := h.OwnerDMChatID()
	switch {
	case err == oob.ErrChallengeExpired:
		// Record cooldown so a re-mention while owner is away does
		// not immediately re-issue another challenge.
		h.recordAuthorizeCooldown(key)
		slog.Info("authorize-mention: challenge expired",
			"component", "handler", "challengeID", c.ID, "chatID", chatID, "userID", userID,
			"cooldown", authorizeCooldownTTL)
		if ownerDM != "" {
			logSendError(SendTextReply(ctx, client, ownerDM,
				fmt.Sprintf("Authorization request for user `%s` in chat `%s` expired (challenge `%s`). New requests from this user in this chat are silenced for %s.",
					userID, chatID, c.ID, authorizeCooldownTTL)))
		}
	case err != nil:
		// Transient wait error (e.g. context canceled): do NOT write
		// cooldown so the owner gets another shot once whatever
		// disrupted the wait clears.
		slog.Warn("authorize-mention: wait failed",
			"component", "handler", "challengeID", c.ID, "error", err)
	case !approved:
		// Explicit deny: enter cooldown so the same user cannot
		// re-trigger a fresh prompt by @mentioning again.
		h.recordAuthorizeCooldown(key)
		slog.Info("authorize-mention: denied",
			"component", "handler", "challengeID", c.ID, "chatID", chatID, "userID", userID,
			"cooldown", authorizeCooldownTTL)
		if ownerDM != "" {
			logSendError(SendTextReply(ctx, client, ownerDM,
				fmt.Sprintf("Denied authorization for user `%s` in chat `%s` (challenge `%s`). New requests from this user in this chat are silenced for %s.",
					userID, chatID, c.ID, authorizeCooldownTTL)))
		}
	default:
		// Approved: belt-and-suspenders clear of any historical
		// cooldown entry. The user becomes trusted via
		// chat_user_allow so Monitor Layer 0 will short-circuit
		// future mentions before they reach this path anyway.
		h.clearAuthorizeCooldown(key)
		// Pass the meta in directly: the deferred dropAuthorizeMeta
		// would otherwise race with takeAuthorizeMeta inside
		// applyAuthorize and lose the cached email.
		h.applyAuthorize(ctx, client, chatID, userID, c.ID, h.peekAuthorizeMeta(c.ID))
	}
}

// applyAuthorize is the post-approval commit step: grants the user
// per-chat trust on both the handler (defense-in-depth) and the
// monitor (live WebSocket allowlist), then persists the grant to
// config.json under the human-friendly identifier (email preferred,
// numeric extension ID fallback). Always emits a confirmation in the
// owner DM so the operator gets unambiguous feedback.
//
// meta is the cached prompt-time metadata supplied by
// awaitAuthorizeMention; the cache itself is freed by the caller's
// deferred dropAuthorizeMeta so this function never mutates the map.
func (h *Handler) applyAuthorize(ctx context.Context, client *ringcentral.Client, chatID, userID, challengeID string, meta authorizeMeta) {
	identifier := userID
	if meta.Email != "" {
		identifier = meta.Email
	} else {
		slog.Warn("authorize-mention: no email available, persisting numeric ID",
			"component", "handler", "userID", userID, "chatID", chatID)
	}

	h.AddChatUserAllow(chatID, userID)
	h.mu.RLock()
	mon := h.authorizeMonitor
	persist := h.authorizePersist
	h.mu.RUnlock()
	if mon != nil {
		mon.AddChatUserAllow(chatID, userID)
	}
	if persist != nil {
		if err := persist(chatID, identifier); err != nil {
			slog.Error("authorize-mention: persist failed",
				"component", "handler", "chatID", chatID, "identifier", identifier, "error", err)
		}
	}

	slog.Info("authorize-mention: granted",
		"component", "handler", "challengeID", challengeID,
		"chatID", chatID, "userID", userID, "identifier", identifier)

	if dm := h.OwnerDMChatID(); dm != "" {
		logSendError(SendTextReply(ctx, client, dm,
			fmt.Sprintf("Authorized `%s` in chat `%s`. Saved to chat_user_allow (challenge `%s`).",
				identifier, chatID, challengeID)))
	}
}

// authorizePendingKey is the dedupe key for an outstanding
// authorize-mention challenge, scoped to (chat, user).
func authorizePendingKey(chatID, userID string) string {
	return chatID + "|" + userID
}

// tryReservePending claims the pending slot for a (chat, user) key.
// Returns false when a challenge is already pending; in that case
// the caller MUST drop the post without issuing a new challenge.
func (h *Handler) tryReservePending(key string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pendingAuthorize == nil {
		h.pendingAuthorize = make(map[string]bool)
	}
	if h.pendingAuthorize[key] {
		return false
	}
	h.pendingAuthorize[key] = true
	return true
}

// releasePending clears the pending slot for a (chat, user) key.
// Always called via defer in awaitAuthorizeMention so any resolution
// path (approve / deny / expire / panic) frees the lock.
func (h *Handler) releasePending(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.pendingAuthorize, key)
}

// storeAuthorizeMeta caches the prompt-time metadata so the approval
// handler can persist the email without a second directory lookup.
func (h *Handler) storeAuthorizeMeta(challengeID string, meta authorizeMeta) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.authorizeMeta == nil {
		h.authorizeMeta = make(map[string]authorizeMeta)
	}
	h.authorizeMeta[challengeID] = meta
}

// peekAuthorizeMeta returns the cached metadata for a challenge
// without removing it. The release happens centrally via the
// deferred dropAuthorizeMeta in awaitAuthorizeMention so all
// resolution paths free the entry. Returns the zero value if no
// metadata was stored.
func (h *Handler) peekAuthorizeMeta(challengeID string) authorizeMeta {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.authorizeMeta[challengeID]
}

// dropAuthorizeMeta removes any cached metadata for a challenge
// without returning it. Used by awaitAuthorizeMention's defer to
// guarantee no entry leaks past challenge resolution, even on
// approve / deny / expire / panic.
func (h *Handler) dropAuthorizeMeta(challengeID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.authorizeMeta, challengeID)
}

// inAuthorizeCooldown reports whether the (chat, user) key is still
// inside the silence window from a recent deny / expire. Stale
// entries are GC'd lazily here to avoid an extra goroutine — the
// total entry count is bounded by the number of distinct
// (chat, non-trusted-user) pairs the bot has ever interacted with
// since process start, which is small in practice.
func (h *Handler) inAuthorizeCooldown(key string) bool {
	h.mu.RLock()
	t, ok := h.authorizeCooldown[key]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Since(t) >= authorizeCooldownTTL {
		h.mu.Lock()
		// Re-check under the write lock in case another goroutine
		// raced and refreshed the entry between our RLock and Lock.
		if t2, ok2 := h.authorizeCooldown[key]; ok2 && time.Since(t2) >= authorizeCooldownTTL {
			delete(h.authorizeCooldown, key)
		}
		h.mu.Unlock()
		return false
	}
	return true
}

// recordAuthorizeCooldown stamps the resolution time for a
// (chat, user) key. Called on deny and expire only — approve never
// records here because the approved user becomes trusted via
// chat_user_allow and Monitor Layer 0 short-circuits future mentions.
func (h *Handler) recordAuthorizeCooldown(key string) {
	h.mu.Lock()
	if h.authorizeCooldown == nil {
		h.authorizeCooldown = make(map[string]time.Time)
	}
	h.authorizeCooldown[key] = time.Now()
	h.mu.Unlock()
}

// clearAuthorizeCooldown removes any historical cooldown entry for a
// (chat, user) key. Belt-and-suspenders called on approve so a
// previously denied user who is later allow-listed by hand and then
// revoked again does not inherit a stale cooldown.
func (h *Handler) clearAuthorizeCooldown(key string) {
	h.mu.Lock()
	delete(h.authorizeCooldown, key)
	h.mu.Unlock()
}
