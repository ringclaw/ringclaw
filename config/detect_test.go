package config

import "testing"

func TestDetectAndConfigure_NoBinaries(t *testing.T) {
	// With an empty PATH, no agents should be detected
	t.Setenv("PATH", t.TempDir())
	// Override HOME to prevent reading real ~/.openclaw/openclaw.json
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	modified := DetectAndConfigure(cfg)

	if len(cfg.Agents) != 0 {
		t.Errorf("expected 0 agents with empty PATH, got %d: %v", len(cfg.Agents), cfg.Agents)
	}
	if cfg.DefaultAgent != "" {
		t.Errorf("expected empty default agent, got %q", cfg.DefaultAgent)
	}
	_ = modified
}

func TestDetectAndConfigure_SkipsExisting(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agents["claude"] = AgentConfig{Type: "http", Endpoint: "https://custom.api/v1"}

	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	DetectAndConfigure(cfg)

	// Existing claude config should not be overwritten
	if cfg.Agents["claude"].Type != "http" {
		t.Errorf("existing agent config was overwritten: %+v", cfg.Agents["claude"])
	}
}

func TestDetectAndConfigure_DefaultOrder(t *testing.T) {
	cfg := DefaultConfig()
	// Manually add agents out of priority order
	cfg.Agents["codex"] = AgentConfig{Type: "cli", Command: "/usr/bin/codex"}
	cfg.Agents["kimi"] = AgentConfig{Type: "acp", Command: "/usr/bin/kimi"}

	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	DetectAndConfigure(cfg)

	// codex has higher priority than kimi in defaultOrder
	if cfg.DefaultAgent != "codex" {
		t.Errorf("expected default agent 'codex' (higher priority), got %q", cfg.DefaultAgent)
	}
}

func TestDetectAndConfigure_DefaultOrder_KeepsExisting(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DefaultAgent = "kimi"
	cfg.Agents["kimi"] = AgentConfig{Type: "acp", Command: "/usr/bin/kimi"}
	cfg.Agents["codex"] = AgentConfig{Type: "cli", Command: "/usr/bin/codex"}

	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	DetectAndConfigure(cfg)

	// Existing valid default should be preserved
	if cfg.DefaultAgent != "kimi" {
		t.Errorf("expected existing default 'kimi' to be preserved, got %q", cfg.DefaultAgent)
	}
}

func TestLoadOpenclawGateway_FromConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &Config{OpenclawGateway: OpenclawGatewayConfig{
		URL:   "wss://gw.example.com",
		Token: "test-token-123",
	}}

	gwURL, gwToken, gwPassword := loadOpenclawGateway(cfg)

	if gwURL != "wss://gw.example.com" {
		t.Errorf("expected URL from config, got %q", gwURL)
	}
	if gwToken != "test-token-123" {
		t.Errorf("expected token from config, got %q", gwToken)
	}
	if gwPassword != "" {
		t.Errorf("expected empty password, got %q", gwPassword)
	}
}

func TestLoadOpenclawGateway_EnvIgnored(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_URL", "wss://env.example.com")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "env-token")
	t.Setenv("OPENCLAW_GATEWAY_PASSWORD", "env-pass")
	t.Setenv("HOME", t.TempDir())

	gwURL, gwToken, gwPassword := loadOpenclawGateway(&Config{})

	if gwURL != "" {
		t.Errorf("OPENCLAW_GATEWAY_URL env must be ignored, got %q", gwURL)
	}
	if gwToken != "" {
		t.Errorf("OPENCLAW_GATEWAY_TOKEN env must be ignored, got %q", gwToken)
	}
	if gwPassword != "" {
		t.Errorf("OPENCLAW_GATEWAY_PASSWORD env must be ignored, got %q", gwPassword)
	}
}

func TestLoadOpenclawGateway_NoConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	gwURL, gwToken, gwPassword := loadOpenclawGateway(&Config{})

	if gwURL != "" {
		t.Errorf("expected empty URL, got %q", gwURL)
	}
	if gwToken != "" {
		t.Errorf("expected empty token, got %q", gwToken)
	}
	if gwPassword != "" {
		t.Errorf("expected empty password, got %q", gwPassword)
	}
}

func TestAgentExists(t *testing.T) {
	cfg := &Config{Agents: map[string]AgentConfig{
		"claude": {Type: "acp"},
	}}

	if !agentExists(cfg, "claude") {
		t.Error("expected claude to exist")
	}
	if agentExists(cfg, "codex") {
		t.Error("expected codex to not exist")
	}
}

func TestDefaultOrder(t *testing.T) {
	order := DefaultOrder()
	if len(order) == 0 {
		t.Fatal("default order should not be empty")
	}
	if order[0] != "claude" {
		t.Errorf("expected first priority to be claude, got %q", order[0])
	}
}
