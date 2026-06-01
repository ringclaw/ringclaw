package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestACPInstallHint_Known(t *testing.T) {
	hint := ACPInstallHint("claude")
	if hint == "" {
		t.Error("expected install hint for claude")
	}
	hint = ACPInstallHint("codex")
	if hint == "" {
		t.Error("expected install hint for codex")
	}
}

func TestACPInstallHint_Unknown(t *testing.T) {
	hint := ACPInstallHint("nonexistent")
	if hint != "" {
		t.Errorf("expected empty hint for unknown agent, got %q", hint)
	}
}

func TestResolveCandidate_NoBinaryNoNpx(t *testing.T) {
	c := agentCandidate{Name: "test"}
	cmd, args := resolveCandidate(c)
	if cmd != "" {
		t.Errorf("expected empty command, got %q", cmd)
	}
	if args != nil {
		t.Errorf("expected nil args, got %v", args)
	}
}

func TestResolveCandidate_BinaryNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	c := agentCandidate{Name: "test", Binary: "nonexistent-binary-xyz-12345"}
	cmd, _ := resolveCandidate(c)
	if cmd != "" {
		t.Errorf("expected empty command for missing binary, got %q", cmd)
	}
}

func TestResolveCandidate_BinaryFound(t *testing.T) {
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "test-agent-bin")
	os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", tmpDir)

	c := agentCandidate{Name: "test", Binary: "test-agent-bin", Args: []string{"--acp"}}
	cmd, args := resolveCandidate(c)
	if cmd == "" {
		t.Fatal("expected non-empty command")
	}
	if len(args) != 1 || args[0] != "--acp" {
		t.Errorf("expected args [--acp], got %v", args)
	}
}

func TestResolveCandidate_NpxNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	c := agentCandidate{Name: "test", NpxPkg: "@test/agent"}
	cmd, _ := resolveCandidate(c)
	if cmd != "" {
		t.Errorf("expected empty command when npx not found, got %q", cmd)
	}
}

func TestDetectAndConfigure_WithFakeBinary(t *testing.T) {
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "claude-agent-acp")
	os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", tmpDir)
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	modified := DetectAndConfigure(cfg)

	if !modified {
		t.Error("expected config to be modified")
	}
	claude, ok := cfg.Agents["claude"]
	if !ok {
		t.Fatal("expected claude agent to be detected")
	}
	if claude.Type != "acp" {
		t.Errorf("expected acp type, got %q", claude.Type)
	}
	if cfg.DefaultAgent != "claude" {
		t.Errorf("expected default agent to be claude, got %q", cfg.DefaultAgent)
	}
}

func TestDetectAndConfigure_UpgradeToACP(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "claude-agent-acp"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", tmpDir)
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	cfg.Agents["claude"] = AgentConfig{Type: "cli", Command: "/usr/bin/claude", Model: "haiku"}

	modified := DetectAndConfigure(cfg)

	if !modified {
		t.Error("expected config to be modified for upgrade")
	}
	claude := cfg.Agents["claude"]
	if claude.Type != "acp" {
		t.Errorf("expected upgrade to acp, got %q", claude.Type)
	}
	if claude.Model != "haiku" {
		t.Errorf("expected model to be preserved as haiku, got %q", claude.Model)
	}
}

func TestDetectAndConfigure_NoUpgradeIfAlreadyACP(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "claude-agent-acp"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", tmpDir)
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	cfg.Agents["claude"] = AgentConfig{Type: "acp", Command: "/custom/claude-acp"}

	DetectAndConfigure(cfg)

	if cfg.Agents["claude"].Command != "/custom/claude-acp" {
		t.Errorf("existing ACP config should not be overwritten, got %q", cfg.Agents["claude"].Command)
	}
}

func TestDetectAndConfigure_DetectsCursorAgentAsACP(t *testing.T) {
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "agent")
	os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", tmpDir)
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	modified := DetectAndConfigure(cfg)
	if !modified {
		t.Fatal("expected config to be modified")
	}
	cursor := cfg.Agents["cursor"]
	if cursor.Type != "acp" {
		t.Fatalf("cursor type = %q, want acp", cursor.Type)
	}
	if cursor.Command != fakeBin {
		t.Fatalf("cursor command = %q, want %q", cursor.Command, fakeBin)
	}
	if len(cursor.Args) != 1 || cursor.Args[0] != "acp" {
		t.Fatalf("cursor args = %v, want [acp]", cursor.Args)
	}
}

func TestDetectAndConfigure_MigratesCursorAgentToModernACP(t *testing.T) {
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "agent")
	os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", tmpDir)
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	cfg.DefaultAgent = "cursor"
	cfg.Agents["cursor"] = AgentConfig{
		Type:             "acp",
		Command:          "/old/path/cursor-agent",
		Args:             []string{"acp", "--old"},
		AllowWrite:       true,
		FullAccess:       true,
		RestrictedModeID: "plan",
	}
	modified := DetectAndConfigure(cfg)
	if !modified {
		t.Fatal("expected config to be modified")
	}
	cursor := cfg.Agents["cursor"]
	if cursor.Type != "acp" {
		t.Fatalf("cursor type = %q, want acp", cursor.Type)
	}
	if cursor.Command != fakeBin {
		t.Fatalf("cursor command = %q, want %q", cursor.Command, fakeBin)
	}
	if len(cursor.Args) != 1 || cursor.Args[0] != "acp" {
		t.Fatalf("cursor args = %v, want [acp]", cursor.Args)
	}
	if cursor.AllowWrite || cursor.FullAccess {
		t.Fatalf("cursor unsafe ACP fields were not cleared: %+v", cursor)
	}
	if cursor.RestrictedModeID != "plan" {
		t.Fatalf("restricted mode = %q, want plan", cursor.RestrictedModeID)
	}
	if cfg.DefaultAgent != "cursor" {
		t.Fatalf("default agent = %q, want cursor", cfg.DefaultAgent)
	}
}

func TestDetectAndConfigure_MigratesCursorCLIToModernACP(t *testing.T) {
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "agent")
	os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", tmpDir)
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	cfg.DefaultAgent = "cursor"
	cfg.Agents["cursor"] = AgentConfig{
		Type:    "cli",
		Command: fakeBin,
	}
	modified := DetectAndConfigure(cfg)
	if !modified {
		t.Fatal("expected config to be modified")
	}
	cursor := cfg.Agents["cursor"]
	if cursor.Type != "acp" || cursor.Command != fakeBin || len(cursor.Args) != 1 || cursor.Args[0] != "acp" {
		t.Fatalf("cursor = %+v, want modern ACP config", cursor)
	}
}

func TestDetectAndConfigure_OpenclawHTTPFallback(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PATH", tmpDir)
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	cfg.OpenclawGateway = OpenclawGatewayConfig{
		URL:   "wss://gw.test.com",
		Token: "tok123",
	}
	modified := DetectAndConfigure(cfg)

	if !modified {
		t.Error("expected config to be modified")
	}
	oc, ok := cfg.Agents["openclaw"]
	if !ok {
		t.Fatal("expected openclaw agent")
	}
	if oc.Type != "http" {
		t.Errorf("expected http type for openclaw fallback, got %q", oc.Type)
	}
	if oc.APIKey != "tok123" {
		t.Errorf("expected API key tok123, got %q", oc.APIKey)
	}
	if oc.Endpoint == "" {
		t.Error("expected non-empty endpoint")
	}
}

func TestDetectAndConfigure_OpenclawBinaryNoGateway(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "openclaw"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", tmpDir)
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	DetectAndConfigure(cfg)

	// openclaw binary found but no gateway => should be deleted
	_, ok := cfg.Agents["openclaw"]
	if ok {
		t.Error("openclaw should be removed when no gateway config")
	}
}

func TestDetectAndConfigure_OpenclawBinaryWithGatewayConfig(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "openclaw"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", tmpDir)
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	cfg.OpenclawGateway = OpenclawGatewayConfig{
		URL:   "wss://gw.test.com",
		Token: "tok-abc",
	}
	DetectAndConfigure(cfg)

	oc, ok := cfg.Agents["openclaw"]
	if !ok {
		t.Fatal("expected openclaw agent with gateway")
	}
	if oc.Type != "acp" {
		t.Errorf("expected acp type, got %q", oc.Type)
	}
	found := false
	for _, a := range oc.Args {
		if a == "--token" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --token in args, got %v", oc.Args)
	}
}

func TestDetectAndConfigure_OpenclawBinaryWithGatewayPassword(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "openclaw"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", tmpDir)
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	cfg.OpenclawGateway = OpenclawGatewayConfig{
		URL:      "ws://127.0.0.1:9999",
		Password: "pass123",
	}
	DetectAndConfigure(cfg)

	oc, ok := cfg.Agents["openclaw"]
	if !ok {
		t.Fatal("expected openclaw agent with gateway")
	}
	found := false
	for _, a := range oc.Args {
		if a == "--password" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --password in args, got %v", oc.Args)
	}
}

func TestLoadOpenclawGateway_JSONConfig_RemoteGateway(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	ocDir := filepath.Join(tmpDir, ".openclaw")
	os.MkdirAll(ocDir, 0o700)

	ocCfg := map[string]interface{}{
		"gateway": map[string]interface{}{
			"remote": map[string]string{
				"url":   "wss://remote.gw.com",
				"token": "remote-tok",
			},
		},
	}
	data, _ := json.Marshal(ocCfg)
	os.WriteFile(filepath.Join(ocDir, "openclaw.json"), data, 0o600)

	gwURL, gwToken, gwPassword := loadOpenclawGateway(&Config{})
	if gwURL != "wss://remote.gw.com" {
		t.Errorf("expected remote URL, got %q", gwURL)
	}
	if gwToken != "remote-tok" {
		t.Errorf("expected remote token, got %q", gwToken)
	}
	if gwPassword != "" {
		t.Errorf("expected empty password, got %q", gwPassword)
	}
}

func TestLoadOpenclawGateway_JSONConfig_LocalGateway_Token(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	ocDir := filepath.Join(tmpDir, ".openclaw")
	os.MkdirAll(ocDir, 0o700)

	ocCfg := map[string]interface{}{
		"gateway": map[string]interface{}{
			"port": 8080,
			"auth": map[string]string{
				"mode":  "token",
				"token": "local-tok",
			},
		},
	}
	data, _ := json.Marshal(ocCfg)
	os.WriteFile(filepath.Join(ocDir, "openclaw.json"), data, 0o600)

	gwURL, gwToken, gwPassword := loadOpenclawGateway(&Config{})
	if gwURL != "ws://127.0.0.1:8080" {
		t.Errorf("expected local URL, got %q", gwURL)
	}
	if gwToken != "local-tok" {
		t.Errorf("expected local token, got %q", gwToken)
	}
	if gwPassword != "" {
		t.Errorf("expected empty password, got %q", gwPassword)
	}
}

func TestLoadOpenclawGateway_JSONConfig_LocalGateway_Password(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	ocDir := filepath.Join(tmpDir, ".openclaw")
	os.MkdirAll(ocDir, 0o700)

	ocCfg := map[string]interface{}{
		"gateway": map[string]interface{}{
			"port": 9090,
			"auth": map[string]string{
				"mode":     "password",
				"password": "local-pass",
			},
		},
	}
	data, _ := json.Marshal(ocCfg)
	os.WriteFile(filepath.Join(ocDir, "openclaw.json"), data, 0o600)

	gwURL, gwToken, gwPassword := loadOpenclawGateway(&Config{})
	if gwURL != "ws://127.0.0.1:9090" {
		t.Errorf("expected local URL, got %q", gwURL)
	}
	if gwToken != "" {
		t.Errorf("expected empty token, got %q", gwToken)
	}
	if gwPassword != "local-pass" {
		t.Errorf("expected local password, got %q", gwPassword)
	}
}

func TestLoadOpenclawGateway_JSONConfig_MalformedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	ocDir := filepath.Join(tmpDir, ".openclaw")
	os.MkdirAll(ocDir, 0o700)
	os.WriteFile(filepath.Join(ocDir, "openclaw.json"), []byte("{bad json"), 0o600)

	gwURL, _, _ := loadOpenclawGateway(&Config{})
	if gwURL != "" {
		t.Errorf("expected empty URL for malformed JSON, got %q", gwURL)
	}
}

func TestLoadOpenclawGateway_JSONConfig_NoPorts(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	ocDir := filepath.Join(tmpDir, ".openclaw")
	os.MkdirAll(ocDir, 0o700)

	ocCfg := map[string]interface{}{
		"gateway": map[string]interface{}{
			"port": 0,
		},
	}
	data, _ := json.Marshal(ocCfg)
	os.WriteFile(filepath.Join(ocDir, "openclaw.json"), data, 0o600)

	gwURL, _, _ := loadOpenclawGateway(&Config{})
	if gwURL != "" {
		t.Errorf("expected empty URL when no port, got %q", gwURL)
	}
}

// TestLoadOpenclawGateway_ConfigOverridesJSON verifies that the config.json
// openclaw_gateway section wins over ~/.openclaw/openclaw.json.
func TestLoadOpenclawGateway_ConfigOverridesJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	ocDir := filepath.Join(tmpDir, ".openclaw")
	os.MkdirAll(ocDir, 0o700)
	ocCfg := map[string]interface{}{
		"gateway": map[string]interface{}{
			"remote": map[string]string{
				"url":   "wss://should-be-ignored.com",
				"token": "ignored",
			},
		},
	}
	data, _ := json.Marshal(ocCfg)
	os.WriteFile(filepath.Join(ocDir, "openclaw.json"), data, 0o600)

	cfg := &Config{OpenclawGateway: OpenclawGatewayConfig{
		URL:   "wss://from-config.com",
		Token: "config-tok",
	}}

	gwURL, gwToken, _ := loadOpenclawGateway(cfg)
	if gwURL != "wss://from-config.com" {
		t.Errorf("config.json openclaw_gateway must win, got %q", gwURL)
	}
	if gwToken != "config-tok" {
		t.Errorf("expected config token, got %q", gwToken)
	}
}

func TestDetectAndConfigure_DefaultAgentPreserved(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PATH", tmpDir)
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	cfg.DefaultAgent = "claude"
	cfg.Agents["claude"] = AgentConfig{Type: "acp", Command: "/usr/bin/claude-acp"}

	DetectAndConfigure(cfg)

	if cfg.DefaultAgent != "claude" {
		t.Errorf("default agent should be preserved, got %q", cfg.DefaultAgent)
	}
}

func TestDetectAndConfigure_DefaultAgentInvalid(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "codex"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", tmpDir)
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	cfg.DefaultAgent = "nonexistent"

	DetectAndConfigure(cfg)

	if cfg.DefaultAgent == "nonexistent" {
		t.Error("default agent should be updated from nonexistent")
	}
}

func TestDetectAndConfigure_MultipleBinaries(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "claude-agent-acp"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(tmpDir, "codex"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(tmpDir, "kimi"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", tmpDir)
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	modified := DetectAndConfigure(cfg)

	if !modified {
		t.Error("expected modified")
	}
	if len(cfg.Agents) < 3 {
		t.Errorf("expected at least 3 agents, got %d", len(cfg.Agents))
	}
	if cfg.DefaultAgent != "claude" {
		t.Errorf("expected default agent claude, got %q", cfg.DefaultAgent)
	}
}

func TestDetectAndConfigure_OpenclawHTTPFallback_WS(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PATH", tmpDir)
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	cfg.OpenclawGateway = OpenclawGatewayConfig{URL: "ws://127.0.0.1:8080"}
	DetectAndConfigure(cfg)

	oc := cfg.Agents["openclaw"]
	if oc.Type != "http" {
		t.Errorf("expected http type, got %q", oc.Type)
	}
	if oc.Endpoint == "" {
		t.Error("expected non-empty endpoint")
	}
}
