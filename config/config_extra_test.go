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
	// Clear env vars that loadEnv would pick up
	for _, k := range []string{
		"RINGCLAW_DEFAULT_AGENT", "RINGCLAW_AGENT_WORKSPACE", "RINGCLAW_API_ADDR",
		"RC_CLIENT_ID", "RC_CLIENT_SECRET", "RC_JWT_TOKEN", "RC_SERVER_URL",
		"RC_CHAT_IDS", "RC_SOURCE_USER_IDS", "RC_BOT_TOKEN", "RC_BOT_MENTION_ONLY",
	} {
		t.Setenv(k, "")
	}

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
	clearConfigEnv(t)

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
	clearConfigEnv(t)

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
	clearConfigEnv(t)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	// JSON with no "agents" key
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"default_agent":"test"}`), 0o600)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Agents == nil {
		t.Error("expected agents map to be initialized")
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("RINGCLAW_DEFAULT_AGENT", "codex")
	t.Setenv("RINGCLAW_AGENT_WORKSPACE", "/env/workspace")
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
	if cfg.DefaultAgent != "codex" {
		t.Errorf("DefaultAgent = %q, want codex", cfg.DefaultAgent)
	}
	if cfg.AgentWorkspace != "/env/workspace" {
		t.Errorf("AgentWorkspace = %q", cfg.AgentWorkspace)
	}
	if cfg.APIAddr != "127.0.0.1:7777" {
		t.Errorf("APIAddr = %q", cfg.APIAddr)
	}
	if cfg.RC.ClientID != "env-cid" {
		t.Errorf("ClientID = %q", cfg.RC.ClientID)
	}
	if cfg.RC.ServerURL != "https://env.example.com" {
		t.Errorf("ServerURL = %q", cfg.RC.ServerURL)
	}
	if len(cfg.RC.ChatIDs) != 2 || cfg.RC.ChatIDs[0] != "ec1" {
		t.Errorf("ChatIDs = %v", cfg.RC.ChatIDs)
	}
	if len(cfg.RC.SourceUserIDs) != 3 || cfg.RC.SourceUserIDs[0] != "u1" {
		t.Errorf("SourceUserIDs = %v", cfg.RC.SourceUserIDs)
	}
	if cfg.RC.BotToken != "env-bot" {
		t.Errorf("BotToken = %q", cfg.RC.BotToken)
	}
	if cfg.RC.BotMentionOnly == nil || !*cfg.RC.BotMentionOnly {
		t.Error("expected BotMentionOnly=true")
	}
}

func TestSave_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	clearConfigEnv(t)

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
			ClientID:                "c1",
			ClientSecret:            "s1",
			JWTToken:                "j1",
			ServerURL:               "https://rc.example.com",
			ChatIDs:                 []string{"chat1"},
			GroupSummaryGroupID:     "g1",
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

func TestIsBotMentionOnly_Default(t *testing.T) {
	rc := RCConfig{}
	if !rc.IsBotMentionOnly() {
		t.Error("expected default IsBotMentionOnly=true")
	}
}

func TestIsBotMentionOnly_ExplicitTrue(t *testing.T) {
	v := true
	rc := RCConfig{BotMentionOnly: &v}
	if !rc.IsBotMentionOnly() {
		t.Error("expected IsBotMentionOnly=true")
	}
}

func TestIsBotMentionOnly_ExplicitFalse(t *testing.T) {
	v := false
	rc := RCConfig{BotMentionOnly: &v}
	if rc.IsBotMentionOnly() {
		t.Error("expected IsBotMentionOnly=false")
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
	// Should not contain *** for empty fields
	if s == "" {
		t.Error("expected non-empty log value string")
	}
}

func TestLoadEnv_SourceUserIDs(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("RC_SOURCE_USER_IDS", "u1, u2")
	clearNonUserIDEnv(t)
	loadEnv(cfg)
	if len(cfg.RC.SourceUserIDs) != 2 {
		t.Fatalf("expected 2 source user IDs, got %d", len(cfg.RC.SourceUserIDs))
	}
	if cfg.RC.SourceUserIDs[0] != "u1" || cfg.RC.SourceUserIDs[1] != "u2" {
		t.Errorf("unexpected user IDs: %v", cfg.RC.SourceUserIDs)
	}
}

func TestLoadEnv_BotMentionOnly_Variants(t *testing.T) {
	tests := []struct {
		envVal string
		want   bool
	}{
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"1", true},
		{"yes", true},
		{"YES", true},
		{"false", false},
		{"0", false},
		{"no", false},
		{"anything", false},
	}
	for _, tt := range tests {
		t.Run(tt.envVal, func(t *testing.T) {
			cfg := DefaultConfig()
			t.Setenv("RC_BOT_MENTION_ONLY", tt.envVal)
			clearNonBotMentionEnv(t)
			loadEnv(cfg)
			if cfg.RC.BotMentionOnly == nil {
				t.Fatal("BotMentionOnly should be set")
			}
			if *cfg.RC.BotMentionOnly != tt.want {
				t.Errorf("envVal=%q: BotMentionOnly = %v, want %v", tt.envVal, *cfg.RC.BotMentionOnly, tt.want)
			}
		})
	}
}

func TestLoadEnv_ServerURL(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("RC_SERVER_URL", "https://custom.rc.example.com")
	clearNonServerURLEnv(t)
	loadEnv(cfg)
	if cfg.RC.ServerURL != "https://custom.rc.example.com" {
		t.Errorf("ServerURL = %q", cfg.RC.ServerURL)
	}
}

func TestLoad_FileReadError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	clearConfigEnv(t)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	// Create config.json as a directory to cause a read error
	cfgPath := filepath.Join(dir, "config.json")
	os.MkdirAll(cfgPath, 0o700)

	_, err := Load()
	if err == nil {
		t.Error("expected error when config.json is a directory")
	}
}

// helpers to clear env vars

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"RINGCLAW_DEFAULT_AGENT", "RINGCLAW_AGENT_WORKSPACE", "RINGCLAW_API_ADDR",
		"RC_CLIENT_ID", "RC_CLIENT_SECRET", "RC_JWT_TOKEN", "RC_SERVER_URL",
		"RC_CHAT_IDS", "RC_SOURCE_USER_IDS", "RC_BOT_TOKEN", "RC_BOT_MENTION_ONLY",
	} {
		t.Setenv(k, "")
	}
}

func clearNonUserIDEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"RINGCLAW_DEFAULT_AGENT", "RINGCLAW_AGENT_WORKSPACE", "RINGCLAW_API_ADDR",
		"RC_CLIENT_ID", "RC_CLIENT_SECRET", "RC_JWT_TOKEN", "RC_SERVER_URL",
		"RC_CHAT_IDS", "RC_BOT_TOKEN", "RC_BOT_MENTION_ONLY",
	} {
		t.Setenv(k, "")
	}
}

func clearNonBotMentionEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"RINGCLAW_DEFAULT_AGENT", "RINGCLAW_AGENT_WORKSPACE", "RINGCLAW_API_ADDR",
		"RC_CLIENT_ID", "RC_CLIENT_SECRET", "RC_JWT_TOKEN", "RC_SERVER_URL",
		"RC_CHAT_IDS", "RC_SOURCE_USER_IDS", "RC_BOT_TOKEN",
	} {
		t.Setenv(k, "")
	}
}

func clearNonServerURLEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"RINGCLAW_DEFAULT_AGENT", "RINGCLAW_AGENT_WORKSPACE", "RINGCLAW_API_ADDR",
		"RC_CLIENT_ID", "RC_CLIENT_SECRET", "RC_JWT_TOKEN",
		"RC_CHAT_IDS", "RC_SOURCE_USER_IDS", "RC_BOT_TOKEN", "RC_BOT_MENTION_ONLY",
	} {
		t.Setenv(k, "")
	}
}
