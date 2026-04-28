package agent

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readSingleResponse reads one stdin line and unmarshals it as a
// JSON-RPC response. Used by gate tests to read what the bot wrote
// back to the agent.
func readSingleResponse(t *testing.T, r io.Reader) rpcResponseOut {
	t.Helper()
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		t.Fatalf("no response read")
	}
	var resp rpcResponseOut
	if err := json.Unmarshal([]byte(scanner.Text()), &resp); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, scanner.Text())
	}
	return resp
}

func TestGate_OwnerSessionAllowsFSRead(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{stdin: stdinWriter, termMgr: newTerminalManager(dir)}
	a.sessionRoles.Store("s-owner", Origin{IsOwner: true})

	doneCh := make(chan rpcResponseOut, 1)
	go func() { doneCh <- readSingleResponse(t, stdinReader) }()

	raw := `{"jsonrpc":"2.0","id":1,"method":"fs/read_text_file","params":{"sessionId":"s-owner","path":"` + target + `"}}`
	a.handleFSReadTextFile(raw)
	stdinWriter.Close()

	resp := <-doneCh
	if resp.Error != nil {
		t.Fatalf("owner FSRead unexpectedly denied: %+v", resp.Error)
	}
}

func TestGate_NonOwnerDeniesFSRead(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{stdin: stdinWriter, termMgr: newTerminalManager(dir)}
	a.sessionRoles.Store("s-evil", Origin{IsOwner: false, SenderID: "u9", Reason: "chat_user_allow"})

	doneCh := make(chan rpcResponseOut, 1)
	go func() { doneCh <- readSingleResponse(t, stdinReader) }()

	raw := `{"jsonrpc":"2.0","id":2,"method":"fs/read_text_file","params":{"sessionId":"s-evil","path":"` + target + `"}}`
	a.handleFSReadTextFile(raw)
	stdinWriter.Close()

	resp := <-doneCh
	if resp.Error == nil {
		t.Fatalf("non-owner FSRead expected to be denied, got result %v", resp.Result)
	}
	if resp.Error.Code != codeNonOwnerDenied {
		t.Errorf("expected code %d, got %d", codeNonOwnerDenied, resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "non-owner") {
		t.Errorf("expected non-owner message, got %q", resp.Error.Message)
	}
}

func TestGate_NonOwnerDeniesFSWrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")

	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{stdin: stdinWriter, allowWrite: true, termMgr: newTerminalManager(dir)}
	a.sessionRoles.Store("s-evil", Origin{IsOwner: false, SenderID: "u9"})

	doneCh := make(chan rpcResponseOut, 1)
	go func() { doneCh <- readSingleResponse(t, stdinReader) }()

	raw := `{"jsonrpc":"2.0","id":3,"method":"fs/write_text_file","params":{"sessionId":"s-evil","path":"` + target + `","content":"x"}}`
	a.handleFSWriteTextFile(raw)
	stdinWriter.Close()

	resp := <-doneCh
	if resp.Error == nil || resp.Error.Code != codeNonOwnerDenied {
		t.Fatalf("expected codeNonOwnerDenied, got %+v", resp.Error)
	}
	if _, err := os.Stat(target); err == nil {
		t.Errorf("file should not have been written")
	}
}

func TestGate_NonOwnerDeniesTerminalCreate(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{stdin: stdinWriter, termMgr: newTerminalManager(t.TempDir())}
	a.sessionRoles.Store("s-evil", Origin{IsOwner: false})

	doneCh := make(chan rpcResponseOut, 1)
	go func() { doneCh <- readSingleResponse(t, stdinReader) }()

	raw := `{"jsonrpc":"2.0","id":4,"method":"terminal/create","params":{"sessionId":"s-evil","command":"echo","args":["hi"]}}`
	a.handleTerminalCreate(raw)
	stdinWriter.Close()

	resp := <-doneCh
	if resp.Error == nil || resp.Error.Code != codeNonOwnerDenied {
		t.Fatalf("expected codeNonOwnerDenied, got %+v", resp.Error)
	}
}

func TestGate_UnknownSessionFailsClosed(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{stdin: stdinWriter, termMgr: newTerminalManager(t.TempDir())}
	// Note: no sessionRoles.Store — gate should fail-closed.

	doneCh := make(chan rpcResponseOut, 1)
	go func() { doneCh <- readSingleResponse(t, stdinReader) }()

	raw := `{"jsonrpc":"2.0","id":5,"method":"terminal/create","params":{"sessionId":"ghost","command":"echo"}}`
	a.handleTerminalCreate(raw)
	stdinWriter.Close()

	resp := <-doneCh
	if resp.Error == nil || resp.Error.Code != codeNonOwnerDenied {
		t.Fatalf("expected fail-closed deny, got %+v", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "unknown session") {
		t.Errorf("expected unknown-session message, got %q", resp.Error.Message)
	}
}

func TestGate_DenyOnlyLogsOncePerSessionMethod(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{stdin: stdinWriter, termMgr: newTerminalManager(t.TempDir())}
	a.sessionRoles.Store("s-evil", Origin{IsOwner: false})

	go io.Copy(io.Discard, stdinReader)
	defer stdinWriter.Close()

	// Two consecutive denies → second should NOT add a new entry to
	// the deniedToolWarned map.
	a.gateNonOwnerToolCall(json.RawMessage(`1`), "fs/read_text_file", "s-evil")
	a.gateNonOwnerToolCall(json.RawMessage(`2`), "fs/read_text_file", "s-evil")

	count := 0
	a.deniedToolWarned.Range(func(k, _ interface{}) bool { count++; return true })
	if count != 1 {
		t.Errorf("expected exactly 1 dedupe entry, got %d", count)
	}
}

func TestGate_PermissionRequest_NonOwnerSelectsDeny(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{stdin: stdinWriter}
	a.sessionRoles.Store("s-evil", Origin{IsOwner: false})

	doneCh := make(chan rpcResponseOut, 1)
	go func() { doneCh <- readSingleResponse(t, stdinReader) }()

	raw := `{"jsonrpc":"2.0","id":7,"method":"session/request_permission","params":{"sessionId":"s-evil","options":[{"optionId":"allow","kind":"allow"},{"optionId":"deny","kind":"deny"}]}}`
	a.handlePermissionRequest(raw)
	stdinWriter.Close()

	resp := <-doneCh
	resultBytes, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(resultBytes), `"deny"`) {
		t.Errorf("expected non-owner permission to select deny, got %s", string(resultBytes))
	}
}

func TestGate_PermissionRequest_NonOwnerWithoutDenyOptionCancels(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{stdin: stdinWriter}
	a.sessionRoles.Store("s-evil", Origin{IsOwner: false})

	doneCh := make(chan rpcResponseOut, 1)
	go func() { doneCh <- readSingleResponse(t, stdinReader) }()

	raw := `{"jsonrpc":"2.0","id":8,"method":"session/request_permission","params":{"sessionId":"s-evil","options":[{"optionId":"allow","kind":"allow"}]}}`
	a.handlePermissionRequest(raw)
	stdinWriter.Close()

	resp := <-doneCh
	resultBytes, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(resultBytes), `cancelled`) {
		t.Errorf("expected cancelled outcome for non-owner, got %s", string(resultBytes))
	}
}

func TestGate_PermissionRequest_UnknownSessionCancels(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{stdin: stdinWriter}

	doneCh := make(chan rpcResponseOut, 1)
	go func() { doneCh <- readSingleResponse(t, stdinReader) }()

	raw := `{"jsonrpc":"2.0","id":9,"method":"session/request_permission","params":{"sessionId":"ghost","options":[{"optionId":"allow","kind":"allow"}]}}`
	a.handlePermissionRequest(raw)
	stdinWriter.Close()

	resp := <-doneCh
	resultBytes, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(resultBytes), `cancelled`) {
		t.Errorf("expected cancelled outcome for unknown session, got %s", string(resultBytes))
	}
}
