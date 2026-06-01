package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// agentCandidate defines one way to run an agent.
// Multiple candidates can map to the same agent name; the first detected wins.
type agentCandidate struct {
	Name   string   // agent name (e.g. "claude", "codex")
	Binary string   // binary to look up in PATH
	NpxPkg string   // npm package for npx fallback (e.g. "@zed-industries/codex-acp")
	Args   []string // extra args (e.g. ["acp"] for cursor)
	Type   string   // "acp", "cli"
	Model  string   // default model
}

// acpInstallHints maps agent names to their ACP adapter install commands.
var acpInstallHints = map[string]string{
	"claude": "npm install -g @agentclientprotocol/claude-agent-acp",
	"codex":  "npm install -g @zed-industries/codex-acp",
}

// ACPInstallHint returns the install command for an agent's ACP adapter, if any.
func ACPInstallHint(name string) string {
	return acpInstallHints[name]
}

// agentCandidates is ordered by priority: for each agent name, earlier entries
// are preferred. E.g. claude ACP is tried before claude CLI.
var agentCandidates = []agentCandidate{
	// claude: prefer standalone ACP binary, then npx, then CLI fallback
	{Name: "claude", Binary: "claude-agent-acp", Type: "acp", Model: "sonnet"},
	{Name: "claude", NpxPkg: "@agentclientprotocol/claude-agent-acp", Type: "acp", Model: "sonnet"},
	{Name: "claude", Binary: "claude", Type: "cli", Model: "sonnet"},
	// codex: prefer standalone ACP binary, then npx, then CLI fallback
	{Name: "codex", Binary: "codex-acp", Type: "acp", Model: ""},
	{Name: "codex", NpxPkg: "@zed-industries/codex-acp", Type: "acp", Model: ""},
	{Name: "codex", Binary: "codex", Type: "cli", Model: ""},
	// Cursor Agent ACP is exposed by the modern Cursor CLI as `agent acp`.
	{Name: "cursor", Binary: "agent", Args: []string{"acp"}, Type: "acp", Model: ""},
	{Name: "kimi", Binary: "kimi", Args: []string{"acp"}, Type: "acp", Model: ""},
	{Name: "gemini", Binary: "gemini", Args: []string{"--acp"}, Type: "acp", Model: ""},
	{Name: "opencode", Binary: "opencode", Args: []string{"acp"}, Type: "acp", Model: ""},
	{Name: "openclaw", Binary: "openclaw", Type: "acp", Model: "openclaw:main"}, // args built dynamically
	{Name: "pi", Binary: "pi-acp", Type: "acp", Model: ""},
	{Name: "copilot", Binary: "copilot", Args: []string{"--acp", "--stdio"}, Type: "acp", Model: ""},
	{Name: "droid", Binary: "droid", Args: []string{"exec", "--output-format", "acp"}, Type: "acp", Model: ""},
	{Name: "iflow", Binary: "iflow", Args: []string{"--experimental-acp"}, Type: "acp", Model: ""},
	{Name: "kiro", Binary: "kiro-cli", Args: []string{"acp"}, Type: "acp", Model: ""},
	{Name: "qwen", Binary: "qwen", Args: []string{"--acp"}, Type: "acp", Model: ""},
	{Name: "augment", Binary: "auggie", Args: []string{"--acp"}, Type: "acp", Model: ""},
}

// DefaultOrder returns the priority list for choosing the default agent.
func DefaultOrder() []string { return defaultOrder }

// defaultOrder defines the priority for choosing the default agent.
// Lower index = higher priority.
var defaultOrder = []string{
	"claude", "codex", "cursor", "kimi", "gemini", "opencode", "openclaw",
	"pi", "copilot", "droid", "iflow", "kiro", "qwen", "augment",
}

// DetectAndConfigure auto-detects local agents and populates the config.
// For each agent name, it picks the highest-priority candidate (acp > cli).
// Returns true if the config was modified.
func DetectAndConfigure(cfg *Config) bool {
	modified := false
	if migrateCursorAgentConfig(cfg) {
		modified = true
	}

	for _, candidate := range agentCandidates {
		existing, exists := cfg.Agents[candidate.Name]

		// Resolve command path: either binary lookup or npx package
		command, args := resolveCandidate(candidate)
		if command == "" {
			continue
		}

		if exists {
			// Try to upgrade non-ACP agents to ACP if a higher-priority ACP binary is available
			if existing.Type == "acp" || candidate.Type != "acp" {
				continue
			}
			slog.Info("upgrading agent to ACP", "component", "config", "name", candidate.Name, "from", existing.Type, "command", command)
			cfg.Agents[candidate.Name] = AgentConfig{
				Type:    "acp",
				Command: command,
				Args:    args,
				Model:   existing.Model,
			}
			modified = true
			continue
		}

		slog.Info("auto-detected agent", "component", "config", "name", candidate.Name, "command", command, "type", candidate.Type)
		cfg.Agents[candidate.Name] = AgentConfig{
			Type:    candidate.Type,
			Command: command,
			Args:    args,
			Model:   candidate.Model,
		}
		modified = true
	}

	// Backfill full_access for codex ACP agent (claude-agent-acp doesn't support set_mode)
	if agCfg, exists := cfg.Agents["codex"]; exists && agCfg.Type == "acp" && !agCfg.FullAccess {
		agCfg.FullAccess = true
		cfg.Agents["codex"] = agCfg
		modified = true
	}

	// Special handling for openclaw: resolve gateway connection from
	// cfg.OpenclawGateway -> ~/.openclaw/openclaw.json -> skip.
	if agCfg, exists := cfg.Agents["openclaw"]; exists && agCfg.Type == "acp" && len(agCfg.Args) == 0 {
		gwURL, gwToken, gwPassword := loadOpenclawGateway(cfg)
		if gwURL != "" {
			args := []string{"acp", "--url", gwURL, "--session", "agent:main:main"}
			if gwToken != "" {
				args = append(args, "--token", gwToken)
			} else if gwPassword != "" {
				args = append(args, "--password", gwPassword)
			}
			agCfg.Args = args
			cfg.Agents["openclaw"] = agCfg
			modified = true
			slog.Info("openclaw ACP configured with gateway", "component", "config", "gateway", gwURL)
		} else {
			slog.Warn("openclaw binary found but no gateway config, skipping ACP", "component", "config")
			delete(cfg.Agents, "openclaw")
			modified = true
		}
	}

	// Fallback: if openclaw not configured, try HTTP via gateway config.
	if _, exists := cfg.Agents["openclaw"]; !exists {
		gwURL, gwToken, _ := loadOpenclawGateway(cfg)
		if gwURL != "" {
			// Convert ws(s):// to http(s):// for HTTP endpoint
			httpURL := gwURL
			httpURL = strings.Replace(httpURL, "wss://", "https://", 1)
			httpURL = strings.Replace(httpURL, "ws://", "http://", 1)
			endpoint := strings.TrimRight(httpURL, "/") + "/v1/chat/completions"
			slog.Info("using openclaw HTTP fallback", "component", "config", "endpoint", endpoint)
			cfg.Agents["openclaw"] = AgentConfig{
				Type:     "http",
				Endpoint: endpoint,
				APIKey:   gwToken,
				Headers:  map[string]string{"x-openclaw-scopes": "operator.write"},
				Model:    "openclaw:main",
			}
			modified = true
		}
	}

	// Pick the highest-priority default agent.
	if cfg.DefaultAgent == "" || !agentExists(cfg, cfg.DefaultAgent) {
		for _, name := range defaultOrder {
			if _, ok := cfg.Agents[name]; ok {
				if cfg.DefaultAgent != name {
					slog.Info("setting default agent", "component", "config", "name", name)
					cfg.DefaultAgent = name
					modified = true
				}
				break
			}
		}
	}

	return modified
}

func migrateCursorAgentConfig(cfg *Config) bool {
	if cfg == nil || cfg.Agents == nil {
		return false
	}
	existing, ok := cfg.Agents["cursor"]
	if !ok {
		return false
	}
	base := filepath.Base(existing.Command)
	if base != "cursor-agent" && !(existing.Type == "cli" && base == "agent") {
		return false
	}
	if path, err := lookPath("agent"); err == nil && path != "" {
		existing.Command = path
	} else if base != "agent" {
		return false
	}
	existing.Type = "acp"
	existing.Args = []string{"acp"}
	existing.AllowWrite = false
	existing.FullAccess = false
	cfg.Agents["cursor"] = existing
	slog.Info("migrated cursor agent to modern ACP command", "component", "config", "command", existing.Command, "args", existing.Args)
	return true
}

// loadOpenclawGateway resolves openclaw gateway connection info.
// Priority: cfg.OpenclawGateway > ~/.openclaw/openclaw.json.
// Returns (url, token, password). url="" means not configured.
func loadOpenclawGateway(cfg *Config) (gwURL, gwToken, gwPassword string) {
	// 1. config.json openclaw_gateway takes priority
	if cfg != nil && cfg.OpenclawGateway.URL != "" {
		return cfg.OpenclawGateway.URL, cfg.OpenclawGateway.Token, cfg.OpenclawGateway.Password
	}

	// 2. Read from ~/.openclaw/openclaw.json
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	data, err := os.ReadFile(filepath.Join(home, ".openclaw", "openclaw.json"))
	if err != nil {
		return
	}

	var ocCfg struct {
		Gateway struct {
			Port int    `json:"port"`
			Mode string `json:"mode"`
			Auth struct {
				Mode     string `json:"mode"`
				Token    string `json:"token"`
				Password string `json:"password"`
			} `json:"auth"`
			Remote struct {
				URL   string `json:"url"`
				Token string `json:"token"`
			} `json:"remote"`
		} `json:"gateway"`
	}
	if err := json.Unmarshal(data, &ocCfg); err != nil {
		slog.Error("failed to parse openclaw config", "component", "config", "error", err)
		return
	}

	gw := ocCfg.Gateway

	// Remote gateway (gateway.remote.url)
	if gw.Remote.URL != "" {
		gwURL = gw.Remote.URL
		gwToken = gw.Remote.Token
		return
	}

	// Local gateway (gateway.port + gateway.auth)
	if gw.Port > 0 {
		gwURL = fmt.Sprintf("ws://127.0.0.1:%d", gw.Port)
		switch gw.Auth.Mode {
		case "token":
			gwToken = gw.Auth.Token
		case "password":
			gwPassword = gw.Auth.Password
		}
		return
	}

	return
}

func agentExists(cfg *Config, name string) bool {
	_, ok := cfg.Agents[name]
	return ok
}

// resolveCandidate resolves a candidate to (command, args).
// Returns ("", nil) if the candidate cannot be resolved.
func resolveCandidate(c agentCandidate) (string, []string) {
	if c.Binary != "" {
		path, err := lookPath(c.Binary)
		if err != nil {
			return "", nil
		}
		return path, c.Args
	}
	if c.NpxPkg != "" {
		npxPath, err := lookPath("npx")
		if err != nil {
			slog.Debug("npx not found, skipping npx-based agent", "component", "config", "package", c.NpxPkg)
			return "", nil
		}
		// Verify Node.js is actually available (npx can exist without a working node)
		if _, err := lookPath("node"); err != nil {
			slog.Warn("npx found but node is not available, skipping", "component", "config", "package", c.NpxPkg)
			return "", nil
		}
		slog.Info("resolved agent via npx", "component", "config", "package", c.NpxPkg)
		args := append([]string{"-y", c.NpxPkg}, c.Args...)
		return npxPath, args
	}
	return "", nil
}

// lookPath finds a binary by name. It first tries exec.LookPath (fast, uses
// current PATH). If that fails, it falls back to resolving via a login shell
// which sources the user's profile (~/.zshrc, ~/.bashrc) — this picks up
// binaries installed through version managers like nvm, mise, etc. that only
// add their paths in interactive shells.
//
// Ported from github.com/fastclaw-ai/weclaw commit b7a2a64.
func lookPath(binary string) (string, error) {
	if p, err := exec.LookPath(binary); err == nil {
		return p, nil
	}

	shell := "zsh"
	if runtime.GOOS != "darwin" {
		shell = "bash"
	}
	out, err := exec.Command(shell, "-lic", "which "+binary).Output()
	if err != nil {
		return "", fmt.Errorf("not found: %s", binary)
	}
	p := strings.TrimSpace(string(out))
	if p == "" || strings.Contains(p, "not found") {
		return "", fmt.Errorf("not found: %s", binary)
	}
	slog.Info("resolved via login shell", "component", "config", "binary", binary, "path", p)
	return p, nil
}
