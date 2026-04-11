# Plan 003: Structured Error Codes

**Date:** 2026-04-11
**Priority:** P2
**Status:** Draft
**Reference:** acpx `ACPX_ERROR_STRATEGY.md`, acpx `src/errors.ts`

## Problem Statement

Agent errors in RingClaw use raw `fmt.Errorf` with no classification. The handler wraps them as `"Error: %v"` for user display. This makes it impossible to:

1. Distinguish timeout vs crash vs empty response in cron retry logic
2. Show actionable user messages ("agent timed out, try again" vs "agent crashed, contact admin")
3. Aggregate errors by type in logs

**Note:** The API layer (`api/server.go`) already handles HTTP status codes correctly via `jsonError(w, msg, code)` — this plan focuses on **agent error classification only**.

## Goals

1. Define a small set of agent error codes for the 3 cases that matter
2. Add an `AgentError` type with retryable flag
3. Use it in handler for user-facing messages and in cron for retry decisions
4. Backward compatible — `error` interface unchanged

## Non-Goals

- Full codebase error rewrite (108 `fmt.Errorf` calls are fine as-is)
- Wrapping ACP JSON-RPC errors (spec-defined codes like -32600 are already structured)
- HTTP status code mapping (already working in `api/server.go`)
- Error telemetry/reporting
- gRPC-style status codes

## Current State

```go
// agent/acp_agent.go — raw errors
return "", fmt.Errorf("prompt error: %w", done.err)
return "", fmt.Errorf("prompt timed out after %v", timeout)

// messaging/handler.go — generic display
reply = fmt.Sprintf("Error: %v", err)

// messaging/cron.go — no error classification for retry
```

## Proposed Solution

### AgentError Type

```go
// agent/errors.go

type AgentErrorCode string

const (
    ErrAgentTimeout  AgentErrorCode = "AGENT_TIMEOUT"
    ErrAgentCrash    AgentErrorCode = "AGENT_CRASH"
    ErrAgentEmpty    AgentErrorCode = "AGENT_EMPTY"
)

type AgentError struct {
    Code      AgentErrorCode
    Message   string // human-readable summary
    Retryable bool
    Cause     error
}

func (e *AgentError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }
func (e *AgentError) Unwrap() error { return e.Cause }

// Constructors
func ErrTimeout(cause error) *AgentError
func ErrCrash(cause error) *AgentError
func ErrEmpty() *AgentError

// Helpers
func IsRetryable(err error) bool   // checks AgentError.Retryable
func UserMessage(err error) string // returns user-friendly message
```

### Integration Points

1. **`agent/acp_agent.go`** — wrap timeout/crash/empty errors:
   ```go
   // Before:
   return "", fmt.Errorf("prompt timed out after %v", timeout)
   // After:
   return "", ErrTimeout(fmt.Errorf("prompt timed out after %v", timeout))
   ```

2. **`messaging/handler.go`** — use `UserMessage()`:
   ```go
   // Before:
   reply = fmt.Sprintf("Error: %v", err)
   // After:
   reply = agent.UserMessage(err)
   ```

3. **`messaging/cron.go`** — use `IsRetryable()`:
   ```go
   if agent.IsRetryable(err) {
       slog.Info("retryable error, will retry next interval", ...)
   }
   ```

## File Layout

```
agent/errors.go       # AgentError, codes, constructors, helpers
agent/errors_test.go  # Error(), Unwrap(), IsRetryable(), UserMessage()
```

Modified:
```
agent/acp_agent.go    # Wrap errors with ErrTimeout/ErrCrash/ErrEmpty
agent/cli_agent.go    # Wrap subprocess errors
messaging/handler.go  # Use agent.UserMessage()
messaging/cron.go     # Use agent.IsRetryable()
```

## Implementation Phases

### Phase 1: Error Type
1. Create `agent/errors.go` with 3 codes + constructors + helpers
2. Tests for Error(), Unwrap(), IsRetryable(), UserMessage()

### Phase 2: Agent Integration
1. Wrap errors in `acp_agent.go` (timeout, crash, empty)
2. Wrap errors in `cli_agent.go` (process exit)
3. Verify existing tests still pass

### Phase 3: Handler Integration
1. Update `handler.go` to use `agent.UserMessage()`
2. Update `cron.go` to use `agent.IsRetryable()`

## Test Plan

- AgentError: Error() format, Unwrap() chain, IsRetryable() for each code
- UserMessage: timeout → "timed out, try again", crash → "encountered an error", empty → "returned empty"
- Integration: existing tests pass, handler shows correct user messages

## References

- acpx error codes: NO_SESSION, TIMEOUT, PERMISSION_DENIED, RUNTIME, USAGE
- acpx NormalizedOutputError: code, message, origin, retryable
