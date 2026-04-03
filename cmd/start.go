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
	"github.com/ringclaw/ringclaw/config"
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

	verifyAgents(cfg)

	// Initialize clients, handler, and services
	c, err := initClients(ctx, cfg)
	if err != nil {
		return err
	}

	handler := initHandler(ctx, cfg)
	initServices(ctx, cfg, c, handler)

	// Start WebSocket monitor
	slog.Info("starting message bridge", "chatIDs", cfg.RC.ChatIDs)

	resolvedUserIDs := cfg.RC.SourceUserIDs
	if len(cfg.RC.SourceUserIDs) > 0 {
		lookupClient := c.bot
		if c.private != nil {
			lookupClient = c.private
		}
		resolvedUserIDs = lookupClient.ResolveUserIDs(ctx, cfg.RC.SourceUserIDs)
		slog.Info("source_user_ids resolved", "count", len(resolvedUserIDs), "ids", resolvedUserIDs)
	}

	monitor := ringcentral.NewMonitor(c.bot, handler.HandleMessage, cfg.RC.ChatIDs, resolvedUserIDs, cfg.RC.IsBotMentionOnly())
	if c.private != nil {
		monitor.SetPrivateClient(c.private)
		c.private.SetMonitor(monitor)
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
