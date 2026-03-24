package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
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

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Validate RC config
	if cfg.RC.ClientID == "" || cfg.RC.ClientSecret == "" || cfg.RC.JWTToken == "" {
		return fmt.Errorf("RingCentral credentials not configured. Set RC_CLIENT_ID, RC_CLIENT_SECRET, RC_JWT_TOKEN environment variables or add to config file")
	}
	if cfg.RC.ChatID == "" {
		return fmt.Errorf("RingCentral chat ID not configured. Set RC_CHAT_ID environment variable or add to config file")
	}

	if config.DetectAndConfigure(cfg) {
		if err := config.Save(cfg); err != nil {
			log.Printf("Warning: failed to save auto-detected config: %v", err)
		} else {
			path, _ := config.ConfigPath()
			log.Printf("Auto-detected agents saved to %s", path)
		}
	}

	// Verify detected agents
	verifyAgents(cfg)

	// Create RingCentral client
	creds := &ringcentral.Credentials{
		ClientID:     cfg.RC.ClientID,
		ClientSecret: cfg.RC.ClientSecret,
		JWTToken:     cfg.RC.JWTToken,
		ChatID:       cfg.RC.ChatID,
		ServerURL:    cfg.RC.ServerURL,
	}
	client := ringcentral.NewClient(creds)

	// Authenticate
	log.Println("Authenticating with RingCentral...")
	if err := client.Authenticate(); err != nil {
		return fmt.Errorf("RingCentral authentication failed: %w", err)
	}
	log.Println("RingCentral authentication successful")

	// Get own extension ID to filter self-messages
	ownerID, err := client.GetExtensionInfo(ctx)
	if err != nil {
		log.Printf("Warning: failed to get extension info: %v", err)
	} else {
		client.SetOwnerID(ownerID)
		log.Printf("Bot owner ID: %s", ownerID)
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

	// Start default agent in background
	go func() {
		if cfg.DefaultAgent == "" {
			log.Println("No default agent configured, staying in echo mode")
			return
		}
		log.Printf("Initializing default agent %q in background...", cfg.DefaultAgent)
		ag := createAgentByName(ctx, cfg, cfg.DefaultAgent)
		if ag == nil {
			log.Printf("Failed to initialize default agent %q, staying in echo mode", cfg.DefaultAgent)
		} else {
			handler.SetDefaultAgent(cfg.DefaultAgent, ag)
		}
	}()

	// Start HTTP API server
	apiAddr := cfg.APIAddr
	if apiAddrFlag != "" {
		apiAddr = apiAddrFlag
	}
	apiServer := api.NewServer(client, apiAddr)
	go func() {
		if err := apiServer.Run(ctx); err != nil {
			log.Printf("API server error: %v", err)
		}
	}()

	// Start WebSocket monitor
	log.Printf("Starting message bridge for chat %s...", cfg.RC.ChatID)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runMonitorWithRestart(ctx, client, handler)
	}()

	wg.Wait()
	log.Println("Monitor stopped")
	return nil
}

// runMonitorWithRestart runs a monitor with automatic restart on failure.
func runMonitorWithRestart(ctx context.Context, client *ringcentral.Client, handler *messaging.Handler) {
	const maxRestartDelay = 30 * time.Second
	restartDelay := 3 * time.Second

	for {
		log.Printf("[monitor] Starting WebSocket monitor...")

		monitor := ringcentral.NewMonitor(client, handler.HandleMessage)
		client.SetMonitor(monitor)
		err := monitor.Run(ctx)

		if ctx.Err() != nil {
			return
		}

		log.Printf("[monitor] Monitor stopped: %v, restarting in %s", err, restartDelay)
		select {
		case <-time.After(restartDelay):
		case <-ctx.Done():
			return
		}

		restartDelay *= 2
		if restartDelay > maxRestartDelay {
			restartDelay = maxRestartDelay
		}
	}
}

// createAgentByName creates and starts an agent by its config name.
func createAgentByName(ctx context.Context, cfg *config.Config, name string) agent.Agent {
	agCfg, ok := cfg.Agents[name]
	if !ok {
		log.Printf("[agent] %q not found in config", name)
		return nil
	}

	switch agCfg.Type {
	case "acp":
		ag := agent.NewACPAgent(agent.ACPAgentConfig{
			Command:      agCfg.Command,
			Args:         agCfg.Args,
			Cwd:          agCfg.Cwd,
			Model:        agCfg.Model,
			SystemPrompt: agCfg.SystemPrompt,
		})
		if err := ag.Start(ctx); err != nil {
			log.Printf("[agent] failed to start ACP agent %q: %v", name, err)
			return nil
		}
		log.Printf("[agent] started ACP agent: %s (command=%s, type=%s, model=%s)", name, agCfg.Command, agCfg.Type, agCfg.Model)
		return ag
	case "cli":
		ag := agent.NewCLIAgent(agent.CLIAgentConfig{
			Name:         name,
			Command:      agCfg.Command,
			Args:         agCfg.Args,
			Cwd:          agCfg.Cwd,
			Model:        agCfg.Model,
			SystemPrompt: agCfg.SystemPrompt,
		})
		log.Printf("[agent] created CLI agent: %s (command=%s, type=%s, model=%s)", name, agCfg.Command, agCfg.Type, agCfg.Model)
		return ag
	case "http":
		if agCfg.Endpoint == "" {
			log.Printf("[agent] HTTP agent %q has no endpoint", name)
			return nil
		}
		ag := agent.NewHTTPAgent(agent.HTTPAgentConfig{
			Endpoint:     agCfg.Endpoint,
			APIKey:       agCfg.APIKey,
			Model:        agCfg.Model,
			SystemPrompt: agCfg.SystemPrompt,
			MaxHistory:   agCfg.MaxHistory,
		})
		log.Printf("[agent] created HTTP agent: %s (endpoint=%s, model=%s)", name, agCfg.Endpoint, agCfg.Model)
		return ag
	default:
		log.Printf("[agent] unknown type %q for %q", agCfg.Type, name)
		return nil
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
	if pid, err := readPid(); err == nil {
		if processExists(pid) {
			fmt.Printf("ringclaw is already running (pid=%d)\n", pid)
			return nil
		}
	}

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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

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

func processExists(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// verifyAgents checks each detected agent and logs availability status.
func verifyAgents(cfg *config.Config) {
	if len(cfg.Agents) == 0 {
		log.Println("[agents] No agents detected")
		return
	}

	log.Println("[agents] Verifying detected agents...")

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
			log.Printf("[agents]   ✓ %-12s (type=%s, %s)", r.name, r.agType, r.detail)
			available = append(available, r.name)
		} else {
			log.Printf("[agents]   ✗ %-12s (type=%s, %s)", r.name, r.agType, r.detail)
			unavailable = append(unavailable, r.name)
		}
	}

	log.Printf("[agents] %d available, %d unavailable (default: %s)",
		len(available), len(unavailable), cfg.DefaultAgent)

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
				log.Printf("[agents] default agent set to %s", name)
				break
			}
		}
	}
}
