package cmd

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/api"
	"github.com/ringclaw/ringclaw/config"
	"github.com/ringclaw/ringclaw/messaging"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// clients holds the initialized RingCentral clients.
type clients struct {
	bot     *ringcentral.Client
	private *ringcentral.Client
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

	var privateClient *ringcentral.Client
	if cfg.RC.HasPrivateApp() {
		slog.Info("initializing private app client...")
		creds := &ringcentral.Credentials{
			ClientID:     cfg.RC.ClientID,
			ClientSecret: cfg.RC.ClientSecret,
			JWTToken:     cfg.RC.JWTToken,
			ServerURL:    cfg.RC.ServerURL,
		}
		privateClient = ringcentral.NewClient(creds)
		if err := privateClient.Authenticate(); err != nil {
			slog.Error("private app authentication failed, continuing without it", "error", err)
			privateClient = nil
		} else {
			slog.Info("private app authentication successful")
			ownerID, err := privateClient.GetExtensionInfo(ctx)
			if err != nil {
				slog.Warn("failed to get private app extension info", "error", err)
			} else {
				privateClient.SetOwnerID(ownerID)
				slog.Info("private app owner ID resolved", "ownerID", ownerID)
			}
		}
	} else {
		slog.Warn("no private app configured — the following features require a Private App and will be unavailable: summarize, directory search, name resolution in ACTION blocks, email-based source_user_ids")
		for _, id := range cfg.RC.SourceUserIDs {
			if strings.Contains(id, "@") {
				slog.Error("source_user_ids contains an email address but Private App is not configured for directory lookup — this entry will be ignored", "email", id)
			}
		}
	}

	// Discover bot DM chat
	if privateClient != nil && privateClient.OwnerID() != "" {
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
			return createAgentByName(ctx, cfg, name)
		},
		func(name string) error {
			cfg.DefaultAgent = name
			return config.Save(cfg)
		},
		FullVersion(),
	)

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

	// Start default agent in background
	go func() {
		if cfg.DefaultAgent == "" {
			slog.Info("no default agent configured, staying in echo mode")
			return
		}
		slog.Info("initializing default agent in background", "agent", cfg.DefaultAgent)
		ag := createAgentByName(ctx, cfg, cfg.DefaultAgent)
		if ag == nil {
			slog.Warn("failed to initialize default agent, staying in echo mode", "agent", cfg.DefaultAgent)
		} else {
			handler.SetDefaultAgent(cfg.DefaultAgent, ag)
		}
	}()

	return handler
}

// initServices starts the API server, cron scheduler, and heartbeat runner.
func initServices(ctx context.Context, cfg *config.Config, c *clients, handler *messaging.Handler) {
	defaultChatID := ""
	if len(cfg.RC.ChatIDs) > 0 {
		defaultChatID = cfg.RC.ChatIDs[0]
	}

	// HTTP API server
	apiAddr := cfg.APIAddr
	if apiAddrFlag != "" {
		apiAddr = apiAddrFlag
	}
	apiClient := c.bot
	if c.private != nil {
		apiClient = c.private
	}
	apiServer := api.NewServer(apiClient, apiAddr, defaultChatID)
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
		hbRunner, err := messaging.NewHeartbeatRunner(cfg.Heartbeat, c.bot, defaultChatID, handler.GetDefaultAgent)
		if err != nil {
			slog.Error("failed to start heartbeat runner", "error", err)
		} else {
			go hbRunner.Start(ctx)
		}
	}
}

// createAgentByName creates and starts an agent by its config name.
func createAgentByName(ctx context.Context, cfg *config.Config, name string) agent.Agent {
	agCfg, ok := cfg.Agents[name]
	if !ok {
		slog.Warn("agent not found in config", "component", "agent", "name", name)
		return nil
	}

	cwd := agentWorkspace(cfg, agCfg)

	switch agCfg.Type {
	case "acp":
		ag := agent.NewACPAgent(agent.ACPAgentConfig{
			Command:      agCfg.Command,
			Args:         agCfg.Args,
			Cwd:          cwd,
			Env:          agCfg.Env,
			Model:        agCfg.Model,
			SystemPrompt: agCfg.SystemPrompt,
		})
		if err := ag.Start(ctx); err != nil {
			slog.Error("failed to start ACP agent", "component", "agent", "name", name, "error", err)
			return nil
		}
		slog.Info("started ACP agent", "component", "agent", "name", name, "command", agCfg.Command, "type", agCfg.Type, "model", agCfg.Model)
		return ag
	case "cli":
		ag := agent.NewCLIAgent(agent.CLIAgentConfig{
			Name:         name,
			Command:      agCfg.Command,
			Args:         agCfg.Args,
			Cwd:          cwd,
			Env:          agCfg.Env,
			Model:        agCfg.Model,
			SystemPrompt: agCfg.SystemPrompt,
		})
		slog.Info("created CLI agent", "component", "agent", "name", name, "command", agCfg.Command, "type", agCfg.Type, "model", agCfg.Model)
		return ag
	case "http":
		if agCfg.Endpoint == "" {
			slog.Warn("HTTP agent has no endpoint", "component", "agent", "name", name)
			return nil
		}
		var timeout time.Duration
		if agCfg.Timeout > 0 {
			timeout = time.Duration(agCfg.Timeout) * time.Second
		}
		ag := agent.NewHTTPAgent(agent.HTTPAgentConfig{
			Name:         name,
			Endpoint:     agCfg.Endpoint,
			APIKey:       agCfg.APIKey,
			Headers:      agCfg.Headers,
			Model:        agCfg.Model,
			SystemPrompt: agCfg.SystemPrompt,
			MaxHistory:   agCfg.MaxHistory,
			Format:       agCfg.Format,
			Cwd:          cwd,
			Sender:       agCfg.Sender,
			ContextMode:  agCfg.ContextMode,
			GroupJID:     agCfg.GroupJID,
			Timeout:      timeout,
		})
		slog.Info("created HTTP agent", "component", "agent", "name", name, "endpoint", agCfg.Endpoint, "model", agCfg.Model, "format", agCfg.Format)
		return ag
	default:
		slog.Warn("unknown agent type", "component", "agent", "type", agCfg.Type, "name", name)
		return nil
	}
}

func agentWorkspace(cfg *config.Config, agCfg config.AgentConfig) string {
	if agCfg.Cwd != "" {
		return agCfg.Cwd
	}
	if cfg != nil {
		return cfg.AgentWorkspace
	}
	return ""
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
