# Plan 001: Mock Agent Testing Framework

**Date:** 2026-04-11
**Priority:** P0
**Status:** Draft
**Reference:** acpx `2026-02-19-mock-agent-testing.md`, acpx `test/mock-agent.ts`

## Problem Statement

RingClaw has zero protocol-level ACP testing. The existing `internal/testutil/mock_agent.go` implements the `Agent` interface (Chat/ResetSession/SetCwd/Info) but does NOT simulate the ACP JSON-RPC protocol over stdio. Current ACP agent tests (`agent/acp_agent_test.go`) only cover data extraction helpers (`extractChunkText`, `extractPromptResultText`).

This means we cannot test:
1. Session lifecycle (initialize → session/new → prompt → response)
2. Error scenarios (timeout, crash, protocol error)
3. Streaming chunks (`agent_message_chunk` notifications)
4. The full Handler → ACPAgent → reply pipeline
5. Multi-turn conversation state

## Goals

1. Build a protocol-level ACP mock that communicates via JSON-RPC 2.0 over pipes
2. Support configurable behaviors: fixed replies, errors, delays
3. Manage session state (create, track, reset)
4. Enable end-to-end testing: incoming message → agent call → reply sent
5. Keep it simple — test utility, not a production agent simulator

## Non-Goals

- Full ACP spec coverage (terminal/*, fs/* capabilities) — session and prompt only
- Replacing existing `internal/testutil/MockAgent` — that stays for interface-level tests
- Testing CLI/HTTP agent types (separate mocks needed)
- External test framework dependencies — use standard `testing` package

## Existing Infrastructure

- `internal/testutil/mock_agent.go` — implements `agent.Agent` interface, records Chat calls, returns configurable responses. **Interface-level only.**
- `internal/testutil/mock_sender.go` — implements `ringcentral.MessageSender`, records all API calls for assertion.
- `agent/acp_agent.go:Start()` — sets up stdin/stdout pipes via `cmd.StdinPipe()` / `cmd.StdoutPipe()`. The mock needs to provide equivalent pipe endpoints.

## Proposed Solution

### MockACPAgent

```go
// agent/mock_acp_test.go (test-only)

type MockACPBehavior struct {
    Reply         string                          // default reply text
    DelayReply    time.Duration                   // delay before replying
    ErrorOnPrompt int                             // fail on Nth prompt (-1 = always)
    ErrorMessage  string                          // error message to return
    PromptHandler func(prompt string) (string, error) // custom handler
}

type MockACPAgent struct {
    behavior    MockACPBehavior
    mu          sync.Mutex
    sessions    map[string][]string // sessionID -> prompts received
    promptCount int
}
```

### Pipe Connection

Use `os.Pipe()` to create stdin/stdout pairs, then construct an `ACPAgent` whose subprocess pipes point to the mock:

```go
func (m *MockACPAgent) Start(ctx context.Context) (*ACPAgent, error) {
    // Create two pipe pairs:
    // agentStdin  (mock reads, ACPAgent writes)
    // agentStdout (mock writes, ACPAgent reads)
    mockIn, agentStdin, _ := os.Pipe()
    agentStdout, mockOut, _ := os.Pipe()

    // Start mock read loop in goroutine
    go m.readLoop(ctx, mockIn, mockOut)

    // Construct ACPAgent with injected pipes
    // (requires adding a test constructor or using unexported fields)
    ...
}
```

### JSON-RPC Methods Handled

| Method | Response |
|--------|----------|
| `initialize` | `{ protocolVersion: "2025-01-01", capabilities: {} }` |
| `session/new` | `{ id: "mock-session-{n}" }` |
| `session/prompt` | Configurable via `MockACPBehavior` |
| `session/set_mode` | Empty success response |

## File Layout

```
agent/mock_acp_test.go       # MockACPAgent + MockACPBehavior (test-only)
agent/acp_agent_test.go      # New protocol-level tests using MockACPAgent
```

## Implementation Phases

### Phase 1: Core Mock (target)
1. Create `agent/mock_acp_test.go` with pipe-based JSON-RPC mock
2. Handle `initialize`, `session/new`, `session/prompt` with fixed replies
3. Test: full lifecycle — Start → initialize → session/new → prompt → response
4. Test: multi-turn conversation (same session reused)

### Phase 2: Error Simulation
1. Add `ErrorOnPrompt`, `ErrorMessage`, `DelayReply`
2. Test: agent timeout, protocol error, empty response
3. Test: session reset (ResetSession creates new session)

### Phase 3: Handler Integration
1. End-to-end test: Handler + MockACPAgent + MockSender
2. Test: incoming post → agent call → SendTextReply recorded
3. Test: ACTION blocks parsed and executed

## Test Plan

- MockACP unit tests: initialize handshake, session create, prompt/response, error simulation
- ACPAgent integration: Start → Chat → response, ResetSession → new session, concurrent sessions
- Handler integration: HandleMessage → ACPAgent → MockSender records correct reply

## References

- acpx `test/mock-agent.ts`: full ACP protocol mock with configurable behaviors
- acpx command-style prompt handling: echo, error, delay
- `agent/acp_agent.go:Start()`: pipe setup via `cmd.StdinPipe()` / `cmd.StdoutPipe()`
- `agent/acp_rpc.go`: JSON-RPC message parsing and dispatch
