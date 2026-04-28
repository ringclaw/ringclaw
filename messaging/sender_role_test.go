package messaging

import (
	"testing"

	"github.com/ringclaw/ringclaw/ringcentral"
)

func TestOriginForPost_SourceUserIDOwner(t *testing.T) {
	h := &Handler{
		trustedSenders: map[string]bool{"u-owner": true},
	}
	post := ringcentral.Post{GroupID: "group-1", CreatorID: "u-owner"}
	origin := h.originForPost(nil, post)
	if !origin.IsOwner {
		t.Errorf("expected owner for source_user_ids match, got %+v", origin)
	}
	if origin.Reason != "source_user_ids" {
		t.Errorf("expected reason=source_user_ids, got %q", origin.Reason)
	}
	if origin.SenderID != "u-owner" {
		t.Errorf("expected senderID=u-owner, got %q", origin.SenderID)
	}
}

func TestOriginForPost_NonOwnerWithChatUserAllow(t *testing.T) {
	h := &Handler{
		trustedSenders: map[string]bool{},
		chatUserAllow:  map[string]map[string]bool{"group-1": {"u-friend": true}},
	}
	post := ringcentral.Post{GroupID: "group-1", CreatorID: "u-friend"}
	origin := h.originForPost(nil, post)
	if origin.IsOwner {
		t.Errorf("chat_user_allow user must NOT be owner, got %+v", origin)
	}
	if origin.Reason != "chat_user_allow" {
		t.Errorf("expected reason=chat_user_allow, got %q", origin.Reason)
	}
}

func TestOriginForPost_NonOwnerStranger(t *testing.T) {
	h := &Handler{
		trustedSenders: map[string]bool{},
	}
	post := ringcentral.Post{GroupID: "group-1", CreatorID: "stranger"}
	origin := h.originForPost(nil, post)
	if origin.IsOwner {
		t.Errorf("stranger must NOT be owner, got %+v", origin)
	}
	if origin.Reason != "non_owner" {
		t.Errorf("expected reason=non_owner, got %q", origin.Reason)
	}
}

func TestOriginForPost_DMIsOwner(t *testing.T) {
	// Use a real bot client so IsBotDM returns true.
	bot := ringcentral.NewBotClient("https://example.com", "token")
	bot.SetDMChatID("dm-1")
	h := &Handler{
		trustedSenders: map[string]bool{}, // empty: stranger in source_user_ids
	}
	post := ringcentral.Post{GroupID: "dm-1", CreatorID: "stranger"}
	origin := h.originForPost(bot, post)
	if !origin.IsOwner {
		t.Errorf("DM-to-bot must be owner regardless of allowlist, got %+v", origin)
	}
	if origin.Reason != "dm_to_bot" {
		t.Errorf("expected reason=dm_to_bot, got %q", origin.Reason)
	}
}

func TestOriginForPost_EmptyCreator(t *testing.T) {
	h := &Handler{trustedSenders: map[string]bool{}}
	post := ringcentral.Post{GroupID: "group-1", CreatorID: ""}
	origin := h.originForPost(nil, post)
	if origin.IsOwner {
		t.Errorf("empty creator must not be owner, got %+v", origin)
	}
}
