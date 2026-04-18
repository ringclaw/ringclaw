package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
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

func TestLoadFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	testCfg := &Config{
		DefaultAgent:   "claude",
		AgentWorkspace: "/tmp/workspace",
		Agents: map[string]AgentConfig{
			"claude": {Type: "acp", Command: "/usr/bin/claude", Model: "sonnet"},
		},
		RC: RCConfig{
			ClientID:     "test-id",
			ClientSecret: "test-secret",
			JWTToken:     "test-jwt",
			ChatIDs:      []string{"test-chat"},
		},
	}
	data, _ := json.MarshalIndent(testCfg, "", "  ")
	os.WriteFile(cfgPath, data, 0o644)

	// Read back and verify
	readData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var loaded Config
	if err := json.Unmarshal(readData, &loaded); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	if loaded.DefaultAgent != "claude" {
		t.Errorf("expected default_agent=claude, got %q", loaded.DefaultAgent)
	}
	if loaded.AgentWorkspace != "/tmp/workspace" {
		t.Errorf("expected agent_workspace=/tmp/workspace, got %q", loaded.AgentWorkspace)
	}
	if _, ok := loaded.Agents["claude"]; !ok {
		t.Error("expected claude agent in config")
	}
	if loaded.RC.ClientID != "test-id" {
		t.Errorf("expected client_id=test-id, got %q", loaded.RC.ClientID)
	}
}

// TestLoadIgnoresEnv verifies that all previously supported env vars
// are silently ignored by Load(); config.json is the sole source.
func TestLoadIgnoresEnv(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	cfgDir := filepath.Join(tmpHome, ".ringclaw")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	jsonPayload := `{
		"default_agent": "from-json",
		"ringcentral": {
			"client_id": "json-cid",
			"bot_token": "json-bot",
			"chat_ids": ["json-chat-1"]
		}
	}`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(jsonPayload), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	envVars := []string{
		"RINGCLAW_DEFAULT_AGENT", "RINGCLAW_AGENT_WORKSPACE", "RINGCLAW_API_ADDR",
		"RC_CLIENT_ID", "RC_CLIENT_SECRET", "RC_JWT_TOKEN", "RC_SERVER_URL",
		"RC_CHAT_IDS", "RC_SOURCE_USER_IDS", "RC_BOT_TOKEN", "RC_BOT_MENTION_ONLY",
	}
	for _, name := range envVars {
		t.Setenv(name, "env-"+name)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.DefaultAgent != "from-json" {
		t.Errorf("DefaultAgent: env must be ignored, got %q", cfg.DefaultAgent)
	}
	if cfg.RC.ClientID != "json-cid" {
		t.Errorf("RC.ClientID: env must be ignored, got %q", cfg.RC.ClientID)
	}
	if cfg.RC.BotToken != "json-bot" {
		t.Errorf("RC.BotToken: env must be ignored, got %q", cfg.RC.BotToken)
	}
	if len(cfg.RC.ChatIDs) != 1 || cfg.RC.ChatIDs[0] != "json-chat-1" {
		t.Errorf("RC.ChatIDs: env must be ignored, got %#v", cfg.RC.ChatIDs)
	}
	if cfg.RC.GroupMentionOnly != nil {
		t.Errorf("RC.GroupMentionOnly: env must be ignored, got %#v", cfg.RC.GroupMentionOnly)
	}
	if cfg.RC.BotMentionOnly != nil {
		t.Errorf("RC.BotMentionOnly: Load must normalize the deprecated field to nil, got %#v", cfg.RC.BotMentionOnly)
	}
	if cfg.AgentWorkspace != "" {
		t.Errorf("AgentWorkspace: env must be ignored, got %q", cfg.AgentWorkspace)
	}
	if cfg.APIAddr != "" {
		t.Errorf("APIAddr: env must be ignored, got %q", cfg.APIAddr)
	}
}

func TestHasPrivateApp(t *testing.T) {
	tests := []struct {
		name   string
		rc     RCConfig
		expect bool
	}{
		{"all set", RCConfig{ClientID: "id", ClientSecret: "secret", JWTToken: "jwt"}, true},
		{"missing client_id", RCConfig{ClientSecret: "secret", JWTToken: "jwt"}, false},
		{"missing client_secret", RCConfig{ClientID: "id", JWTToken: "jwt"}, false},
		{"missing jwt_token", RCConfig{ClientID: "id", ClientSecret: "secret"}, false},
		{"all empty", RCConfig{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rc.HasPrivateApp(); got != tt.expect {
				t.Errorf("HasPrivateApp() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestGroupSummaryDefaults(t *testing.T) {
	rc := RCConfig{}
	if rc.HasGroupSummary() {
		t.Fatal("expected group summary disabled by default")
	}
	if got := rc.GroupSummaryLimit(); got != 200 {
		t.Fatalf("expected default group summary limit 200, got %d", got)
	}
}

func TestGroupSummaryConfiguredLimit(t *testing.T) {
	rc := RCConfig{GroupSummaryGroupID: "group-1", GroupSummaryMessageLimit: 42}
	if !rc.HasGroupSummary() {
		t.Fatal("expected group summary enabled")
	}
	if got := rc.GroupSummaryLimit(); got != 42 {
		t.Fatalf("expected configured group summary limit 42, got %d", got)
	}
}

func TestSaveAndReload(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	original := &Config{
		DefaultAgent:   "codex",
		AgentWorkspace: "/tmp/workspace",
		Agents: map[string]AgentConfig{
			"codex": {Type: "cli", Command: "/usr/bin/codex"},
		},
	}

	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	os.MkdirAll(filepath.Dir(cfgPath), 0o700)
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatalf("write error: %v", err)
	}

	readData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}

	var reloaded Config
	if err := json.Unmarshal(readData, &reloaded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if reloaded.DefaultAgent != original.DefaultAgent {
		t.Errorf("default agent mismatch: %q vs %q", reloaded.DefaultAgent, original.DefaultAgent)
	}
	if reloaded.AgentWorkspace != original.AgentWorkspace {
		t.Errorf("agent workspace mismatch: %q vs %q", reloaded.AgentWorkspace, original.AgentWorkspace)
	}
	if reloaded.Agents["codex"].Type != "cli" {
		t.Errorf("agent type mismatch: got %q", reloaded.Agents["codex"].Type)
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"debug", "DEBUG"},
		{"DEBUG", "DEBUG"},
		{"info", "INFO"},
		{"INFO", "INFO"},
		{"warn", "WARN"},
		{"warning", "WARN"},
		{"error", "ERROR"},
		{"", "INFO"},
		{"unknown", "INFO"},
	}
	for _, tt := range tests {
		got := ParseLogLevel(tt.input).String()
		if got != tt.want {
			t.Errorf("ParseLogLevel(%q) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestDebugMode(t *testing.T) {
	SetDebugMode(false)
	if IsDebug() {
		t.Error("expected debug mode off")
	}
	SetDebugMode(true)
	if !IsDebug() {
		t.Error("expected debug mode on")
	}
	SetDebugMode(false)
}

func TestRCConfigLogValue_RedactsSensitive(t *testing.T) {
	rc := RCConfig{
		ClientID:     "my-client-id",
		ClientSecret: "super-secret",
		JWTToken:     "jwt-token-value",
		BotToken:     "bot-token-value",
		ServerURL:    "https://platform.ringcentral.com",
		ChatIDs:      []string{"chat1", "chat2"},
	}

	v := rc.LogValue()
	s := v.String()

	if !contains(s, "my-client-id") {
		t.Error("LogValue should include client_id")
	}
	if contains(s, "super-secret") {
		t.Error("LogValue should NOT include raw client_secret")
	}
	if contains(s, "jwt-token-value") {
		t.Error("LogValue should NOT include raw jwt_token")
	}
	if contains(s, "bot-token-value") {
		t.Error("LogValue should NOT include raw bot_token")
	}
	if !contains(s, "***") {
		t.Error("LogValue should contain *** redaction markers")
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
