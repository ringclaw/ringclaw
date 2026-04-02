package agent

import (
	"encoding/json"
	"testing"
)

func TestExtractChunkText(t *testing.T) {
	update := &sessionUpdate{Text: "hello world"}
	got := extractChunkText(update)
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

func TestExtractChunkText_FromContent(t *testing.T) {
	content, _ := json.Marshal(map[string]string{"type": "text", "text": "from content"})
	update := &sessionUpdate{Content: json.RawMessage(content)}
	got := extractChunkText(update)
	if got != "from content" {
		t.Errorf("expected 'from content', got %q", got)
	}
}

func TestExtractChunkText_Empty(t *testing.T) {
	update := &sessionUpdate{}
	got := extractChunkText(update)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExtractPromptResultText(t *testing.T) {
	result, _ := json.Marshal(map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": "part1"},
			{"type": "text", "text": "part2"},
		},
	})
	got := extractPromptResultText(json.RawMessage(result))
	if got != "part1part2" {
		t.Errorf("expected 'part1part2', got %q", got)
	}
}

func TestExtractPromptResultText_FlatText(t *testing.T) {
	result, _ := json.Marshal(map[string]string{"text": "flat response"})
	got := extractPromptResultText(json.RawMessage(result))
	if got != "flat response" {
		t.Errorf("expected 'flat response', got %q", got)
	}
}

func TestExtractPromptResultText_Nil(t *testing.T) {
	got := extractPromptResultText(nil)
	if got != "" {
		t.Errorf("expected empty for nil, got %q", got)
	}
}

func TestACPAgent_SessionReuse(t *testing.T) {
	a := &ACPAgent{
		sessions: make(map[string]string),
	}

	// Simulate session creation
	a.sessions["conv-1"] = "session-abc"

	a.mu.Lock()
	sid, exists := a.sessions["conv-1"]
	a.mu.Unlock()

	if !exists || sid != "session-abc" {
		t.Errorf("expected session reuse, got exists=%v, sid=%q", exists, sid)
	}

	// Different conversation should not have a session
	a.mu.Lock()
	_, exists = a.sessions["conv-2"]
	a.mu.Unlock()
	if exists {
		t.Error("expected no session for conv-2")
	}
}

func TestStripACPStdoutLine_ANSI(t *testing.T) {
	raw := "\x1b[31m" + `{"jsonrpc":"2.0","id":1,"result":{}}` + "\x1b[0m"
	got := stripACPStdoutLine(raw)
	want := `{"jsonrpc":"2.0","id":1,"result":{}}`
	if got != want {
		t.Fatalf("stripACPStdoutLine = %q, want %q", got, want)
	}
	var msg rpcResponse
	if err := json.Unmarshal([]byte(got), &msg); err != nil {
		t.Fatalf("json.Unmarshal after strip: %v", err)
	}
	if msg.ID == nil || *msg.ID != 1 {
		t.Fatalf("parsed id: %+v", msg.ID)
	}
}

func TestStripACPStdoutLine_LeadingNoise(t *testing.T) {
	raw := "\x1b[?2004h" + ` {"jsonrpc":"2.0","method":"session/update"}`
	got := stripACPStdoutLine(raw)
	if got != `{"jsonrpc":"2.0","method":"session/update"}` {
		t.Fatalf("got %q", got)
	}
}

func TestStripACPStdoutLine_BOM(t *testing.T) {
	got := stripACPStdoutLine("\ufeff" + `{"jsonrpc":"2.0"}`)
	if got != `{"jsonrpc":"2.0"}` {
		t.Fatalf("got %q", got)
	}
}
