package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCall_RoundTrip(t *testing.T) {
	// Set up pipes: agent writes to stdinWriter (-> stdinReader consumed by mock),
	// mock writes to stdoutWriter (-> stdoutReader consumed by readLoop via scanner).
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()

	// Mock: read the request from stdin, then write a matching response to stdout.
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		for scanner.Scan() {
			line := scanner.Text()
			var req rpcRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				continue
			}
			resp := rpcResponseOut{
				JSONRPC: "2.0",
				ID:      mustMarshal(req.ID),
				Result:  mustMarshalRaw(map[string]string{"ok": "true"}),
			}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(stdoutWriter, "%s\n", data)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := a.call(ctx, "test/method", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("call returned error: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if parsed["ok"] != "true" {
		t.Errorf("expected ok=true, got %v", parsed)
	}

	// Cleanup
	stdinWriter.Close()
	stdoutWriter.Close()
}

func TestCall_ContextCancellation(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}

	// Drain stdin so writes don't block
	go io.Copy(io.Discard, stdinReader)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := a.call(ctx, "test/method", nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	stdinWriter.Close()
	stdoutWriter.Close()
}

func TestCall_ErrorResponse(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()

	go func() {
		scanner := bufio.NewScanner(stdinReader)
		for scanner.Scan() {
			line := scanner.Text()
			var req rpcRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				continue
			}
			resp := rpcResponseOut{
				JSONRPC: "2.0",
				ID:      mustMarshal(req.ID),
				Error:   &rpcError{Code: -32600, Message: "invalid request"},
			}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(stdoutWriter, "%s\n", data)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := a.call(ctx, "test/bad", nil)
	if err == nil {
		t.Fatal("expected error for error response")
	}
	if !strings.Contains(err.Error(), "invalid request") {
		t.Errorf("expected error to contain 'invalid request', got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "-32600") {
		t.Errorf("expected error to contain code -32600, got %q", err.Error())
	}

	stdinWriter.Close()
	stdoutWriter.Close()
}

func TestCall_ErrorResponse_WithStderr(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	stderrWriter := &acpStderrWriter{prefix: "test"}
	stderrWriter.Write([]byte("detailed stderr error\n"))

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
		stderr:   stderrWriter,
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()

	go func() {
		scanner := bufio.NewScanner(stdinReader)
		for scanner.Scan() {
			line := scanner.Text()
			var req rpcRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				continue
			}
			resp := rpcResponseOut{
				JSONRPC: "2.0",
				ID:      mustMarshal(req.ID),
				Error:   &rpcError{Code: -32000, Message: "Internal error"},
			}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(stdoutWriter, "%s\n", data)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := a.call(ctx, "test/internal", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	// When error message is "Internal error" and stderr has details, should use stderr
	if !strings.Contains(err.Error(), "detailed stderr error") {
		t.Errorf("expected stderr detail in error, got %q", err.Error())
	}

	stdinWriter.Close()
	stdoutWriter.Close()
}

func TestCall_ProcessExit(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	// Drain stdin so the call's write doesn't block
	go io.Copy(io.Discard, stdinReader)

	go a.readLoop()

	// Close stdout to simulate process exit — readLoop will close pending channels
	stdoutWriter.Close()

	// Wait for readLoop to process the close
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := a.call(ctx, "test/method", nil)
	if err == nil {
		t.Fatal("expected error for process exit")
	}

	stdinWriter.Close()
}

func TestNotify_WritesValidJSON(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()

	a := &ACPAgent{
		stdin: stdinWriter,
	}

	var received string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			received = scanner.Text()
		}
		close(done)
	}()

	a.notify("session/end", map[string]string{"sessionId": "sess-1"})

	stdinWriter.Close()
	<-done

	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(received), &msg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if msg["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %v", msg["jsonrpc"])
	}
	if msg["method"] != "session/end" {
		t.Errorf("expected method session/end, got %v", msg["method"])
	}
	if msg["id"] != nil {
		t.Error("notification should not have id")
	}
	params, ok := msg["params"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected params map, got %T", msg["params"])
	}
	if params["sessionId"] != "sess-1" {
		t.Errorf("expected sessionId sess-1, got %v", params["sessionId"])
	}
}

func TestNotify_NilParams(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()

	a := &ACPAgent{
		stdin: stdinWriter,
	}

	var received string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			received = scanner.Text()
		}
		close(done)
	}()

	a.notify("session/end", nil)

	stdinWriter.Close()
	<-done

	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(received), &msg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, hasParams := msg["params"]; hasParams {
		t.Error("notification with nil params should not include params key")
	}
}

func TestReadLoop_DispatchesResponses(t *testing.T) {
	_, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	// Register a pending request with ID=42
	ch := make(chan *rpcResponse, 1)
	a.pendingMu.Lock()
	a.pending[42] = ch
	a.pendingMu.Unlock()

	go a.readLoop()

	// Feed a response with ID=42
	id := int64(42)
	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  map[string]string{"data": "hello"},
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(stdoutWriter, "%s\n", data)
	stdoutWriter.Close()

	select {
	case msg := <-ch:
		if msg == nil {
			t.Fatal("received nil response")
		}
		var result map[string]string
		json.Unmarshal(msg.Result, &result)
		if result["data"] != "hello" {
			t.Errorf("expected data=hello, got %v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	stdinWriter.Close()
}

func TestReadLoop_DispatchesResponsesWithStringID(t *testing.T) {
	_, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	ch := make(chan *rpcResponse, 1)
	a.pendingMu.Lock()
	a.pending[42] = ch
	a.pendingMu.Unlock()

	go a.readLoop()

	fmt.Fprintln(stdoutWriter, `{"jsonrpc":"2.0","id":"42","result":{"data":"hello"}}`)
	stdoutWriter.Close()

	select {
	case msg := <-ch:
		if msg == nil {
			t.Fatal("received nil response")
		}
		var result map[string]string
		json.Unmarshal(msg.Result, &result)
		if result["data"] != "hello" {
			t.Errorf("expected data=hello, got %v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	stdinWriter.Close()
}

func TestReadLoop_AcceptsUUIDStringIDMessages(t *testing.T) {
	var msg rpcResponse
	raw := `{"jsonrpc":"2.0","id":"1900d5fe-c749-40f5-9383-1dfe9dfe14f5","method":"session/update","params":{}}`
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("uuid string id should parse: %v", err)
	}
	if msg.ID == nil {
		t.Fatal("expected id to be present")
	}
	if msg.ID.Int != nil {
		t.Fatalf("uuid id should not be coerced to int: %v", *msg.ID.Int)
	}
	if string(msg.ID.Raw) != `"1900d5fe-c749-40f5-9383-1dfe9dfe14f5"` {
		t.Fatalf("unexpected raw id: %s", string(msg.ID.Raw))
	}
}

func TestReadLoop_SkipsEmptyLines(t *testing.T) {
	_, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	ch := make(chan *rpcResponse, 1)
	a.pendingMu.Lock()
	a.pending[1] = ch
	a.pendingMu.Unlock()

	go a.readLoop()

	// Write empty lines followed by a valid response
	fmt.Fprintf(stdoutWriter, "\n\n")
	id := int64(1)
	resp := map[string]interface{}{"jsonrpc": "2.0", "id": id, "result": "ok"}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(stdoutWriter, "%s\n", data)
	stdoutWriter.Close()

	select {
	case msg := <-ch:
		if msg == nil {
			t.Fatal("received nil response")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: readLoop should skip empty lines and still dispatch")
	}

	stdinWriter.Close()
}

func TestReadLoop_ClosesAllPendingOnExit(t *testing.T) {
	_, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	ch1 := make(chan *rpcResponse, 1)
	ch2 := make(chan *rpcResponse, 1)
	a.pendingMu.Lock()
	a.pending[10] = ch1
	a.pending[20] = ch2
	a.pendingMu.Unlock()

	go a.readLoop()

	// Close stdout to end readLoop
	stdoutWriter.Close()

	// Both channels should be closed
	select {
	case _, ok := <-ch1:
		if ok {
			t.Error("expected ch1 to be closed without value")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ch1 close")
	}
	select {
	case _, ok := <-ch2:
		if ok {
			t.Error("expected ch2 to be closed without value")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ch2 close")
	}

	// started should be set to false
	a.mu.Lock()
	started := a.started
	a.mu.Unlock()
	if started {
		t.Error("expected started=false after readLoop ends")
	}

	stdinWriter.Close()
}

func TestReadLoop_DispatchesTerminalCreate(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()

	// Capture response from handleTerminalCreate
	done := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			done <- scanner.Text()
		}
	}()

	a.sessionRoles.Store("s1", Origin{IsOwner: true})
	// Feed terminal/create request to readLoop
	req := `{"jsonrpc":"2.0","id":100,"method":"terminal/create","params":{"sessionId":"s1","command":"echo","args":["hi"]}}`
	fmt.Fprintf(stdoutWriter, "%s\n", req)

	select {
	case resp := <-done:
		var r struct {
			Result *termCreateResponse `json:"result"`
		}
		json.Unmarshal([]byte(resp), &r)
		if r.Result == nil {
			t.Fatal("expected terminal create result")
		}
		if !strings.HasPrefix(r.Result.TerminalID, "term_") {
			t.Errorf("unexpected terminal ID: %q", r.Result.TerminalID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for terminal/create response")
	}

	stdoutWriter.Close()
	stdinWriter.Close()
}

func TestReadLoop_DispatchesFSRead(t *testing.T) {
	dir := t.TempDir()
	testFile := dir + "/test.txt"
	os.WriteFile(testFile, []byte("hello"), 0o644)

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(dir),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()

	done := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			done <- scanner.Text()
		}
	}()

	a.sessionRoles.Store("s1", Origin{IsOwner: true})
	req := fmt.Sprintf(`{"jsonrpc":"2.0","id":101,"method":"fs/read_text_file","params":{"sessionId":"s1","path":"%s"}}`, testFile)
	fmt.Fprintf(stdoutWriter, "%s\n", req)

	select {
	case resp := <-done:
		var r struct {
			Result *fsReadResponse `json:"result"`
		}
		json.Unmarshal([]byte(resp), &r)
		if r.Result == nil {
			t.Fatal("expected fs read result")
		}
		if r.Result.Content != "hello" {
			t.Errorf("expected 'hello', got %q", r.Result.Content)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}

	stdoutWriter.Close()
	stdinWriter.Close()
}

func TestReadLoop_DispatchesFSWrite(t *testing.T) {
	dir := t.TempDir()
	testFile := dir + "/write.txt"

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:      stdinWriter,
		scanner:    bufio.NewScanner(stdoutReader),
		started:    true,
		allowWrite: true,
		pending:    make(map[int64]chan *rpcResponse),
		notifyCh:   make(map[string]chan *sessionUpdate),
		sessions:   make(map[string]string),
		termMgr:    newTerminalManager(dir),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()

	done := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			done <- scanner.Text()
		}
	}()

	a.sessionRoles.Store("s1", Origin{IsOwner: true})
	req := fmt.Sprintf(`{"jsonrpc":"2.0","id":102,"method":"fs/write_text_file","params":{"sessionId":"s1","path":"%s","content":"written"}}`, testFile)
	fmt.Fprintf(stdoutWriter, "%s\n", req)

	select {
	case <-done:
		data, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		if string(data) != "written" {
			t.Errorf("expected 'written', got %q", string(data))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}

	stdoutWriter.Close()
	stdinWriter.Close()
}

func TestReadLoop_DispatchesPermissionRequest(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()

	done := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			done <- scanner.Text()
		}
	}()

	a.sessionRoles.Store("s1", Origin{IsOwner: true})
	req := `{"jsonrpc":"2.0","id":103,"method":"session/request_permission","params":{"sessionId":"s1","toolCall":{},"options":[{"optionId":"allow","name":"Allow","kind":"allow"}]}}`
	fmt.Fprintf(stdoutWriter, "%s\n", req)

	select {
	case resp := <-done:
		if !strings.Contains(resp, "allow") {
			t.Errorf("expected allow in response, got %s", resp)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}

	stdoutWriter.Close()
	stdinWriter.Close()
}

func TestReadLoop_UnhandledMethod(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()
	go io.Copy(io.Discard, stdinReader)

	// Send an unknown method
	req := `{"jsonrpc":"2.0","method":"unknown/method","params":{}}`
	fmt.Fprintf(stdoutWriter, "%s\n", req)

	// Send it again — should be deduplicated via loggedMethods
	fmt.Fprintf(stdoutWriter, "%s\n", req)

	// Give time for processing
	time.Sleep(50 * time.Millisecond)

	stdoutWriter.Close()
	stdinWriter.Close()
}

func TestReadLoop_SessionUpdate_ToolCall(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	notifyCh := make(chan *sessionUpdate, 16)
	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: map[string]chan *sessionUpdate{"sess-1": notifyCh},
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()
	go io.Copy(io.Discard, stdinReader)

	// Send a tool_call update
	update := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]interface{}{
			"sessionId": "sess-1",
			"update": map[string]interface{}{
				"sessionUpdate": "tool_call",
				"title":         "Tool: read_file",
				"status":        "running",
				"rawInput":      map[string]interface{}{"tool": "read_file", "arguments": map[string]string{"path": "/tmp/x"}},
			},
		},
	}
	data, _ := json.Marshal(update)
	fmt.Fprintf(stdoutWriter, "%s\n", data)

	select {
	case u := <-notifyCh:
		if u.SessionUpdate != "tool_call" {
			t.Errorf("expected tool_call, got %q", u.SessionUpdate)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	stdoutWriter.Close()
	stdinWriter.Close()
}

func TestReadLoop_SessionUpdate_ToolCallUpdate(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	notifyCh := make(chan *sessionUpdate, 16)
	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: map[string]chan *sessionUpdate{"sess-1": notifyCh},
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()
	go io.Copy(io.Discard, stdinReader)

	// Send a tool_call_update
	update := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]interface{}{
			"sessionId": "sess-1",
			"update": map[string]interface{}{
				"sessionUpdate": "tool_call_update",
				"status":        "completed",
				"rawOutput":     `{"content":[{"text":"output data"}]}`,
			},
		},
	}
	data, _ := json.Marshal(update)
	fmt.Fprintf(stdoutWriter, "%s\n", data)

	select {
	case u := <-notifyCh:
		if u.SessionUpdate != "tool_call_update" {
			t.Errorf("expected tool_call_update, got %q", u.SessionUpdate)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	stdoutWriter.Close()
	stdinWriter.Close()
}

func TestReadLoop_SessionUpdate(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	notifyCh := make(chan *sessionUpdate, 16)
	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: map[string]chan *sessionUpdate{"sess-1": notifyCh},
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()

	// Drain stdin so writes don't block
	go io.Copy(io.Discard, stdinReader)

	// Send a session/update notification
	update := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]interface{}{
			"sessionId": "sess-1",
			"update": map[string]interface{}{
				"sessionUpdate": "agent_message_chunk",
				"text":          "hello from agent",
			},
		},
	}
	data, _ := json.Marshal(update)
	fmt.Fprintf(stdoutWriter, "%s\n", data)

	select {
	case u := <-notifyCh:
		if u.SessionUpdate != "agent_message_chunk" {
			t.Errorf("expected agent_message_chunk, got %q", u.SessionUpdate)
		}
		if u.Text != "hello from agent" {
			t.Errorf("expected text 'hello from agent', got %q", u.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for session update")
	}

	stdoutWriter.Close()
	stdinWriter.Close()
}

func TestHandlePermissionRequest(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()

	a := &ACPAgent{
		stdin: stdinWriter,
	}
	a.sessionRoles.Store("s1", Origin{IsOwner: true})

	// Capture response
	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := `{"jsonrpc":"2.0","id":5,"method":"session/request_permission","params":{"sessionId":"s1","toolCall":{},"options":[{"optionId":"allow-once","name":"Allow Once","kind":"allow"},{"optionId":"deny","name":"Deny","kind":"deny"}]}}`
	a.handlePermissionRequest(raw)

	stdinWriter.Close()
	<-done

	var resp rpcResponseOut
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %q", resp.JSONRPC)
	}

	// Check the response contains the allow option
	resultBytes, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(resultBytes), "allow-once") {
		t.Errorf("expected result to contain allow-once, got %s", string(resultBytes))
	}
}

func TestHandlePermissionRequest_FallbackToFirstOption(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()

	a := &ACPAgent{
		stdin: stdinWriter,
	}
	a.sessionRoles.Store("s1", Origin{IsOwner: true})

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	// No option has kind="allow"; should fall back to first option's ID
	raw := `{"jsonrpc":"2.0","id":6,"method":"session/request_permission","params":{"sessionId":"s1","toolCall":{},"options":[{"optionId":"approve-all","name":"Approve All","kind":"approve"},{"optionId":"deny","name":"Deny","kind":"deny"}]}}`
	a.handlePermissionRequest(raw)

	stdinWriter.Close()
	<-done

	var resp rpcResponseOut
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(resultBytes), "approve-all") {
		t.Errorf("expected fallback to first option 'approve-all', got %s", string(resultBytes))
	}
}

func TestHandlePermissionRequest_EmptyOptions(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()

	a := &ACPAgent{
		stdin: stdinWriter,
	}
	a.sessionRoles.Store("s1", Origin{IsOwner: true})

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	// Empty options array; should fall back to hardcoded "allow"
	raw := `{"jsonrpc":"2.0","id":7,"method":"session/request_permission","params":{"sessionId":"s1","toolCall":{},"options":[]}}`
	a.handlePermissionRequest(raw)

	stdinWriter.Close()
	<-done

	var resp rpcResponseOut
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(resultBytes), `"allow"`) {
		t.Errorf("expected fallback to 'allow', got %s", string(resultBytes))
	}
}

func TestSendResponse(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{stdin: stdinWriter}

	var received string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			received = scanner.Text()
		}
		close(done)
	}()

	a.sendResponse(json.RawMessage(`99`), map[string]string{"status": "ok"})

	stdinWriter.Close()
	<-done

	var resp rpcResponseOut
	if err := json.Unmarshal([]byte(received), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(resp.ID) != "99" {
		t.Errorf("expected id 99, got %s", string(resp.ID))
	}
	if resp.Error != nil {
		t.Errorf("expected no error, got %+v", resp.Error)
	}
}

func TestSendErrorResponse(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{stdin: stdinWriter}

	var received string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			received = scanner.Text()
		}
		close(done)
	}()

	a.sendErrorResponse(json.RawMessage(`7`), -32600, "bad request")

	stdinWriter.Close()
	<-done

	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   *rpcError       `json:"error"`
	}
	if err := json.Unmarshal([]byte(received), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(resp.ID) != "7" {
		t.Errorf("expected id 7, got %s", string(resp.ID))
	}
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != -32600 {
		t.Errorf("expected code -32600, got %d", resp.Error.Code)
	}
	if resp.Error.Message != "bad request" {
		t.Errorf("expected message 'bad request', got %q", resp.Error.Message)
	}
}

func TestCall_MultipleSequential(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()

	// Mock: echo back with the ID
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		for scanner.Scan() {
			line := scanner.Text()
			var req rpcRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				continue
			}
			resp := rpcResponseOut{
				JSONRPC: "2.0",
				ID:      mustMarshal(req.ID),
				Result:  mustMarshalRaw(map[string]interface{}{"method": req.Method, "id": req.ID}),
			}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(stdoutWriter, "%s\n", data)
		}
	}()

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		method := fmt.Sprintf("test/method%d", i)
		result, err := a.call(ctx, method, nil)
		if err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
		var parsed map[string]interface{}
		json.Unmarshal(result, &parsed)
		if parsed["method"] != method {
			t.Errorf("call %d: expected method %q, got %v", i, method, parsed["method"])
		}
	}

	stdinWriter.Close()
	stdoutWriter.Close()
}

func TestCall_ConcurrentRequests(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	a := &ACPAgent{
		stdin:    stdinWriter,
		scanner:  bufio.NewScanner(stdoutReader),
		started:  true,
		pending:  make(map[int64]chan *rpcResponse),
		notifyCh: make(map[string]chan *sessionUpdate),
		sessions: make(map[string]string),
		termMgr:  newTerminalManager(t.TempDir()),
	}
	a.scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	go a.readLoop()

	// Mock: echo back with the ID
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		for scanner.Scan() {
			line := scanner.Text()
			var req rpcRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				continue
			}
			resp := rpcResponseOut{
				JSONRPC: "2.0",
				ID:      mustMarshal(req.ID),
				Result:  mustMarshalRaw(map[string]interface{}{"id": req.ID}),
			}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(stdoutWriter, "%s\n", data)
		}
	}()

	ctx := context.Background()
	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := a.call(ctx, fmt.Sprintf("test/concurrent%d", idx), nil)
			if err != nil {
				errCh <- fmt.Errorf("call %d failed: %w", idx, err)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}

	stdinWriter.Close()
	stdoutWriter.Close()
}

// mustMarshal marshals v to json.RawMessage, panicking on error.
func mustMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return json.RawMessage(data)
}

// mustMarshalRaw marshals v and returns the result as interface{} for use in rpcResponseOut.Result.
func mustMarshalRaw(v interface{}) interface{} {
	return v
}
