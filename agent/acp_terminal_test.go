package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// --- terminalBuffer tests ---

func TestTerminalBuffer_Write(t *testing.T) {
	buf := &terminalBuffer{}
	n, err := buf.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5, got %d", n)
	}
	if buf.String() != "hello" {
		t.Errorf("expected 'hello', got %q", buf.String())
	}
}

func TestTerminalBuffer_WriteMultiple(t *testing.T) {
	buf := &terminalBuffer{}
	buf.Write([]byte("hello "))
	buf.Write([]byte("world"))
	if buf.String() != "hello world" {
		t.Errorf("expected 'hello world', got %q", buf.String())
	}
}

func TestTerminalBuffer_Truncation(t *testing.T) {
	buf := &terminalBuffer{limit: 10}
	buf.Write([]byte("12345"))
	buf.Write([]byte("67890"))
	buf.Write([]byte("ABCDE"))

	got := buf.String()
	if len(got) > 10 {
		t.Errorf("expected length <= 10, got %d: %q", len(got), got)
	}
	if !buf.Truncated() {
		t.Error("expected Truncated() == true")
	}
}

func TestTerminalBuffer_NoTruncation(t *testing.T) {
	buf := &terminalBuffer{limit: 100}
	buf.Write([]byte("short"))
	if buf.Truncated() {
		t.Error("expected Truncated() == false for short write")
	}
}

func TestTerminalBuffer_ZeroLimit(t *testing.T) {
	buf := &terminalBuffer{limit: 0}
	buf.Write([]byte("no limit"))
	if buf.Truncated() {
		t.Error("expected Truncated() == false for zero limit")
	}
	if buf.String() != "no limit" {
		t.Errorf("expected 'no limit', got %q", buf.String())
	}
}

// --- terminalManager tests ---

func TestNewTerminalManager(t *testing.T) {
	tm := newTerminalManager("/tmp/test")
	if tm.cwd != "/tmp/test" {
		t.Errorf("expected cwd '/tmp/test', got %q", tm.cwd)
	}
	if tm.terminals == nil {
		t.Error("expected non-nil terminals map")
	}
}

func TestTerminalManager_Create(t *testing.T) {
	tm := newTerminalManager(t.TempDir())
	req := termCreateRequest{
		Command: "echo",
		Args:    []string{"hello"},
	}
	id, err := tm.create(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if !strings.HasPrefix(id, "term_") {
		t.Errorf("expected id prefix 'term_', got %q", id)
	}

	// Wait for process to finish
	tm.mu.Lock()
	proc, ok := tm.terminals[id]
	tm.mu.Unlock()
	if !ok {
		t.Fatal("terminal not found in map")
	}
	<-proc.done
}

func TestTerminalManager_Output(t *testing.T) {
	tm := newTerminalManager(t.TempDir())
	req := termCreateRequest{
		Command: "echo",
		Args:    []string{"test output"},
	}
	id, err := tm.create(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Wait for process to exit
	tm.mu.Lock()
	proc := tm.terminals[id]
	tm.mu.Unlock()
	<-proc.done

	resp, err := tm.output(id)
	if err != nil {
		t.Fatalf("output failed: %v", err)
	}
	if !strings.Contains(resp.Output, "test output") {
		t.Errorf("expected output to contain 'test output', got %q", resp.Output)
	}
	if resp.ExitStatus == nil {
		t.Fatal("expected exit status after process exits")
	}
	if resp.ExitStatus.ExitCode == nil || *resp.ExitStatus.ExitCode != 0 {
		t.Errorf("expected exit code 0")
	}
}

func TestTerminalManager_Output_NotFound(t *testing.T) {
	tm := newTerminalManager(t.TempDir())
	_, err := tm.output("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent terminal")
	}
}

func TestTerminalManager_WaitForExit(t *testing.T) {
	tm := newTerminalManager(t.TempDir())
	shellCmd := "sh"
	shellArgs := []string{"-c", "sleep 0.1 && exit 0"}
	if runtime.GOOS == "windows" {
		shellCmd = "cmd"
		shellArgs = []string{"/c", "exit 0"}
	}
	req := termCreateRequest{
		Command: shellCmd,
		Args:    shellArgs,
	}
	id, err := tm.create(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	resp, err := tm.waitForExit(id)
	if err != nil {
		t.Fatalf("waitForExit failed: %v", err)
	}
	if resp.ExitCode == nil || *resp.ExitCode != 0 {
		t.Errorf("expected exit code 0")
	}
}

func TestTerminalManager_WaitForExit_NotFound(t *testing.T) {
	tm := newTerminalManager(t.TempDir())
	_, err := tm.waitForExit("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent terminal")
	}
}

func TestTerminalManager_Kill(t *testing.T) {
	tm := newTerminalManager(t.TempDir())
	req := termCreateRequest{
		Command: "sleep",
		Args:    []string{"60"},
	}
	id, err := tm.create(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	err = tm.kill(id)
	if err != nil {
		t.Fatalf("kill failed: %v", err)
	}

	// Verify process exited
	tm.mu.Lock()
	proc := tm.terminals[id]
	tm.mu.Unlock()
	select {
	case <-proc.done:
	case <-time.After(2 * time.Second):
		t.Fatal("process did not exit after kill")
	}
}

func TestTerminalManager_Kill_NotFound(t *testing.T) {
	tm := newTerminalManager(t.TempDir())
	err := tm.kill("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent terminal")
	}
}

func TestTerminalManager_Kill_AlreadyExited(t *testing.T) {
	tm := newTerminalManager(t.TempDir())
	req := termCreateRequest{
		Command: "echo",
		Args:    []string{"done"},
	}
	id, err := tm.create(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Wait for natural exit
	tm.mu.Lock()
	proc := tm.terminals[id]
	tm.mu.Unlock()
	<-proc.done

	// Kill after exit should be a no-op
	err = tm.kill(id)
	if err != nil {
		t.Fatalf("kill of exited process failed: %v", err)
	}
}

func TestTerminalManager_Release(t *testing.T) {
	tm := newTerminalManager(t.TempDir())
	req := termCreateRequest{
		Command: "sleep",
		Args:    []string{"60"},
	}
	id, err := tm.create(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	tm.release(id)

	// Verify removed from map
	tm.mu.Lock()
	_, ok := tm.terminals[id]
	tm.mu.Unlock()
	if ok {
		t.Error("expected terminal removed from map after release")
	}
}

func TestTerminalManager_Release_NotFound(t *testing.T) {
	tm := newTerminalManager(t.TempDir())
	// Should not panic
	tm.release("nonexistent")
}

func TestTerminalManager_Release_AlreadyExited(t *testing.T) {
	tm := newTerminalManager(t.TempDir())
	req := termCreateRequest{
		Command: "echo",
		Args:    []string{"done"},
	}
	id, err := tm.create(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Wait for exit
	tm.mu.Lock()
	proc := tm.terminals[id]
	tm.mu.Unlock()
	<-proc.done

	tm.release(id)

	tm.mu.Lock()
	_, ok := tm.terminals[id]
	tm.mu.Unlock()
	if ok {
		t.Error("expected terminal removed after release")
	}
}

func TestTerminalManager_Cleanup(t *testing.T) {
	tm := newTerminalManager(t.TempDir())

	// Create multiple terminals
	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		req := termCreateRequest{
			Command: "sleep",
			Args:    []string{"60"},
		}
		id, err := tm.create(req)
		if err != nil {
			t.Fatalf("create %d failed: %v", i, err)
		}
		ids[i] = id
	}

	tm.cleanup()

	// All terminals should be released
	tm.mu.Lock()
	remaining := len(tm.terminals)
	tm.mu.Unlock()
	if remaining != 0 {
		t.Errorf("expected 0 terminals after cleanup, got %d", remaining)
	}
}

func TestTerminalManager_Create_WithCwd(t *testing.T) {
	dir := t.TempDir()
	tm := newTerminalManager("/default/path")

	req := termCreateRequest{
		Command: "pwd",
		Cwd:     dir,
	}
	id, err := tm.create(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	tm.mu.Lock()
	proc := tm.terminals[id]
	tm.mu.Unlock()
	<-proc.done

	resp, err := tm.output(id)
	if err != nil {
		t.Fatalf("output failed: %v", err)
	}
	if !strings.Contains(resp.Output, filepath.Base(dir)) {
		t.Errorf("expected output to contain cwd %q, got %q", dir, resp.Output)
	}
}

func TestTerminalManager_Create_WithEnv(t *testing.T) {
	tm := newTerminalManager(t.TempDir())

	shellCmd := "sh"
	shellArgs := []string{"-c", "echo $TEST_VAR_XYZ"}
	if runtime.GOOS == "windows" {
		shellCmd = "cmd"
		shellArgs = []string{"/c", "echo %TEST_VAR_XYZ%"}
	}

	req := termCreateRequest{
		Command: shellCmd,
		Args:    shellArgs,
		Env:     []envVar{{Name: "TEST_VAR_XYZ", Value: "hello123"}},
	}
	id, err := tm.create(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	tm.mu.Lock()
	proc := tm.terminals[id]
	tm.mu.Unlock()
	<-proc.done

	resp, err := tm.output(id)
	if err != nil {
		t.Fatalf("output failed: %v", err)
	}
	if !strings.Contains(resp.Output, "hello123") {
		t.Errorf("expected output to contain 'hello123', got %q", resp.Output)
	}
}

func TestTerminalManager_Create_WithOutputByteLimit(t *testing.T) {
	tm := newTerminalManager(t.TempDir())

	limit := 20
	req := termCreateRequest{
		Command:         "sh",
		Args:            []string{"-c", "echo 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'"},
		OutputByteLimit: &limit,
	}
	id, err := tm.create(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	tm.mu.Lock()
	proc := tm.terminals[id]
	tm.mu.Unlock()
	<-proc.done

	resp, err := tm.output(id)
	if err != nil {
		t.Fatalf("output failed: %v", err)
	}
	if len(resp.Output) > limit {
		t.Errorf("expected output length <= %d, got %d", limit, len(resp.Output))
	}
	if !resp.Truncated {
		t.Error("expected Truncated == true")
	}
}

func TestTerminalManager_Create_BadCommand(t *testing.T) {
	tm := newTerminalManager(t.TempDir())
	req := termCreateRequest{
		Command: "nonexistent_command_xyz",
	}
	_, err := tm.create(req)
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}
}

func TestTerminalManager_NonZeroExit(t *testing.T) {
	tm := newTerminalManager(t.TempDir())
	req := termCreateRequest{
		Command: "sh",
		Args:    []string{"-c", "exit 42"},
	}
	id, err := tm.create(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	resp, err := tm.waitForExit(id)
	if err != nil {
		t.Fatalf("waitForExit failed: %v", err)
	}
	if resp.ExitCode == nil || *resp.ExitCode != 42 {
		code := -1
		if resp.ExitCode != nil {
			code = *resp.ExitCode
		}
		t.Errorf("expected exit code 42, got %d", code)
	}
}

// --- parseAgentRequest tests ---

func TestParseAgentRequest_Valid(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":5,"method":"terminal/create","params":{"command":"echo","args":["hello"]}}`
	id, params, err := parseAgentRequest(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(id) != "5" {
		t.Errorf("expected id 5, got %s", string(id))
	}
	if params == nil {
		t.Fatal("expected non-nil params")
	}

	var req termCreateRequest
	if err := json.Unmarshal(params, &req); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if req.Command != "echo" {
		t.Errorf("expected command 'echo', got %q", req.Command)
	}
}

func TestParseAgentRequest_InvalidJSON(t *testing.T) {
	_, _, err := parseAgentRequest("not json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseAgentRequest_MissingFields(t *testing.T) {
	id, params, err := parseAgentRequest(`{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != nil && string(id) != "null" {
		t.Errorf("expected nil/null id, got %s", string(id))
	}
	if params != nil && string(params) != "null" {
		t.Errorf("expected nil/null params, got %s", string(params))
	}
}

// --- splitLines / joinLines tests ---

func TestSplitLines_Basic(t *testing.T) {
	lines := splitLines("line1\nline2\nline3\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "line1\n" {
		t.Errorf("expected 'line1\\n', got %q", lines[0])
	}
	if lines[2] != "line3\n" {
		t.Errorf("expected 'line3\\n', got %q", lines[2])
	}
}

func TestSplitLines_NoTrailingNewline(t *testing.T) {
	lines := splitLines("line1\nline2")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if lines[1] != "line2" {
		t.Errorf("expected 'line2', got %q", lines[1])
	}
}

func TestSplitLines_Empty(t *testing.T) {
	lines := splitLines("")
	if len(lines) != 0 {
		t.Errorf("expected 0 lines for empty string, got %d", len(lines))
	}
}

func TestSplitLines_SingleLine(t *testing.T) {
	lines := splitLines("single")
	if len(lines) != 1 || lines[0] != "single" {
		t.Errorf("expected ['single'], got %v", lines)
	}
}

func TestJoinLines_Basic(t *testing.T) {
	lines := []string{"line1\n", "line2\n"}
	got := joinLines(lines)
	if got != "line1\nline2\n" {
		t.Errorf("expected 'line1\\nline2\\n', got %q", got)
	}
}

func TestJoinLines_Empty(t *testing.T) {
	got := joinLines(nil)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestSplitJoinRoundTrip(t *testing.T) {
	original := "line1\nline2\nline3\n"
	got := joinLines(splitLines(original))
	if got != original {
		t.Errorf("round-trip mismatch: got %q, want %q", got, original)
	}
}

// --- ACP handler integration tests via ACPAgent ---

func TestHandleTerminalCreate(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()

	a := &ACPAgent{
		stdin:   stdinWriter,
		termMgr: newTerminalManager(t.TempDir()),
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := `{"jsonrpc":"2.0","id":1,"method":"terminal/create","params":{"command":"echo","args":["hello"]}}`
	a.handleTerminalCreate(raw)

	stdinWriter.Close()
	<-done

	var resp struct {
		Result termCreateResponse `json:"result"`
		Error  *rpcError          `json:"error"`
	}
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if !strings.HasPrefix(resp.Result.TerminalID, "term_") {
		t.Errorf("expected terminal ID prefix 'term_', got %q", resp.Result.TerminalID)
	}
}

func TestHandleTerminalOutput(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()

	a := &ACPAgent{
		stdin:   stdinWriter,
		termMgr: newTerminalManager(t.TempDir()),
	}

	// Create a terminal first
	req := termCreateRequest{Command: "echo", Args: []string{"output test"}}
	id, err := a.termMgr.create(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	// Wait for it to finish
	a.termMgr.mu.Lock()
	proc := a.termMgr.terminals[id]
	a.termMgr.mu.Unlock()
	<-proc.done

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"terminal/output","params":{"sessionId":"s1","terminalId":"%s"}}`, id)
	a.handleTerminalOutput(raw)

	stdinWriter.Close()
	<-done

	var resp struct {
		Result termOutputResponse `json:"result"`
		Error  *rpcError          `json:"error"`
	}
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if !strings.Contains(resp.Result.Output, "output test") {
		t.Errorf("expected output to contain 'output test', got %q", resp.Result.Output)
	}
}

func TestHandleTerminalKill(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()

	a := &ACPAgent{
		stdin:   stdinWriter,
		termMgr: newTerminalManager(t.TempDir()),
	}

	req := termCreateRequest{Command: "sleep", Args: []string{"60"}}
	id, err := a.termMgr.create(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"terminal/kill","params":{"sessionId":"s1","terminalId":"%s"}}`, id)
	a.handleTerminalKill(raw)

	stdinWriter.Close()
	<-done

	var resp struct {
		Error *rpcError `json:"error"`
	}
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

func TestHandleTerminalRelease(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()

	a := &ACPAgent{
		stdin:   stdinWriter,
		termMgr: newTerminalManager(t.TempDir()),
	}

	req := termCreateRequest{Command: "echo", Args: []string{"done"}}
	id, err := a.termMgr.create(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := fmt.Sprintf(`{"jsonrpc":"2.0","id":4,"method":"terminal/release","params":{"sessionId":"s1","terminalId":"%s"}}`, id)
	a.handleTerminalRelease(raw)

	stdinWriter.Close()
	<-done

	// Verify response is valid
	var resp struct {
		Error *rpcError `json:"error"`
	}
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	// Verify removed from map
	a.termMgr.mu.Lock()
	_, ok := a.termMgr.terminals[id]
	a.termMgr.mu.Unlock()
	if ok {
		t.Error("expected terminal removed after release")
	}
}

func TestHandleFSReadTextFile(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	os.WriteFile(testFile, []byte("line1\nline2\nline3\n"), 0o644)

	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{
		stdin:   stdinWriter,
		termMgr: newTerminalManager(dir),
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := fmt.Sprintf(`{"jsonrpc":"2.0","id":10,"method":"fs/read_text_file","params":{"sessionId":"s1","path":"%s"}}`, testFile)
	a.handleFSReadTextFile(raw)

	stdinWriter.Close()
	<-done

	var resp struct {
		Result fsReadResponse `json:"result"`
		Error  *rpcError      `json:"error"`
	}
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Result.Content != "line1\nline2\nline3\n" {
		t.Errorf("expected file content, got %q", resp.Result.Content)
	}
}

func TestHandleFSReadTextFile_WithLineAndLimit(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	os.WriteFile(testFile, []byte("line1\nline2\nline3\nline4\nline5\n"), 0o644)

	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{
		stdin:   stdinWriter,
		termMgr: newTerminalManager(dir),
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := fmt.Sprintf(`{"jsonrpc":"2.0","id":11,"method":"fs/read_text_file","params":{"sessionId":"s1","path":"%s","line":2,"limit":2}}`, testFile)
	a.handleFSReadTextFile(raw)

	stdinWriter.Close()
	<-done

	var resp struct {
		Result fsReadResponse `json:"result"`
		Error  *rpcError      `json:"error"`
	}
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	// Line 2 (1-based) means start at index 1, limit 2 lines
	if resp.Result.Content != "line2\nline3\n" {
		t.Errorf("expected 'line2\\nline3\\n', got %q", resp.Result.Content)
	}
}

func TestHandleFSReadTextFile_NotFound(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{
		stdin:   stdinWriter,
		termMgr: newTerminalManager(t.TempDir()),
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := `{"jsonrpc":"2.0","id":12,"method":"fs/read_text_file","params":{"sessionId":"s1","path":"/nonexistent/file.txt"}}`
	a.handleFSReadTextFile(raw)

	stdinWriter.Close()
	<-done

	var resp struct {
		Error *rpcError `json:"error"`
	}
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestHandleFSWriteTextFile(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "write_test.txt")

	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{
		stdin:      stdinWriter,
		allowWrite: true,
		termMgr:    newTerminalManager(dir),
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := fmt.Sprintf(`{"jsonrpc":"2.0","id":13,"method":"fs/write_text_file","params":{"sessionId":"s1","path":"%s","content":"written content"}}`, testFile)
	a.handleFSWriteTextFile(raw)

	stdinWriter.Close()
	<-done

	var resp struct {
		Error *rpcError `json:"error"`
	}
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	// Verify file was written
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "written content" {
		t.Errorf("expected 'written content', got %q", string(data))
	}
}

func TestHandleFSWriteTextFile_Denied(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{
		stdin:      stdinWriter,
		allowWrite: false, // write denied
		termMgr:    newTerminalManager(t.TempDir()),
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := `{"jsonrpc":"2.0","id":14,"method":"fs/write_text_file","params":{"sessionId":"s1","path":"/tmp/test.txt","content":"denied"}}`
	a.handleFSWriteTextFile(raw)

	stdinWriter.Close()
	<-done

	var resp struct {
		Error *rpcError `json:"error"`
	}
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for denied write")
	}
	if !strings.Contains(resp.Error.Message, "permission denied") {
		t.Errorf("expected 'permission denied' error, got %q", resp.Error.Message)
	}
}

func TestHandleTerminalWaitForExit(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()

	a := &ACPAgent{
		stdin:   stdinWriter,
		termMgr: newTerminalManager(t.TempDir()),
	}

	req := termCreateRequest{Command: "echo", Args: []string{"done"}}
	id, err := a.termMgr.create(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := fmt.Sprintf(`{"jsonrpc":"2.0","id":20,"method":"terminal/wait_for_exit","params":{"sessionId":"s1","terminalId":"%s"}}`, id)
	a.handleTerminalWaitForExit(raw)

	// handleTerminalWaitForExit runs in a goroutine, wait for response
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for wait_for_exit response")
	}

	stdinWriter.Close()

	var resp struct {
		Result termWaitResponse `json:"result"`
		Error  *rpcError        `json:"error"`
	}
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Result.ExitCode == nil || *resp.Result.ExitCode != 0 {
		t.Errorf("expected exit code 0")
	}
}

func TestHandleTerminalCreate_InvalidParams(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{
		stdin:   stdinWriter,
		termMgr: newTerminalManager(t.TempDir()),
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := `{"jsonrpc":"2.0","id":30,"method":"terminal/create","params":"invalid"}`
	a.handleTerminalCreate(raw)

	stdinWriter.Close()
	<-done

	var resp struct {
		Error *rpcError `json:"error"`
	}
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for invalid params")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("expected code -32602, got %d", resp.Error.Code)
	}
}

func TestHandleTerminalOutput_InvalidParams(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{
		stdin:   stdinWriter,
		termMgr: newTerminalManager(t.TempDir()),
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := `{"jsonrpc":"2.0","id":31,"method":"terminal/output","params":"invalid"}`
	a.handleTerminalOutput(raw)

	stdinWriter.Close()
	<-done

	var resp struct {
		Error *rpcError `json:"error"`
	}
	json.Unmarshal([]byte(response), &resp)
	if resp.Error == nil {
		t.Fatal("expected error for invalid params")
	}
}

func TestHandleTerminalKill_InvalidParams(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{
		stdin:   stdinWriter,
		termMgr: newTerminalManager(t.TempDir()),
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := `{"jsonrpc":"2.0","id":32,"method":"terminal/kill","params":"invalid"}`
	a.handleTerminalKill(raw)

	stdinWriter.Close()
	<-done

	var resp struct {
		Error *rpcError `json:"error"`
	}
	json.Unmarshal([]byte(response), &resp)
	if resp.Error == nil {
		t.Fatal("expected error for invalid params")
	}
}

func TestHandleTerminalRelease_InvalidParams(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{
		stdin:   stdinWriter,
		termMgr: newTerminalManager(t.TempDir()),
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := `{"jsonrpc":"2.0","id":33,"method":"terminal/release","params":"invalid"}`
	a.handleTerminalRelease(raw)

	stdinWriter.Close()
	<-done

	var resp struct {
		Error *rpcError `json:"error"`
	}
	json.Unmarshal([]byte(response), &resp)
	if resp.Error == nil {
		t.Fatal("expected error for invalid params")
	}
}

func TestHandleFSReadTextFile_InvalidParams(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{
		stdin:   stdinWriter,
		termMgr: newTerminalManager(t.TempDir()),
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := `{"jsonrpc":"2.0","id":34,"method":"fs/read_text_file","params":"invalid"}`
	a.handleFSReadTextFile(raw)

	stdinWriter.Close()
	<-done

	var resp struct {
		Error *rpcError `json:"error"`
	}
	json.Unmarshal([]byte(response), &resp)
	if resp.Error == nil {
		t.Fatal("expected error for invalid params")
	}
}

func TestHandleFSWriteTextFile_InvalidParams(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{
		stdin:      stdinWriter,
		allowWrite: true,
		termMgr:    newTerminalManager(t.TempDir()),
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := `{"jsonrpc":"2.0","id":35,"method":"fs/write_text_file","params":"invalid"}`
	a.handleFSWriteTextFile(raw)

	stdinWriter.Close()
	<-done

	var resp struct {
		Error *rpcError `json:"error"`
	}
	json.Unmarshal([]byte(response), &resp)
	if resp.Error == nil {
		t.Fatal("expected error for invalid params")
	}
}

func TestHandleTerminalOutput_NotFound(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	a := &ACPAgent{
		stdin:   stdinWriter,
		termMgr: newTerminalManager(t.TempDir()),
	}

	var response string
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdinReader)
		if scanner.Scan() {
			response = scanner.Text()
		}
		close(done)
	}()

	raw := `{"jsonrpc":"2.0","id":15,"method":"terminal/output","params":{"sessionId":"s1","terminalId":"nonexistent"}}`
	a.handleTerminalOutput(raw)

	stdinWriter.Close()
	<-done

	var resp struct {
		Error *rpcError `json:"error"`
	}
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for nonexistent terminal")
	}
}
