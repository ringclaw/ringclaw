package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/ringclaw/ringclaw/config"
)

func TestCreate_Success(t *testing.T) {
	// Use a unique type to avoid collision with init()-registered factories.
	const typ = "test-create-success"
	globalRegistry.mu.Lock()
	globalRegistry.factories[typ] = func(_ context.Context, name string, _ config.AgentConfig, _ string) (Agent, error) {
		return &stubAgent{info: AgentInfo{Name: name, Type: typ}}, nil
	}
	globalRegistry.mu.Unlock()
	defer func() {
		globalRegistry.mu.Lock()
		delete(globalRegistry.factories, typ)
		globalRegistry.mu.Unlock()
	}()

	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"myagent": {Type: typ},
		},
	}
	ag := Create(context.Background(), cfg, "myagent")
	if ag == nil {
		t.Fatal("expected non-nil agent")
	}
	if ag.Info().Name != "myagent" {
		t.Errorf("got name %q, want %q", ag.Info().Name, "myagent")
	}
}

func TestCreate_MissingConfig(t *testing.T) {
	cfg := &config.Config{Agents: map[string]config.AgentConfig{}}
	ag := Create(context.Background(), cfg, "nonexistent")
	if ag != nil {
		t.Error("expected nil for missing config")
	}
}

func TestCreate_UnknownType(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"myagent": {Type: "unknown-type-xyz"},
		},
	}
	ag := Create(context.Background(), cfg, "myagent")
	if ag != nil {
		t.Error("expected nil for unknown type")
	}
}

func TestCreate_FactoryError(t *testing.T) {
	const typ = "test-create-error"
	globalRegistry.mu.Lock()
	globalRegistry.factories[typ] = func(_ context.Context, _ string, _ config.AgentConfig, _ string) (Agent, error) {
		return nil, fmt.Errorf("factory failed")
	}
	globalRegistry.mu.Unlock()
	defer func() {
		globalRegistry.mu.Lock()
		delete(globalRegistry.factories, typ)
		globalRegistry.mu.Unlock()
	}()

	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"bad": {Type: typ},
		},
	}
	ag := Create(context.Background(), cfg, "bad")
	if ag != nil {
		t.Error("expected nil when factory returns error")
	}
}

func TestRegisteredTypes(t *testing.T) {
	types := RegisteredTypes()
	if len(types) == 0 {
		t.Fatal("expected at least init()-registered types")
	}
	// The init() functions in acp_agent.go, cli_agent.go, http_agent.go register these.
	want := map[string]bool{"acp": false, "cli": false, "http": false}
	for _, tp := range types {
		if _, ok := want[tp]; ok {
			want[tp] = true
		}
	}
	for tp, found := range want {
		if !found {
			t.Errorf("missing registered type %q", tp)
		}
	}
}

func TestAgentWorkspace_AgentCwd(t *testing.T) {
	cfg := &config.Config{AgentWorkspace: "/global"}
	agCfg := config.AgentConfig{Cwd: "/agent-specific"}
	got := AgentWorkspace(cfg, agCfg)
	if got != "/agent-specific" {
		t.Errorf("got %q, want /agent-specific", got)
	}
}

func TestAgentWorkspace_GlobalFallback(t *testing.T) {
	cfg := &config.Config{AgentWorkspace: "/global"}
	agCfg := config.AgentConfig{}
	got := AgentWorkspace(cfg, agCfg)
	if got != "/global" {
		t.Errorf("got %q, want /global", got)
	}
}

func TestAgentWorkspace_NilConfig(t *testing.T) {
	agCfg := config.AgentConfig{}
	got := AgentWorkspace(nil, agCfg)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// stubAgent is a minimal Agent for registry tests.
type stubAgent struct {
	info AgentInfo
}

func (s *stubAgent) Chat(_ context.Context, _, _ string) (string, error) { return "", nil }
func (s *stubAgent) ResetSession(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (s *stubAgent) SetCwd(_ string)    {}
func (s *stubAgent) Info() AgentInfo    { return s.info }
