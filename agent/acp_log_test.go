package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTruncateRaw_Empty(t *testing.T) {
	got := truncateRaw(nil, 100)
	if got != "" {
		t.Errorf("expected empty for nil, got %q", got)
	}
	got = truncateRaw(json.RawMessage{}, 100)
	if got != "" {
		t.Errorf("expected empty for empty slice, got %q", got)
	}
}

func TestTruncateRaw_Short(t *testing.T) {
	data := json.RawMessage(`{"key":"value"}`)
	got := truncateRaw(data, 100)
	if got != `{"key":"value"}` {
		t.Errorf("expected full string, got %q", got)
	}
}

func TestTruncateRaw_Long(t *testing.T) {
	data := json.RawMessage(`{"key":"a very long value that should be truncated"}`)
	got := truncateRaw(data, 10)
	if got != `{"key":"a ...` {
		t.Errorf("expected truncated string, got %q", got)
	}
	if len(got) != 13 { // 10 chars + "..."
		t.Errorf("expected length 13, got %d", len(got))
	}
}

func TestTruncateRaw_ExactLength(t *testing.T) {
	data := json.RawMessage(`12345`)
	got := truncateRaw(data, 5)
	if got != "12345" {
		t.Errorf("expected exact string, got %q", got)
	}
}

func TestExtractToolAndArgs_Empty(t *testing.T) {
	tool, args := extractToolAndArgs(nil)
	if tool != "" || args != "" {
		t.Errorf("expected empty for nil, got tool=%q args=%q", tool, args)
	}
}

func TestExtractToolAndArgs_ValidWithServer(t *testing.T) {
	data, _ := json.Marshal(map[string]interface{}{
		"server":    "jira",
		"tool":      "jira_search",
		"arguments": map[string]string{"query": "test"},
	})
	tool, args := extractToolAndArgs(json.RawMessage(data))
	if tool != "jira/jira_search" {
		t.Errorf("expected 'jira/jira_search', got %q", tool)
	}
	if args == "" {
		t.Error("expected non-empty args")
	}
}

func TestExtractToolAndArgs_ValidWithoutServer(t *testing.T) {
	data, _ := json.Marshal(map[string]interface{}{
		"tool":      "read_file",
		"arguments": map[string]string{"path": "/tmp/test"},
	})
	tool, _ := extractToolAndArgs(json.RawMessage(data))
	if tool != "read_file" {
		t.Errorf("expected 'read_file', got %q", tool)
	}
}

func TestExtractToolAndArgs_NoTool(t *testing.T) {
	data := json.RawMessage(`{"server":"jira"}`)
	tool, _ := extractToolAndArgs(data)
	if tool != "" {
		t.Errorf("expected empty tool, got %q", tool)
	}
}

func TestExtractToolAndArgs_InvalidJSON(t *testing.T) {
	data := json.RawMessage(`not valid json`)
	tool, args := extractToolAndArgs(data)
	if tool != "" {
		t.Errorf("expected empty tool, got %q", tool)
	}
	// Should return truncated raw
	if args == "" {
		t.Error("expected non-empty args (raw fallback)")
	}
}

func TestExtractToolAndArgs_LongArgs(t *testing.T) {
	longVal := make([]byte, 300)
	for i := range longVal {
		longVal[i] = 'x'
	}
	data, _ := json.Marshal(map[string]interface{}{
		"tool":      "test_tool",
		"arguments": map[string]string{"data": string(longVal)},
	})
	tool, args := extractToolAndArgs(json.RawMessage(data))
	if tool != "test_tool" {
		t.Errorf("expected 'test_tool', got %q", tool)
	}
	if len(args) > 204 { // 200 + "..."
		t.Errorf("expected args truncated, got length %d", len(args))
	}
}

func TestExtractToolOutput_Empty(t *testing.T) {
	got := extractToolOutput(nil, 200)
	if got != "" {
		t.Errorf("expected empty for nil, got %q", got)
	}
}

func TestExtractToolOutput_PlainString(t *testing.T) {
	data := json.RawMessage(`"hello world"`)
	got := extractToolOutput(data, 200)
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

func TestExtractToolOutput_ContentArray(t *testing.T) {
	v := map[string]interface{}{
		"content": []map[string]string{
			{"text": "result text"},
		},
		"isError": false,
	}
	data, _ := json.Marshal(v)
	got := extractToolOutput(json.RawMessage(data), 200)
	if got != "result text" {
		t.Errorf("expected 'result text', got %q", got)
	}
}

func TestExtractToolOutput_ContentArrayWithError(t *testing.T) {
	v := map[string]interface{}{
		"content": []map[string]string{
			{"text": "something failed"},
		},
		"isError": true,
	}
	data, _ := json.Marshal(v)
	got := extractToolOutput(json.RawMessage(data), 200)
	if got != "[error] something failed" {
		t.Errorf("expected '[error] something failed', got %q", got)
	}
}

func TestExtractToolOutput_LongContent(t *testing.T) {
	longText := make([]byte, 300)
	for i := range longText {
		longText[i] = 'a'
	}
	v := map[string]interface{}{
		"content": []map[string]string{
			{"text": string(longText)},
		},
	}
	data, _ := json.Marshal(v)
	got := extractToolOutput(json.RawMessage(data), 50)
	if len(got) > 54 { // 50 + "..."
		t.Errorf("expected truncated, got length %d", len(got))
	}
}

func TestExtractToolOutput_RawLong(t *testing.T) {
	longVal := make([]byte, 300)
	for i := range longVal {
		longVal[i] = 'b'
	}
	got := extractToolOutput(json.RawMessage(longVal), 50)
	if len(got) > 54 { // 50 + "..."
		t.Errorf("expected truncated raw, got length %d", len(got))
	}
}

func TestExtractToolOutput_QuotedString(t *testing.T) {
	data := json.RawMessage(`"some \"quoted\" output"`)
	got := extractToolOutput(data, 200)
	if got != `some "quoted" output` {
		t.Errorf("expected unquoted string, got %q", got)
	}
}

func TestAcpStderrWriter_Write(t *testing.T) {
	w := &acpStderrWriter{prefix: "test"}
	n, err := w.Write([]byte("line 1\nline 2\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 14 {
		t.Errorf("expected 14 bytes written, got %d", n)
	}
}

func TestAcpStderrWriter_LastError(t *testing.T) {
	w := &acpStderrWriter{prefix: "test"}
	w.Write([]byte("first error\nsecond error\n"))

	got := w.LastError()
	if got != "second error" {
		t.Errorf("expected 'second error', got %q", got)
	}

	// LastError resets
	got = w.LastError()
	if got != "" {
		t.Errorf("expected empty after reset, got %q", got)
	}
}

func TestAcpStderrWriter_IgnoresIndented(t *testing.T) {
	w := &acpStderrWriter{prefix: "test"}
	w.Write([]byte("real error\n  indented line\n"))

	got := w.LastError()
	if got != "real error" {
		t.Errorf("expected 'real error', got %q", got)
	}
}

func TestAcpStderrWriter_IgnoresTraceback(t *testing.T) {
	w := &acpStderrWriter{prefix: "test"}
	w.Write([]byte("real error\nTraceback (most recent call last):\n"))

	got := w.LastError()
	if got != "real error" {
		t.Errorf("expected 'real error', got %q", got)
	}
}

func TestAcpStderrWriter_IgnoresEllipsis(t *testing.T) {
	w := &acpStderrWriter{prefix: "test"}
	w.Write([]byte("real error\n... more traceback\n"))

	got := w.LastError()
	if got != "real error" {
		t.Errorf("expected 'real error', got %q", got)
	}
}

func TestAcpStderrWriter_EmptyLines(t *testing.T) {
	w := &acpStderrWriter{prefix: "test"}
	w.Write([]byte("\n\n"))

	got := w.LastError()
	if got != "" {
		t.Errorf("expected empty for empty lines, got %q", got)
	}
}

func TestAcpStderrWriter_MultipleWrites(t *testing.T) {
	w := &acpStderrWriter{prefix: "test"}
	w.Write([]byte("error 1\n"))
	w.Write([]byte("error 2\n"))

	got := w.LastError()
	if got != "error 2" {
		t.Errorf("expected 'error 2', got %q", got)
	}
}

func TestAcpStderrWriter_SkipsBraceOnlyLines(t *testing.T) {
	// Mirrors the multi-line dump claude-agent-acp prints when set_mode
	// fails: a closing `}` on its own line must not displace the actual
	// error detail line.
	w := &acpStderrWriter{prefix: "test"}
	dump := "Error handling request {\n" +
		"  jsonrpc: '2.0',\n" +
		"  method: 'session/set_mode',\n" +
		"} {\n" +
		"  code: -32603,\n" +
		"  data: { details: 'Invalid Mode' }\n" +
		"}\n"
	w.Write([]byte(dump))

	got := w.LastError()
	if got != "Error handling request {" {
		t.Errorf("expected first non-indented non-structural line, got %q", got)
	}
}

func TestExtractNpxCacheDir(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "npm error with single-quoted path",
			line: "ENOTEMPTY: directory not empty, rename '/Users/x/.npm/_npx/d820eb7d96bc2600/node_modules/@anthropic-ai/claude-agent-sdk' -> '/Users/x/.npm/_npx/d820eb7d96bc2600/node_modules/@anthropic-ai/.claude-agent-sdk-ce9fpONq'",
			want: "/Users/x/.npm/_npx/d820eb7d96bc2600",
		},
		{
			name: "codex acp path",
			line: "ENOTEMPTY: directory not empty, rename '/home/user/.npm/_npx/abc123/node_modules/@zed-industries/codex-acp' -> '/home/user/.npm/_npx/abc123/node_modules/.tmp'",
			want: "/home/user/.npm/_npx/abc123",
		},
		{
			name: "no npx path",
			line: "ENOTEMPTY: directory not empty, rename '/tmp/foo' -> '/tmp/bar'",
			want: "",
		},
		{
			name: "empty line",
			line: "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractNpxCacheDir(tt.line)
			if got != tt.want {
				t.Errorf("extractNpxCacheDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAcpStderrWriter_NpxCorruptedDir(t *testing.T) {
	w := &acpStderrWriter{prefix: "test"}
	if dir := w.NpxCorruptedDir(); dir != "" {
		t.Errorf("expected empty before write, got %q", dir)
	}

	w.Write([]byte("npm error code ENOTEMPTY\n"))
	w.Write([]byte("npm error syscall rename\n"))
	w.Write([]byte("npm error path /Users/x/.npm/_npx/abc123/node_modules/@anthropic-ai/claude-agent-sdk\n"))
	w.Write([]byte("ENOTEMPTY: directory not empty, rename '/Users/x/.npm/_npx/abc123/node_modules/@anthropic-ai/claude-agent-sdk' -> '/Users/x/.npm/_npx/abc123/node_modules/.tmp'\n"))

	dir := w.NpxCorruptedDir()
	if dir != "/Users/x/.npm/_npx/abc123" {
		t.Errorf("expected '/Users/x/.npm/_npx/abc123', got %q", dir)
	}
}

func TestAcpStderrWriter_NpxCorruptedDir_NoMatch(t *testing.T) {
	w := &acpStderrWriter{prefix: "test"}
	w.Write([]byte("some other error\n"))

	if dir := w.NpxCorruptedDir(); dir != "" {
		t.Errorf("expected empty for non-ENOTEMPTY error, got %q", dir)
	}
}

func TestIsStructuralOnly(t *testing.T) {
	cases := map[string]bool{
		"":                   false,
		"{":                  true,
		"}":                  true,
		"} {":                true,
		"},":                 true,
		"[":                  true,
		"]":                  true,
		"{}":                 true,
		"  }":                true,
		"Error":              false,
		"data: 'x'":          false,
		"Invalid Mode":       false,
		"Error handling {":   false,
	}
	for input, want := range cases {
		got := isStructuralOnly(strings.TrimSpace(input))
		if got != want {
			t.Errorf("isStructuralOnly(%q) = %v, want %v", input, got, want)
		}
	}
}
