package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsSetModeUnsupportedErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unrelated", errors.New("connection refused"), false},
		{"invalid mode", errors.New("agent error (code -32603): Invalid Mode"), true},
		{"invalid mode lowercase", errors.New("agent error: invalid mode"), true},
		{"unknown mode", errors.New("Unknown Mode"), true},
		{"method not found", errors.New("Method not found"), true},
		{"unsupported mode", errors.New("unsupported mode"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSetModeUnsupportedErr(tc.err); got != tc.want {
				t.Errorf("isSetModeUnsupportedErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestGetOrCreateSession_SetModeUnsupportedCache verifies that after the
// first set_mode rejection ("Invalid Mode"), subsequent session creations
// skip the set_mode RPC entirely.
func TestGetOrCreateSession_SetModeUnsupportedCache(t *testing.T) {
	SetFullAccessAck(true)
	t.Cleanup(ResetFullAccessAck)

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:      stdinWriter,
		scanner:    bufio.NewScanner(stdoutReader),
		started:    true,
		pending:    make(map[int64]chan *rpcResponse),
		notifyCh:   make(map[string]chan *sessionUpdate),
		sessions:   make(map[string]string),
		cwd:        t.TempDir(),
		fullAccess: true,
		termMgr:    newTerminalManager(t.TempDir()),
		stderr:     &acpStderrWriter{prefix: "[test]"},
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()

	var setModeCalls int32
	sessionCounter := 0
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		for scanner.Scan() {
			line := scanner.Text()
			var req rpcRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				continue
			}
			switch req.Method {
			case "session/new":
				sessionCounter++
				resp := rpcResponseOut{
					JSONRPC: "2.0",
					ID:      mustMarshal(req.ID),
					Result:  map[string]string{"sessionId": fmt.Sprintf("sess-%d", sessionCounter)},
				}
				data, _ := json.Marshal(resp)
				fmt.Fprintf(stdoutWriter, "%s\n", data)
			case "session/set_mode":
				atomic.AddInt32(&setModeCalls, 1)
				// Always reject with Invalid Mode
				resp := rpcResponseOut{
					JSONRPC: "2.0",
					ID:      mustMarshal(req.ID),
					Error:   &rpcError{Code: -32603, Message: "Invalid Mode"},
				}
				data, _ := json.Marshal(resp)
				fmt.Fprintf(stdoutWriter, "%s\n", data)
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First session: set_mode is attempted, fails, flag is set
	if _, _, err := a.getOrCreateSession(ctx, "conv-1", Origin{IsOwner: true}); err != nil {
		t.Fatalf("session 1: %v", err)
	}
	if got := atomic.LoadInt32(&setModeCalls); got != 1 {
		t.Errorf("after first session: expected 1 set_mode call, got %d", got)
	}
	if !a.setModeUnsupported.Load() {
		t.Error("expected setModeUnsupported flag to be true after Invalid Mode error")
	}

	// Second session: set_mode should be skipped
	if _, _, err := a.getOrCreateSession(ctx, "conv-2", Origin{IsOwner: true}); err != nil {
		t.Fatalf("session 2: %v", err)
	}
	if got := atomic.LoadInt32(&setModeCalls); got != 1 {
		t.Errorf("after second session: expected set_mode to be skipped (still 1 call), got %d", got)
	}

	// Third session: still skipped
	if _, _, err := a.getOrCreateSession(ctx, "conv-3", Origin{IsOwner: true}); err != nil {
		t.Fatalf("session 3: %v", err)
	}
	if got := atomic.LoadInt32(&setModeCalls); got != 1 {
		t.Errorf("after third session: expected set_mode to be skipped (still 1 call), got %d", got)
	}

	stdinWriter.Close()
	stdoutWriter.Close()
}

// TestDemoteFullAccessSessions_SkippedWhenSetModeUnsupported verifies the
// demote path also short-circuits when the agent has already been flagged
// as not supporting set_mode.
func TestDemoteFullAccessSessions_SkippedWhenSetModeUnsupported(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: map[string]string{"conv-a": "sid-a", "conv-b": "sid-b"},
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.setModeUnsupported.Store(true)

	var setModeCalls int32
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		for scanner.Scan() {
			line := scanner.Text()
			var req rpcRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				continue
			}
			if req.Method == "session/set_mode" {
				atomic.AddInt32(&setModeCalls, 1)
			}
		}
	}()

	a.demoteFullAccessSessions(context.Background())

	// Give the goroutine a moment to receive nothing
	time.Sleep(50 * time.Millisecond)

	if got := atomic.LoadInt32(&setModeCalls); got != 0 {
		t.Errorf("expected demote to skip set_mode when unsupported, got %d calls", got)
	}

	stdinWriter.Close()
	stdoutWriter.Close()
	_ = stdoutReader
}
