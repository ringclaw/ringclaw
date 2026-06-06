package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/ringclaw/ringclaw/messaging/persona"
	"github.com/ringclaw/ringclaw/paths"
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
	Bot            BotConfig `json:"bot,omitempty"`
	DefaultAgent   string    `json:"default_agent"`
	AgentWorkspace string    `json:"agent_workspace,omitempty"`
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
	Mesh                    MeshConfig             `json:"mesh,omitempty"`
	Heartbeat               HeartbeatConfig        `json:"heartbeat,omitempty"`
	Cron                    CronConfig             `json:"cron,omitempty"`
	OpenclawGateway         OpenclawGatewayConfig  `json:"openclaw_gateway,omitempty"`
	// FullAccessAck acknowledges that ACP agents with `full_access: true`
	// will execute MCP tool calls without per-call approval. When nil
	// (omitted), this is treated as false. config.json is the sole
	// source. See docs/security/index.md "ACP Full-Access Mode".
	FullAccessAck *bool `json:"full_access_ack,omitempty"`

	// Persona holds the SOUL + layered MEMORY configuration. Zero
	// value is valid (the feature defaults to enabled with stock
	// paths); see messaging/persona for the full resolution logic.
	Persona persona.Config `json:"persona,omitempty"`
	// BotContent carries runtime-provisioned bot brain artifacts that
	// should be materialized locally before the bot starts.
	BotContent BotContentConfig `json:"bot_content,omitempty"`
}

// MeshConfig enables AVA Control Plane Agent Mesh task polling for managed
// runtimes. The runtime identity comes from the Control Plane claim config.
type MeshConfig struct {
	Enabled         bool                          `json:"enabled,omitempty"`
	ControlPlaneURL string                        `json:"control_plane_url,omitempty"`
	AgentID         string                        `json:"agent_id,omitempty"`
	RoleID          string                        `json:"role_id,omitempty"`
	RoleName        string                        `json:"role_name,omitempty"`
	PollInterval    string                        `json:"poll_interval,omitempty"`
	AllowedActions  []string                      `json:"allowed_actions,omitempty"`
	RolePeers       map[string]MeshRolePeerConfig `json:"role_peers,omitempty"`
}

type MeshRolePeerConfig struct {
	RoleID        string   `json:"role_id"`
	RoleName      string   `json:"role_name,omitempty"`
	BotID         string   `json:"bot_id,omitempty"`
	DisplayName   string   `json:"display_name,omitempty"`
	ExtensionID   string   `json:"extension_id,omitempty"`
	PersonID      string   `json:"person_id,omitempty"`
	SharedChatIDs []string `json:"shared_chat_ids,omitempty"`
}

type BotContentConfig struct {
	SoulMarkdown string `json:"soul_markdown,omitempty"`
}

// BotConfig identifies a long-lived RingClaw bot runtime. In Kubernetes,
// these fields separate multiple bot pods that may share the same image,
// agent gateway, and external model provider.
type BotConfig struct {
	ID                    string `json:"id,omitempty"`
	TenantID              string `json:"tenant_id,omitempty"`
	OwnerUserID           string `json:"owner_user_id,omitempty"`
	ConversationNamespace string `json:"conversation_namespace,omitempty"`
}

// EffectiveConversationNamespace returns the namespace used to isolate agent
// sessions across bots. It is intentionally independent from RingCentral chat
// IDs so multiple tenants/accounts can share one external AI gateway safely.
func (b BotConfig) EffectiveConversationNamespace() string {
	if ns := strings.TrimSpace(b.ConversationNamespace); ns != "" {
		return ns
	}
	id := strings.TrimSpace(b.ID)
	tenant := strings.TrimSpace(b.TenantID)
	switch {
	case tenant != "" && id != "":
		return tenant + "/" + id
	case id != "":
		return id
	default:
		return ""
	}
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
	// AllowAllSenders restores the legacy sender trust model: any sender
	// that passes the chat gate may drive the bot. This is intentionally
	// explicit because it bypasses source_user_ids and chat_user_allow.
	AllowAllSenders *bool `json:"allow_all_senders,omitempty"`
	// Capabilities records optional Product/AVA capabilities this runtime is
	// expected to support. It is advisory metadata for onboarding, K8S
	// rendering, and operator checks; actual enforcement remains the selected
	// RingCentral app scopes and user permissions.
	Capabilities []string `json:"capabilities,omitempty"`
	ServerURL    string   `json:"server_url,omitempty"`
	// VideoServerURL is the RingCentral Video API server. When empty,
	// RingClaw derives it from ServerURL for known production hosts.
	VideoServerURL string `json:"video_server_url,omitempty"`
	BotToken       string `json:"bot_token,omitempty"`

	// GroupMentionOnly, when true (default), makes the bot only
	// respond to messages where it is @mentioned in group chats. Bot
	// DMs always respond regardless. Rename of the legacy
	// bot_mention_only field; see the BotMentionOnly comment below.
	GroupMentionOnly *bool `json:"group_mention_only,omitempty"`

	// AllowUnlistedGroupChats, when true, bypasses the chat_ids gate
	// for any non-bot-DM chat. This lets operators keep chat_ids for
	// explicit routing while still accepting messages from group-like
	// chats the bot has already joined.
	AllowUnlistedGroupChats *bool `json:"allow_unlisted_group_chats,omitempty"`

	// Deprecated: renamed to GroupMentionOnly. The bot_mention_only
	// JSON field is still accepted for backward compatibility —
	// Load() copies it into GroupMentionOnly and emits a WARN so
	// operators know to rename it. Remove in a future release.
	BotMentionOnly *bool `json:"bot_mention_only,omitempty"`

	GroupSummaryGroupID      string `json:"group_summary_group_id,omitempty"`
	GroupSummaryMessageLimit int    `json:"group_summary_message_limit,omitempty"`

	// AllowGroupMentionAuthorize controls the OOB approval flow for
	// non-trusted group mentions. When a user not on the sender
	// allowlist @mentions the bot in an allowed group chat, instead
	// of silently dropping the message the bot posts a `/approval`
	// challenge to the owner DM. On approval the requester is added
	// to ChatUserAllow for that chat only and persisted to
	// config.json.
	//
	// SECURITY ADVISORY (v0.4.2): the default is reverted to false
	// (nil/unset = OFF) because OOB approval grants the requester
	// the bot's full agent capability — including filesystem access,
	// terminal commands, and external HTTP — via the agent tool-call
	// channel. v0.4.1 had defaulted this on; v0.4.2 reverts to the
	// v0.4.0 behavior while v0.5.0 introduces a restricted agent
	// backend for non-owner senders.
	//
	// Requires Private App + resolved owner DM when explicitly set.
	// Without those, the feature is disabled at startup with an
	// ERROR log.
	AllowGroupMentionAuthorize *bool `json:"allow_group_mention_authorize,omitempty"`

	// ChatUserAllow maps chat ID -> additional trusted user
	// identifiers allowed to drive the bot in THAT chat only.
	// Entries may be numeric extension IDs, email addresses, or
	// E.164 phone numbers; non-numeric forms are resolved to numeric
	// IDs at startup via the Private App directory (same path used
	// by SourceUserIDs). Populated automatically by the
	// authorize-mention OOB flow; operators may also pre-seed it by
	// hand. Empty (or absent chat key) means "no per-chat
	// exception".
	//
	// SECURITY ADVISORY (v0.4.2): listed users gain the bot's full
	// agent capability in their authorized chats — including
	// filesystem access (List, Read, Write), terminal commands, and
	// external HTTP — through the agent tool-call channel. v0.4.2
	// force-clears this map at startup and emits an ERROR log so
	// operators must consciously re-add entries. v0.5.0 will route
	// these users through a restricted agent backend that allows
	// only text replies and RingCentral ACTION blocks.
	ChatUserAllow map[string][]string `json:"chat_user_allow,omitempty"`
}

// IsAuthorizeMentionEnabled reports whether the authorize-mention OOB
// flow is on. v0.4.1 defaulted this to true; v0.4.2 reverts to the
// v0.4.0 behavior of default-OFF because the OOB grant carries the
// bot's full agent capability (FS / terminal / web). v0.5.0 will
// reintroduce a default-ON path once the restricted agent backend
// lands.
//
// Operators who want the OOB flow today must set the field to true
// explicitly and accept that approved users gain full agent
// capability in their authorized chats.
//
// The runtime requirements (Private App + resolved owner DM) are
// validated in cmd/start.go; when they are missing the feature is
// disabled with an ERROR log.
func (rc RCConfig) IsAuthorizeMentionEnabled() bool {
	if rc.AllowGroupMentionAuthorize == nil {
		return false
	}
	return *rc.AllowGroupMentionAuthorize
}

// IsAuthorizeMentionExplicit reports whether the operator set the
// allow_group_mention_authorize field explicitly (regardless of
// value). Retained for forward compatibility with v0.5.0 where the
// log level may again differ between explicit-on and defaulted-on.
// In v0.4.2 the default is OFF, so explicit-on is the only "on"
// path.
func (rc RCConfig) IsAuthorizeMentionExplicit() bool {
	return rc.AllowGroupMentionAuthorize != nil
}

// AddChatUserAllow appends a trusted identifier (email / numeric ID /
// phone number) to ChatUserAllow[chatID], deduplicating by
// case-insensitive equality. Returns true when a new entry was
// appended, false when the identifier was already present.
//
// Initializes the map lazily so callers can use the helper on a
// freshly loaded zero-value Config. The caller is responsible for
// calling Save afterwards if persistence is desired.
func (rc *RCConfig) AddChatUserAllow(chatID, identifier string) bool {
	chatID = strings.TrimSpace(chatID)
	identifier = strings.TrimSpace(identifier)
	if chatID == "" || identifier == "" {
		return false
	}
	if rc.ChatUserAllow == nil {
		rc.ChatUserAllow = make(map[string][]string)
	}
	for _, e := range rc.ChatUserAllow[chatID] {
		if strings.EqualFold(e, identifier) {
			return false
		}
	}
	rc.ChatUserAllow[chatID] = append(rc.ChatUserAllow[chatID], identifier)
	return true
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

// IsAllowUnlistedGroupChats reports whether the monitor should accept
// messages from chats outside chat_ids as long as they are not the
// bot's own DM chat. Defaults to false.
func (rc RCConfig) IsAllowUnlistedGroupChats() bool {
	if rc.AllowUnlistedGroupChats == nil {
		return false
	}
	return *rc.AllowUnlistedGroupChats
}

// IsAllowAllSenders reports whether the runtime should allow any sender that
// passes the chat gate to drive the bot. Defaults to false.
func (rc RCConfig) IsAllowAllSenders() bool {
	if rc.AllowAllSenders == nil {
		return false
	}
	return *rc.AllowAllSenders
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
	Type       string            `json:"type"`                  // "acp", "cli", or "http"
	Command    string            `json:"command,omitempty"`     // binary path (cli/acp type)
	Args       []string          `json:"args,omitempty"`        // extra args for command (e.g. ["acp"] for cursor)
	Aliases    []string          `json:"aliases,omitempty"`     // custom trigger commands (e.g. ["gpt", "4o"])
	Cwd        string            `json:"cwd,omitempty"`         // working directory (workspace)
	Env        map[string]string `json:"env,omitempty"`         // extra environment variables (cli/acp type)
	AllowWrite bool              `json:"allow_write,omitempty"` // grant file write permission to ACP agent (default: false)
	FullAccess bool              `json:"full_access,omitempty"` // call session/set_mode "full-access" on ACP session creation

	// RestrictedModeID overrides the built-in agent → restricted
	// modeId map that ringclaw uses for non-owner senders. When
	// empty (default), the agent layer picks `spec` for droid and
	// `plan` for claude / gemini / qwen / cursor (with a heuristic
	// fallback for unknown agents). When non-empty, the value MUST
	// be one of the modes the agent advertises in its
	// `availableModes` list; otherwise the override is ignored and
	// ringclaw falls back to the built-in selection.
	//
	// SECURITY NOTE (v0.4.3): ringclaw protects non-owner sessions
	// via TWO layers — (1) the ACP `session/set_mode` call covered
	// by this field, and (2) a fail-closed client-side gate that
	// denies every fs/* and terminal/* tool call from non-owner
	// sessions regardless of the agent's mode behavior. WebFetch /
	// WebSearch / MCP tools are dispatched directly by the agent
	// process and therefore rely on Layer 1 alone — see
	// docs/security/sender-allowlist.md for the limitation
	// statement and the v0.5.0 OS-sandbox roadmap that closes it.
	RestrictedModeID string `json:"restricted_mode_id,omitempty"`

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
	return paths.ConfigFile()
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
	return SaveTo(path, cfg)
}

// SaveTo saves the configuration to an explicit path. This is used by
// non-interactive Kubernetes onboarding flows that render config into a
// projected volume instead of the default ~/.ringclaw/config.json.
func SaveTo(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return os.WriteFile(path, data, 0o600)
}
