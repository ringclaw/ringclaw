package config

import "testing"

func TestRCConfig_IsAuthorizeMentionEnabled(t *testing.T) {
	t.Run("nil pointer defaults to disabled", func(t *testing.T) {
		var rc RCConfig
		if rc.IsAuthorizeMentionEnabled() {
			t.Fatalf("zero-value RCConfig must report disabled")
		}
	})
	t.Run("explicit false", func(t *testing.T) {
		f := false
		rc := RCConfig{AllowGroupMentionAuthorize: &f}
		if rc.IsAuthorizeMentionEnabled() {
			t.Fatalf("AllowGroupMentionAuthorize=false must report disabled")
		}
	})
	t.Run("explicit true", func(t *testing.T) {
		tr := true
		rc := RCConfig{AllowGroupMentionAuthorize: &tr}
		if !rc.IsAuthorizeMentionEnabled() {
			t.Fatalf("AllowGroupMentionAuthorize=true must report enabled")
		}
	})
}

func TestRCConfig_AddChatUserAllow(t *testing.T) {
	t.Run("rejects empty chat or identifier", func(t *testing.T) {
		var rc RCConfig
		if rc.AddChatUserAllow("", "alice@example.com") {
			t.Fatalf("empty chatID must return false")
		}
		if rc.AddChatUserAllow("c1", "  ") {
			t.Fatalf("blank identifier must return false")
		}
		if rc.ChatUserAllow != nil {
			t.Fatalf("rejected calls must not initialize the map: %#v", rc.ChatUserAllow)
		}
	})

	t.Run("lazy map init + first insertion", func(t *testing.T) {
		var rc RCConfig
		if !rc.AddChatUserAllow("c1", "alice@example.com") {
			t.Fatalf("first insertion must return true")
		}
		if got := rc.ChatUserAllow["c1"]; len(got) != 1 || got[0] != "alice@example.com" {
			t.Fatalf("unexpected map contents: %#v", rc.ChatUserAllow)
		}
	})

	t.Run("dedupe is case-insensitive", func(t *testing.T) {
		var rc RCConfig
		rc.AddChatUserAllow("c1", "Alice@Example.COM")
		if rc.AddChatUserAllow("c1", "alice@example.com") {
			t.Fatalf("duplicate (case-insensitive) must return false")
		}
		if got := rc.ChatUserAllow["c1"]; len(got) != 1 {
			t.Fatalf("dedupe must preserve original entry only: %#v", got)
		}
	})

	t.Run("isolates entries by chat", func(t *testing.T) {
		var rc RCConfig
		rc.AddChatUserAllow("c1", "alice@example.com")
		if !rc.AddChatUserAllow("c2", "alice@example.com") {
			t.Fatalf("same identifier in different chat must return true")
		}
		if len(rc.ChatUserAllow) != 2 {
			t.Fatalf("expected 2 chat keys, got %d", len(rc.ChatUserAllow))
		}
	})

	t.Run("trims whitespace", func(t *testing.T) {
		var rc RCConfig
		if !rc.AddChatUserAllow("  c1  ", "  alice@example.com  ") {
			t.Fatalf("trimmed insertion must succeed")
		}
		if got := rc.ChatUserAllow["c1"]; len(got) != 1 || got[0] != "alice@example.com" {
			t.Fatalf("trim must strip whitespace: %#v", got)
		}
	})
}
