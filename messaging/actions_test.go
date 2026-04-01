package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
		ServerURL:    srv.URL,
	}
	client := ringcentral.NewClient(creds)
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))
	return client, srv
}

func newNamedTestClients(serverURL string) (*ringcentral.Client, *ringcentral.Client) {
	botClient := ringcentral.NewBotClient(serverURL, "bot-token")
	botClient.SetDMChatID("dm-chat")

	privateClient := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID:     "id",
		ClientSecret: "secret",
		JWTToken:     "jwt",
		ServerURL:    serverURL,
	})
	privateClient.Auth().SetTokenForTest("private-token", time.Now().Add(1*time.Hour))
	return botClient, privateClient
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

func TestParseAgentActions_NoAction(t *testing.T) {
	reply := "This is a normal reply without any actions."
	clean, actions := ParseAgentActions(reply)
	if clean != reply {
		t.Errorf("expected clean reply to be original, got %q", clean)
	}
	if len(actions) != 0 {
		t.Errorf("expected 0 actions, got %d", len(actions))
	}
}

func TestParseAgentActions_Note(t *testing.T) {
	reply := `Here is the summary.

ACTION:NOTE title=Meeting Summary
## Key Points
- Discussed API design
- Agreed on deadline
END_ACTION`

	clean, actions := ParseAgentActions(reply)
	if !strings.Contains(clean, "Here is the summary") {
		t.Errorf("clean reply should contain main text, got %q", clean)
	}
	if strings.Contains(clean, "ACTION:") {
		t.Errorf("clean reply should not contain ACTION block, got %q", clean)
	}
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != "NOTE" {
		t.Errorf("expected NOTE, got %s", actions[0].Type)
	}
	if actions[0].Params["title"] != "Meeting Summary" {
		t.Errorf("expected title 'Meeting Summary', got %q", actions[0].Params["title"])
	}
	if !strings.Contains(actions[0].Body, "Key Points") {
		t.Errorf("expected body to contain 'Key Points', got %q", actions[0].Body)
	}
}

func TestParseAgentActions_Task(t *testing.T) {
	reply := `Done.

ACTION:TASK subject=Review PR #6
END_ACTION`

	clean, actions := ParseAgentActions(reply)
	if clean != "Done." {
		t.Errorf("expected 'Done.', got %q", clean)
	}
	if len(actions) != 1 || actions[0].Type != "TASK" {
		t.Fatalf("expected 1 TASK action, got %v", actions)
	}
	if actions[0].Params["subject"] != "Review PR #6" {
		t.Errorf("expected subject 'Review PR #6', got %q", actions[0].Params["subject"])
	}
}

func TestParseAgentActions_Event(t *testing.T) {
	reply := `Meeting scheduled.

ACTION:EVENT title=Team Standup start=2026-03-26T14:00:00Z end=2026-03-26T15:00:00Z
END_ACTION`

	clean, actions := ParseAgentActions(reply)
	if clean != "Meeting scheduled." {
		t.Errorf("expected 'Meeting scheduled.', got %q", clean)
	}
	if len(actions) != 1 || actions[0].Type != "EVENT" {
		t.Fatalf("expected 1 EVENT action, got %v", actions)
	}
	if actions[0].Params["title"] != "Team Standup" {
		t.Errorf("expected title 'Team Standup', got %q", actions[0].Params["title"])
	}
	if actions[0].Params["start"] != "2026-03-26T14:00:00Z" {
		t.Errorf("expected start '2026-03-26T14:00:00Z', got %q", actions[0].Params["start"])
	}
	if actions[0].Params["end"] != "2026-03-26T15:00:00Z" {
		t.Errorf("expected end '2026-03-26T15:00:00Z', got %q", actions[0].Params["end"])
	}
}

func TestParseAgentActions_Multiple(t *testing.T) {
	reply := `Summary done.

ACTION:NOTE title=Summary
content here
END_ACTION

ACTION:TASK subject=Follow up on action items
END_ACTION`

	clean, actions := ParseAgentActions(reply)
	if clean != "Summary done." {
		t.Errorf("expected 'Summary done.', got %q", clean)
	}
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}
	if actions[0].Type != "NOTE" {
		t.Errorf("expected first action NOTE, got %s", actions[0].Type)
	}
	if actions[1].Type != "TASK" {
		t.Errorf("expected second action TASK, got %s", actions[1].Type)
	}
}

func TestParseAgentActions_Card(t *testing.T) {
	reply := `Here is a progress card.

ACTION:CARD
{
  "type": "AdaptiveCard",
  "version": "1.3",
  "body": [
    {"type": "TextBlock", "text": "Project Status", "weight": "bolder"},
    {"type": "FactSet", "facts": [{"title": "Sprint", "value": "42"}]}
  ]
}
END_ACTION`

	clean, actions := ParseAgentActions(reply)
	if !strings.Contains(clean, "progress card") {
		t.Errorf("clean reply should contain main text, got %q", clean)
	}
	if strings.Contains(clean, "ACTION:") {
		t.Errorf("clean reply should not contain ACTION block")
	}
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != "CARD" {
		t.Errorf("expected CARD, got %s", actions[0].Type)
	}
	if !strings.Contains(actions[0].Body, "AdaptiveCard") {
		t.Errorf("body should contain AdaptiveCard JSON, got %q", actions[0].Body)
	}
	// Validate JSON
	if !json.Valid([]byte(actions[0].Body)) {
		t.Errorf("body should be valid JSON, got %q", actions[0].Body)
	}
}

func TestParseAgentActions_CardWithNoteCombo(t *testing.T) {
	reply := `Done.

ACTION:NOTE title=Meeting Notes
content
END_ACTION

ACTION:CARD
{"type":"AdaptiveCard","version":"1.3","body":[{"type":"TextBlock","text":"Hello"}]}
END_ACTION`

	clean, actions := ParseAgentActions(reply)
	if clean != "Done." {
		t.Errorf("expected 'Done.', got %q", clean)
	}
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}
	if actions[0].Type != "NOTE" {
		t.Errorf("expected NOTE, got %s", actions[0].Type)
	}
	if actions[1].Type != "CARD" {
		t.Errorf("expected CARD, got %s", actions[1].Type)
	}
}

func TestIsActionCommand_Card(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"/card get abc123", true},
		{"/card delete abc123", true},
		{"/card", true},
		{"/cards", false},
	}
	for _, tt := range tests {
		if got := IsActionCommand(tt.text); got != tt.want {
			t.Errorf("IsActionCommand(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestFormatActionHelp(t *testing.T) {
	for _, cmd := range []string{"/task", "/note", "/event", "/card"} {
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

func TestExtractChatID(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"12345", "12345"},
		{"![:Team](137158549510)", "137158549510"},
		{"![:Person](608081020)", "608081020"},
		{" 12345 ", "12345"},
	}
	for _, tt := range tests {
		if got := extractChatID(tt.input); got != tt.want {
			t.Errorf("extractChatID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsNumericID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"12345", true},
		{"0", true},
		{"608081020", true},
		{"", false},
		{"abc", false},
		{"123abc", false},
		{"12 34", false},
		{"Ian Zhang", false},
	}
	for _, tt := range tests {
		if got := isNumericID(tt.input); got != tt.want {
			t.Errorf("isNumericID(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestResolveNameToChatID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "directory/entries/search") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"records": []map[string]string{
					{"id": "person-1", "firstName": "Ian", "lastName": "Zhang", "email": "ian@example.com"},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "conversations") {
			json.NewEncoder(w).Encode(map[string]string{"id": "dm-chat-99", "type": "Direct"})
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	client, _ := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	// Override with our custom server
	creds := &ringcentral.Credentials{ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL}
	client = ringcentral.NewClient(creds)
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	chatID, err := resolveNameToChatID(context.Background(), client, "Ian Zhang")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chatID != "dm-chat-99" {
		t.Errorf("expected dm-chat-99, got %q", chatID)
	}
}

func TestResolveNameToPersonID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"records": []map[string]string{
				{"id": "person-42", "firstName": "Ian", "lastName": "Zhang"},
			},
		})
	}))
	defer srv.Close()

	creds := &ringcentral.Credentials{ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL}
	client := ringcentral.NewClient(creds)
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	personID, err := resolveNameToPersonID(context.Background(), client, "Ian")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if personID != "person-42" {
		t.Errorf("expected person-42, got %q", personID)
	}
}

func TestResolveChatParam_Numeric(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	id, err := resolveChatParam(context.Background(), client, "12345")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "12345" {
		t.Errorf("expected 12345, got %q", id)
	}
}

func TestResolveChatParam_Mention(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	id, err := resolveChatParam(context.Background(), client, "![:Team](137158549510)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "137158549510" {
		t.Errorf("expected 137158549510, got %q", id)
	}
}

func TestParseAgentActions_CardWithChatID(t *testing.T) {
	reply := `Card sent.

ACTION:CARD chatid=137158549510
{"type":"AdaptiveCard","version":"1.3","body":[{"type":"TextBlock","text":"Hello"}]}
END_ACTION`

	clean, actions := ParseAgentActions(reply)
	if clean != "Card sent." {
		t.Errorf("expected 'Card sent.', got %q", clean)
	}
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != "CARD" {
		t.Errorf("expected CARD, got %s", actions[0].Type)
	}
	if actions[0].Params["chatid"] != "137158549510" {
		t.Errorf("expected chatid '137158549510', got %q", actions[0].Params["chatid"])
	}
}

func TestExecuteAgentActions_CardUsesBotClientInDM(t *testing.T) {
	var mu sync.Mutex
	var authHeaders []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "ac1",
			"type":    "AdaptiveCard",
			"version": "1.3",
		})
	}))
	defer srv.Close()

	botClient, privateClient := newNamedTestClients(srv.URL)
	actions := []AgentAction{{
		Type: "CARD",
		Body: `{"type":"AdaptiveCard","version":"1.3","body":[]}`,
	}}

	results := ExecuteAgentActions(context.Background(), botClient, privateClient, "dm-chat", actions)
	if len(results) != 0 {
		t.Fatalf("expected no action errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(authHeaders) != 1 {
		t.Fatalf("expected 1 request, got %d", len(authHeaders))
	}
	if authHeaders[0] != "Bearer bot-token" {
		t.Fatalf("expected bot token, got %q", authHeaders[0])
	}
}

func TestExecuteAgentActions_CardUsesPrivateClientInGroup(t *testing.T) {
	var mu sync.Mutex
	var authHeaders []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "ac1",
			"type":    "AdaptiveCard",
			"version": "1.3",
		})
	}))
	defer srv.Close()

	botClient, privateClient := newNamedTestClients(srv.URL)
	actions := []AgentAction{{
		Type: "CARD",
		Body: `{"type":"AdaptiveCard","version":"1.3","body":[]}`,
	}}

	results := ExecuteAgentActions(context.Background(), botClient, privateClient, "group-chat", actions)
	if len(results) != 0 {
		t.Fatalf("expected no action errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	// No ListChats call — only the card creation request
	if len(authHeaders) != 1 {
		t.Fatalf("expected 1 request, got %d", len(authHeaders))
	}
	if authHeaders[0] != "Bearer private-token" {
		t.Fatalf("expected private token for card create, got %q", authHeaders[0])
	}
}

func TestExecuteAgentActions_CardFallsBackToBotWithoutPrivateClient(t *testing.T) {
	var mu sync.Mutex
	var authHeaders []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "ac1",
			"type":    "AdaptiveCard",
			"version": "1.3",
		})
	}))
	defer srv.Close()

	botClient := ringcentral.NewBotClient(srv.URL, "bot-token")
	actions := []AgentAction{{
		Type: "CARD",
		Body: `{"type":"AdaptiveCard","version":"1.3","body":[]}`,
	}}

	results := ExecuteAgentActions(context.Background(), botClient, botClient, "group-chat", actions)
	if len(results) != 0 {
		t.Fatalf("expected no action errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(authHeaders) != 1 {
		t.Fatalf("expected 1 request, got %d", len(authHeaders))
	}
	if authHeaders[0] != "Bearer bot-token" {
		t.Fatalf("expected bot token fallback, got %q", authHeaders[0])
	}
}
