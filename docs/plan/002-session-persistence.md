# Plan 002: Session Persistence

**Date:** 2026-04-11
**Priority:** P1
**Status:** Draft
**Reference:** acpx `2026-02-17-session-management.md`, acpx `src/session/persistence/`

## Problem Statement

ACP agent session mappings (`conversationID → sessionID`) are stored in memory only (`agent/acp_agent.go:59: sessions map[string]string`). When the process restarts (upgrade, crash, `/reload`, machine reboot), all sessions are lost:

1. Every conversation starts fresh after restart, losing multi-turn context
2. The ACP subprocess may still be running with valid sessions, but RingClaw has lost the mapping
3. `/new` command state is not persisted

## Goals

1. Persist `conversationID → sessionID` mappings to disk, restore on startup
2. Include minimal metadata (agent name, timestamps) for diagnostics
3. Implement stale session cleanup
4. Keep the `Agent` interface unchanged

## Non-Goals

- Persisting full message history (managed by ACP agents themselves)
- Cross-machine session replication
- Session encryption
- CLI/HTTP agent session persistence (Phase 1 is ACP only)

## Current State

```go
// agent/acp_agent.go
type ACPAgent struct {
    sessions map[string]string // conversationID -> sessionID (in-memory only)
}

// Initialized empty on every start:
sessions: make(map[string]string)
```

## Proposed Solution

### SessionRecord (minimal)

```go
// agent/session_store.go

type SessionRecord struct {
    ConversationID string    `json:"conversationId"`
    ACPSessionID   string    `json:"acpSessionId"`
    AgentName      string    `json:"agentName"`
    CreatedAt      time.Time `json:"createdAt"`
    LastUsedAt     time.Time `json:"lastUsedAt"`
}
```

### SessionStore

```go
type SessionStore struct {
    mu      sync.RWMutex
    path    string                    // ~/.ringclaw/sessions.json
    records map[string]*SessionRecord // conversationID -> record
}

func NewSessionStore(path string) *SessionStore
func (s *SessionStore) Load() error                        // read from disk
func (s *SessionStore) Save() error                        // write to disk
func (s *SessionStore) Get(convID string) (string, bool)   // returns sessionID
func (s *SessionStore) Put(r *SessionRecord) error         // upsert + save
func (s *SessionStore) Touch(convID string)                // update LastUsedAt
func (s *SessionStore) Delete(convID string)               // remove + save
func (s *SessionStore) CleanStale(maxAge time.Duration) int // remove old entries
```

### ACPAgent Integration

```go
type ACPAgent struct {
    // ... existing fields ...
    store *SessionStore // nil = disabled (backward compatible)
}
```

Key changes to `getOrCreateSession`:
1. Check `store.Get(conversationID)` first — if found, reuse sessionID
2. If agent rejects the sessionID (session expired/unknown), create new session and update store
3. On successful chat, call `store.Touch(conversationID)`
4. On `ResetSession`, call `store.Delete(conversationID)`

### Recovery Strategy

No PID liveness check needed. Simple approach:
1. On startup, load sessions from disk
2. On first use of a restored session, send the prompt normally
3. If agent returns a session error, transparently create a new session
4. This handles all cases: agent still running (reuse works), agent restarted (fallback works)

> **Note:** Check if ACP `session/new` supports a `resume` parameter. If claude-agent-acp supports it, we can pass the old sessionID as a hint.

## File Layout

```
agent/session_store.go       # SessionRecord, SessionStore
agent/session_store_test.go  # CRUD, Save/Load roundtrip, CleanStale, concurrency
```

Modified:
```
agent/acp_agent.go           # Add store field, integrate into getOrCreateSession/chat/Reset
agent/registry.go            # Pass SessionStore through Create()
cmd/start_init.go            # Initialize SessionStore, CleanStale(7 days) on startup
```

## Implementation Phases

### Phase 1: SessionStore
1. Create `agent/session_store.go` with CRUD + Load/Save
2. Single file at `~/.ringclaw/sessions.json`
3. Tests: Put/Get/Touch/Delete/CleanStale/concurrent access (-race)

### Phase 2: ACPAgent Integration
1. Add `store` field to ACPAgent
2. `getOrCreateSession` checks store first
3. Transparent fallback on session error
4. `ResetSession` deletes from store

### Phase 3: Startup Integration
1. `cmd/start_init.go` creates SessionStore, calls Load()
2. `CleanStale(7 * 24 * time.Hour)` on startup
3. Pass store through agent registry

## Test Plan

- SessionStore: CRUD, Save+Load roundtrip, CleanStale, concurrent access (-race)
- ACPAgent: getOrCreateSession reads from store, ResetSession deletes, fallback on error
- Integration: create → stop → load → resume cycle

## References

- acpx `src/session/persistence/repository.ts`: JSON file storage
- acpx SessionRecord: acpxRecordId, acpSessionId, agentCommand, cwd, timestamps
- acpx loadSession protocol for recovery
