package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeACPSetup wires an ACPAgent up against in-memory pipes so tests
// can drive session/new + session/set_mode interactions deterministically.
type fakeACPSetup struct {
	a              *ACPAgent
	stdoutWriter   *io.PipeWriter
	stdinReader    *io.PipeReader
	stdinWriter    *io.PipeWriter
	stdoutReader   *io.PipeReader
	setModeCalls   *int32
	setModeArgs    *[]map[string]interface{}
	setModeBehave  func(req rpcRequest) rpcResponseOut
	availableModes []SessionMode
}

func newFakeACPSetup(t *testing.T, command string, modes []SessionMode, setMode func(rpcRequest) rpcResponseOut) *fakeACPSetup {
	t.Helper()
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		command:  command,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		cwd:      t.TempDir(),
		termMgr:  newTerminalManager(t.TempDir()),
		stderr:   &acpStderrWriter{prefix: "[test]"},
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()

	var setModeCalls int32
	var setModeArgs []map[string]interface{}
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		sessionCounter := 0
		for scanner.Scan() {
			line := scanner.Text()
			var req rpcRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				continue
			}
			switch req.Method {
			case "session/new":
				sessionCounter++
				newResp := newSessionResult{
					SessionID: fmt.Sprintf("sess-%d", sessionCounter),
				}
				if len(modes) > 0 {
					newResp.Modes = &sessionModesField{
						AvailableModes: modes,
					}
				}
				resp := rpcResponseOut{
					JSONRPC: "2.0",
					ID:      mustMarshal(req.ID),
					Result:  newResp,
				}
				data, _ := json.Marshal(resp)
				fmt.Fprintf(stdoutWriter, "%s\n", data)
			case "session/set_mode":
				atomic.AddInt32(&setModeCalls, 1)
				var raw struct {
					Params map[string]interface{} `json:"params"`
				}
				_ = json.Unmarshal([]byte(line), &raw)
				setModeArgs = append(setModeArgs, raw.Params)
				resp := setMode(req)
				resp.JSONRPC = "2.0"
				resp.ID = mustMarshal(req.ID)
				data, _ := json.Marshal(resp)
				fmt.Fprintf(stdoutWriter, "%s\n", data)
			}
		}
	}()

	return &fakeACPSetup{
		a:              a,
		stdoutWriter:   stdoutWriter,
		stdinReader:    stdinReader,
		stdinWriter:    stdinWriter,
		stdoutReader:   stdoutReader,
		setModeCalls:   &setModeCalls,
		setModeArgs:    &setModeArgs,
		setModeBehave:  setMode,
		availableModes: modes,
	}
}

func (s *fakeACPSetup) close() {
	s.stdoutWriter.Close()
	s.stdinWriter.Close()
}

func TestGetOrCreateSession_OwnerSkipsRestrictedSetMode(t *testing.T) {
	setup := newFakeACPSetup(t, "droid",
		[]SessionMode{{ID: "spec"}, {ID: "auto-medium"}},
		func(req rpcRequest) rpcResponseOut {
			return rpcResponseOut{Result: struct{}{}}
		},
	)
	defer setup.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sid, _, err := setup.a.getOrCreateSession(ctx, "conv-owner", Origin{IsOwner: true})
	if err != nil {
		t.Fatalf("session error: %v", err)
	}
	if sid == "" {
		t.Fatal("empty sessionID")
	}
	// owner path takes the legacy full-access branch, which is gated
	// on a.fullAccess (false here), so set_mode should never be
	// called.
	if got := atomic.LoadInt32(setup.setModeCalls); got != 0 {
		t.Errorf("expected 0 set_mode calls for owner, got %d", got)
	}
}

func TestGetOrCreateSession_NonOwnerCallsSetModeOnce(t *testing.T) {
	setup := newFakeACPSetup(t, "droid",
		[]SessionMode{{ID: "spec"}, {ID: "auto-medium"}},
		func(req rpcRequest) rpcResponseOut {
			return rpcResponseOut{Result: struct{}{}}
		},
	)
	defer setup.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sid, _, err := setup.a.getOrCreateSession(ctx, "conv-evil", Origin{IsOwner: false, SenderID: "u9"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(setup.setModeCalls); got != 1 {
		t.Errorf("expected 1 set_mode call, got %d", got)
	}
	args := *setup.setModeArgs
	if len(args) != 1 || args[0]["modeId"] != "spec" {
		t.Errorf("expected set_mode modeId=spec, got %+v", args)
	}
	// session role should be cached as non-owner so the gate still
	// denies fs/* calls coming back over the wire.
	v, ok := setup.a.sessionRoles.Load(sid)
	if !ok {
		t.Fatal("sessionRoles missing entry for new session")
	}
	if origin := v.(Origin); origin.IsOwner {
		t.Errorf("expected non-owner origin in sessionRoles, got %+v", origin)
	}
}

func TestGetOrCreateSession_NonOwnerSetModeUnsupported_FailsClosed(t *testing.T) {
	setup := newFakeACPSetup(t, "droid",
		[]SessionMode{{ID: "spec"}},
		func(req rpcRequest) rpcResponseOut {
			return rpcResponseOut{
				Error: &rpcError{Code: -32601, Message: "Method not found"},
			}
		},
	)
	defer setup.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, err := setup.a.getOrCreateSession(ctx, "conv-evil", Origin{IsOwner: false, SenderID: "u9"})
	if !errors.Is(err, errRestrictedModeUnsupported) {
		t.Fatalf("expected errRestrictedModeUnsupported, got %v", err)
	}
	// session should NOT have been registered in a.sessions.
	setup.a.mu.Lock()
	_, registered := setup.a.sessions["conv-evil"]
	setup.a.mu.Unlock()
	if registered {
		t.Error("conv-evil session should have been rolled back on fail-closed")
	}
	// Cache entry should be set so subsequent attempts skip the call.
	if _, cached := setup.a.restrictedSetModeUnsupported.Load("droid|spec"); !cached {
		t.Error("expected setModeUnsupported cache entry")
	}
}

func TestGetOrCreateSession_NoRestrictedModeAvailable_FailsClosed(t *testing.T) {
	// Agent advertises only "yolo" — no plan/spec/read keyword.
	setup := newFakeACPSetup(t, "/opt/strange-agent",
		[]SessionMode{{ID: "yolo"}},
		func(req rpcRequest) rpcResponseOut {
			return rpcResponseOut{Result: struct{}{}}
		},
	)
	defer setup.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, err := setup.a.getOrCreateSession(ctx, "conv-evil", Origin{IsOwner: false})
	if !errors.Is(err, errRestrictedModeUnsupported) {
		t.Fatalf("expected errRestrictedModeUnsupported, got %v", err)
	}
	// set_mode should not have been called at all (no candidate modeID).
	if got := atomic.LoadInt32(setup.setModeCalls); got != 0 {
		t.Errorf("expected 0 set_mode calls when no mode available, got %d", got)
	}
}

func TestChat_NonOwnerFailClosed_ReturnsRefusalText(t *testing.T) {
	setup := newFakeACPSetup(t, "/opt/strange-agent",
		[]SessionMode{{ID: "yolo"}},
		func(req rpcRequest) rpcResponseOut {
			return rpcResponseOut{Result: struct{}{}}
		},
	)
	defer setup.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ctx = WithOrigin(ctx, Origin{IsOwner: false, SenderID: "u9"})
	reply, err := setup.a.chatWithEntries(ctx, "conv-evil", []promptEntry{{Type: "text", Text: "hi"}})
	if err != nil {
		t.Fatalf("expected nil error (fail-closed converts to refusal text), got %v", err)
	}
	if !strings.Contains(reply, "fail-closed") || !strings.Contains(reply, "non-owner") {
		t.Errorf("expected refusal text, got %q", reply)
	}
}

func TestRestrictedModeRefusalText_IncludesCommand(t *testing.T) {
	got := restrictedModeRefusalText("droid")
	if !strings.Contains(got, "droid") {
		t.Errorf("expected command in refusal, got %q", got)
	}
}
