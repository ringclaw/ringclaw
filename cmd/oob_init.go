package cmd

import (
	"log/slog"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/messaging"
	"github.com/ringclaw/ringclaw/messaging/oob"
)

// initOOBManager constructs the in-memory OOB manager used by Phase 2b
// and wires it to both the message handler (for the `/approval` reply
// router and `/full-access` command) and the ACP agent layer (so a
// live full-access grant flips new sessions into `set_mode
// "full-access"` until the grant expires).
//
// Phase 2b dropped the on-disk PIN file, bcrypt dependency and the
// one-time PIN stderr announcement. Owner identity is established by
// the bot DM itself plus the trusted-sender allowlist, not by a shared
// secret.
func initOOBManager(handler *messaging.Handler, c *clients) error {
	mgr := oob.New(oob.Options{})

	dmChat := ""
	if c != nil && c.bot != nil {
		dmChat = botDMChatID(c)
	}
	handler.SetOOBManager(mgr, dmChat)
	agent.SetFullAccessGrantSource(mgr.FullAccessActive)
	if dmChat == "" {
		slog.Warn("bot DM chat with owner not resolved; /full-access and cross-chat notifications will be disabled until it is",
			"component", "start")
	} else {
		slog.Info("OOB approval flow active",
			"component", "start",
			"ownerDMChatID", dmChat,
			"mode", "phase2b:/approval",
		)
	}
	return nil
}

// botDMChatID returns the bot's DM chat ID with the trusted owner, or
// empty if it has not been resolved yet.
func botDMChatID(c *clients) string {
	if c == nil || c.bot == nil || c.private == nil {
		return ""
	}
	return c.bot.DMChatID()
}
