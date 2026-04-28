package agent

import (
	"strings"
	"testing"
)

func TestResolveRestrictedModeID_DroidPicksSpec(t *testing.T) {
	available := []SessionMode{
		{ID: "normal"},
		{ID: "spec"},
		{ID: "auto-medium"},
	}
	id, source := ResolveRestrictedModeID("/usr/local/bin/droid acp", available, "")
	if id != "spec" {
		t.Errorf("expected spec, got %q", id)
	}
	if !strings.HasPrefix(source, ResolveSourceBuiltin) {
		t.Errorf("expected builtin source, got %q", source)
	}
}

func TestResolveRestrictedModeID_ClaudePicksPlan(t *testing.T) {
	available := []SessionMode{
		{ID: "default"},
		{ID: "plan"},
		{ID: "acceptEdits"},
		{ID: "auto"},
		{ID: "dontAsk"},
	}
	id, _ := ResolveRestrictedModeID("/usr/bin/claude-code-acp", available, "")
	if id != "plan" {
		t.Errorf("expected plan, got %q", id)
	}
}

func TestResolveRestrictedModeID_GeminiPicksPlan(t *testing.T) {
	available := []SessionMode{{ID: "default"}, {ID: "plan"}}
	id, _ := ResolveRestrictedModeID("gemini --experimental-acp", available, "")
	if id != "plan" {
		t.Errorf("expected plan, got %q", id)
	}
}

func TestResolveRestrictedModeID_QwenPicksPlan(t *testing.T) {
	available := []SessionMode{{ID: "default"}, {ID: "plan"}, {ID: "yolo"}}
	id, _ := ResolveRestrictedModeID("qwen --acp", available, "")
	if id != "plan" {
		t.Errorf("expected plan, got %q", id)
	}
}

func TestResolveRestrictedModeID_CursorPicksPlan(t *testing.T) {
	available := []SessionMode{{ID: "plan"}, {ID: "agent"}, {ID: "ask"}}
	id, _ := ResolveRestrictedModeID("cursor-agent acp", available, "")
	if id != "plan" {
		t.Errorf("expected plan, got %q", id)
	}
}

func TestResolveRestrictedModeID_UnknownAgent_HeuristicMatch(t *testing.T) {
	available := []SessionMode{
		{ID: "edit", Name: "Edit Mode"},
		{ID: "review", Name: "Plan Mode"},
	}
	id, source := ResolveRestrictedModeID("/opt/some-future-agent rpc", available, "")
	if id != "review" {
		t.Errorf("expected heuristic to pick review (Plan Mode), got %q", id)
	}
	if !strings.HasPrefix(source, ResolveSourceHeuristic) {
		t.Errorf("expected heuristic source, got %q", source)
	}
}

func TestResolveRestrictedModeID_UnknownAgent_ReadOnlyKeyword(t *testing.T) {
	available := []SessionMode{{ID: "edit"}, {ID: "read-only"}, {ID: "yolo"}}
	id, _ := ResolveRestrictedModeID("/opt/unknown", available, "")
	if id != "read-only" {
		t.Errorf("expected read-only, got %q", id)
	}
}

func TestResolveRestrictedModeID_UnknownAgent_NoMatchReturnsEmpty(t *testing.T) {
	available := []SessionMode{{ID: "edit"}, {ID: "yolo"}}
	id, source := ResolveRestrictedModeID("/opt/unknown", available, "")
	if id != "" {
		t.Errorf("expected empty modeID, got %q", id)
	}
	if source != ResolveSourceMissing {
		t.Errorf("expected ResolveSourceMissing, got %q", source)
	}
}

func TestResolveRestrictedModeID_OverrideWinsWhenAvailable(t *testing.T) {
	available := []SessionMode{{ID: "spec"}, {ID: "plan"}, {ID: "custom-readonly"}}
	id, source := ResolveRestrictedModeID("droid acp", available, "custom-readonly")
	if id != "custom-readonly" {
		t.Errorf("expected override, got %q", id)
	}
	if source != ResolveSourceConfig {
		t.Errorf("expected config source, got %q", source)
	}
}

func TestResolveRestrictedModeID_OverrideMissingFallsThrough(t *testing.T) {
	available := []SessionMode{{ID: "spec"}, {ID: "plan"}}
	id, source := ResolveRestrictedModeID("droid acp", available, "non-existent-mode")
	if id != "spec" {
		t.Errorf("expected fallback to builtin spec, got %q", id)
	}
	if !strings.HasPrefix(source, ResolveSourceBuiltin) {
		t.Errorf("expected builtin fallback, got %q", source)
	}
}

func TestResolveRestrictedModeID_BuiltinModeMissingFallsToHeuristic(t *testing.T) {
	// Claude pattern matches but the agent's modes don't include
	// "plan" — should fall through to the heuristic which finds
	// "Plan Mode" via the name.
	available := []SessionMode{{ID: "default"}, {ID: "review", Name: "Plan Mode"}}
	id, source := ResolveRestrictedModeID("claude-code-acp", available, "")
	if id != "review" {
		t.Errorf("expected heuristic fallback to review, got %q", id)
	}
	if !strings.HasPrefix(source, ResolveSourceHeuristic) {
		t.Errorf("expected heuristic source, got %q", source)
	}
}

func TestResolveRestrictedModeID_EmptyAvailableReturnsMissing(t *testing.T) {
	id, source := ResolveRestrictedModeID("droid acp", nil, "")
	if id != "" || source != ResolveSourceMissing {
		t.Errorf("expected (\"\", missing), got (%q, %q)", id, source)
	}
}

func TestAvailableModeIDs(t *testing.T) {
	got := AvailableModeIDs([]SessionMode{{ID: "a"}, {ID: "b"}})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("unexpected: %v", got)
	}
}
