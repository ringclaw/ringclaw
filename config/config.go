package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds the application configuration.
type Config struct {
	DefaultAgent string                 `json:"default_agent"`
	APIAddr      string                 `json:"api_addr,omitempty"`
	Agents       map[string]AgentConfig `json:"agents"`
	RC           RCConfig               `json:"ringcentral,omitempty"`
}

// RCConfig holds RingCentral connection configuration.
type RCConfig struct {
	ClientID       string   `json:"client_id,omitempty"`
	ClientSecret   string   `json:"client_secret,omitempty"`
	JWTToken       string   `json:"jwt_token,omitempty"`
	ChatIDs        []string `json:"chat_ids,omitempty"`
	ServerURL      string   `json:"server_url,omitempty"`
	BotToken       string   `json:"bot_token,omitempty"`
	BotMentionOnly *bool    `json:"bot_mention_only,omitempty"`
}

// HasPrivateApp returns true if all private app credentials are configured.
func (rc RCConfig) HasPrivateApp() bool {
	return rc.ClientID != "" && rc.ClientSecret != "" && rc.JWTToken != ""
}

// IsBotMentionOnly returns whether the bot requires @mention in group chats.
// Defaults to true if not explicitly set.
func (rc RCConfig) IsBotMentionOnly() bool {
	if rc.BotMentionOnly == nil {
		return true
	}
	return *rc.BotMentionOnly
}

// AgentConfig holds configuration for a single agent.
type AgentConfig struct {
	Type         string            `json:"type"`                    // "acp", "cli", or "http"
	Command      string            `json:"command,omitempty"`       // binary path (cli/acp type)
	Args         []string          `json:"args,omitempty"`          // extra args for command (e.g. ["acp"] for cursor)
	Aliases      []string          `json:"aliases,omitempty"`       // custom trigger commands (e.g. ["gpt", "4o"])
	Cwd          string            `json:"cwd,omitempty"`           // working directory (workspace)
	Env          map[string]string `json:"env,omitempty"`           // extra environment variables (cli/acp type)
	Model        string            `json:"model,omitempty"`         // model name
	SystemPrompt string            `json:"system_prompt,omitempty"` // system prompt
	Endpoint     string            `json:"endpoint,omitempty"`      // API endpoint (http type)
	APIKey       string            `json:"api_key,omitempty"`       // API key (http type)
	Headers      map[string]string `json:"headers,omitempty"`       // extra HTTP headers (http type)
	MaxHistory   int               `json:"max_history,omitempty"`   // max history (http type)
}

// BuildAliasMap builds a map from custom alias to agent name from all agent configs.
func BuildAliasMap(agents map[string]AgentConfig) map[string]string {
	m := make(map[string]string)
	for name, cfg := range agents {
		for _, alias := range cfg.Aliases {
			m[alias] = name
		}
	}
	return m
}

// DefaultConfig returns an empty configuration.
func DefaultConfig() *Config {
	return &Config{
		Agents: make(map[string]AgentConfig),
	}
}

// ConfigPath returns the path to the config file.
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ringclaw", "config.json"), nil
}

// Load loads configuration from disk and environment variables.
func Load() (*Config, error) {
	cfg := DefaultConfig()

	path, err := ConfigPath()
	if err != nil {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			loadEnv(cfg)
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Agents == nil {
		cfg.Agents = make(map[string]AgentConfig)
	}

	loadEnv(cfg)
	return cfg, nil
}

func loadEnv(cfg *Config) {
	if v := os.Getenv("RINGCLAW_DEFAULT_AGENT"); v != "" {
		cfg.DefaultAgent = v
	}
	if v := os.Getenv("RINGCLAW_API_ADDR"); v != "" {
		cfg.APIAddr = v
	}
	if v := os.Getenv("RC_CLIENT_ID"); v != "" {
		cfg.RC.ClientID = v
	}
	if v := os.Getenv("RC_CLIENT_SECRET"); v != "" {
		cfg.RC.ClientSecret = v
	}
	if v := os.Getenv("RC_JWT_TOKEN"); v != "" {
		cfg.RC.JWTToken = v
	}
	if v := os.Getenv("RC_SERVER_URL"); v != "" {
		cfg.RC.ServerURL = v
	}
	if v := os.Getenv("RC_BOT_TOKEN"); v != "" {
		cfg.RC.BotToken = v
	}
}

// Save saves the configuration to disk.
func Save(cfg *Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return os.WriteFile(path, data, 0o600)
}
