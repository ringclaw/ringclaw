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
	DefaultAgent   string `json:"default_agent"`
	AgentWorkspace string `json:"agent_workspace,omitempty"`
	// AgentAllowWorkspaceList is the list of directory roots that /cwd
	// and Agent.SetCwd are allowed to target. ~/.ringclaw/workspace and
	// the legacy AgentWorkspace are always implicitly merged in by
	// cmd/start so the agent's default cwd is admissible. See
	// docs/security/index.md "Workspace Path Restrictions".
	AgentAllowWorkspaceList []string               `json:"agent_allow_workspace_list,omitempty"`
	APIAddr                 string                 `json:"api_addr,omitempty"`
	LogLevel                string                 `json:"log_level,omitempty"`  // "debug", "info" (default), "warn", "error"
	LogFormat               string                 `json:"log_format,omitempty"` // "text" (default), "json", "color"
	Agents                  map[string]AgentConfig `json:"agents"`
	RC                      RCConfig               `json:"ringcentral,omitempty"`
	Heartbeat               HeartbeatConfig        `json:"heartbeat,omitempty"`
	Cron                    CronConfig             `json:"cron,omitempty"`
	OpenclawGateway         OpenclawGatewayConfig  `json:"openclaw_gateway,omitempty"`
	// FullAccessAck acknowledges that ACP agents with `full_access: true`
	// will execute MCP tool calls without per-call approval. When nil
	// (omitted), this is treated as false. config.json is the sole
	// source. See docs/security/index.md "ACP Full-Access Mode".
	FullAccessAck *bool `json:"full_access_ack,omitempty"`
}

// OpenclawGatewayConfig holds the connection info for the external openclaw
// gateway consumed by the auto-detected `openclaw` agent. When URL is empty,
// ringclaw falls back to reading ~/.openclaw/openclaw.json.
type OpenclawGatewayConfig struct {
	URL      string `json:"url,omitempty"`
	Token    string `json:"token,omitempty"`
	Password string `json:"password,omitempty"`
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
	ClientID      string   `json:"client_id,omitempty"`
	ClientSecret  string   `json:"client_secret,omitempty"`
	JWTToken      string   `json:"jwt_token,omitempty"`
	ChatIDs       []string `json:"chat_ids,omitempty"`
	SourceUserIDs []string `json:"source_user_ids,omitempty"`
	ServerURL     string   `json:"server_url,omitempty"`
	BotToken      string   `json:"bot_token,omitempty"`

	// GroupMentionOnly, when true (default), makes the bot only
	// respond to messages where it is @mentioned in group chats. Bot
	// DMs always respond regardless. Rename of the legacy
	// bot_mention_only field; see the BotMentionOnly comment below.
	GroupMentionOnly *bool `json:"group_mention_only,omitempty"`

	// Deprecated: renamed to GroupMentionOnly. The bot_mention_only
	// JSON field is still accepted for backward compatibility —
	// Load() copies it into GroupMentionOnly and emits a WARN so
	// operators know to rename it. Remove in a future release.
	BotMentionOnly *bool `json:"bot_mention_only,omitempty"`

	GroupSummaryGroupID      string `json:"group_summary_group_id,omitempty"`
	GroupSummaryMessageLimit int    `json:"group_summary_message_limit,omitempty"`
}

// LogValue implements slog.LogValuer to redact sensitive fields.
func (rc RCConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("client_id", rc.ClientID),
		slog.String("client_secret", redact(rc.ClientSecret)),
		slog.String("jwt_token", redact(rc.JWTToken)),
		slog.String("bot_token", redact(rc.BotToken)),
		slog.String("server_url", rc.ServerURL),
		slog.Int("chat_ids_count", len(rc.ChatIDs)),
	)
}

func redact(s string) string {
	if s == "" {
		return ""
	}
	return "***"
}

// HasPrivateApp returns true if all private app credentials are configured.
func (rc RCConfig) HasPrivateApp() bool {
	return rc.ClientID != "" && rc.ClientSecret != "" && rc.JWTToken != ""
}

// IsGroupMentionOnly returns whether the bot requires @mention in
// group chats. Defaults to true if not explicitly set. Bot DMs are
// not affected by this flag — DMs always receive every message.
//
// Load() normalizes the deprecated bot_mention_only field into
// GroupMentionOnly, so callers reading the parsed config can always
// rely on the new field.
func (rc RCConfig) IsGroupMentionOnly() bool {
	if rc.GroupMentionOnly == nil {
		return true
	}
	return *rc.GroupMentionOnly
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
	AllowWrite   bool              `json:"allow_write,omitempty"`   // grant file write permission to ACP agent (default: false)
	FullAccess   bool              `json:"full_access,omitempty"`   // call session/set_mode "full-access" on ACP session creation
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

// Load loads configuration from ~/.ringclaw/config.json. Environment
// variables are no longer consulted; config.json is the sole source.
func Load() (*Config, error) {
	cfg := DefaultConfig()

	path, err := ConfigPath()
	if err != nil {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
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
	normalizeDeprecatedFields(cfg)

	return cfg, nil
}

// normalizeDeprecatedFields folds legacy JSON field names into their
// current counterparts after a raw Unmarshal. Each branch emits a
// single WARN so operators see the rename once per startup and can
// migrate their config file; the next Save rewrites only the new
// field because the old one is cleared here.
func normalizeDeprecatedFields(cfg *Config) {
	if cfg.RC.BotMentionOnly != nil {
		if cfg.RC.GroupMentionOnly == nil {
			cfg.RC.GroupMentionOnly = cfg.RC.BotMentionOnly
			slog.Warn("config: `bot_mention_only` has been renamed to `group_mention_only`; migrated automatically (next save drops the old field)",
				"component", "config")
		} else {
			slog.Warn("config: both `group_mention_only` and the deprecated `bot_mention_only` are set; keeping `group_mention_only` and discarding `bot_mention_only`",
				"component", "config")
		}
		cfg.RC.BotMentionOnly = nil
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
