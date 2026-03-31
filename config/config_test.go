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
		DefaultAgent: "claude",
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

	loadEnv(cfg)

	if cfg.RC.ClientID != "from-env" {
		t.Errorf("expected env override for client_id, got %q", cfg.RC.ClientID)
	}
	if cfg.RC.ClientSecret != "env-secret" {
		t.Errorf("expected env-secret, got %q", cfg.RC.ClientSecret)
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

func TestSaveAndReload(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	original := &Config{
		DefaultAgent: "codex",
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
	if reloaded.Agents["codex"].Type != "cli" {
		t.Errorf("agent type mismatch: got %q", reloaded.Agents["codex"].Type)
	}
}
