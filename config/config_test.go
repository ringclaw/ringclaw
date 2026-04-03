package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestLoadEnvOverride(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RC.ClientID = "from-file"

	t.Setenv("RC_CLIENT_ID", "from-env")
	t.Setenv("RC_CLIENT_SECRET", "env-secret")
	t.Setenv("RC_JWT_TOKEN", "env-jwt")
	t.Setenv("RC_CHAT_IDS", "chat-1, chat-2 ,,chat-3")
	t.Setenv("RC_BOT_MENTION_ONLY", "false")

	loadEnv(cfg)

	if cfg.RC.ClientID != "from-env" {
		t.Errorf("expected env override for client_id, got %q", cfg.RC.ClientID)
	}
	if cfg.RC.ClientSecret != "env-secret" {
		t.Errorf("expected env-secret, got %q", cfg.RC.ClientSecret)
	}
	if len(cfg.RC.ChatIDs) != 3 {
		t.Fatalf("expected 3 chat IDs, got %d", len(cfg.RC.ChatIDs))
	}
	if cfg.RC.ChatIDs[0] != "chat-1" || cfg.RC.ChatIDs[1] != "chat-2" || cfg.RC.ChatIDs[2] != "chat-3" {
		t.Fatalf("unexpected chat IDs: %#v", cfg.RC.ChatIDs)
	}
	if cfg.RC.BotMentionOnly == nil || *cfg.RC.BotMentionOnly {
		t.Fatalf("expected bot_mention_only=false, got %#v", cfg.RC.BotMentionOnly)
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
