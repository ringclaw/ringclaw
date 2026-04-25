package cmd

import (
	"context"
	"log/slog"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/messaging"
	"github.com/ringclaw/ringclaw/messaging/oob"
)

// initOOBManager constructs the in-memory OOB manager and wires it to
// both the message handler (for the `/approval` reply router and
// `/full-access` command) and the ACP agent layer:
//
//   - FullAccessActive is polled by the agent on every new ACP session
//     so an active grant flips the session into `set_mode
//     "full-access"`.
//   - SetFullAccessRevokeHook demotes every LIVE ACP session back to
//     the default mode the moment /full-access revoke runs or the TTL
//     expires, so existing sessions cannot linger in full-access after
//     the grant is gone.
//
// onReady, if non-nil, is called with the constructed manager so the
// caller can pass it to other subsystems (e.g. the API server).
func initOOBManager(handler *messaging.Handler, c *clients, onReady func(*oob.Manager)) error {
	mgr := oob.New(oob.Options{})

	dmChat := ""
	if c != nil && c.bot != nil {
		dmChat = botDMChatID(c)
	}
	handler.SetOOBManager(mgr, dmChat)
	agent.SetFullAccessGrantSource(mgr.FullAccessActive)
	mgr.SetFullAccessRevokeHook(func() {
		agent.DemoteAllACPFullAccess(context.Background())
	})
	if onReady != nil {
		onReady(mgr)
	}
	if dmChat == "" {
		slog.Warn("bot DM chat with owner not resolved; /full-access and cross-chat notifications will be disabled until it is",
			"component", "start")
	} else {
		slog.Info("OOB approval flow active",
			"component", "start",
			"ownerDMChatID", dmChat,
			"mode", "terminal:/approval",
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
