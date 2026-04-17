package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/messaging"
	"github.com/ringclaw/ringclaw/messaging/oob"
)

// initOOBManager loads (or creates) the on-disk PIN file under
// ~/.ringclaw and installs the resulting Manager on the handler so the
// Phase 2 PIN-gated approval flow is active. The bot DM chat ID is
// pulled off the bot client; if it is unavailable the manager is still
// created (so /full-access PIN flows can be wired later) but the
// handler is told to disable OOB by passing an empty owner DM.
//
// A freshly generated PIN is printed to the local terminal exactly
// once. We deliberately bypass slog for the print so the secret does
// not end up in JSON logs that may be shipped off-host.
func initOOBManager(handler *messaging.Handler, c *clients) error {
	dir := ringclawDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	mgr, newPIN, err := oob.Load(oob.LoadOptions{Dir: dir})
	if err != nil {
		return err
	}
	if newPIN != "" {
		announceFreshPIN(newPIN, filepath.Join(dir, oob.PinFileName))
	} else {
		slog.Info("OOB approval PIN loaded",
			"component", "start", "path", filepath.Join(dir, oob.PinFileName), "createdAt", mgr.CreatedAt())
	}

	dmChat := ""
	if c != nil && c.bot != nil {
		// Best-effort: if the bot DM chat is known we can post approval
		// cards there. If not, the handler keeps OOB installed but the
		// gate falls back to the Phase 1 warn-log behavior.
		dmChat = botDMChatID(c)
	}
	handler.SetOOBManager(mgr, dmChat)
	// Wire the dynamic full-access source so /full-access TTL grants
	// flip ACP sessions into full-access mode without needing to
	// re-instantiate any agents. The check is consulted on every new
	// ACP session (see agent.ACPAgent.getOrCreateSession), so an
	// expired grant naturally drops back to the default safe mode.
	agent.SetFullAccessGrantSource(mgr.FullAccessActive)
	if dmChat == "" {
		slog.Warn("bot DM chat with owner not resolved; OOB approval cards cannot be delivered. Cross-chat ACTION dispatches will fall back to Phase 1 warn-log behavior",
			"component", "start")
	} else {
		slog.Info("OOB approval flow active", "component", "start", "ownerDMChatID", dmChat)
	}
	return nil
}

// botDMChatID returns the bot's DM chat ID with the trusted owner, or
// empty if it has not been resolved yet. We re-derive it via IsBotDM
// instead of exposing a getter to keep the ringcentral.Client surface
// minimal.
func botDMChatID(c *clients) string {
	if c == nil || c.bot == nil || c.private == nil {
		return ""
	}
	// Probe with the owner ID — IsBotDM only cares whether the chat is
	// the bot's DM, but the chat ID itself is held internally on the
	// bot client. Use a tiny helper to extract it via reflection-free
	// API: we know SetDMChatID was called in initClients and stored on
	// the client. Read it back by asking IsBotDM with the value of
	// dmChatID; since we don't have direct access we expose a read by
	// delegation through a public method.
	return c.bot.DMChatID()
}

// announceFreshPIN prints the freshly generated PIN to the local TTY
// exactly once. Using fmt.Fprintln ensures the secret does not pass
// through structured logs that may be aggregated off-host.
func announceFreshPIN(pin, path string) {
	banner := "================ RingClaw OOB approval PIN ================"
	fmt.Fprintln(os.Stderr, banner)
	fmt.Fprintln(os.Stderr, "  PIN:", pin)
	fmt.Fprintln(os.Stderr, "  Hash file:", path, "(mode 0600, bcrypt)")
	fmt.Fprintln(os.Stderr, "  This PIN is shown ONCE. Record it now.")
	fmt.Fprintln(os.Stderr, "  Phase 2 high-risk actions (cross-chat MESSAGE/CARD,")
	fmt.Fprintln(os.Stderr, "  /full-access) will require you to type this PIN")
	fmt.Fprintln(os.Stderr, "  back into the bot DM to approve.")
	fmt.Fprintln(os.Stderr, banner)
}
