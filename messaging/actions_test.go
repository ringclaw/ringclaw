package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
)

func newTestActionClient(handler http.HandlerFunc) (*ringcentral.Client, *httptest.Server) {
	srv := httptest.NewServer(handler)
	creds := &ringcentral.Credentials{
		ClientID:     "id",
		ClientSecret: "secret",
		JWTToken:     "jwt",
		ChatID:       "test-chat",
		ServerURL:    srv.URL,
	}
	client := ringcentral.NewClient(creds)
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))
	return client, srv
}

func TestHandleActionCommand_TaskList(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"records": []map[string]string{{"id": "t1", "subject": "Buy milk", "status": "Pending"}},
		})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task list")
	if !strings.Contains(result, "t1") || !strings.Contains(result, "Buy milk") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_TaskCreate(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "t1", "subject": "New task"})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task create New task")
	if !strings.Contains(result, "created") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_NoteCreate(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "n1", "title": "Meeting", "status": "Draft"})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/note create Meeting | some body")
	if !strings.Contains(result, "n1") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_EventCreate(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "e1", "title": "Standup"})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/event create Standup 2026-03-26T14:00:00Z 2026-03-26T15:00:00Z")
	if !strings.Contains(result, "created") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_UnknownSubcommand(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task unknown")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage help, got: %s", result)
	}
}

func TestHandleActionCommand_MissingSubcommand(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage help, got: %s", result)
	}
}

func TestIsActionCommand(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"/task list", true},
		{"/task create test", true},
		{"/note list", true},
		{"/event create meeting 2026-01-01T10:00:00Z 2026-01-01T11:00:00Z", true},
		{"/Task list", true},
		{"/help", false},
		{"/status", false},
		{"hello", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsActionCommand(tt.text); got != tt.want {
			t.Errorf("IsActionCommand(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestExtractAfter(t *testing.T) {
	tests := []struct {
		raw, keyword, want string
	}{
		{"/task create buy milk", "create", "buy milk"},
		{"/note create Meeting Notes | body text", "create", "Meeting Notes | body text"},
		{"/task create", "create", ""},
		{"no match", "create", ""},
	}
	for _, tt := range tests {
		if got := extractAfter(tt.raw, tt.keyword); got != tt.want {
			t.Errorf("extractAfter(%q, %q) = %q, want %q", tt.raw, tt.keyword, got, tt.want)
		}
	}
}

func TestSplitNoteTitleBody(t *testing.T) {
	tests := []struct {
		content, wantTitle, wantBody string
	}{
		{"Meeting Notes | discussed API design", "Meeting Notes", "discussed API design"},
		{"Quick Note", "Quick Note", ""},
		{"Title | ", "Title", ""},
	}
	for _, tt := range tests {
		title, body := splitNoteTitleBody(tt.content)
		if title != tt.wantTitle || body != tt.wantBody {
			t.Errorf("splitNoteTitleBody(%q) = (%q, %q), want (%q, %q)", tt.content, title, body, tt.wantTitle, tt.wantBody)
		}
	}
}

func TestParseKeyValues(t *testing.T) {
	tests := []struct {
		input string
		want  []keyValue
	}{
		{"subject=new title", []keyValue{{key: "subject", value: "new title"}}},
		{"title=hello", []keyValue{{key: "title", value: "hello"}}},
		{"", nil},
	}
	for _, tt := range tests {
		got := parseKeyValues(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("parseKeyValues(%q) returned %d items, want %d", tt.input, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if got[i].key != tt.want[i].key || got[i].value != tt.want[i].value {
				t.Errorf("parseKeyValues(%q)[%d] = {%q, %q}, want {%q, %q}", tt.input, i, got[i].key, got[i].value, tt.want[i].key, tt.want[i].value)
			}
		}
	}
}

func TestStatusEmoji(t *testing.T) {
	tests := []struct {
		status, want string
	}{
		{"Completed", "[v]"},
		{"InProgress", "[~]"},
		{"Pending", "[ ]"},
		{"", "[ ]"},
	}
	for _, tt := range tests {
		if got := statusEmoji(tt.status); got != tt.want {
			t.Errorf("statusEmoji(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestWantsNoteOutput(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		// Chinese
		{"总结一下Nova工具人今天的聊天内容，并用Note的方式发送", true},
		{"总结今天的内容，创建Note", true},
		{"总结一下，用笔记发送到群里", true},
		{"总结一下，保存为note", true},
		{"总结一下，以Note发送", true},
		{"summarize today and send as note", true},
		// English
		{"summarize and create a note", true},
		{"summarize this chat as a note", true},
		{"summarize in note format", true},
		// Should NOT match
		{"总结一下今天的聊天", false},
		{"summarize today", false},
		{"/note list", false},
		{"hello", false},
	}
	for _, tt := range tests {
		if got := wantsNoteOutput(tt.text); got != tt.want {
			t.Errorf("wantsNoteOutput(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestFormatActionHelp(t *testing.T) {
	for _, cmd := range []string{"/task", "/note", "/event"} {
		help := formatActionHelp(cmd)
		if help == "" {
			t.Errorf("formatActionHelp(%q) returned empty string", cmd)
		}
	}
	help := formatActionHelp("/unknown")
	if help == "" {
		t.Error("formatActionHelp(/unknown) returned empty string")
	}
}
