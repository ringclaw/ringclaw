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

	"github.com/lmittmann/tint"
	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/config"
	"github.com/ringclaw/ringclaw/messaging/oob"
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

	// Initialize log level and format: flag > config > default
	levelStr := cfg.LogLevel
	if logLevelFlag != "" {
		levelStr = logLevelFlag
	}
	logLevel := config.ParseLogLevel(levelStr)
	config.SetDebugMode(logLevel == slog.LevelDebug)

	formatStr := strings.ToLower(cfg.LogFormat)
	if logFormatFlag != "" {
		formatStr = strings.ToLower(logFormatFlag)
	}
	var logHandler slog.Handler
	switch formatStr {
	case "json":
		logHandler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
	case "text":
		logHandler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
	default:
		logHandler = tint.NewHandler(os.Stderr, &tint.Options{Level: logLevel, TimeFormat: time.DateTime})
	}
	slog.SetDefault(slog.New(logHandler))

	// Validate RC config: bot token is required, private app is optional
	if cfg.RC.BotToken == "" {
		return fmt.Errorf("bot token not configured. Add ringcentral.bot_token to ~/.ringclaw/config.json or run 'ringclaw setup' for guided configuration")
	}
	if len(cfg.RC.ChatIDs) == 0 {
		return fmt.Errorf("RingCentral chat IDs not configured. Add chat_ids to config file")
	}

	if config.DetectAndConfigure(cfg) {
		if err := config.Save(cfg); err != nil {
			slog.Warn("failed to save auto-detected config", "error", err)
		} else {
			path, _ := config.ConfigPath(dirFlag)
			slog.Info("auto-detected agents saved", "path", path)
		}
	}

	verifyAgents(cfg)

	// Configure the cwd allowlist before any agent is created so
	// /cwd and Agent.SetCwd are pinned to a safe set of subtrees.
	// Finding #2 from the security review.
	//
	// The effective allowlist is the union of:
	//   - cfg.AgentAllowWorkspaceList (operator-controlled list)
	//   - cfg.AgentWorkspace          (legacy default cwd, implicitly trusted)
	//   - ~/.ringclaw/workspace       (always-on default)
	//
	// Duplicates and empty entries are dropped by agent.SetWorkspaceRoots.
	{
		roots := make([]string, 0, len(cfg.AgentAllowWorkspaceList)+2)
		roots = append(roots, cfg.AgentAllowWorkspaceList...)
		if cfg.AgentWorkspace != "" {
			roots = append(roots, cfg.AgentWorkspace)
		}
		if home, err := os.UserHomeDir(); err == nil {
			roots = append(roots, filepath.Join(home, ".ringclaw", "workspace"))
		}
		agent.SetWorkspaceRoots(roots)
		if effective := agent.WorkspaceRoots(); len(effective) > 0 {
			slog.Info("workspace allowlist configured", "component", "start", "roots", effective)
		}
	}

	// Resolve the ACP full-access acknowledgement. config.json is the
	// sole source; any previously supported env override was removed.
	// Finding #6 from the security review.
	{
		ack := false
		source := "default(false)"
		if cfg.FullAccessAck != nil {
			ack = *cfg.FullAccessAck
			source = "config.full_access_ack"
		}
		agent.SetFullAccessAck(ack)
		if ack {
			slog.Warn("full_access acknowledgement ACTIVE: any agent with full_access:true will be allowed to disable MCP guardrails",
				"component", "start", "source", source)
		} else {
			slog.Info("full_access acknowledgement not granted: full_access:true on any agent will be downgraded with a warning",
				"component", "start", "source", source)
		}
	}

	// Initialize clients, handler, and services
	c, err := initClients(ctx, cfg)
	if err != nil {
		return err
	}

	handler := initHandler(ctx, cfg)

	// OOB manager must be initialized before initServices so the API
	// server can expose the approval endpoints.
	var oobMgr *oob.Manager
	if err := initOOBManager(handler, c, func(m *oob.Manager) { oobMgr = m }); err != nil {
		slog.Error("failed to initialize OOB approval manager; /full-access and cross-chat notices will be disabled",
			"component", "start", "error", err)
	}
	initServices(ctx, cfg, c, handler, oobMgr)

	// Start WebSocket monitor
	slog.Info("starting message bridge", "chatIDs", cfg.RC.ChatIDs)

	resolvedUserIDs := cfg.RC.SourceUserIDs
	if len(cfg.RC.SourceUserIDs) > 0 {
		resolvedUserIDs = c.lookupClient().ResolveUserIDs(ctx, cfg.RC.SourceUserIDs)
		slog.Info("source_user_ids resolved", "count", len(resolvedUserIDs), "ids", resolvedUserIDs)
	}

	monitor := ringcentral.NewMonitor(c.bot, handler.HandleMessage, cfg.RC.ChatIDs, resolvedUserIDs, cfg.RC.IsGroupMentionOnly())
	if c.private != nil {
		monitor.SetPrivateClient(c.private)
		c.private.SetMonitor(monitor)
		if ownerID := c.private.OwnerID(); ownerID != "" {
			monitor.AddTrustedSender(ownerID)
			handler.AddTrustedSender(ownerID)
		}
	}
	for _, id := range resolvedUserIDs {
		handler.AddTrustedSender(id)
	}

	// chat_user_allow: resolve identifiers (email / phone / numeric)
	// to numeric user IDs and inject into both Monitor + Handler.
	// v0.4.4 restores the v0.4.1 resolve-and-inject path; the v0.4.2
	// startup force-clear is removed now that v0.4.3 enforces a
	// non-owner ceiling on these sessions: Layer A (read-only ACP
	// mode via session/set_mode) plus Layer B (fail-closed deny of
	// fs/* / terminal/* / session/request_permission). Plain-text
	// replies and RC ACTION blocks (MESSAGE / TASK / NOTE / EVENT)
	// remain available; WebFetch / WebSearch / MCP custom tools are
	// still best-effort under Layer A only and are scheduled to be
	// closed by the v0.5.0 OS-level sandbox.
	if len(cfg.RC.ChatUserAllow) > 0 {
		lookupClient := c.lookupClient()
		resolvedChatAllow := make(map[string][]string, len(cfg.RC.ChatUserAllow))
		total := 0
		for chatID, list := range cfg.RC.ChatUserAllow {
			ids := lookupClient.ResolveUserIDs(ctx, list)
			if len(ids) == 0 {
				slog.Warn("chat_user_allow entry resolved to zero numeric IDs; the chat ID must be a real RC group ID (numeric string), not a team display name, and identifiers must exist in the directory",
					"component", "start", "chatID", chatID, "rawIdentifiers", list)
				continue
			}
			resolvedChatAllow[chatID] = ids
			total += len(ids)
			for _, uid := range ids {
				handler.AddChatUserAllow(chatID, uid)
			}
		}
		monitor.SetChatUserAllow(resolvedChatAllow)
		slog.Info("chat_user_allow resolved (v0.4.3+ non-owner ceiling enforced for these users)",
			"component", "start", "chats", len(resolvedChatAllow), "users", total)
	}

	// Wire the authorize-mention OOB flow only when explicitly opted
	// in. v0.4.2 reverted the v0.4.1 default-on flip; the flag must
	// be explicit-true. v0.4.4 demotes the OOB-enabled startup notice
	// from WARN to INFO and updates the description: v0.4.3+ approved
	// users run under the non-owner ceiling (no fs/terminal/permission_request),
	// not "full agent capability".
	if cfg.RC.IsAuthorizeMentionEnabled() {
		slog.Info("authorize-mention OOB enabled — approved users run under v0.4.3+ non-owner ceiling (no fs/terminal/permission_request; plain-text replies + RC ACTION blocks remain available); v0.5.0 will tighten WebFetch/MCP via OS-level sandbox",
			"component", "start")
		ownerDM := handler.OwnerDMChatID()
		if c.private == nil || ownerDM == "" {
			slog.Error("allow_group_mention_authorize requires Private App + resolved owner DM; feature disabled",
				"component", "start", "hasPrivateApp", c.private != nil, "ownerDMChatID", ownerDM)
		} else {
			persist := func(chatID, identifier string) error {
				if cfg.RC.AddChatUserAllow(chatID, identifier) {
					return config.Save(cfg)
				}
				return nil
			}
			handler.SetAuthorizeMention(persist, monitor)
			monitor.SetMentionAuthorize(handler.AuthorizeMention)
			slog.Info("authorize-mention OOB flow active",
				"component", "start", "ownerDMChatID", ownerDM)
		}
	}

	// Mandatory sender allowlist: monitor and handler both deny anyone not on
	// the trusted set. Findings #1 and #7 from the security review.
	monitor.EnforceSenderAllowlist()
	handler.EnforceSenderAllowlist()
	if !monitor.HasTrustedSenders() && len(cfg.RC.ChatUserAllow) == 0 {
		slog.Error("sender allowlist is empty: no source_user_ids configured and no Private App owner detected; the bot will drop ALL incoming messages until you add ringcentral.source_user_ids or configure a Private App",
			"component", "start")
	}
	c.bot.SetMonitor(monitor)
	if err := monitor.Run(ctx); err != nil && ctx.Err() == nil {
		slog.Error("monitor stopped unexpectedly", "component", "monitor", "error", err)
	}
	slog.Info("monitor stopped")
	return nil
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

// stopAllRingclaw stops the running ringclaw process identified by the PID file.
func stopAllRingclaw() {
	if pid, err := readPid(); err == nil && processExists(pid) {
		if p, err := os.FindProcess(pid); err == nil {
			_ = signalTerminate(p)
		}
	}
	os.Remove(pidFile())
}
