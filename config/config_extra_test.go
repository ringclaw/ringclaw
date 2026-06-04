package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_MissingFile_ReturnsDefault(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Agents == nil {
		t.Error("expected non-nil agents map")
	}
	if cfg.DefaultAgent != "" {
		t.Errorf("expected empty default agent, got %q", cfg.DefaultAgent)
	}
}

func TestLoad_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)

	testCfg := &Config{
		DefaultAgent:   "claude",
		AgentWorkspace: "/tmp/ws",
		APIAddr:        "127.0.0.1:9999",
		LogLevel:       "debug",
		Agents: map[string]AgentConfig{
			"claude": {Type: "acp", Command: "/usr/bin/claude", Model: "sonnet"},
		},
		RC: RCConfig{
			ClientID:     "cid",
			ClientSecret: "csec",
			JWTToken:     "jwt",
			BotToken:     "bot-tok",
			ServerURL:    "https://rc.example.com",
			ChatIDs:      []string{"c1", "c2"},
		},
		Heartbeat: HeartbeatConfig{Enabled: true, Interval: "1h"},
	}
	data, _ := json.MarshalIndent(testCfg, "", "  ")
	os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.DefaultAgent != "claude" {
		t.Errorf("DefaultAgent = %q, want claude", cfg.DefaultAgent)
	}
	if cfg.AgentWorkspace != "/tmp/ws" {
		t.Errorf("AgentWorkspace = %q, want /tmp/ws", cfg.AgentWorkspace)
	}
	if cfg.APIAddr != "127.0.0.1:9999" {
		t.Errorf("APIAddr = %q", cfg.APIAddr)
	}
	if len(cfg.Agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(cfg.Agents))
	}
	if cfg.RC.ClientID != "cid" {
		t.Errorf("ClientID = %q", cfg.RC.ClientID)
	}
	if !cfg.Heartbeat.Enabled {
		t.Error("expected heartbeat enabled")
	}
}

func TestLoad_MalformedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("{invalid json"), 0o600)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestLoad_NilAgentsInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"default_agent":"test"}`), 0o600)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Agents == nil {
		t.Error("expected agents map to be initialized")
	}
}

// TestLoad_EnvVarsIgnored verifies that previously supported env vars are
// now silently ignored and never influence Load() when config.json exists.
func TestLoad_EnvVarsIgnored(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	payload := `{
		"default_agent": "json-agent",
		"agent_workspace": "/json/ws",
		"api_addr": "127.0.0.1:3333",
		"ringcentral": {
			"client_id": "json-cid",
			"bot_token": "json-bot",
			"chat_ids": ["json-c1"]
		}
	}`
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(payload), 0o600)

	t.Setenv("RINGCLAW_DEFAULT_AGENT", "codex")
	t.Setenv("RINGCLAW_AGENT_WORKSPACE", "/env/workspace")
	t.Setenv("RINGCLAW_AGENT_ALLOW_WORKSPACE_LIST", "/env/a,/env/b")
	t.Setenv("RINGCLAW_API_ADDR", "127.0.0.1:7777")
	t.Setenv("RC_CLIENT_ID", "env-cid")
	t.Setenv("RC_CLIENT_SECRET", "env-csec")
	t.Setenv("RC_JWT_TOKEN", "env-jwt")
	t.Setenv("RC_SERVER_URL", "https://env.example.com")
	t.Setenv("RC_CHAT_IDS", "ec1, ec2")
	t.Setenv("RC_SOURCE_USER_IDS", "u1, u2, u3")
	t.Setenv("RC_BOT_TOKEN", "env-bot")
	t.Setenv("RC_BOT_MENTION_ONLY", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.DefaultAgent != "json-agent" {
		t.Errorf("DefaultAgent: env must be ignored, got %q", cfg.DefaultAgent)
	}
	if cfg.AgentWorkspace != "/json/ws" {
		t.Errorf("AgentWorkspace: env must be ignored, got %q", cfg.AgentWorkspace)
	}
	if len(cfg.AgentAllowWorkspaceList) != 0 {
		t.Errorf("AgentAllowWorkspaceList: env must be ignored, got %v", cfg.AgentAllowWorkspaceList)
	}
	if cfg.APIAddr != "127.0.0.1:3333" {
		t.Errorf("APIAddr: env must be ignored, got %q", cfg.APIAddr)
	}
	if cfg.RC.ClientID != "json-cid" {
		t.Errorf("RC.ClientID: env must be ignored, got %q", cfg.RC.ClientID)
	}
	if cfg.RC.ClientSecret != "" {
		t.Errorf("RC.ClientSecret: env must be ignored, got %q", cfg.RC.ClientSecret)
	}
	if cfg.RC.ServerURL != "" {
		t.Errorf("RC.ServerURL: env must be ignored, got %q", cfg.RC.ServerURL)
	}
	if len(cfg.RC.ChatIDs) != 1 || cfg.RC.ChatIDs[0] != "json-c1" {
		t.Errorf("RC.ChatIDs: env must be ignored, got %v", cfg.RC.ChatIDs)
	}
	if len(cfg.RC.SourceUserIDs) != 0 {
		t.Errorf("RC.SourceUserIDs: env must be ignored, got %v", cfg.RC.SourceUserIDs)
	}
	if cfg.RC.BotToken != "json-bot" {
		t.Errorf("RC.BotToken: env must be ignored, got %q", cfg.RC.BotToken)
	}
	if cfg.RC.GroupMentionOnly != nil {
		t.Errorf("RC.GroupMentionOnly: env must be ignored, got %#v", cfg.RC.GroupMentionOnly)
	}
	if cfg.RC.BotMentionOnly != nil {
		t.Errorf("RC.BotMentionOnly: Load must normalize the deprecated field to nil, got %#v", cfg.RC.BotMentionOnly)
	}
}

func TestSave_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	original := &Config{
		DefaultAgent:   "kimi",
		AgentWorkspace: "/ws",
		APIAddr:        "127.0.0.1:8080",
		LogLevel:       "warn",
		LogFormat:      "json",
		Agents: map[string]AgentConfig{
			"kimi": {Type: "acp", Command: "/usr/bin/kimi", Args: []string{"acp"}, Model: "kimi-v1"},
			"http-agent": {
				Type:     "http",
				Endpoint: "https://api.example.com/v1",
				APIKey:   "key123",
				Headers:  map[string]string{"X-Custom": "val"},
			},
		},
		RC: RCConfig{
			ClientID:                 "c1",
			ClientSecret:             "s1",
			JWTToken:                 "j1",
			ServerURL:                "https://rc.example.com",
			ChatIDs:                  []string{"chat1"},
			GroupSummaryGroupID:      "g1",
			GroupSummaryMessageLimit: 100,
		},
		Heartbeat: HeartbeatConfig{Enabled: true, Interval: "15m", ActiveHours: "09:00-17:00"},
	}

	if err := Save(original); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if loaded.DefaultAgent != original.DefaultAgent {
		t.Errorf("DefaultAgent: got %q, want %q", loaded.DefaultAgent, original.DefaultAgent)
	}
	if loaded.LogLevel != "warn" {
		t.Errorf("LogLevel: got %q", loaded.LogLevel)
	}
	if loaded.LogFormat != "json" {
		t.Errorf("LogFormat: got %q", loaded.LogFormat)
	}
	if len(loaded.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(loaded.Agents))
	}
	kimi := loaded.Agents["kimi"]
	if kimi.Type != "acp" || kimi.Command != "/usr/bin/kimi" {
		t.Errorf("kimi agent: %+v", kimi)
	}
	if len(kimi.Args) != 1 || kimi.Args[0] != "acp" {
		t.Errorf("kimi args: %v", kimi.Args)
	}
	httpAg := loaded.Agents["http-agent"]
	if httpAg.Endpoint != "https://api.example.com/v1" {
		t.Errorf("http endpoint: %q", httpAg.Endpoint)
	}
	if loaded.RC.GroupSummaryGroupID != "g1" {
		t.Errorf("GroupSummaryGroupID: %q", loaded.RC.GroupSummaryGroupID)
	}
	if loaded.Heartbeat.ActiveHours != "09:00-17:00" {
		t.Errorf("ActiveHours: %q", loaded.Heartbeat.ActiveHours)
	}
}

func TestSave_CreatesDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := DefaultConfig()
	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	path := filepath.Join(tmpDir, ".ringclaw", "config.json")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config file not created: %v", err)
	}
}

func TestConfigPath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	p, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath() error: %v", err)
	}
	expected := filepath.Join(tmpDir, ".ringclaw", "config.json")
	if p != expected {
		t.Errorf("ConfigPath() = %q, want %q", p, expected)
	}
}

func TestConfigPath_EnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "bot-a", "config.json")
	t.Setenv("RINGCLAW_CONFIG", override)

	p, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath() error: %v", err)
	}
	if p != override {
		t.Fatalf("ConfigPath() = %q, want %q", p, override)
	}
}

func TestBotConfigEffectiveConversationNamespace(t *testing.T) {
	cases := []struct {
		name string
		bot  BotConfig
		want string
	}{
		{"explicit", BotConfig{ConversationNamespace: "custom"}, "custom"},
		{"tenant and bot", BotConfig{TenantID: "tenant", ID: "bot"}, "tenant/bot"},
		{"bot only", BotConfig{ID: "bot"}, "bot"},
		{"empty", BotConfig{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.bot.EffectiveConversationNamespace(); got != tc.want {
				t.Fatalf("EffectiveConversationNamespace() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsGroupMentionOnly_Default(t *testing.T) {
	rc := RCConfig{}
	if !rc.IsGroupMentionOnly() {
		t.Error("expected default IsGroupMentionOnly=true")
	}
}

func TestIsGroupMentionOnly_ExplicitTrue(t *testing.T) {
	v := true
	rc := RCConfig{GroupMentionOnly: &v}
	if !rc.IsGroupMentionOnly() {
		t.Error("expected IsGroupMentionOnly=true")
	}
}

func TestIsGroupMentionOnly_ExplicitFalse(t *testing.T) {
	v := false
	rc := RCConfig{GroupMentionOnly: &v}
	if rc.IsGroupMentionOnly() {
		t.Error("expected IsGroupMentionOnly=false")
	}
}

func TestIsAllowUnlistedGroupChats_Default(t *testing.T) {
	rc := RCConfig{}
	if rc.IsAllowUnlistedGroupChats() {
		t.Error("expected default IsAllowUnlistedGroupChats=false")
	}
}

func TestIsAllowUnlistedGroupChats_ExplicitTrue(t *testing.T) {
	v := true
	rc := RCConfig{AllowUnlistedGroupChats: &v}
	if !rc.IsAllowUnlistedGroupChats() {
		t.Error("expected IsAllowUnlistedGroupChats=true")
	}
}

func TestIsAllowUnlistedGroupChats_ExplicitFalse(t *testing.T) {
	v := false
	rc := RCConfig{AllowUnlistedGroupChats: &v}
	if rc.IsAllowUnlistedGroupChats() {
		t.Error("expected IsAllowUnlistedGroupChats=false")
	}
}

// TestLoad_DeprecatedBotMentionOnly_IsMigrated confirms that a
// config.json written before the rename still boots and that Load
// transparently migrates `bot_mention_only` into `group_mention_only`,
// clearing the legacy field so the next Save writes only the new
// name.
func TestLoad_DeprecatedBotMentionOnly_IsMigrated(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := tmpDir + "/.ringclaw"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `{"ringcentral": {"bot_token": "t", "chat_ids": ["c"], "bot_mention_only": false}}`
	if err := os.WriteFile(dir+"/config.json", []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RC.BotMentionOnly != nil {
		t.Errorf("Load must clear the deprecated BotMentionOnly after migration; got %#v", cfg.RC.BotMentionOnly)
	}
	if cfg.RC.GroupMentionOnly == nil || *cfg.RC.GroupMentionOnly {
		t.Errorf("Load must copy bot_mention_only into GroupMentionOnly; got %#v", cfg.RC.GroupMentionOnly)
	}
	if cfg.RC.IsGroupMentionOnly() {
		t.Error("IsGroupMentionOnly should be false (migrated from bot_mention_only=false)")
	}
}

// TestLoad_NewFieldWinsOverDeprecated confirms that when both
// `group_mention_only` and the legacy `bot_mention_only` are set,
// Load keeps the new field and discards the old one.
func TestLoad_NewFieldWinsOverDeprecated(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := tmpDir + "/.ringclaw"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Legacy says false, new says true — new must win.
	content := `{"ringcentral": {"bot_token": "t", "chat_ids": ["c"], "bot_mention_only": false, "group_mention_only": true}}`
	if err := os.WriteFile(dir+"/config.json", []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RC.BotMentionOnly != nil {
		t.Errorf("Load must clear the deprecated BotMentionOnly; got %#v", cfg.RC.BotMentionOnly)
	}
	if cfg.RC.GroupMentionOnly == nil || !*cfg.RC.GroupMentionOnly {
		t.Errorf("GroupMentionOnly must remain true; got %#v", cfg.RC.GroupMentionOnly)
	}
}

// TestLoad_PersonaConfig_ParsesAllFields confirms every field under
// the `persona` block in config.json is decoded into the expected
// Go value. This is the regression net for the persona feature — if
// the JSON tag on any field drifts, one of these assertions fails.
func TestLoad_PersonaConfig_ParsesAllFields(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := tmpDir + "/.ringclaw"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `{
		"ringcentral": {"bot_token": "t", "chat_ids": ["c"]},
		"persona": {
			"enabled": false,
			"soul_file": "/custom/SOUL.md",
			"memory_dir": "/custom/memory",
			"max_soul_chars": 1111,
			"max_chat_memory_chars": 2222,
			"max_user_memory_chars": 3333,
			"max_global_memory_chars": 4444
		}
	}`
	if err := os.WriteFile(dir+"/config.json", []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Persona.Enabled == nil || *cfg.Persona.Enabled {
		t.Errorf("Persona.Enabled: want false, got %#v", cfg.Persona.Enabled)
	}
	if cfg.Persona.SoulFile != "/custom/SOUL.md" {
		t.Errorf("Persona.SoulFile = %q", cfg.Persona.SoulFile)
	}
	if cfg.Persona.MemoryDir != "/custom/memory" {
		t.Errorf("Persona.MemoryDir = %q", cfg.Persona.MemoryDir)
	}
	if cfg.Persona.MaxSoulChars != 1111 {
		t.Errorf("Persona.MaxSoulChars = %d", cfg.Persona.MaxSoulChars)
	}
	if cfg.Persona.MaxChatMemoryChars != 2222 {
		t.Errorf("Persona.MaxChatMemoryChars = %d", cfg.Persona.MaxChatMemoryChars)
	}
	if cfg.Persona.MaxUserMemoryChars != 3333 {
		t.Errorf("Persona.MaxUserMemoryChars = %d", cfg.Persona.MaxUserMemoryChars)
	}
	if cfg.Persona.MaxGlobalMemoryChars != 4444 {
		t.Errorf("Persona.MaxGlobalMemoryChars = %d", cfg.Persona.MaxGlobalMemoryChars)
	}
}

// TestLoad_PersonaConfig_DefaultsWhenAbsent confirms that an absent
// `persona` block yields the zero value — Config.Persona.IsEnabled()
// still returns true (feature-on-by-default), and Resolved() fills
// paths with stock locations.
func TestLoad_PersonaConfig_DefaultsWhenAbsent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := tmpDir + "/.ringclaw"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `{"ringcentral": {"bot_token": "t", "chat_ids": ["c"]}}`
	if err := os.WriteFile(dir+"/config.json", []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Persona.IsEnabled() {
		t.Error("Persona.IsEnabled should default to true when block absent")
	}
	resolved := cfg.Persona.Resolved()
	if resolved.MaxSoulChars == 0 || resolved.MaxChatMemoryChars == 0 {
		t.Errorf("Resolved must populate caps from defaults: %#v", resolved)
	}
	if resolved.SoulFile == "" || resolved.MemoryDir == "" {
		t.Errorf("Resolved must populate paths: %#v", resolved)
	}
}

func TestGroupSummaryGroup_Whitespace(t *testing.T) {
	rc := RCConfig{GroupSummaryGroupID: "  g1  "}
	if got := rc.GroupSummaryGroup(); got != "g1" {
		t.Errorf("GroupSummaryGroup() = %q, want g1", got)
	}
}

func TestGroupSummaryGroup_Empty(t *testing.T) {
	rc := RCConfig{GroupSummaryGroupID: "  "}
	if rc.HasGroupSummary() {
		t.Error("expected HasGroupSummary=false for whitespace-only")
	}
}

func TestGroupSummaryLimit_Zero(t *testing.T) {
	rc := RCConfig{GroupSummaryMessageLimit: 0}
	if got := rc.GroupSummaryLimit(); got != 200 {
		t.Errorf("GroupSummaryLimit() = %d, want 200", got)
	}
}

func TestGroupSummaryLimit_Negative(t *testing.T) {
	rc := RCConfig{GroupSummaryMessageLimit: -10}
	if got := rc.GroupSummaryLimit(); got != 200 {
		t.Errorf("GroupSummaryLimit() = %d, want 200", got)
	}
}

func TestBuildAliasMap(t *testing.T) {
	agents := map[string]AgentConfig{
		"claude": {Aliases: []string{"c", "sonnet"}},
		"codex":  {Aliases: []string{"cx"}},
		"kimi":   {},
	}
	m := BuildAliasMap(agents)
	if m["c"] != "claude" {
		t.Errorf("alias 'c' = %q, want claude", m["c"])
	}
	if m["sonnet"] != "claude" {
		t.Errorf("alias 'sonnet' = %q, want claude", m["sonnet"])
	}
	if m["cx"] != "codex" {
		t.Errorf("alias 'cx' = %q, want codex", m["cx"])
	}
	if _, ok := m["kimi"]; ok {
		t.Error("kimi has no aliases, should not appear in map")
	}
}

func TestBuildAliasMap_Empty(t *testing.T) {
	m := BuildAliasMap(map[string]AgentConfig{})
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

func TestRedact(t *testing.T) {
	if got := redact(""); got != "" {
		t.Errorf("redact empty = %q, want empty", got)
	}
	if got := redact("secret"); got != "***" {
		t.Errorf("redact non-empty = %q, want ***", got)
	}
}

func TestRCConfigLogValue_EmptyFields(t *testing.T) {
	rc := RCConfig{}
	v := rc.LogValue()
	s := v.String()
	if s == "" {
		t.Error("expected non-empty log value string")
	}
}

func TestLoad_FileReadError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	cfgPath := filepath.Join(dir, "config.json")
	os.MkdirAll(cfgPath, 0o700)

	_, err := Load()
	if err == nil {
		t.Error("expected error when config.json is a directory")
	}
}
