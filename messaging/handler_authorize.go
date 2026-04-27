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
// at debug level and replaced with the raw ID so the prompt never
// blocks on flaky network. readClient is preferred (Private App
// credentials yield the email field); when nil the prompt falls back
// to bare IDs.
func (h *Handler) collectAuthorizeMeta(ctx context.Context, readClient *ringcentral.Client, post ringcentral.Post) authorizeMeta {
	meta := authorizeMeta{}
	if readClient == nil {
		return meta
	}
	if chat, err := readClient.GetChat(ctx, post.GroupID); err == nil && chat != nil {
		name := strings.TrimSpace(chat.Name)
		if name == "" {
			name = strings.TrimSpace(chat.Type)
		}
		meta.ChatName = name
	} else if err != nil {
		slog.Debug("authorize-mention: chat lookup failed", "component", "handler", "chatID", post.GroupID, "error", err)
	}
	if p, err := readClient.GetPersonInfo(ctx, post.CreatorID); err == nil && p != nil {
		meta.Email = strings.TrimSpace(p.Email)
		meta.DisplayName = strings.TrimSpace(strings.TrimSpace(p.FirstName) + " " + strings.TrimSpace(p.LastName))
	} else if err != nil {
		slog.Debug("authorize-mention: person lookup failed", "component", "handler", "userID", post.CreatorID, "error", err)
	}
	return meta
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

	chatLabel := post.GroupID
	if meta.ChatName != "" {
		chatLabel = fmt.Sprintf("%s (id=%s)", meta.ChatName, post.GroupID)
	}

	userLabel := post.CreatorID
	switch {
	case meta.DisplayName != "" && meta.Email != "":
		userLabel = fmt.Sprintf("%s <%s> (id=%s)", meta.DisplayName, meta.Email, post.CreatorID)
	case meta.DisplayName != "":
		userLabel = fmt.Sprintf("%s (id=%s)", meta.DisplayName, post.CreatorID)
	case meta.Email != "":
		userLabel = fmt.Sprintf("%s (id=%s)", meta.Email, post.CreatorID)
	}

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
func (h *Handler) awaitAuthorizeMention(client *ringcentral.Client, c *oob.Challenge, chatID, userID, key string) {
	defer h.releasePending(key)

	mgr := h.OOBManager()
	if mgr == nil {
		h.dropAuthorizeMeta(c.ID)
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
		slog.Info("authorize-mention: challenge expired",
			"component", "handler", "challengeID", c.ID, "chatID", chatID, "userID", userID)
		h.dropAuthorizeMeta(c.ID)
		if ownerDM != "" {
			logSendError(SendTextReply(ctx, client, ownerDM,
				fmt.Sprintf("Authorization request for user `%s` in chat `%s` expired (challenge `%s`). They must @mention again to retry.",
					userID, chatID, c.ID)))
		}
	case err != nil:
		slog.Warn("authorize-mention: wait failed",
			"component", "handler", "challengeID", c.ID, "error", err)
		h.dropAuthorizeMeta(c.ID)
	case !approved:
		slog.Info("authorize-mention: denied",
			"component", "handler", "challengeID", c.ID, "chatID", chatID, "userID", userID)
		h.dropAuthorizeMeta(c.ID)
		if ownerDM != "" {
			logSendError(SendTextReply(ctx, client, ownerDM,
				fmt.Sprintf("Denied authorization for user `%s` in chat `%s` (challenge `%s`).", userID, chatID, c.ID)))
		}
	default:
		h.applyAuthorize(ctx, client, chatID, userID, c.ID)
	}
}

// applyAuthorize is the post-approval commit step: grants the user
// per-chat trust on both the handler (defense-in-depth) and the
// monitor (live WebSocket allowlist), then persists the grant to
// config.json under the human-friendly identifier (email preferred,
// numeric extension ID fallback). Always emits a confirmation in the
// owner DM so the operator gets unambiguous feedback.
func (h *Handler) applyAuthorize(ctx context.Context, client *ringcentral.Client, chatID, userID, challengeID string) {
	meta := h.takeAuthorizeMeta(challengeID)
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

// takeAuthorizeMeta returns and removes the cached metadata for a
// challenge. Returns the zero value if no metadata was stored.
func (h *Handler) takeAuthorizeMeta(challengeID string) authorizeMeta {
	h.mu.Lock()
	defer h.mu.Unlock()
	meta, ok := h.authorizeMeta[challengeID]
	if ok {
		delete(h.authorizeMeta, challengeID)
	}
	return meta
}

// dropAuthorizeMeta removes any cached metadata for a challenge
// without returning it. Called on non-approval paths.
func (h *Handler) dropAuthorizeMeta(challengeID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.authorizeMeta, challengeID)
}
