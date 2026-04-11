# Plan 005: Flow Architecture

**Date:** 2026-04-11
**Priority:** P4
**Status:** Draft
**Depends on:** Plan 003 (Error Codes — for node result types)
**Reference:** acpx `2026-03-25-acpx-flows-architecture.md`, acpx `src/flows/`

## Problem Statement

RingClaw message processing is a procedural function chain: `HandleMessage` → `dispatchToAgent` → `chatWithAgent` → `sendReplyWithActions`. This is **currently sufficient** — `dispatchToAgent` is ~15 lines and works well.

However, as features grow, this approach may limit:
1. Multi-step workflows (classify intent → fetch context → call agent → post-process → reply)
2. Per-step timeout control (entire chain shares one context timeout)
3. Checkpoint/recovery (if RC API fails after agent responds, the response is lost)
4. Step-level observability

> **YAGNI Note:** This plan is aspirational. No user has requested multi-step workflows. Implement only when a concrete use case emerges. The one actionable idea (checkpoint) is called out in Phase 1.

## Goals

1. Define a declarative Flow model where message processing is a graph of typed nodes
2. Support node types: agent call, action execution, compute, checkpoint
3. Provide per-node timeout control and result tracking
4. Coexist with current handler — no forced migration

## Non-Goals

- Visual flow editor or DSL parser
- Replacing the current handler (both systems coexist)
- Flow persistence or runtime modification
- Phase 1 implementation of the full engine

## What to Implement First: Checkpoint Only

The one concrete, immediate-value idea from this plan is the **checkpoint concept**: save the agent reply to disk before attempting to send it via RC API. This prevents data loss when the RC API call fails.

```go
// messaging/checkpoint.go

func SaveCheckpoint(chatID, reply string) error {
    path := filepath.Join(checkpointDir, chatID+".json")
    data, _ := json.Marshal(map[string]string{"reply": reply, "ts": time.Now().Format(time.RFC3339)})
    return os.WriteFile(path, data, 0o600)
}

func LoadCheckpoint(chatID string) (string, bool) { ... }
func ClearCheckpoint(chatID string) { os.Remove(...) }
```

Integration in `dispatchToAgent`:
```go
reply, err := h.chatWithAgent(ctx, ag, conversationID, message)
if err == nil && reply != "" {
    SaveCheckpoint(post.GroupID, reply) // save before sending
}
h.sendReplyWithActions(ctx, client, readClient, post, reply, placeholderID)
ClearCheckpoint(post.GroupID) // clear after successful send
```

On startup, check for orphaned checkpoints and retry sending.

## Future: Full Flow Engine (when needed)

### Core Types

```go
// messaging/flow.go (future)

type FlowOutcome string
const (
    OutcomeOk       FlowOutcome = "ok"
    OutcomeTimedOut FlowOutcome = "timed_out"
    OutcomeFailed   FlowOutcome = "failed"
)

type FlowNode interface {
    Name() string
    Run(ctx *FlowContext) (interface{}, error)
}

type FlowDefinition struct {
    Name    string
    StartAt string
    Nodes   map[string]FlowNode
    Edges   []FlowEdge
}

type FlowEngine struct {
    flows map[string]*FlowDefinition
}
```

### Built-in Nodes (future)

- `AgentNode` — call AI agent with per-node timeout
- `ParseActionsNode` — extract ACTION blocks from reply
- `SendReplyNode` — send to RingCentral
- `CheckpointNode` — save intermediate state
- `ComputeNode` — generic computation

## File Layout

### Phase 1 (checkpoint only)
```
messaging/checkpoint.go       # SaveCheckpoint, LoadCheckpoint, ClearCheckpoint
messaging/checkpoint_test.go  # roundtrip, orphan detection
```

### Future (full engine)
```
messaging/flow.go             # FlowDefinition, FlowEngine, FlowContext
messaging/flow_nodes.go       # Built-in node types
messaging/flow_test.go        # Engine tests
```

## Implementation Phases

### Phase 1: Checkpoint (implement now if needed)
1. Create `messaging/checkpoint.go`
2. Integrate into `dispatchToAgent`
3. Orphan checkpoint recovery on startup
4. Test: save/load roundtrip, clear after send

### Phase 2-4: Full Engine (defer until concrete use case)
2. Flow engine core: node execution, edge matching, result tracking
3. Default flows: simple_chat replaces dispatchToAgent
4. Advanced flows: intent classification, multi-agent fan-out

## Test Plan

- Checkpoint: save/load roundtrip, clear, orphan detection, concurrent access
- Future engine: linear flow, branching, timeout propagation, cancellation

## References

- acpx `FlowDefinition`: startAt, nodes, edges
- acpx `FlowNodeResult`: outcome, duration, output
- acpx node types: acp, action, compute, checkpoint
- acpx `src/flows/definition.ts`, `src/flows/runtime.ts`
