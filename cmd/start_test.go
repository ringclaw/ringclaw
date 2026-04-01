package cmd

import (
	"testing"

	"github.com/ringclaw/ringclaw/config"
)

func TestAgentWorkspace_UsesGlobalWorkspaceWhenAgentCwdEmpty(t *testing.T) {
	cfg := &config.Config{AgentWorkspace: "/tmp/global-workspace"}
	agCfg := config.AgentConfig{}

	got := agentWorkspace(cfg, agCfg)

	if got != "/tmp/global-workspace" {
		t.Fatalf("agentWorkspace() = %q, want %q", got, "/tmp/global-workspace")
	}
}

func TestAgentWorkspace_PrefersAgentSpecificCwd(t *testing.T) {
	cfg := &config.Config{AgentWorkspace: "/tmp/global-workspace"}
	agCfg := config.AgentConfig{Cwd: "/tmp/agent-workspace"}

	got := agentWorkspace(cfg, agCfg)

	if got != "/tmp/agent-workspace" {
		t.Fatalf("agentWorkspace() = %q, want %q", got, "/tmp/agent-workspace")
	}
}
