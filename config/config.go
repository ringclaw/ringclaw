package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

var debugMode atomic.Bool

// SetDebugMode enables or disables debug mode globally.
func SetDebugMode(enabled bool) { debugMode.Store(enabled) }

// IsDebug returns true if debug mode is active.
func IsDebug() bool { return debugMode.Load() }

// ParseLogLevel converts a string to slog.Level.
func ParseLogLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Config holds the application configuration.
type Config struct {
	DefaultAgent   string                 `json:"default_agent"`
	AgentWorkspace string                 `json:"agent_workspace,omitempty"`
	APIAddr        string                 `json:"api_addr,omitempty"`
	LogLevel       string                 `json:"log_level,omitempty"`  // "debug", "info" (default), "warn", "error"
	LogFormat      string                 `json:"log_format,omitempty"` // "text" (default), "json", "color"
	Agents         map[string]AgentConfig `json:"agents"`
	RC             RCConfig               `json:"ringcentral,omitempty"`
	Heartbeat      HeartbeatConfig        `json:"heartbeat,omitempty"`
	Cron           CronConfig             `json:"cron,omitempty"`
}

// HeartbeatConfig holds heartbeat runner configuration.
type HeartbeatConfig struct {
	Enabled     bool   `json:"enabled,omitempty"`
	Interval    string `json:"interval,omitempty"`     // duration string, default "30m"
	ActiveHours string `json:"active_hours,omitempty"` // "HH:MM-HH:MM", e.g. "09:00-18:00"
	Timezone    string `json:"timezone,omitempty"`     // IANA timezone, default local
}

// CronConfig holds cron scheduler configuration.
type CronConfig struct {
	Enabled bool `json:"enabled,omitempty"` // default true when jobs exist
}

// RCConfig holds RingCentral connection configuration.
type RCConfig struct {
	ClientID       string   `json:"client_id,omitempty"`
	ClientSecret   string   `json:"client_secret,omitempty"`
	JWTToken       string   `json:"jwt_token,omitempty"`
	ChatIDs        []string `json:"chat_ids,omitempty"`
	SourceUserIDs  []string `json:"source_user_ids,omitempty"`
	ServerURL      string   `json:"server_url,omitempty"`
	BotToken       string   `json:"bot_token,omitempty"`
	BotMentionOnly *bool    `json:"bot_mention_only,omitempty"`

	GroupSummaryGroupID      string `json:"group_summary_group_id,omitempty"`
	GroupSummaryMessageLimit int    `json:"group_summary_message_limit,omitempty"`
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

const defaultGroupSummaryMessageLimit = 200

// GroupSummaryLimit returns the configured summarize message limit.
// Defaults to 200 when unset or invalid.
func (rc RCConfig) GroupSummaryLimit() int {
	if rc.GroupSummaryMessageLimit <= 0 {
		return defaultGroupSummaryMessageLimit
	}
	return rc.GroupSummaryMessageLimit
}

// GroupSummaryGroup returns the configured group ID that is allowed to use
// current-group summarize.
func (rc RCConfig) GroupSummaryGroup() string {
	return strings.TrimSpace(rc.GroupSummaryGroupID)
}

// HasGroupSummary returns whether current-group summarize is enabled by config.
func (rc RCConfig) HasGroupSummary() bool {
	return rc.GroupSummaryGroup() != ""
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
	MaxHistory   int               `json:"max_history,omitempty"`   // max history messages (http type, openai format)
	Format       string            `json:"format,omitempty"`        // HTTP API format: "openai" (default) or "nanoclaw"
	Sender       string            `json:"sender,omitempty"`        // sender name (http type, nanoclaw format)
	ContextMode  string            `json:"context_mode,omitempty"`  // context mode (http type, nanoclaw format)
	GroupJID     string            `json:"group_jid,omitempty"`     // group JID (http type, nanoclaw format)
	Timeout      int               `json:"timeout,omitempty"`       // HTTP timeout in seconds (http type)
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
	if v := os.Getenv("RINGCLAW_AGENT_WORKSPACE"); v != "" {
		cfg.AgentWorkspace = v
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
	if v := os.Getenv("RC_CHAT_IDS"); v != "" {
		parts := strings.Split(v, ",")
		chatIDs := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				chatIDs = append(chatIDs, part)
			}
		}
		cfg.RC.ChatIDs = chatIDs
	}
	if v := os.Getenv("RC_SOURCE_USER_IDS"); v != "" {
		parts := strings.Split(v, ",")
		userIDs := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				userIDs = append(userIDs, part)
			}
		}
		cfg.RC.SourceUserIDs = userIDs
	}
	if v := os.Getenv("RC_BOT_TOKEN"); v != "" {
		cfg.RC.BotToken = v
	}
	if v := os.Getenv("RC_BOT_MENTION_ONLY"); v != "" {
		value := strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes")
		cfg.RC.BotMentionOnly = &value
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
