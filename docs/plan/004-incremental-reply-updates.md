# Plan 004: Incremental Reply Updates

**Date:** 2026-04-11
**Priority:** P3
**Status:** Draft
**Reference:** acpx `json-patch-plus.md`, acpx `2026-03-31-flow-replay-live-transport.md`

## Problem Statement

RingClaw currently uses a two-step reply process:
1. Send a "Thinking..." placeholder immediately (`SendTypingPlaceholder`)
2. Replace with the full response when the agent finishes (`UpdatePostText`)

Problems:
1. **No incremental updates** — users may wait minutes seeing only "Thinking...", then the full reply appears at once
2. **Wasted reading time** — for long responses, users can't read earlier parts while the agent is still writing
3. **No cancel feedback** — when a user sends `/new` to interrupt, there's no way to show partial progress
4. **Update conflicts** — rapid updates may fail due to RC API rate limiting

## Key Insight: Chunks Already Arrive

ACP agents already send `agent_message_chunk` notifications during streaming. RingClaw receives them in `acp_rpc.go:207` but **suppresses them**:

```go
case "agent_message_chunk", "agent_thought_chunk":
    // Suppress noisy streaming chunks
```

The data is already there — we just need to accumulate it and periodically update the placeholder.

## Goals

1. Incrementally update the placeholder with partial text as chunks arrive
2. Throttle updates to respect RingCentral API rate limits
3. Support cancellation with partial result display
4. Backward compatible — disable-able via config, non-ACP agents unchanged

## Non-Goals

- SSE/WebSocket transport to RC client (RC handles its own push)
- JSON Patch protocol (RC API uses `UpdatePostText` with full text, no patches)
- Character-level streaming (too many API calls)
- CLI/HTTP agent streaming in Phase 1

## Proposed Solution

### StreamState

```go
// messaging/streaming.go

type StreamingConfig struct {
    Enabled     bool          `json:"streaming_enabled"`
    MinInterval time.Duration `json:"streaming_interval"`  // default 2s
    MinNewChars int           `json:"streaming_min_chars"` // default 100
    MaxUpdates  int           `json:"streaming_max_updates"` // default 20
}

type StreamState struct {
    mu            sync.Mutex
    chatID        string
    placeholderID string
    client        *ringcentral.Client
    text          strings.Builder
    lastSentLen   int
    lastSentAt    time.Time
    updateCount   int
    done          bool
    config        StreamingConfig
}

func NewStreamState(chatID, placeholderID string, client *ringcentral.Client, cfg StreamingConfig) *StreamState

// Append adds new text. If enough text has accumulated AND enough time
// has passed since the last update, sends an UpdatePostText call.
func (s *StreamState) Append(ctx context.Context, chunk string) error

// Finalize sends the final complete text.
func (s *StreamState) Finalize(ctx context.Context, finalText string) error

// Cancel shows partial text with a "[cancelled]" suffix.
func (s *StreamState) Cancel(ctx context.Context)
```

### Agent Interface Extension

```go
// agent/agent.go

// Streamer is an optional interface for agents that support streaming.
type Streamer interface {
    ChatStream(ctx context.Context, conversationID, message string, onChunk func(chunk string)) (string, error)
}
```

ACP agents implement `Streamer` by forwarding `agent_message_chunk` notifications to the callback. Non-ACP agents don't implement it and continue using the existing `Chat()` path.

### Handler Integration

```go
// messaging/handler.go — updated dispatchToAgent

placeholderID, _ := SendTypingPlaceholder(ctx, client, post.GroupID)

if streamer, ok := ag.(agent.Streamer); ok && h.streamingCfg.Enabled && placeholderID != "" {
    streamState := NewStreamState(post.GroupID, placeholderID, client, h.streamingCfg)
    reply, err = streamer.ChatStream(ctx, convID, message, func(chunk string) {
        streamState.Append(ctx, chunk)
    })
    streamState.Finalize(ctx, cleanReply)
} else {
    reply, err = ag.Chat(ctx, convID, message)
    // existing UpdatePostText path
}
```

## RC API Rate Limits

Before choosing `MinInterval`, verify:
- RingCentral Team Messaging `PATCH /chats/{chatId}/posts/{postId}` rate limit
- Typical limit is ~30 req/min per extension — `MinInterval=2s` ≈ 30 updates/min, should be safe
- `MaxUpdates=20` provides additional safety net

## File Layout

```
messaging/streaming.go       # StreamState, StreamingConfig
messaging/streaming_test.go  # throttling behavior, Finalize, Cancel
```

Modified:
```
agent/agent.go               # Add Streamer optional interface
agent/acp_agent.go           # Implement ChatStream, forward chunks
agent/acp_rpc.go             # Stop suppressing agent_message_chunk, forward to callback
messaging/handler.go         # Add streamingCfg, use ChatStream when available
config/config.go             # Add StreamingConfig
```

## Implementation Phases

### Phase 1: StreamState + Throttling
1. Create `messaging/streaming.go`
2. Append with MinInterval + MinNewChars + MaxUpdates throttling
3. Finalize and Cancel
4. Tests: throttling behavior, edge cases

### Phase 2: ACP Streaming
1. Add `Streamer` interface to `agent/agent.go`
2. Implement `ChatStream` in `agent/acp_agent.go`
3. Forward `agent_message_chunk` notifications instead of suppressing
4. Test with MockACPAgent (depends on Plan 001)

### Phase 3: Handler Integration
1. Add `streamingCfg` to Handler
2. Use `ChatStream` when available, fall back to `Chat`
3. End-to-end test with MockACPAgent + MockSender

## Test Plan

- StreamState: Append accumulation, throttle conditions, Finalize, Cancel, concurrent access (-race)
- Streamer: ChatStream receives chunks, returns final text
- Integration: MockACPAgent streams → StreamState → MockSender.UpdatePost called periodically

## References

- acpx JSON Patch+: extended RFC 6902 with "append" op (not needed for RC — we use full-text UpdatePost)
- acpx SSE transport: snapshot + incremental patches
- `agent/acp_rpc.go:207`: `agent_message_chunk` already received but suppressed
- RC API: `PATCH /team-messaging/v1/chats/{chatId}/posts/{postId}`
