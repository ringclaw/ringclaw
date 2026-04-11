package cmd

import (
	"testing"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/config"
)

func TestAgentWorkspace_UsesGlobalWorkspaceWhenAgentCwdEmpty(t *testing.T) {
	cfg := &config.Config{AgentWorkspace: "/tmp/global-workspace"}
	agCfg := config.AgentConfig{}

	got := agent.AgentWorkspace(cfg, agCfg)

	if got != "/tmp/global-workspace" {
		t.Fatalf("AgentWorkspace() = %q, want %q", got, "/tmp/global-workspace")
	}
}

func TestAgentWorkspace_BothEmpty_ReturnsEmpty(t *testing.T) {
	cfg := &config.Config{}
	agCfg := config.AgentConfig{}

	got := agent.AgentWorkspace(cfg, agCfg)

	if got != "" {
		t.Fatalf("AgentWorkspace() = %q, want empty string", got)
	}
}

func TestAgentWorkspace_PrefersAgentSpecificCwd(t *testing.T) {
	cfg := &config.Config{AgentWorkspace: "/tmp/global-workspace"}
	agCfg := config.AgentConfig{Cwd: "/tmp/agent-workspace"}

	got := agent.AgentWorkspace(cfg, agCfg)

	if got != "/tmp/agent-workspace" {
		t.Fatalf("AgentWorkspace() = %q, want %q", got, "/tmp/agent-workspace")
	}
}
