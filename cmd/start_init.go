package cmd

import (
	"context"
	"log/slog"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/api"
	"github.com/ringclaw/ringclaw/config"
	"github.com/ringclaw/ringclaw/messaging"
	"github.com/ringclaw/ringclaw/messaging/heartbeat"
	"github.com/ringclaw/ringclaw/messaging/oob"
	"github.com/ringclaw/ringclaw/messaging/persona"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// clients holds the initialized RingCentral clients.
type clients struct {
	bot     *ringcentral.Client
	private *ringcentral.Client
}

// lookupClient returns the preferred client for directory lookups
// (resolving emails / phone numbers to numeric IDs, fetching person
// info, etc.): the Private App client when configured, otherwise
// the bot client. Centralizes the "private if available, else bot"
// pattern that initSenders / initChatUserAllow / API server bootstrap
// all repeated inline.
func (c *clients) lookupClient() *ringcentral.Client {
	if c == nil {
		return nil
	}
	if c.private != nil {
		return c.private
	}
	return c.bot
}

// initClients creates bot and optional private app clients.
func initClients(ctx context.Context, cfg *config.Config) (*clients, error) {
	slog.Info("initializing bot client...")
	botClient := ringcentral.NewBotClient(cfg.RC.ServerURL, cfg.RC.BotToken)
	botOwnerID, err := botClient.GetExtensionInfo(ctx)
	if err != nil {
		slog.Warn("failed to get bot extension info", "error", err)
	} else {
		botClient.SetOwnerID(botOwnerID)
		slog.Info("bot extension ID resolved", "botOwnerID", botOwnerID)
	}

	slog.Info("initializing private app client...")
	creds := &ringcentral.Credentials{
		ClientID:     cfg.RC.ClientID,
		ClientSecret: cfg.RC.ClientSecret,
		JWTToken:     cfg.RC.JWTToken,
		ServerURL:    cfg.RC.ServerURL,
	}
	privateClient := ringcentral.NewClient(creds)
	if err := privateClient.Authenticate(); err != nil {
		return nil, err
	}
	slog.Info("private app authentication successful")
	ownerID, err := privateClient.GetExtensionInfo(ctx)
	if err != nil {
		return nil, err
	}
	privateClient.SetOwnerID(ownerID)
	slog.Info("private app owner ID resolved", "ownerID", ownerID)

	// Discover bot DM chat
	if privateClient.OwnerID() != "" {
		dmChatID, err := botClient.FindDirectChat(ctx, privateClient.OwnerID())
		if err != nil {
			slog.Warn("failed to find bot DM chat with installer", "error", err)
		} else {
			botClient.SetDMChatID(dmChatID)
			slog.Info("bot DM chat resolved", "chatID", dmChatID)
		}
	}

	return &clients{bot: botClient, private: privateClient}, nil
}

// initHandler creates the message handler with agent metas and aliases.
func initHandler(ctx context.Context, cfg *config.Config) *messaging.Handler {
	handler := messaging.NewHandler(
		func(ctx context.Context, name string) agent.Agent {
			return agent.Create(ctx, cfg, name)
		},
		func(name string) error {
			cfg.DefaultAgent = name
			return config.Save(cfg)
		},
		FullVersion(),
	)
	if ns := cfg.Bot.EffectiveConversationNamespace(); ns != "" {
		handler.SetConversationNamespace(ns)
		slog.Info("agent conversation namespace configured", "component", "start", "namespace", ns)
	}

	// Populate agent metas for /status
	var metas []messaging.AgentMeta
	for name, agCfg := range cfg.Agents {
		command := agCfg.Command
		if agCfg.Type == "http" {
			command = agCfg.Endpoint
		}
		metas = append(metas, messaging.AgentMeta{
			Name:    name,
			Type:    agCfg.Type,
			Command: command,
			Model:   agCfg.Model,
		})
	}
	handler.SetAgentMetas(metas)

	// Load custom aliases
	customAliases := config.BuildAliasMap(cfg.Agents)
	if len(customAliases) > 0 {
		handler.SetCustomAliases(customAliases)
		checkAliasConflicts(cfg, customAliases)
	}
	handler.SetGroupSummaryConfig(cfg.RC.GroupSummaryGroup(), cfg.RC.GroupSummaryLimit())

	// Persona + memory banner: a single Loader feeds every dispatch so
	// switching agents or resetting sessions keeps the operator's
	// SOUL.md and layered memory visible. Disabled by config? the
	// loader's Enabled() reports false and the handler silently emits
	// an empty banner.
	personaCfg := cfg.Persona.Resolved()
	if personaCfg.Enabled {
		store := persona.NewStore(personaCfg)
		// Best-effort template creation — never fatal; a missing soul
		// just means no persona injection until the operator edits
		// the file (or re-runs).
		if err := store.EnsureSoulTemplate(); err != nil {
			slog.Warn("persona: EnsureSoulTemplate failed", "component", "start", "error", err)
		}
		handler.SetPersonaLoader(persona.NewLoader(store))
		slog.Info("persona loader installed",
			"component", "start",
			"soul", personaCfg.SoulFile,
			"memoryDir", personaCfg.MemoryDir,
		)
	} else {
		slog.Info("persona disabled via config", "component", "start")
	}

	// Set up reload callback for /reload command
	handler.SetReloadAgents(func() ([]messaging.AgentMeta, map[string]string, []string) {
		before := make(map[string]bool, len(cfg.Agents))
		for name := range cfg.Agents {
			before[name] = true
		}

		config.DetectAndConfigure(cfg)
		_ = config.Save(cfg)

		var metas []messaging.AgentMeta
		var added []string
		for name, agCfg := range cfg.Agents {
			command := agCfg.Command
			if agCfg.Type == "http" {
				command = agCfg.Endpoint
			}
			metas = append(metas, messaging.AgentMeta{
				Name:    name,
				Type:    agCfg.Type,
				Command: command,
				Model:   agCfg.Model,
			})
			if !before[name] {
				added = append(added, name)
			}
		}
		aliases := config.BuildAliasMap(cfg.Agents)
		return metas, aliases, added
	})

	// Start default agent in background
	go func() {
		if cfg.DefaultAgent == "" {
			slog.Info("no default agent configured, staying in echo mode")
			return
		}
		slog.Info("initializing default agent in background", "agent", cfg.DefaultAgent)
		ag := agent.Create(ctx, cfg, cfg.DefaultAgent)
		if ag == nil {
			slog.Warn("failed to initialize default agent, staying in echo mode", "agent", cfg.DefaultAgent)
		} else {
			handler.SetDefaultAgent(cfg.DefaultAgent, ag)
		}
	}()

	return handler
}

// initServices starts the API server, cron scheduler, and heartbeat runner.
func initServices(ctx context.Context, cfg *config.Config, c *clients, handler *messaging.Handler, oobMgr *oob.Manager) {
	defaultChatID := ""
	if len(cfg.RC.ChatIDs) > 0 {
		defaultChatID = cfg.RC.ChatIDs[0]
	}

	// HTTP API server
	apiAddr := cfg.APIAddr
	if apiAddrFlag != "" {
		apiAddr = apiAddrFlag
	}
	apiClient := c.lookupClient()
	apiToken, err := api.LoadOrCreateToken()
	if err != nil {
		slog.Warn("failed to load API token, API will be unauthenticated", "component", "api", "error", err)
	}
	apiServer, err := api.NewServer(apiClient, apiAddr, defaultChatID, apiToken, oobMgr)
	if err != nil {
		slog.Error("failed to create API server", "error", err)
		return
	}
	go func() {
		if err := apiServer.Run(ctx); err != nil {
			slog.Error("API server error", "error", err)
		}
	}()

	// Cron scheduler
	cronStorePath, _ := messaging.DefaultCronStorePath()
	cronStore := messaging.NewCronStore(cronStorePath)
	if err := cronStore.Load(); err != nil {
		slog.Warn("failed to load cron jobs", "error", err)
	}
	handler.SetCronStore(cronStore)

	cronScheduler := messaging.NewCronScheduler(cronStore, c.bot, defaultChatID, func(name string) agent.Agent {
		if name == "" {
			return handler.GetDefaultAgent()
		}
		ag, _ := handler.GetAgent(ctx, name)
		return ag
	})
	go cronScheduler.Start(ctx)

	// Heartbeat runner
	if cfg.Heartbeat.Enabled {
		sendFn := func(ctx context.Context, chatID, text string) error {
			return messaging.SendTextReply(ctx, c.bot, chatID, text)
		}
		hbRunner, err := heartbeat.NewHeartbeatRunner(cfg.Heartbeat, sendFn, defaultChatID, handler.GetDefaultAgent, messaging.HeartbeatPrompt)
		if err != nil {
			slog.Error("failed to start heartbeat runner", "error", err)
		} else {
			go hbRunner.Start(ctx)
		}
	}
}

// checkAliasConflicts warns about alias conflicts at startup.
func checkAliasConflicts(cfg *config.Config, aliases map[string]string) {
	reserved := map[string]bool{"status": true, "help": true, "new": true, "clear": true, "info": true}

	seen := make(map[string]string)
	for alias, agentName := range aliases {
		if reserved[alias] {
			slog.Warn("alias conflicts with reserved command", "component", "config", "alias", alias, "agent", agentName)
			continue
		}
		if _, ok := cfg.Agents[alias]; ok {
			slog.Warn("alias shadows agent name", "component", "config", "alias", alias, "agent", agentName)
		}
		if prev, dup := seen[alias]; dup {
			slog.Warn("duplicate alias across agents", "component", "config", "alias", alias, "agents", prev+","+agentName)
		}
		seen[alias] = agentName
	}
}
