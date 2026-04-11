package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ringclaw/ringclaw/config"
)

// Factory creates an Agent from the given config.
// name is the agent's config key (e.g. "claude"), cfg is its AgentConfig,
// and cwd is the resolved working directory.
type Factory func(ctx context.Context, name string, cfg config.AgentConfig, cwd string) (Agent, error)

// registry holds agent type -> factory mappings.
type registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

var globalRegistry = &registry{factories: make(map[string]Factory)}

// Register adds a factory for the given agent type (e.g. "acp", "cli", "http").
// Typically called from init() in each agent implementation file.
func Register(agentType string, f Factory) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	if _, exists := globalRegistry.factories[agentType]; exists {
		panic(fmt.Sprintf("agent: factory already registered for type %q", agentType))
	}
	globalRegistry.factories[agentType] = f
}

// Create creates and initializes an agent by its config name.
// Returns nil with a warning log if the agent type is unknown or config is missing.
func Create(ctx context.Context, cfg *config.Config, name string) Agent {
	agCfg, ok := cfg.Agents[name]
	if !ok {
		slog.Warn("agent not found in config", "component", "agent", "name", name)
		return nil
	}

	cwd := AgentWorkspace(cfg, agCfg)

	globalRegistry.mu.RLock()
	f, ok := globalRegistry.factories[agCfg.Type]
	globalRegistry.mu.RUnlock()

	if !ok {
		slog.Warn("unknown agent type", "component", "agent", "type", agCfg.Type, "name", name)
		return nil
	}

	ag, err := f(ctx, name, agCfg, cwd)
	if err != nil {
		slog.Error("failed to create agent", "component", "agent", "name", name, "type", agCfg.Type, "error", err)
		return nil
	}

	info := ag.Info()
	slog.Info("created agent", "component", "agent", "name", name, "type", agCfg.Type, "model", info.Model, "command", info.Command)
	return ag
}

// RegisteredTypes returns all registered agent types.
func RegisteredTypes() []string {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	types := make([]string, 0, len(globalRegistry.factories))
	for t := range globalRegistry.factories {
		types = append(types, t)
	}
	return types
}

// AgentWorkspace resolves the working directory for an agent,
// preferring the agent-specific Cwd over the global AgentWorkspace.
func AgentWorkspace(cfg *config.Config, agCfg config.AgentConfig) string {
	if agCfg.Cwd != "" {
		return agCfg.Cwd
	}
	if cfg != nil {
		return cfg.AgentWorkspace
	}
	return ""
}
