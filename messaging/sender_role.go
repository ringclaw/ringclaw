package messaging

import (
	"context"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// originForPost computes the Origin metadata that should accompany an
// agent prompt produced from a RingCentral post.
//
// Two-state model (v0.4.3):
//
//   - IsOwner = true  → sender is on the trusted-senders allowlist
//     (which is itself sourced from `source_user_ids` plus the
//     resolved Private App owner). chat_user_allow / OOB-approved
//     users are NOT considered owners.
//   - IsOwner = false → restricted: agent layer will request a
//     read-only ACP mode AND fail-closed deny every fs/* +
//     terminal/* tool call from the resulting session.
//
// DM-to-bot is treated as IsOwner=true regardless of allowlist
// state. The DM channel is the trust boundary (see
// docs/security/index.md "DM is the trust boundary"); only the
// operator can write into the bot DM.
func (h *Handler) originForPost(client *ringcentral.Client, post ringcentral.Post) agent.Origin {
	creatorID := post.CreatorID
	if client != nil && client.IsBotDM(post.GroupID) {
		return agent.Origin{IsOwner: true, SenderID: creatorID, Reason: "dm_to_bot"}
	}
	if h.isTrustedSender(creatorID) {
		return agent.Origin{IsOwner: true, SenderID: creatorID, Reason: "source_user_ids"}
	}
	reason := "non_owner"
	if h.isChatUserAllowed(post.GroupID, creatorID) {
		reason = "chat_user_allow"
	}
	return agent.Origin{IsOwner: false, SenderID: creatorID, Reason: reason}
}

// withOriginForPost returns a derived context that carries the Origin
// for the given post, ready to be passed to agent.Chat / ChatWithImages /
// ChatWithAudio.
func (h *Handler) withOriginForPost(ctx context.Context, client *ringcentral.Client, post ringcentral.Post) context.Context {
	return agent.WithOrigin(ctx, h.originForPost(client, post))
}
