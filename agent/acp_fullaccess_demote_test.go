package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newDemoteAgent builds an ACPAgent that is plumbed into a mock
// stdin/stdout pipe pair and prepopulates a.sessions with a known
// (conversationID -> sessionID) map so demoteFullAccessSessions has
// something to operate on. The caller drives the mock ACP responder
// via the returned reader/writer. Unlike newMockACPAgentForChat we
// mark the agent as started so demotion is not an early no-op.
func newDemoteAgent(t *testing.T, sessions map[string]string) (a *ACPAgent, stdinReader *io.PipeReader, stdoutWriter *io.PipeWriter) {
	t.Helper()
	stdinReader2, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter2 := io.Pipe()
	a = &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		command:  "mock-acp",
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: sessions,
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	go a.readLoop()
	return a, stdinReader2, stdoutWriter2
}

// respondSetMode reads session/set_mode requests from the mock stdin
// and responds according to `respond(sessionID) -> (err string, ok)`.
// Returns the set of sessionIDs that were targeted by set_mode
// "default" so callers can assert ordering / completeness.
func respondSetMode(t *testing.T, stdinReader io.Reader, stdoutWriter io.Writer, respond func(sessionID string) (errMsg string, ok bool)) (*sync.Map, func()) {
	t.Helper()
	var seen sync.Map // sessionID -> modeID
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(stdinReader)
		scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
		for scanner.Scan() {
			line := scanner.Text()
			var req struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
				Params struct {
					SessionID string `json:"sessionId"`
					ModeID    string `json:"modeId"`
				} `json:"params"`
			}
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				continue
			}
			if req.Method != "session/set_mode" {
				continue
			}
			seen.Store(req.Params.SessionID, req.Params.ModeID)
			if errMsg, ok := respond(req.Params.SessionID); !ok {
				resp := rpcResponseOut{
					JSONRPC: "2.0",
					ID:      mustMarshal(req.ID),
					Error:   &rpcError{Code: -32000, Message: errMsg},
				}
				data, _ := json.Marshal(resp)
				fmt.Fprintf(stdoutWriter, "%s\n", data)
			} else {
				resp := rpcResponseOut{
					JSONRPC: "2.0",
					ID:      mustMarshal(req.ID),
					Result:  map[string]string{},
				}
				data, _ := json.Marshal(resp)
				fmt.Fprintf(stdoutWriter, "%s\n", data)
			}
		}
	}()
	return &seen, func() { <-done }
}

// TestDemoteFullAccessSessions_Success: every session receives
// session/set_mode "default" and, on success, STAYS in a.sessions so
// conversation history is preserved.
func TestDemoteFullAccessSessions_Success(t *testing.T) {
	sessions := map[string]string{
		"conv-a": "sid-a",
		"conv-b": "sid-b",
	}
	a, stdinReader, stdoutWriter := newDemoteAgent(t, sessions)
	defer stdinReader.Close()
	defer stdoutWriter.Close()

	seen, _ := respondSetMode(t, stdinReader, stdoutWriter, func(string) (string, bool) {
		return "", true
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.demoteFullAccessSessions(ctx)

	for _, want := range []string{"sid-a", "sid-b"} {
		gotAny, ok := seen.Load(want)
		if !ok {
			t.Fatalf("expected set_mode to be sent for %s", want)
		}
		if gotAny.(string) != "default" {
			t.Fatalf("expected modeId=default for %s, got %q", want, gotAny)
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.sessions) != 2 {
		t.Fatalf("on success sessions must be preserved, got %v", a.sessions)
	}
}

// TestDemoteFullAccessSessions_FailureDropsSession: when set_mode
// returns an error for a given session, that entry must be removed
// from a.sessions so the next prompt for that conversation rebuilds a
// fresh (default-mode) session.
func TestDemoteFullAccessSessions_FailureDropsSession(t *testing.T) {
	sessions := map[string]string{
		"conv-a": "sid-good",
		"conv-b": "sid-bad",
	}
	a, stdinReader, stdoutWriter := newDemoteAgent(t, sessions)
	defer stdinReader.Close()
	defer stdoutWriter.Close()

	_, _ = respondSetMode(t, stdinReader, stdoutWriter, func(sid string) (string, bool) {
		if sid == "sid-bad" {
			return "agent stuck", false
		}
		return "", true
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.demoteFullAccessSessions(ctx)

	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.sessions["conv-a"]; !ok {
		t.Fatalf("successful session should stay in map, got %v", a.sessions)
	}
	if _, ok := a.sessions["conv-b"]; ok {
		t.Fatalf("failed demotion should drop session from map, got %v", a.sessions)
	}
}

// TestDemoteFullAccessSessions_StoppedAgentIsNoop: a Stopped agent
// must not attempt any RPCs; the guard prevents us from writing to a
// closed stdin (which would panic/error).
func TestDemoteFullAccessSessions_StoppedAgentIsNoop(t *testing.T) {
	a := &ACPAgent{
		sessions: map[string]string{"c": "s"},
		started:  false,
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.demoteFullAccessSessions(context.Background())
	// If we got here without panicking, the guard held.
}

// TestDemoteAllACPFullAccess_WalksRegistry: the package-level walker
// must reach every registered agent and skip unregistered ones.
func TestDemoteAllACPFullAccess_WalksRegistry(t *testing.T) {
	sA, sB := map[string]string{"c-a": "sid-a"}, map[string]string{"c-b": "sid-b"}
	agentA, readerA, writerA := newDemoteAgent(t, sA)
	agentB, readerB, writerB := newDemoteAgent(t, sB)
	defer readerA.Close()
	defer writerA.Close()
	defer readerB.Close()
	defer writerB.Close()

	var demotedA, demotedB int32
	_, _ = respondSetMode(t, readerA, writerA, func(sid string) (string, bool) {
		if sid == "sid-a" {
			atomic.AddInt32(&demotedA, 1)
		}
		return "", true
	})
	_, _ = respondSetMode(t, readerB, writerB, func(sid string) (string, bool) {
		if sid == "sid-b" {
			atomic.AddInt32(&demotedB, 1)
		}
		return "", true
	})

	registerActiveACPAgent(agentA)
	registerActiveACPAgent(agentB)
	t.Cleanup(func() {
		unregisterActiveACPAgent(agentA)
		unregisterActiveACPAgent(agentB)
	})

	DemoteAllACPFullAccess(context.Background())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&demotedA) == 1 && atomic.LoadInt32(&demotedB) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&demotedA) != 1 {
		t.Fatalf("agent A did not receive set_mode default for sid-a")
	}
	if atomic.LoadInt32(&demotedB) != 1 {
		t.Fatalf("agent B did not receive set_mode default for sid-b")
	}

	// Unregister A, run again: only B should be targeted.
	unregisterActiveACPAgent(agentA)
	atomic.StoreInt32(&demotedA, 0)
	atomic.StoreInt32(&demotedB, 0)
	DemoteAllACPFullAccess(context.Background())

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&demotedB) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&demotedA) != 0 {
		t.Fatalf("agent A was unregistered but still received set_mode: count=%d", atomic.LoadInt32(&demotedA))
	}
	if atomic.LoadInt32(&demotedB) != 1 {
		t.Fatalf("agent B should still be reachable after A unregisters, count=%d", atomic.LoadInt32(&demotedB))
	}
}
