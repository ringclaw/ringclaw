package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/api"
	"github.com/ringclaw/ringclaw/config"
	"github.com/ringclaw/ringclaw/messaging"
	"github.com/ringclaw/ringclaw/ringcentral"
	"github.com/spf13/cobra"
)

var (
	foregroundFlag bool
	apiAddrFlag    string
)

func init() {
	startCmd.Flags().BoolVarP(&foregroundFlag, "foreground", "f", false, "Run in foreground (default is background)")
	startCmd.Flags().StringVar(&apiAddrFlag, "api-addr", "", "API server listen address (default 127.0.0.1:18011)")
	rootCmd.AddCommand(startCmd)
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the RingCentral message bridge",
	RunE:  runStart,
}

func runStart(cmd *cobra.Command, args []string) error {
	if !foregroundFlag {
		return runDaemon()
	}

	ctx, cancel := notifyContext(context.Background())
	defer cancel()

	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Validate RC config: bot token is required, private app is optional
	if cfg.RC.BotToken == "" {
		return fmt.Errorf("bot token not configured. Set RC_BOT_TOKEN environment variable or add bot_token to config file. Run 'ringclaw setup' for guided configuration")
	}
	if len(cfg.RC.ChatIDs) == 0 {
		return fmt.Errorf("RingCentral chat IDs not configured. Add chat_ids to config file")
	}

	if config.DetectAndConfigure(cfg) {
		if err := config.Save(cfg); err != nil {
			slog.Warn("failed to save auto-detected config", "error", err)
		} else {
			path, _ := config.ConfigPath()
			slog.Info("auto-detected agents saved", "path", path)
		}
	}

	// Verify detected agents
	verifyAgents(cfg)

	// Create bot client (required — used for WS connection and replies)
	slog.Info("initializing bot client...")
	botClient := ringcentral.NewBotClient(cfg.RC.ServerURL, cfg.RC.BotToken)
	botOwnerID, err := botClient.GetExtensionInfo(ctx)
	if err != nil {
		slog.Warn("failed to get bot extension info", "error", err)
	} else {
		botClient.SetOwnerID(botOwnerID)
		slog.Info("bot extension ID resolved", "botOwnerID", botOwnerID)
	}

	// Create private app client (optional — enables summarize, cross-chat access)
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
		slog.Info("no private app configured, summarize and cross-chat features disabled")
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

	// Create handler
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

	// Load custom aliases from agent configs and check for conflicts
	customAliases := config.BuildAliasMap(cfg.Agents)
	if len(customAliases) > 0 {
		handler.SetCustomAliases(customAliases)
		checkAliasConflicts(cfg, customAliases)
	}

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

	// Start HTTP API server (use private client if available for broader access)
	apiAddr := cfg.APIAddr
	if apiAddrFlag != "" {
		apiAddr = apiAddrFlag
	}
	defaultChatID := ""
	if len(cfg.RC.ChatIDs) > 0 {
		defaultChatID = cfg.RC.ChatIDs[0]
	}
	apiClient := botClient
	if privateClient != nil {
		apiClient = privateClient
	}
	apiServer := api.NewServer(apiClient, apiAddr, defaultChatID)
	go func() {
		if err := apiServer.Run(ctx); err != nil {
			slog.Error("API server error", "error", err)
		}
	}()

	// Start cron scheduler
	cronStorePath, _ := messaging.DefaultCronStorePath()
	cronStore := messaging.NewCronStore(cronStorePath)
	if err := cronStore.Load(); err != nil {
		slog.Warn("failed to load cron jobs", "error", err)
	}
	handler.SetCronStore(cronStore)

	cronScheduler := messaging.NewCronScheduler(cronStore, botClient, defaultChatID, func(name string) agent.Agent {
		if name == "" {
			return handler.GetDefaultAgent()
		}
		ag, _ := handler.GetAgent(ctx, name)
		return ag
	})
	go cronScheduler.Start(ctx)

	// Start heartbeat runner
	if cfg.Heartbeat.Enabled {
		hbRunner, err := messaging.NewHeartbeatRunner(cfg.Heartbeat, botClient, defaultChatID, handler.GetDefaultAgent)
		if err != nil {
			slog.Error("failed to start heartbeat runner", "error", err)
		} else {
			go hbRunner.Start(ctx)
		}
	}

	// Start WebSocket monitor (bot client drives WS connection)
	slog.Info("starting message bridge", "chatIDs", cfg.RC.ChatIDs)

	// Resolve source_user_ids: emails are looked up via directory API; numeric IDs are kept as-is.
	resolvedUserIDs := cfg.RC.SourceUserIDs
	if len(cfg.RC.SourceUserIDs) > 0 {
		lookupClient := botClient
		if privateClient != nil {
			lookupClient = privateClient
		}
		resolvedUserIDs = lookupClient.ResolveUserIDs(ctx, cfg.RC.SourceUserIDs)
		slog.Info("source_user_ids resolved", "count", len(resolvedUserIDs), "ids", resolvedUserIDs)
	}

	monitor := ringcentral.NewMonitor(botClient, handler.HandleMessage, cfg.RC.ChatIDs, resolvedUserIDs, cfg.RC.IsBotMentionOnly())
	if privateClient != nil {
		monitor.SetPrivateClient(privateClient)
		privateClient.SetMonitor(monitor)
	}
	botClient.SetMonitor(monitor)
	if err := monitor.Run(ctx); err != nil && ctx.Err() == nil {
		slog.Error("monitor stopped unexpectedly", "component", "monitor", "error", err)
	}
	slog.Info("monitor stopped")
	return nil
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
// Ported from github.com/fastclaw-ai/weclaw commit 9f5c458.
func checkAliasConflicts(cfg *config.Config, aliases map[string]string) {
	reserved := map[string]bool{"status": true, "help": true, "new": true, "clear": true, "info": true}

	seen := make(map[string]string) // alias -> first agent that claimed it
	for alias, agent := range aliases {
		if reserved[alias] {
			slog.Warn("alias conflicts with reserved command", "component", "config", "alias", alias, "agent", agent)
			continue
		}
		if _, ok := cfg.Agents[alias]; ok {
			slog.Warn("alias shadows agent name", "component", "config", "alias", alias, "agent", agent)
		}
		if prev, dup := seen[alias]; dup {
			slog.Warn("duplicate alias across agents", "component", "config", "alias", alias, "agents", prev+","+agent)
		}
		seen[alias] = agent
	}
}

// --- Daemon mode ---

func ringclawDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ringclaw")
}

func pidFile() string {
	return filepath.Join(ringclawDir(), "ringclaw.pid")
}

func logFile() string {
	return filepath.Join(ringclawDir(), "ringclaw.log")
}

func runDaemon() error {
	// Kill any existing ringclaw processes before starting a new one
	stopAllRingclaw()

	if err := os.MkdirAll(ringclawDir(), 0o700); err != nil {
		return fmt.Errorf("create ringclaw dir: %w", err)
	}

	lf, err := os.OpenFile(logFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	cmd := exec.Command(exe, "start", "-f")
	cmd.Stdout = lf
	cmd.Stderr = lf
	setSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		lf.Close()
		return fmt.Errorf("start daemon: %w", err)
	}

	pid := cmd.Process.Pid
	os.WriteFile(pidFile(), []byte(fmt.Sprintf("%d", pid)), 0o644)

	cmd.Process.Release()
	lf.Close()

	fmt.Printf("ringclaw started in background (pid=%d)\n", pid)
	fmt.Printf("Log: %s\n", logFile())
	fmt.Printf("Stop: ringclaw stop\n")
	return nil
}

func readPid() (int, error) {
	data, err := os.ReadFile(pidFile())
	if err != nil {
		return 0, err
	}
	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		return 0, err
	}
	return pid, nil
}

// verifyAgents checks each detected agent and logs availability status.
func verifyAgents(cfg *config.Config) {
	if len(cfg.Agents) == 0 {
		slog.Info("no agents detected", "component", "agents")
		return
	}

	slog.Info("verifying detected agents", "component", "agents")

	type result struct {
		name   string
		agType string
		cmd    string
		ok     bool
		detail string
	}

	results := make(chan result, len(cfg.Agents))
	var wg sync.WaitGroup

	for name, agCfg := range cfg.Agents {
		wg.Add(1)
		go func(name string, agCfg config.AgentConfig) {
			defer wg.Done()
			r := result{name: name, agType: agCfg.Type, cmd: agCfg.Command}

			switch agCfg.Type {
			case "cli", "acp":
				// Quick version/help check with timeout
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, agCfg.Command, "--version")
				out, err := cmd.Output()
				if err != nil {
					// Try --help as fallback
					cmd = exec.CommandContext(ctx, agCfg.Command, "--help")
					out, err = cmd.Output()
				}
				if err != nil {
					r.ok = false
					r.detail = "binary found but not responding"
				} else {
					r.ok = true
					ver := strings.TrimSpace(strings.Split(string(out), "\n")[0])
					if len(ver) > 60 {
						ver = ver[:60] + "..."
					}
					r.detail = ver
				}
			case "http":
				r.ok = true
				r.cmd = agCfg.Endpoint
				r.detail = "http endpoint"
			default:
				r.ok = false
				r.detail = "unknown type"
			}

			results <- r
		}(name, agCfg)
	}

	wg.Wait()
	close(results)

	var available, unavailable []string
	for r := range results {
		if r.ok {
			slog.Info("agent available", "component", "agents", "name", r.name, "type", r.agType, "detail", r.detail)
			available = append(available, r.name)
		} else {
			slog.Warn("agent unavailable", "component", "agents", "name", r.name, "type", r.agType, "detail", r.detail)
			unavailable = append(unavailable, r.name)
		}
	}

	slog.Info("agent verification complete", "component", "agents", "available", len(available), "unavailable", len(unavailable), "default", cfg.DefaultAgent)

	// Remove unavailable agents from config
	for _, name := range unavailable {
		delete(cfg.Agents, name)
		if cfg.DefaultAgent == name {
			cfg.DefaultAgent = ""
		}
	}

	// Re-pick default if removed
	if cfg.DefaultAgent == "" && len(available) > 0 {
		for _, name := range config.DefaultOrder() {
			if _, ok := cfg.Agents[name]; ok {
				cfg.DefaultAgent = name
				slog.Info("default agent set", "component", "agents", "name", name)
				break
			}
		}
	}
}

// stopAllRingclaw kills all running ringclaw processes (by PID file and by process scan).
func stopAllRingclaw() {
	// 1. Kill by PID file
	if pid, err := readPid(); err == nil && processExists(pid) {
		if p, err := os.FindProcess(pid); err == nil {
			_ = signalTerminate(p)
		}
	}
	os.Remove(pidFile())

	// 2. Kill any remaining ringclaw processes by scanning
	exe, err := os.Executable()
	if err != nil {
		return
	}
	killByName(exe)
	time.Sleep(500 * time.Millisecond)
}
