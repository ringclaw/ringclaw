package config

import "testing"

func TestRCConfig_IsReplyInThread(t *testing.T) {
	t.Run("nil defaults to disabled", func(t *testing.T) {
		var rc RCConfig
		if rc.IsReplyInThread() {
			t.Fatal("zero-value must report disabled")
		}
	})
	t.Run("explicit false disables", func(t *testing.T) {
		f := false
		rc := RCConfig{ReplyInThread: &f}
		if rc.IsReplyInThread() {
			t.Fatal("ReplyInThread=false must report disabled")
		}
	})
	t.Run("explicit true enables", func(t *testing.T) {
		tr := true
		rc := RCConfig{ReplyInThread: &tr}
		if !rc.IsReplyInThread() {
			t.Fatal("ReplyInThread=true must report enabled")
		}
	})
}
