package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/messaging/oob"
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

func writeCloudCalendarPermissionError(w http.ResponseWriter) {
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errorCode": "InsufficientPermissions",
		"message":   "In order to call this API endpoint, application needs to have [ManageCloudCalendars] permission",
		"errors": []map[string]string{{
			"errorCode":      "CMN-401",
			"message":        "In order to call this API endpoint, application needs to have [ManageCloudCalendars] permission",
			"permissionName": "ManageCloudCalendars",
		}},
		"permissionName": "ManageCloudCalendars",
	})
}

func TestHandleActionCommand_TaskList(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"records": []map[string]string{{"id": "t1", "subject": "Buy milk", "status": "Pending", "creationTime": time.Now().Format(time.RFC3339)}},
		})
	})
	defer srv.Close()
	client.SetOwnerID("fiji-user-1")

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
		{"/video create Design Review", true},
		{"/phone ringout +14155550100 +14155550199", true},
		{"/sms send +14155550199 hello", true},
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

func TestHandleActionCommand_VideoCreate(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rcvideo/v2/account/~/extension/~/bridges" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ringcentral.VideoBridge{
			ID:   "bridge-1",
			Name: "Design Review",
			Discovery: ringcentral.VideoBridgeDiscovery{
				Web: "https://v.ringcentral.com/join/123",
			},
		})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/video create Design Review")
	if !strings.Contains(result, "Video meeting created") || !strings.Contains(result, "https://v.ringcentral.com/join/123") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_VideoList(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/rcvideo/v1/history/meetings" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ringcentral.VideoMeetingHistoryList{
			Meetings: []ringcentral.VideoMeetingHistory{{
				ID:          "meeting-1",
				DisplayName: "明天12点会议",
				Status:      "Done",
				StartTime:   "2026-06-01T12:00:00Z",
			}},
		})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/video list")
	if !strings.Contains(result, "meeting-1") || !strings.Contains(result, "明天12点会议") || !strings.Contains(result, "Done") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_VideoPermissionErrorIsActionable(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorCode":"InsufficientPermissions","message":"In order to call this API endpoint, application needs to have [Video] permission","permissionName":"Video"}`))
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/video list")
	if !strings.Contains(result, "Private JWT App") || !strings.Contains(result, "Video") {
		t.Fatalf("expected actionable Video permission message, got %q", result)
	}
}

func TestHandleActionCommand_VideoCreateKeepsTitleWordsAfterType(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rcvideo/v2/account/~/extension/~/bridges" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body ringcentral.CreateVideoBridgeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode video request: %v", err)
		}
		if body.Name != "Design Review" || body.Type != "Scheduled" {
			t.Fatalf("unexpected video request: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ringcentral.VideoBridge{
			ID:   "bridge-1",
			Name: body.Name,
			Type: body.Type,
			Discovery: ringcentral.VideoBridgeDiscovery{
				Web: "https://v.ringcentral.com/join/123",
			},
		})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/video create Design type=Scheduled Review")
	if !strings.Contains(result, "Video meeting created") || !strings.Contains(result, "https://v.ringcentral.com/join/123") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_VideoCreateScheduledWithTimesCreatesEvent(t *testing.T) {
	var sawBridge bool
	var sawEvent bool
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rcvideo/v2/account/~/extension/~/bridges":
			sawBridge = true
			var body ringcentral.CreateVideoBridgeRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode video request: %v", err)
			}
			if body.Name != "Design Review" || body.Type != "Scheduled" {
				t.Fatalf("unexpected video request: %+v", body)
			}
			json.NewEncoder(w).Encode(ringcentral.VideoBridge{
				ID:   "bridge-1",
				Name: body.Name,
				Type: body.Type,
				Discovery: ringcentral.VideoBridgeDiscovery{
					Web: "https://v.ringcentral.com/join/123",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/events":
			sawEvent = true
			var body ringcentral.CreateEventRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode event request: %v", err)
			}
			if body.Title != "Design Review" ||
				body.StartTime != "2026-06-01T10:00:00Z" ||
				body.EndTime != "2026-06-01T11:00:00Z" ||
				body.Location != "RingCentral Video" ||
				!strings.Contains(body.Description, "https://v.ringcentral.com/join/123") {
				t.Fatalf("unexpected event request: %+v", body)
			}
			json.NewEncoder(w).Encode(ringcentral.Event{
				ID:        "event-1",
				Title:     body.Title,
				StartTime: body.StartTime,
				EndTime:   body.EndTime,
				Location:  body.Location,
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/video create Design type=Scheduled start=2026-06-01T10:00:00Z end=2026-06-01T11:00:00Z Review")
	if !sawBridge || !sawEvent {
		t.Fatalf("expected bridge and event creation, sawBridge=%v sawEvent=%v", sawBridge, sawEvent)
	}
	if !strings.Contains(result, "Scheduled video meeting created") ||
		!strings.Contains(result, "event-1") ||
		!strings.Contains(result, "https://v.ringcentral.com/join/123") {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_PhoneRingOut(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/restapi/v1.0/account/~/extension/~/ring-out" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ringcentral.RingOut{
			ID:     "ringout-1",
			Status: ringcentral.RingOutStatus{CallStatus: "InProgress"},
		})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/phone ringout +14155550100 +14155550199")
	if !strings.Contains(result, "ringout-1") || !strings.Contains(result, "InProgress") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_PhoneRingOutUsesForwardingNumberByDefault(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/forwarding-number":
			json.NewEncoder(w).Encode(ringcentral.ForwardingNumberList{
				Records: []ringcentral.ForwardingNumber{{
					PhoneNumber: "+14155550100",
					Label:       "My mobile",
					Features:    []string{"RingOut"},
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/extension/~/ring-out":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode ringout request: %v", err)
			}
			from, _ := body["from"].(map[string]any)
			if from["phoneNumber"] != "+14155550100" {
				t.Fatalf("from should use current extension forwarding number, got %+v", body["from"])
			}
			to, _ := body["to"].(map[string]any)
			if to["phoneNumber"] != "+14155550199" {
				t.Fatalf("unexpected ringout request: %+v", body)
			}
			json.NewEncoder(w).Encode(ringcentral.RingOut{
				ID:     "ringout-1",
				Status: ringcentral.RingOutStatus{CallStatus: "InProgress"},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/phone ringout +14155550199")
	if !strings.Contains(result, "ringout-1") || !strings.Contains(result, "InProgress") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_PhoneRingOutDoesNotUseDirectNumberAsDefaultFrom(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/forwarding-number":
			json.NewEncoder(w).Encode(ringcentral.ForwardingNumberList{})
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/phone-number":
			t.Fatalf("direct extension phone numbers must not be used as RingOut callback defaults")
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/extension/~/ring-out":
			t.Fatalf("RingOut should not start without a configured forwarding/callback number")
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/phone ringout +14155550199")
	if !strings.Contains(result, "forwarding") || !strings.Contains(result, "callback") {
		t.Fatalf("expected forwarding/callback setup guidance, got %q", result)
	}
}

func TestHandleActionCommand_PhoneRingOutPermissionErrorIsActionable(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorCode":"InsufficientPermissions","message":"In order to call this API endpoint, application needs to have [RingOut] permission","permissionName":"RingOut"}`))
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/phone ringout +14155550100 +12123753080")
	if !strings.Contains(result, "Private JWT App") || !strings.Contains(result, "RingOut") {
		t.Fatalf("expected actionable RingOut permission message, got %q", result)
	}
}

func TestHandleActionCommand_PhoneMissedCallsFiltersByResult(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/restapi/v1.0/account/~/extension/~/call-log" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ringcentral.CallLogList{
			Records: []ringcentral.CallLogRecord{
				{ID: "missed-1", StartTime: "2026-06-01T10:00:00Z", Direction: "Inbound", Result: "Missed", From: ringcentral.CallLogParty{PhoneNumber: "+12125550100"}, To: ringcentral.CallLogParty{PhoneNumber: "+14155550100"}},
				{ID: "answered-1", StartTime: "2026-06-01T11:00:00Z", Direction: "Inbound", Result: "Call connected", From: ringcentral.CallLogParty{PhoneNumber: "+12125550101"}, To: ringcentral.CallLogParty{PhoneNumber: "+14155550100"}},
			},
		})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/phone missed")
	if !strings.Contains(result, "missed-1") || strings.Contains(result, "answered-1") || !strings.Contains(result, "Missed") {
		t.Fatalf("expected only missed calls with result displayed, got %q", result)
	}
}

func TestHandleActionCommand_PhoneMissedCallsEmptyResultUsesMissedMessage(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/restapi/v1.0/account/~/extension/~/call-log" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ringcentral.CallLogList{})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/phone missed")
	if result != "No missed call records found." {
		t.Fatalf("expected missed-call empty state, got %q", result)
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
		{"John Lin", false},
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

	id, err := resolveChatParam(context.Background(), client, "12345", "current-chat")
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

	id, err := resolveChatParam(context.Background(), client, "![:Team](137158549510)", "current-chat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "137158549510" {
		t.Errorf("expected 137158549510, got %q", id)
	}
}

func TestResolveChatParam_SelfPronoun(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	for _, pronoun := range []string{"我", "me", "myself", "私", "나", "moi", "yo", "ich", "я"} {
		id, err := resolveChatParam(context.Background(), client, pronoun, "current-chat-123")
		if err != nil {
			t.Fatalf("resolveChatParam(%q): unexpected error: %v", pronoun, err)
		}
		if id != "current-chat-123" {
			t.Errorf("resolveChatParam(%q) = %q, want %q", pronoun, id, "current-chat-123")
		}
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

func TestParseAgentActions_VideoAndPhoneCallParams(t *testing.T) {
	reply := `ACTION:VIDEO title=Design Review type=Scheduled chatid=team
END_ACTION
ACTION:PHONE_CALL to=+14155550199
END_ACTION
ACTION:PHONE_CALL to=Grace He
END_ACTION
ACTION:SMS to=+14155550198 from=+14155550100
Running late by 10 minutes.
END_ACTION`

	_, actions := ParseAgentActions(reply)
	if len(actions) != 4 {
		t.Fatalf("expected 4 actions, got %d", len(actions))
	}
	if actions[0].Type != "VIDEO" || actions[0].Params["title"] != "Design Review" || actions[0].Params["type"] != "Scheduled" {
		t.Fatalf("unexpected video action: %+v", actions[0])
	}
	if actions[1].Type != "PHONE_CALL" || actions[1].Params["from"] != "" || actions[1].Params["to"] != "+14155550199" {
		t.Fatalf("unexpected phone call action: %+v", actions[1])
	}
	if actions[2].Type != "PHONE_CALL" || actions[2].Params["to"] != "Grace He" {
		t.Fatalf("unexpected named phone call action: %+v", actions[2])
	}
	if actions[3].Type != "SMS" || actions[3].Params["to"] != "+14155550198" || actions[3].Params["from"] != "+14155550100" || actions[3].Body != "Running late by 10 minutes." {
		t.Fatalf("unexpected sms action: %+v", actions[3])
	}
}

func TestParseAgentActions_VideoListParams(t *testing.T) {
	reply := `ACTION:VIDEO_LIST scope=today important=true limit=5
END_ACTION`

	_, actions := ParseAgentActions(reply)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != "VIDEO_LIST" ||
		actions[0].Params["scope"] != "today" ||
		actions[0].Params["important"] != "true" ||
		actions[0].Params["limit"] != "5" {
		t.Fatalf("unexpected video list action: %+v", actions[0])
	}
}

func TestParseAgentActions_PhoneCallLogParams(t *testing.T) {
	reply := `ACTION:PHONE_CALLLOG scope=recent days=15 missing=true summary=true next_actions=true limit=10
END_ACTION`

	_, actions := ParseAgentActions(reply)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != "PHONE_CALLLOG" ||
		actions[0].Params["scope"] != "recent" ||
		actions[0].Params["days"] != "15" ||
		actions[0].Params["missing"] != "true" ||
		actions[0].Params["summary"] != "true" ||
		actions[0].Params["next_actions"] != "true" ||
		actions[0].Params["limit"] != "10" {
		t.Fatalf("unexpected phone call log action: %+v", actions[0])
	}
}

func TestExecuteAgentActions_VideoCreatesBridgeAndPostsLink(t *testing.T) {
	var posted bool
	var recorded []ActionEvent
	restore := SetActionEventRecorder(func(_ context.Context, event ActionEvent) {
		recorded = append(recorded, event)
	})
	defer restore()
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rcvideo/v2/account/~/extension/~/bridges":
			var body ringcentral.CreateVideoBridgeRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode video request: %v", err)
			}
			if body.Name != "Design Review" || body.Type != "Scheduled" {
				t.Fatalf("unexpected video request: %+v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ringcentral.VideoBridge{
				ID:   "bridge-1",
				Name: "Design Review",
				Type: "Scheduled",
				Discovery: ringcentral.VideoBridgeDiscovery{
					Web: "https://v.ringcentral.com/join/123",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/chats/c1/posts":
			posted = true
			var body ringcentral.CreatePostRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode post request: %v", err)
			}
			if !strings.Contains(body.Text, "https://v.ringcentral.com/join/123") {
				t.Fatalf("expected join URL in post, got %q", body.Text)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "VIDEO",
		Params: map[string]string{"title": "Design Review", "type": "Scheduled"},
	}}, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if !posted {
		t.Fatal("expected video join link post")
	}
	if len(recorded) != 1 {
		t.Fatalf("recorded events = %#v, want one", recorded)
	}
	if recorded[0].Type != "VIDEO" || recorded[0].Status != "completed" {
		t.Fatalf("recorded event = %#v", recorded[0])
	}
	if recorded[0].Details["target_chat"] != "c1" || recorded[0].Details["bridge_id"] != "bridge-1" {
		t.Fatalf("recorded details = %#v", recorded[0].Details)
	}
}

func TestExecuteAgentActions_VideoScheduledWithTimesCreatesEventAndPostsDetails(t *testing.T) {
	var posted string
	var recorded []ActionEvent
	restore := SetActionEventRecorder(func(_ context.Context, event ActionEvent) {
		recorded = append(recorded, event)
	})
	defer restore()
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rcvideo/v2/account/~/extension/~/bridges":
			var body ringcentral.CreateVideoBridgeRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode video request: %v", err)
			}
			if body.Name != "Design Review" || body.Type != "Scheduled" {
				t.Fatalf("unexpected video request: %+v", body)
			}
			json.NewEncoder(w).Encode(ringcentral.VideoBridge{
				ID:   "bridge-1",
				Name: "Design Review",
				Type: "Scheduled",
				Discovery: ringcentral.VideoBridgeDiscovery{
					Web: "https://v.ringcentral.com/join/123",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/events":
			var body ringcentral.CreateEventRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode event request: %v", err)
			}
			if body.Title != "Design Review" ||
				body.StartTime != "2026-06-01T10:00:00Z" ||
				body.EndTime != "2026-06-01T11:00:00Z" ||
				!strings.Contains(body.Description, "https://v.ringcentral.com/join/123") {
				t.Fatalf("unexpected event request: %+v", body)
			}
			json.NewEncoder(w).Encode(ringcentral.Event{
				ID:        "event-1",
				Title:     body.Title,
				StartTime: body.StartTime,
				EndTime:   body.EndTime,
				Location:  "RingCentral Video",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/chats/c1/posts":
			var body ringcentral.CreatePostRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode post request: %v", err)
			}
			posted = body.Text
			json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type: "VIDEO",
		Params: map[string]string{
			"title": "Design Review",
			"type":  "Scheduled",
			"start": "2026-06-01T10:00:00Z",
			"end":   "2026-06-01T11:00:00Z",
		},
	}}, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if !strings.Contains(posted, "Scheduled video meeting created") ||
		!strings.Contains(posted, "event-1") ||
		!strings.Contains(posted, "https://v.ringcentral.com/join/123") {
		t.Fatalf("unexpected posted text: %q", posted)
	}
	if len(recorded) != 1 {
		t.Fatalf("recorded events = %#v, want one", recorded)
	}
	if recorded[0].Details["bridge_id"] != "bridge-1" || recorded[0].Details["event_id"] != "event-1" {
		t.Fatalf("recorded details = %#v", recorded[0].Details)
	}
}

func TestExecuteAgentActions_PhoneCallLogTodayPostsSummaryAndNextActions(t *testing.T) {
	var posted string
	var listed bool
	var smsSent bool
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/call-log":
			listed = true
			q := r.URL.Query()
			if q.Get("recordCount") != "100" || q.Get("dateFrom") == "" || q.Get("dateTo") == "" {
				t.Fatalf("expected today call-log query, got %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(ringcentral.CallLogList{
				Records: []ringcentral.CallLogRecord{
					{
						ID:        "missed-1",
						StartTime: time.Now().UTC().Format(time.RFC3339),
						Direction: "Inbound",
						Result:    "Missed",
						From:      ringcentral.CallLogParty{PhoneNumber: "+12125550100", Name: "Customer A"},
						To:        ringcentral.CallLogParty{PhoneNumber: "+14155550100"},
					},
					{
						ID:        "answered-1",
						StartTime: time.Now().UTC().Format(time.RFC3339),
						Direction: "Outbound",
						Result:    "Accepted",
						Duration:  320,
						From:      ringcentral.CallLogParty{PhoneNumber: "+14155550100"},
						To:        ringcentral.CallLogParty{PhoneNumber: "+12125550199", Name: "Partner B"},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/phone-number":
			json.NewEncoder(w).Encode(ringcentral.ExtensionPhoneNumberList{
				Records: []ringcentral.ExtensionPhoneNumber{{PhoneNumber: "+14155550100", Status: "Normal"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/extension/~/sms":
			smsSent = true
			var body ringcentral.CreateSMSRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode sms request: %v", err)
			}
			if len(body.To) != 1 || body.To[0].PhoneNumber != "+12125550100" {
				t.Fatalf("unexpected sms request body: %+v", body)
			}
			json.NewEncoder(w).Encode(ringcentral.SMSMessage{ID: "sms-1", MessageStatus: "Queued"})
		case r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/chats/c1/posts":
			var body ringcentral.CreatePostRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode post request: %v", err)
			}
			posted = body.Text
			json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type: "PHONE_CALLLOG",
		Params: map[string]string{
			"scope":        "today",
			"missing":      "true",
			"summary":      "true",
			"next_actions": "true",
			"limit":        "10",
		},
	}}, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if !listed {
		t.Fatal("expected call log API call")
	}
	if !smsSent {
		t.Fatal("expected missed-call follow-up SMS")
	}
	if !strings.Contains(posted, "Call summary today") ||
		!strings.Contains(posted, "Missed calls: 1") ||
		!strings.Contains(posted, "Customer A") ||
		!strings.Contains(posted, "SMS sent to Customer A (+12125550100)") ||
		!strings.Contains(posted, "click/call Customer A (+12125550100)") {
		t.Fatalf("unexpected posted call summary: %q", posted)
	}
}

func TestExecuteAgentActions_PhoneCallLogFiltersMissedCalls(t *testing.T) {
	var posted string
	var listed bool
	var smsSent bool
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/fiji-user-1/call-log":
			listed = true
			q := r.URL.Query()
			if q.Get("direction") != "Inbound" || q.Get("result") != "" || q.Get("dateFrom") == "" || q.Get("dateTo") == "" {
				t.Fatalf("expected missed-call extension query, got %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(ringcentral.CallLogList{
				Records: []ringcentral.CallLogRecord{{
					ID:        "missed-1",
					StartTime: time.Now().UTC().Format(time.RFC3339),
					Direction: "Inbound",
					Result:    "Missed",
					From:      ringcentral.CallLogParty{PhoneNumber: "+12125550100", Name: "Customer A"},
					To:        ringcentral.CallLogParty{PhoneNumber: "+14155550100"},
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/phone-number":
			json.NewEncoder(w).Encode(ringcentral.ExtensionPhoneNumberList{
				Records: []ringcentral.ExtensionPhoneNumber{{PhoneNumber: "+14155550100", Status: "Normal"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/extension/~/sms":
			smsSent = true
			json.NewEncoder(w).Encode(ringcentral.SMSMessage{ID: "sms-2", MessageStatus: "Queued"})
		case r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/chats/c1/posts":
			var body ringcentral.CreatePostRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode post request: %v", err)
			}
			posted = body.Text
			json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type: "PHONE_CALLLOG",
		Params: map[string]string{
			"scope":        "today",
			"missing":      "true",
			"summary":      "true",
			"next_actions": "true",
			"limit":        "10",
		},
	}}, ActionContext{OriginIsOwner: true, RequesterID: "fiji-user-1"})
	if len(results) != 0 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if !listed {
		t.Fatal("expected extension call-log query")
	}
	if !smsSent {
		t.Fatal("expected missed-call follow-up SMS")
	}
	if !strings.Contains(posted, "Call summary today") ||
		!strings.Contains(posted, "Missed calls: 1") ||
		!strings.Contains(posted, "Customer A") ||
		!strings.Contains(posted, "SMS sent to Customer A (+12125550100)") {
		t.Fatalf("unexpected posted call summary: %q", posted)
	}
}

func TestExecuteAgentActions_PhoneCallLogRecentDaysFindsOlderMissedCalls(t *testing.T) {
	var posted string
	var listed bool
	var smsSent bool
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/fiji-user-1/call-log":
			listed = true
			q := r.URL.Query()
			if q.Get("direction") != "Inbound" || q.Get("result") != "" || q.Get("dateFrom") == "" || q.Get("dateTo") == "" {
				t.Fatalf("expected dated inbound extension query with client-side result filtering, got %s", r.URL.RawQuery)
			}
			if q.Get("recordCount") != "100" {
				t.Fatalf("expected widened missed-call search window, got %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(ringcentral.CallLogList{
				Records: []ringcentral.CallLogRecord{
					{
						ID:        "answered-older",
						StartTime: time.Now().AddDate(0, 0, -4).UTC().Format(time.RFC3339),
						Direction: "Inbound",
						Result:    "Accepted",
						From:      ringcentral.CallLogParty{PhoneNumber: "+12125550101", Name: "Answered Caller"},
						To:        ringcentral.CallLogParty{PhoneNumber: "+14155550100"},
					},
					{
						ID:        "missed-older",
						StartTime: time.Now().AddDate(0, 0, -13).UTC().Format(time.RFC3339),
						Direction: "Inbound",
						Result:    "Missed",
						From:      ringcentral.CallLogParty{PhoneNumber: "+12125550102", Name: "Tina Zhang"},
						To:        ringcentral.CallLogParty{PhoneNumber: "+14155550100"},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/phone-number":
			json.NewEncoder(w).Encode(ringcentral.ExtensionPhoneNumberList{
				Records: []ringcentral.ExtensionPhoneNumber{{PhoneNumber: "+14155550100", Status: "Normal"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/extension/~/sms":
			smsSent = true
			json.NewEncoder(w).Encode(ringcentral.SMSMessage{ID: "sms-3", MessageStatus: "Queued"})
		case r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/chats/c1/posts":
			var body ringcentral.CreatePostRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode post request: %v", err)
			}
			posted = body.Text
			json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type: "PHONE_CALLLOG",
		Params: map[string]string{
			"scope":        "recent",
			"days":         "15",
			"missing":      "true",
			"summary":      "true",
			"next_actions": "true",
			"limit":        "10",
		},
	}}, ActionContext{OriginIsOwner: true, RequesterID: "fiji-user-1"})
	if len(results) != 0 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if !listed {
		t.Fatal("expected extension call-log query")
	}
	if !smsSent {
		t.Fatal("expected missed-call follow-up SMS")
	}
	if !strings.Contains(posted, "missed-older") ||
		!strings.Contains(posted, "Tina Zhang") ||
		strings.Contains(posted, "answered-older") ||
		!strings.Contains(posted, "SMS sent to Tina Zhang (+12125550102)") {
		t.Fatalf("expected only older missed call in posted summary, got %q", posted)
	}
}

func TestExecuteAgentActions_PhoneCallLogRecentDefaultsToDatedWindow(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/fiji-user-1/call-log":
			q := r.URL.Query()
			if q.Get("dateFrom") == "" || q.Get("dateTo") == "" || q.Get("result") != "" {
				t.Fatalf("expected recent call-log query to use a dated window and client-side result filtering, got %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(ringcentral.CallLogList{
				Records: []ringcentral.CallLogRecord{{
					ID:        "missed-default-window",
					StartTime: time.Now().AddDate(0, 0, -12).UTC().Format(time.RFC3339),
					Direction: "Inbound",
					Result:    "Missed",
					From:      ringcentral.CallLogParty{Name: "Grace He"},
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/directory/entries/search":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode directory search: %v", err)
			}
			if body["searchString"] != "Grace He" {
				t.Fatalf("unexpected directory search body: %+v", body)
			}
			json.NewEncoder(w).Encode(ringcentral.DirectorySearchResult{
				Records: []ringcentral.DirectoryEntry{{
					ID:        "person-grace",
					FirstName: "Grace",
					LastName:  "He",
					PhoneNumbers: []ringcentral.ContactPhoneNumber{{
						PhoneNumber: "+12123753080",
						Type:        "DirectNumber",
					}},
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/phone-number":
			json.NewEncoder(w).Encode(ringcentral.ExtensionPhoneNumberList{
				Records: []ringcentral.ExtensionPhoneNumber{{PhoneNumber: "+14155550100", Status: "Normal"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/extension/~/sms":
			var body ringcentral.CreateSMSRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode sms request: %v", err)
			}
			if len(body.To) != 1 || body.To[0].PhoneNumber != "+12123753080" {
				t.Fatalf("unexpected sms request body: %+v", body)
			}
			json.NewEncoder(w).Encode(ringcentral.SMSMessage{ID: "sms-4", MessageStatus: "Queued"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	text, count, err := phoneCallLogFromParams(context.Background(), client, map[string]string{
		"scope":        "recent",
		"missing":      "true",
		"next_actions": "true",
		"limit":        "10",
	}, "fiji-user-1")
	if err != nil {
		t.Fatalf("phoneCallLogFromParams() error = %v", err)
	}
	if count != 1 ||
		!strings.Contains(text, "missed-default-window") ||
		!strings.Contains(text, "Grace He (+12123753080)") ||
		!strings.Contains(text, "SMS sent to Grace He (+12123753080)") {
		t.Fatalf("expected recent missed call from default dated window, got count=%d text=%q", count, text)
	}
}

func TestExecuteAgentActions_VideoListTodayPostsImportantMeetings(t *testing.T) {
	var posted string
	var listed bool
	now := time.Now()
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/cloud-calendars/ucc":
			listed = true
			if r.URL.Query().Get("sync") != "true" {
				t.Fatalf("unexpected cloud calendar query: %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(ringcentral.CloudCalendarList{
				Records: []ringcentral.CloudCalendar{{
					ID:         "cal-1",
					ProviderID: "office365",
					CalendarID: "cal-1",
					Name:       "Work",
					Connected:  true,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/cloud-calendars/ucc/office365/~/cal-1/events":
			q := r.URL.Query()
			if q.Get("includeNonRcEvents") != "true" || q.Get("startTimeFrom") == "" || q.Get("startTimeTo") == "" {
				t.Fatalf("unexpected cloud event query: %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(ringcentral.CloudCalendarEventList{
				Records: []ringcentral.CloudCalendarEvent{{
					ID:        "event-today",
					Subject:   "客户升级复盘",
					StartTime: now.UTC().Format(time.RFC3339),
					EndTime:   now.Add(30 * time.Minute).UTC().Format(time.RFC3339),
					Location:  "RingCentral Video",
				}, {
					ID:        "event-tomorrow",
					Subject:   "Tomorrow Meeting",
					StartTime: now.AddDate(0, 0, 1).UTC().Format(time.RFC3339),
					EndTime:   now.AddDate(0, 0, 1).Add(30 * time.Minute).UTC().Format(time.RFC3339),
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/chats/c1/posts":
			var body ringcentral.CreatePostRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode post request: %v", err)
			}
			posted = body.Text
			json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()
	client.SetOwnerID("fiji-user-1")

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "VIDEO_LIST",
		Params: map[string]string{"scope": "today", "important": "true", "limit": "5"},
	}}, ActionContext{OriginIsOwner: true, RequesterID: "fiji-user-1"})
	if len(results) != 0 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if !listed {
		t.Fatal("expected Video list API call")
	}
	if !strings.Contains(posted, "Upcoming meetings today") ||
		!strings.Contains(posted, "Title:") ||
		!strings.Contains(posted, "客户升级复盘") ||
		!strings.Contains(posted, "RingCentral Video") ||
		strings.Contains(posted, "Tomorrow Meeting") {
		t.Fatalf("unexpected posted video list: %q", posted)
	}
}

func TestExecuteAgentActions_VideoListTodayCloudCalendarPermissionErrorDoesNotFallback(t *testing.T) {
	var posted string
	var listedFallbackEvents bool
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/cloud-calendars/ucc"):
			writeCloudCalendarPermissionError(w)
		case r.Method == http.MethodGet && r.URL.Path == "/team-messaging/v1/events":
			listedFallbackEvents = true
			t.Fatalf("should not fall back to team events when cloud calendars are unavailable")
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()
	client.SetOwnerID("fiji-user-1")

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "VIDEO_LIST",
		Params: map[string]string{"scope": "today", "important": "true", "limit": "5"},
	}}, ActionContext{OriginIsOwner: true, RequesterID: "fiji-user-1"})
	if listedFallbackEvents {
		t.Fatal("expected no fallback event query")
	}
	if len(results) != 1 || !strings.Contains(results[0], "ManageCloudCalendars permission is missing") {
		t.Fatalf("expected cloud calendar permission error, got %+v", results)
	}
	if posted != "" {
		t.Fatalf("expected no posted fallback result, got %q", posted)
	}
}

func TestExecuteAgentActions_VideoListTodayUsesCloudCalendarEvents(t *testing.T) {
	var posted string
	now := time.Now()
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/cloud-calendars/ucc":
			if r.URL.Query().Get("sync") != "true" {
				t.Fatalf("unexpected cloud calendar query: %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(ringcentral.CloudCalendarList{
				Records: []ringcentral.CloudCalendar{{
					ID:         "cal-1",
					ProviderID: "office365",
					CalendarID: "cal-1",
					Name:       "Work",
					Connected:  true,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/cloud-calendars/ucc/office365/~/cal-1/events":
			q := r.URL.Query()
			if q.Get("includeNonRcEvents") != "true" || q.Get("startTimeFrom") == "" || q.Get("startTimeTo") == "" {
				t.Fatalf("unexpected cloud event query: %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(ringcentral.CloudCalendarEventList{
				Records: []ringcentral.CloudCalendarEvent{{
					ID:          "cloud-event-1",
					Subject:     "Customer escalation review",
					StartTime:   now.UTC().Format(time.RFC3339),
					EndTime:     now.Add(45 * time.Minute).UTC().Format(time.RFC3339),
					Location:    "RingCentral Video",
					Description: "Review escalation status, blockers, and owner actions.",
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/chats/c1/posts":
			var body ringcentral.CreatePostRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode post request: %v", err)
			}
			posted = body.Text
			json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()
	client.SetOwnerID("fiji-user-1")

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "VIDEO_LIST",
		Params: map[string]string{"scope": "today", "important": "true", "limit": "5"},
	}}, ActionContext{OriginIsOwner: true, RequesterID: "fiji-user-1"})
	if len(results) != 0 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if !strings.Contains(posted, "Customer escalation review") ||
		!strings.Contains(posted, "Description: Review escalation status") ||
		strings.Contains(posted, "team_messaging_event") {
		t.Fatalf("expected cloud calendar meeting details, got %q", posted)
	}
}

func TestExecuteAgentActions_VideoListRecentUsesMeetingHistory(t *testing.T) {
	var posted string
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rcvideo/v1/history/meetings":
			json.NewEncoder(w).Encode(ringcentral.VideoMeetingHistoryList{
				Meetings: []ringcentral.VideoMeetingHistory{{
					ID:          "meeting-history",
					DisplayName: "上周复盘",
					Status:      "Done",
					StartTime:   time.Now().AddDate(0, 0, -3).UTC().Format(time.RFC3339),
					HostInfo:    ringcentral.VideoMeetingParticipant{DisplayName: "Summer Gan"},
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/chats/c1/posts":
			var body ringcentral.CreatePostRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode post request: %v", err)
			}
			posted = body.Text
			json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()
	client.SetOwnerID("fiji-user-1")

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "VIDEO_LIST",
		Params: map[string]string{"scope": "recent", "limit": "5"},
	}}, ActionContext{OriginIsOwner: true, RequesterID: "fiji-user-1"})
	if len(results) != 0 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if !strings.Contains(posted, "Video meeting records") ||
		!strings.Contains(posted, "meeting-history") ||
		!strings.Contains(posted, "上周复盘") {
		t.Fatalf("unexpected posted video history: %q", posted)
	}
}

func TestExecuteAgentActions_VideoListUpcomingUsesFutureCloudCalendarWindow(t *testing.T) {
	var posted string
	now := time.Now()
	var eventQuery url.Values
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/cloud-calendars/ucc":
			if r.URL.Query().Get("sync") != "true" {
				t.Fatalf("unexpected cloud calendar query: %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(ringcentral.CloudCalendarList{
				Records: []ringcentral.CloudCalendar{{
					ID:         "cal-1",
					ProviderID: "office365",
					CalendarID: "cal-1",
					Name:       "Work",
					Connected:  true,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/cloud-calendars/ucc/office365/~/cal-1/events":
			eventQuery = r.URL.Query()
			if eventQuery.Get("includeNonRcEvents") != "true" || eventQuery.Get("startTimeFrom") == "" || eventQuery.Get("startTimeTo") == "" {
				t.Fatalf("unexpected cloud event query: %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(ringcentral.CloudCalendarEventList{
				Records: []ringcentral.CloudCalendarEvent{
					{
						ID:          "future-event-1",
						Subject:     "Roadmap sync",
						StartTime:   now.Add(48 * time.Hour).UTC().Format(time.RFC3339),
						EndTime:     now.Add(49 * time.Hour).UTC().Format(time.RFC3339),
						Location:    "RingCentral Video",
						Description: "Discuss the next release plan.",
					},
					{
						ID:          "past-event-1",
						Subject:     "Already done",
						StartTime:   now.Add(-2 * time.Hour).UTC().Format(time.RFC3339),
						EndTime:     now.Add(-1 * time.Hour).UTC().Format(time.RFC3339),
						Location:    "Room 1",
						Description: "Past meeting should not appear.",
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/chats/c1/posts":
			var body ringcentral.CreatePostRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode post request: %v", err)
			}
			posted = body.Text
			json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()
	client.SetOwnerID("fiji-user-1")

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "VIDEO_LIST",
		Params: map[string]string{"scope": "upcoming", "limit": "5"},
	}}, ActionContext{OriginIsOwner: true, RequesterID: "fiji-user-1"})
	if len(results) != 0 {
		t.Fatalf("unexpected results: %+v", results)
	}
	start, err := time.Parse(time.RFC3339, eventQuery.Get("startTimeFrom"))
	if err != nil {
		t.Fatalf("parse upcoming startTimeFrom: %v", err)
	}
	end, err := time.Parse(time.RFC3339, eventQuery.Get("startTimeTo"))
	if err != nil {
		t.Fatalf("parse upcoming startTimeTo: %v", err)
	}
	if !start.After(now.Add(-2 * time.Minute)) {
		t.Fatalf("expected upcoming query to start near now, got start=%s now=%s", start, now.UTC())
	}
	if end.Before(start.AddDate(0, 0, 13)) {
		t.Fatalf("expected upcoming query to span roughly 14 days, got start=%s end=%s", start, end)
	}
	if !strings.Contains(posted, "Upcoming meetings") ||
		!strings.Contains(posted, "Roadmap sync") ||
		strings.Contains(posted, "Already done") {
		t.Fatalf("expected only future upcoming meeting in post, got %q", posted)
	}
}

func TestExecuteAgentActions_VideoListRejectsMismatchedRequesterJWT(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("video history should fail before HTTP when requester and JWT owner differ")
	})
	defer srv.Close()
	client.SetOwnerID("jwt-owner")

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "VIDEO_LIST",
		Params: map[string]string{"scope": "today"},
	}}, ActionContext{OriginIsOwner: true, RequesterID: "fiji-user-1"})

	if len(results) != 1 ||
		!strings.Contains(results[0], "FIJI requester extension is fiji-user-1") ||
		!strings.Contains(results[0], "Private JWT owner is jwt-owner") ||
		!strings.Contains(results[0], "will not fall back to company-level history") {
		t.Fatalf("expected requester/JWT mismatch message, got %+v", results)
	}
}

func TestExecuteAgentActions_VideoAllowedByDefaultCapabilities(t *testing.T) {
	var recorded []ActionEvent
	restore := SetActionEventRecorder(func(_ context.Context, event ActionEvent) {
		recorded = append(recorded, event)
	})
	defer restore()
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rcvideo/v2/account/~/extension/~/bridges":
			json.NewEncoder(w).Encode(ringcentral.VideoBridge{
				ID:        "bridge-1",
				Name:      "Design Review",
				Discovery: ringcentral.VideoBridgeDiscovery{Web: "https://v.ringcentral.com/join/123"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/chats/c1/posts":
			json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "VIDEO",
		Params: map[string]string{"title": "Design Review"},
	}}, ActionContext{
		OriginIsOwner: true,
		Capabilities:  []string{"message", "summary"},
	})
	if len(results) != 0 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if len(recorded) != 1 || recorded[0].Type != "VIDEO" || recorded[0].Status != "completed" {
		t.Fatalf("recorded events = %#v", recorded)
	}
}

func TestExecuteAgentActions_PhoneCallRequiresOwner(t *testing.T) {
	var recorded []ActionEvent
	restore := SetActionEventRecorder(func(_ context.Context, event ActionEvent) {
		recorded = append(recorded, event)
	})
	defer restore()
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("non-owner phone call should not call API: %s %s", r.Method, r.URL.Path)
	})
	defer srv.Close()

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "PHONE_CALL",
		Params: map[string]string{"to": "+14155550199"},
	}}, ActionContext{OriginIsOwner: false})
	if len(results) != 1 || !strings.Contains(results[0], "owner") {
		t.Fatalf("expected owner refusal, got %+v", results)
	}
	if len(recorded) != 1 || recorded[0].Type != "PHONE_CALL" || recorded[0].Status != "blocked" {
		t.Fatalf("recorded events = %#v", recorded)
	}
	if recorded[0].Details["reason"] != "owner_required" {
		t.Fatalf("recorded details = %#v", recorded[0].Details)
	}
}

func TestExecuteAgentActions_PhoneCallRecordsFijiClientAction(t *testing.T) {
	var recorded []ActionEvent
	restore := SetActionEventRecorder(func(_ context.Context, event ActionEvent) {
		recorded = append(recorded, event)
	})
	defer restore()
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("PHONE_CALL should not use RingOut REST API: %s %s", r.Method, r.URL.Path)
	})
	defer srv.Close()

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "PHONE_CALL",
		Params: map[string]string{"to": "+14155550199"},
	}}, ActionContext{
		OriginIsOwner: true,
		Capabilities:  []string{"message", "summary", "video"},
	})
	if len(results) != 1 || !strings.Contains(results[0], "FIJI") {
		t.Fatalf("expected FIJI make-call guidance, got %+v", results)
	}
	if len(recorded) != 1 || recorded[0].Type != "PHONE_CALL" || recorded[0].Status != "client_action_required" {
		t.Fatalf("recorded events = %#v", recorded)
	}
	if recorded[0].Details["client_action"] != "make_call" || recorded[0].Details["to_number"] != "+14155550199" {
		t.Fatalf("recorded details = %#v", recorded[0].Details)
	}
}

func TestExecuteAgentActions_RingOutWithChatIDStillRequiresOwnerBeforeOOB(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("non-owner RINGOUT with chatid should not call API or OOB path: %s %s", r.Method, r.URL.Path)
	})
	defer srv.Close()

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type: "RINGOUT",
		Params: map[string]string{
			"from":   "+14155550100",
			"to":     "+14155550199",
			"chatid": "target-chat",
		},
	}}, ActionContext{
		OriginIsOwner: false,
		OOB:           oob.New(oob.Options{}),
		OwnerDMChat:   "owner-dm",
		OwnerID:       "owner-1",
	})
	if len(results) != 1 || !strings.Contains(results[0], "owner") {
		t.Fatalf("expected owner refusal before OOB, got %+v", results)
	}
}

func TestExecuteAgentActions_PhoneCallOwnerResolvesPersonNameForFijiClient(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/directory/entries/search":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode directory search: %v", err)
			}
			if body["searchString"] != "Grace He" {
				t.Fatalf("unexpected directory search body: %+v", body)
			}
			json.NewEncoder(w).Encode(ringcentral.DirectorySearchResult{
				Records: []ringcentral.DirectoryEntry{{
					ID:        "person-grace",
					FirstName: "Grace",
					LastName:  "He",
					PhoneNumbers: []ringcentral.ContactPhoneNumber{{
						PhoneNumber: "+12123753080",
						Type:        "DirectNumber",
					}},
				}},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()
	var recorded []ActionEvent
	restore := SetActionEventRecorder(func(_ context.Context, event ActionEvent) {
		recorded = append(recorded, event)
	})
	defer restore()

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "PHONE_CALL",
		Params: map[string]string{"to": "Grace He"},
	}}, ActionContext{OriginIsOwner: true})
	if len(results) != 1 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if len(recorded) != 1 || recorded[0].Details["to_number"] != "+12123753080" || recorded[0].Details["target_label"] != "Grace He" {
		t.Fatalf("recorded events = %#v", recorded)
	}
}

func TestExecuteAgentActions_PhoneCallOwnerResolvesAddressBookContactForFijiClient(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/directory/entries/search":
			json.NewEncoder(w).Encode(ringcentral.DirectorySearchResult{
				Records: []ringcentral.DirectoryEntry{{
					ID:        "person-grace",
					FirstName: "Grace",
					LastName:  "He",
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/address-book/contact":
			if r.URL.Query().Get("searchString") != "Grace He" {
				t.Fatalf("unexpected contact query: %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(ringcentral.ContactList{
				Records: []ringcentral.Contact{{
					ID:            "contact-grace",
					FirstName:     "Grace",
					LastName:      "He",
					MobilePhone:   "+12123753080",
					BusinessPhone: "+12120000000",
				}},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()
	var recorded []ActionEvent
	restore := SetActionEventRecorder(func(_ context.Context, event ActionEvent) {
		recorded = append(recorded, event)
	})
	defer restore()

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "PHONE_CALL",
		Params: map[string]string{"to": "Grace He"},
	}}, ActionContext{OriginIsOwner: true})
	if len(results) != 1 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if len(recorded) != 1 || recorded[0].Details["to_number"] != "+12123753080" {
		t.Fatalf("recorded events = %#v", recorded)
	}
}

func TestExecuteAgentActions_SMSResolvesAddressBookContactPhoneNumber(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/directory/entries/search":
			json.NewEncoder(w).Encode(ringcentral.DirectorySearchResult{
				Records: []ringcentral.DirectoryEntry{{ID: "person-grace", FirstName: "Grace", LastName: "He"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/address-book/contact":
			if r.URL.Query().Get("searchString") != "Grace He" {
				t.Fatalf("unexpected contact query: %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(ringcentral.ContactList{
				Records: []ringcentral.Contact{{
					ID:          "contact-grace",
					FirstName:   "Grace",
					LastName:    "He",
					MobilePhone: "+12123753080",
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/phone-number":
			json.NewEncoder(w).Encode(ringcentral.ExtensionPhoneNumberList{
				Records: []ringcentral.ExtensionPhoneNumber{{PhoneNumber: "+14155550100", Status: "Normal"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/extension/~/sms":
			var body ringcentral.CreateSMSRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode sms request: %v", err)
			}
			if len(body.To) != 1 || body.To[0].PhoneNumber != "+12123753080" {
				t.Fatalf("expected resolved contact phone number, got %+v", body)
			}
			json.NewEncoder(w).Encode(ringcentral.SMSMessage{ID: "sms-1", MessageStatus: "Sent"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/posts"):
			json.NewEncoder(w).Encode(ringcentral.Post{ID: "post-1"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "SMS",
		Params: map[string]string{"to": "Grace He"},
		Body:   "test",
	}}, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestExecuteAgentActions_ClinicalRefillSMSResolvesPatientMemoryPhone(t *testing.T) {
	memoryDir := t.TempDir()
	t.Setenv("RINGCLAW_MEMORY_DIR", memoryDir)
	entityDir := filepath.Join(memoryDir, "entities")
	if err := os.MkdirAll(entityDir, 0o700); err != nil {
		t.Fatalf("create entity dir: %v", err)
	}
	entity := `# 患者档案 · AX-2847

## 基本信息

患者姓名：Maria Lopez
患者 ID：AX-2847
手机号：+12025462999
`
	if err := os.WriteFile(filepath.Join(entityDir, "AX-2847.md"), []byte(entity), 0o600); err != nil {
		t.Fatalf("write entity memory: %v", err)
	}

	var smsTo string
	var posted string
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/phone-number":
			json.NewEncoder(w).Encode(ringcentral.ExtensionPhoneNumberList{
				Records: []ringcentral.ExtensionPhoneNumber{{PhoneNumber: "+14155550100", Status: "Normal"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/extension/~/sms":
			var body ringcentral.CreateSMSRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode sms request: %v", err)
			}
			if len(body.To) != 1 {
				t.Fatalf("expected one SMS recipient, got %+v", body.To)
			}
			smsTo = body.To[0].PhoneNumber
			json.NewEncoder(w).Encode(ringcentral.SMSMessage{ID: "sms-clinical-1", MessageStatus: "Queued"})
		case r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/chats/c1/posts":
			var body ringcentral.CreatePostRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode post request: %v", err)
			}
			posted = body.Text
			json.NewEncoder(w).Encode(ringcentral.Post{ID: "post-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/directory/entries/search":
			t.Fatalf("clinical patient SMS target should be resolved from memory before directory search")
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "SMS",
		Params: map[string]string{"to": "Maria Lopez"},
		Body:   "Your refill has been approved.",
	}}, ActionContext{
		OriginIsOwner: true,
		OriginalText:  "approve RX-20260606-AX2847",
	})
	if len(results) != 0 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if smsTo != "+12025462999" {
		t.Fatalf("SMS recipient = %q, want memory phone", smsTo)
	}
	if !strings.Contains(posted, "Maria Lopez") || !strings.Contains(posted, "+12025462999") {
		t.Fatalf("expected confirmation to include patient label and resolved phone, got %q", posted)
	}
}

func TestExecuteAgentActions_ClinicalRefillSMSResolvesPatientIDTargetFromMemory(t *testing.T) {
	memoryDir := t.TempDir()
	t.Setenv("RINGCLAW_MEMORY_DIR", memoryDir)
	entityDir := filepath.Join(memoryDir, "entities")
	if err := os.MkdirAll(entityDir, 0o700); err != nil {
		t.Fatalf("create entity dir: %v", err)
	}
	entity := `# 患者档案 · AX-2847

患者姓名：Maria Lopez
患者 ID：AX-2847
手机号：+12025462999
`
	if err := os.WriteFile(filepath.Join(entityDir, "AX-2847.md"), []byte(entity), 0o600); err != nil {
		t.Fatalf("write entity memory: %v", err)
	}

	var smsTo string
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/phone-number":
			json.NewEncoder(w).Encode(ringcentral.ExtensionPhoneNumberList{
				Records: []ringcentral.ExtensionPhoneNumber{{PhoneNumber: "+14155550100", Status: "Normal"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/extension/~/sms":
			var body ringcentral.CreateSMSRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode sms request: %v", err)
			}
			if len(body.To) != 1 {
				t.Fatalf("expected one SMS recipient, got %+v", body.To)
			}
			smsTo = body.To[0].PhoneNumber
			json.NewEncoder(w).Encode(ringcentral.SMSMessage{ID: "sms-clinical-1", MessageStatus: "Queued"})
		case r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/chats/c1/posts":
			json.NewEncoder(w).Encode(ringcentral.Post{ID: "post-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/directory/entries/search":
			t.Fatalf("clinical patient ID SMS target should be resolved from memory before directory search")
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "SMS",
		Params: map[string]string{"to": "AX-2847"},
		Body:   "Your refill has been approved.",
	}}, ActionContext{
		OriginIsOwner: true,
		OriginalText:  "continue refill workflow after provider approval",
	})
	if len(results) != 0 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if smsTo != "+12025462999" {
		t.Fatalf("SMS recipient = %q, want memory phone", smsTo)
	}
}

func TestExecuteAgentActions_LegacyRingOutAliasesToFijiClientCallAndIgnoresFrom(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("legacy RINGOUT should not call RingOut REST API: %s %s", r.Method, r.URL.Path)
	})
	defer srv.Close()
	var recorded []ActionEvent
	restore := SetActionEventRecorder(func(_ context.Context, event ActionEvent) {
		recorded = append(recorded, event)
	})
	defer restore()

	results := ExecuteAgentActions(context.Background(), client, client, "c1", []AgentAction{{
		Type:   "RINGOUT",
		Params: map[string]string{"from": "8102", "to": "+12123753080"},
	}}, ActionContext{OriginIsOwner: true})
	if len(results) != 1 || !strings.Contains(results[0], "FIJI") {
		t.Fatalf("expected FIJI client-call guidance, got %+v", results)
	}
	if len(recorded) != 1 || recorded[0].Type != "RINGOUT" || recorded[0].Status != "client_action_required" || recorded[0].Details["to_number"] != "+12123753080" {
		t.Fatalf("recorded events = %#v", recorded)
	}
}

// TestExecuteAgentActions_CardUsesBotClientInDM locks current-chat behavior:
// CARD actions posted back to the triggering chat use the bot identity so
// the Private App owner does not need to be a member of that chat.
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

	results := ExecuteAgentActions(context.Background(), botClient, privateClient, "dm-chat", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Fatalf("expected no action errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(authHeaders) != 1 {
		t.Fatalf("expected 1 request, got %d", len(authHeaders))
	}
	if authHeaders[0] != "Bearer bot-token" {
		t.Fatalf("expected bot token for card create in bot DM, got %q", authHeaders[0])
	}
}

func TestExecuteAgentActions_CardUsesBotClientInGroup(t *testing.T) {
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

	results := ExecuteAgentActions(context.Background(), botClient, privateClient, "group-chat", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Fatalf("expected no action errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	// No ListChats call — only the card creation request
	if len(authHeaders) != 1 {
		t.Fatalf("expected 1 request, got %d", len(authHeaders))
	}
	if authHeaders[0] != "Bearer bot-token" {
		t.Fatalf("expected bot token for card create in origin group, got %q", authHeaders[0])
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

	results := ExecuteAgentActions(context.Background(), botClient, botClient, "group-chat", actions, ActionContext{OriginIsOwner: true})
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

func TestHandleActionCommand_NoteLock(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/lock") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/note lock n123")
	if !strings.Contains(result, "locked") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_NoteUnlock(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/unlock") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/note unlock n123")
	if !strings.Contains(result, "unlocked") || !strings.Contains(result, "n123") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_NoteLockMissingID(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/note lock")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage text, got: %s", result)
	}
}

func TestHandleActionCommand_EventListGroup(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"records": []map[string]string{{"id": "e1", "title": "Team Standup", "startTime": "2026-04-01T09:00:00Z"}},
		})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/event list chat123")
	if !strings.Contains(result, "e1") || !strings.Contains(result, "Team Standup") {
		t.Errorf("unexpected result: %s", result)
	}
	if !strings.Contains(result, "chat123") {
		t.Errorf("expected group ID in output: %s", result)
	}
}

func TestFormatActionHelp_NoteIncludesLockUnlock(t *testing.T) {
	result := HandleActionCommand(context.Background(), nil, "c1", "/note")
	if !strings.Contains(result, "lock") || !strings.Contains(result, "unlock") {
		t.Errorf("note help should mention lock/unlock: %s", result)
	}
}

func TestFormatActionHelp_EventIncludesChatId(t *testing.T) {
	result := HandleActionCommand(context.Background(), nil, "c1", "/event")
	if !strings.Contains(result, "chatId") {
		t.Errorf("event help should mention chatId: %s", result)
	}
}

func TestParseAgentActions_Message(t *testing.T) {
	reply := "好的，帮你发给 Jason。\n\nACTION:MESSAGE chatid=12345\nhello world\nEND_ACTION"
	clean, actions := ParseAgentActions(reply)
	if !strings.Contains(clean, "帮你发给") {
		t.Errorf("clean reply missing text: %s", clean)
	}
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != "MESSAGE" {
		t.Errorf("expected MESSAGE, got %s", actions[0].Type)
	}
	if actions[0].Params["chatid"] != "12345" {
		t.Errorf("expected chatid=12345, got %s", actions[0].Params["chatid"])
	}
	if strings.TrimSpace(actions[0].Body) != "hello world" {
		t.Errorf("expected body 'hello world', got %q", actions[0].Body)
	}
}

func TestParseAgentActions_MessageHeaderWithChinesePunctuation(t *testing.T) {
	reply := "ACTION:MESSAGE，并附 audit notice：\n![:Person](20894271004) Hi，想和你聊一下最近的培训计划。\nEND_ACTION"
	_, actions := ParseAgentActions(reply)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != "MESSAGE" {
		t.Fatalf("expected MESSAGE, got %q", actions[0].Type)
	}
	if got := strings.TrimSpace(actions[0].Body); got != "![:Person](20894271004) Hi，想和你聊一下最近的培训计划。" {
		t.Fatalf("unexpected body: %q", got)
	}
}

func TestExecuteAgentActions_Message(t *testing.T) {
	var mu sync.Mutex
	var postedBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/posts") && r.Method == "POST" {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			postedBody, _ = body["text"].(string)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "p1"})
	}))
	defer srv.Close()

	// Reply client points at the same server so the pre-notice (sent
	// via the reply client) can succeed — otherwise the fail-closed
	// gate would refuse the action before the target write.
	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actionClient := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	actionClient.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type:   "MESSAGE",
		Params: map[string]string{"chatid": "12345"},
		Body:   "面试晚点到",
	}}

	// Cross-chat MESSAGE now requires a live audit channel
	// (OwnerDMChat) per the Finding-2 fail-closed gate.
	results := ExecuteAgentActions(context.Background(), client, actionClient, "current-chat", actions, ActionContext{
		OriginIsOwner: true,
		OwnerDMChat:   "owner-dm-1",
		RequesterID:   "owner-user",
	})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(postedBody, "面试晚点到") {
		t.Errorf("expected message body in post, got %q", postedBody)
	}
}

func TestExecuteAgentActions_MessageChatIDMentionFallsBackToCurrentChatMention(t *testing.T) {
	var mu sync.Mutex
	var postPath string
	var postedBody string
	var replyClientPosts int
	var actionClientPosts int

	replySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/posts") && r.Method == "POST" {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			replyClientPosts++
			postPath = r.URL.Path
			postedBody, _ = body["text"].(string)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "p1"})
	}))
	defer replySrv.Close()

	actionSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/posts") && r.Method == "POST" {
			mu.Lock()
			actionClientPosts++
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "p-action"})
	}))
	defer actionSrv.Close()

	replyClient := ringcentral.NewBotClient(replySrv.URL, "bot-token")
	replyClient.SetOwnerID("20762282004")
	actionClient := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: actionSrv.URL,
	})
	actionClient.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type:   "MESSAGE",
		Params: map[string]string{"chatid": "Personal alice 3"},
		Body:   "想同步一下最近的培训计划安排。",
	}}

	results := ExecuteAgentActions(context.Background(), replyClient, actionClient, "current-chat", actions, ActionContext{
		OriginIsOwner: true,
		Mentions: []ringcentral.Mention{
			{ID: "20762282004", Type: "Person", Name: "tom bot A"},
			{ID: "20894271004", Type: "Person", Name: "Personal alice 3"},
		},
	})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(postPath, "/chats/current-chat/") {
		t.Errorf("expected post to current chat, got %q", postPath)
	}
	if replyClientPosts != 1 {
		t.Errorf("expected current-chat mention MESSAGE to use reply/bot client, got %d posts", replyClientPosts)
	}
	if actionClientPosts != 0 {
		t.Errorf("expected current-chat mention MESSAGE not to use action/private client, got %d posts", actionClientPosts)
	}
	if !strings.HasPrefix(postedBody, "![:Person](20894271004) ![:Person](20762282004)") {
		t.Errorf("expected target+relay mention prefix, got %q", postedBody)
	}
	if !strings.Contains(postedBody, "培训计划") {
		t.Errorf("expected message body in post, got %q", postedBody)
	}
}

func TestExecuteAgentActions_MessageInOriginChatUsesBotClient(t *testing.T) {
	var mu sync.Mutex
	var replyClientPosts int
	var actionClientPosts int

	replySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/posts") && r.Method == http.MethodPost {
			mu.Lock()
			replyClientPosts++
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "p-reply"})
	}))
	defer replySrv.Close()

	actionSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/posts") && r.Method == http.MethodPost {
			mu.Lock()
			actionClientPosts++
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "p-action"})
	}))
	defer actionSrv.Close()

	replyClient := ringcentral.NewBotClient(replySrv.URL, "bot-token")
	actionClient := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: actionSrv.URL,
	})
	actionClient.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	results := ExecuteAgentActions(context.Background(), replyClient, actionClient, "origin-chat", []AgentAction{{
		Type: "MESSAGE",
		Body: "我会直接处理这条续剂请求。",
	}}, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if replyClientPosts != 1 {
		t.Fatalf("expected origin-chat MESSAGE to use bot/reply client, got %d posts", replyClientPosts)
	}
	if actionClientPosts != 0 {
		t.Fatalf("expected origin-chat MESSAGE not to use private/action client, got %d posts", actionClientPosts)
	}
}

func TestExecuteAgentActions_CardInOriginChatUsesBotClient(t *testing.T) {
	var mu sync.Mutex
	var replyCards int
	var actionCards int

	replySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/adaptive-cards") && r.Method == http.MethodPost {
			mu.Lock()
			replyCards++
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "card-reply"})
	}))
	defer replySrv.Close()

	actionSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/adaptive-cards") && r.Method == http.MethodPost {
			mu.Lock()
			actionCards++
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "card-action"})
	}))
	defer actionSrv.Close()

	replyClient := ringcentral.NewBotClient(replySrv.URL, "bot-token")
	actionClient := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: actionSrv.URL,
	})
	actionClient.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	results := ExecuteAgentActions(context.Background(), replyClient, actionClient, "origin-chat", []AgentAction{{
		Type: "CARD",
		Body: `{"type":"AdaptiveCard","version":"1.3","body":[]}`,
	}}, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if replyCards != 1 {
		t.Fatalf("expected origin-chat CARD to use bot/reply client, got %d cards", replyCards)
	}
	if actionCards != 0 {
		t.Fatalf("expected origin-chat CARD not to use private/action client, got %d cards", actionCards)
	}
}

func TestExecuteAgentActions_MessageRelayCollaboratorPreservedWithoutChatID(t *testing.T) {
	var mu sync.Mutex
	var postPath string
	var postedBody string

	replySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/posts") && r.Method == "POST" {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			postPath = r.URL.Path
			postedBody, _ = body["text"].(string)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "p1"})
	}))
	defer replySrv.Close()

	actionSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "p-action"})
	}))
	defer actionSrv.Close()

	replyClient := ringcentral.NewBotClient(replySrv.URL, "bot-token")
	replyClient.SetOwnerID("20891451004")
	actionClient := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: actionSrv.URL,
	})
	actionClient.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type: "MESSAGE",
		Body: "![:Person](20891451004) 想跟你对一下新员工培训计划的安排，方便同步下时间节点和分工吗？",
	}}

	results := ExecuteAgentActions(context.Background(), replyClient, actionClient, "current-chat", actions, ActionContext{
		OriginIsOwner: true,
		RelayCollaborator: &ringcentral.Mention{
			ID:   "20894271004",
			Type: "Person",
			Name: "Personal alice 3",
		},
	})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(postPath, "/chats/current-chat/") {
		t.Errorf("expected post to current chat, got %q", postPath)
	}
	if !strings.HasPrefix(postedBody, "![:Person](20894271004) ![:Person](20891451004)") {
		t.Fatalf("expected collaborator+self relay prefix, got %q", postedBody)
	}
	if !strings.Contains(postedBody, "培训计划") {
		t.Fatalf("expected message body in post, got %q", postedBody)
	}
}

func TestExecuteAgentActions_MessageToRolePeerAddsMentionAndSharedChat(t *testing.T) {
	var mu sync.Mutex
	var postPath string
	var postedBody string
	replySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/directory/entries/search") && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ringcentral.DirectorySearchResult{Records: []ringcentral.DirectoryEntry{{
				ID:        "nursecoord-person",
				FirstName: "Nursecoord",
				LastName:  "Department",
			}}})
			return
		}
		if strings.Contains(r.URL.Path, "/posts") && r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			postPath = r.URL.Path
			postedBody, _ = body["text"].(string)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "p1"})
	}))
	defer replySrv.Close()

	replyClient := ringcentral.NewBotClient(replySrv.URL, "bot-token")
	replyClient.SetOwnerID("alexis-ext")
	actions := []AgentAction{{
		Type: "MESSAGE",
		Params: map[string]string{
			"to_role_id": "role-nursecoord-bot",
		},
		Body: "Alexis 今日缺勤，请接手续剂队列和跟进项。",
	}}

	results := ExecuteAgentActions(context.Background(), replyClient, nil, "origin-chat", actions, ActionContext{
		OriginIsOwner: true,
		RolePeers: map[string]RolePeer{
			"role-nursecoord-bot": {
				RoleID:        "role-nursecoord-bot",
				DisplayName:   "Nursecoord Department",
				ExtensionID:   "nursecoord-ext",
				SharedChatIDs: []string{"shared-admin-chat"},
			},
		},
	})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(postPath, "/chats/shared-admin-chat/") {
		t.Fatalf("expected post to shared chat, got %q", postPath)
	}
	if !strings.HasPrefix(postedBody, "![:Person](nursecoord-person) ") {
		t.Fatalf("expected nursecoord mention prefix, got %q", postedBody)
	}
	if !strings.Contains(postedBody, "续剂队列") {
		t.Fatalf("expected handoff body in post, got %q", postedBody)
	}
}

func TestExecuteAgentActions_MessageToRolePeerUsesConfiguredPersonID(t *testing.T) {
	var mu sync.Mutex
	var postPath string
	var postedBody string
	replySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/directory/entries/search") ||
			(strings.Contains(r.URL.Path, "/team-messaging/v1/chats/shared-admin-chat") && r.Method == http.MethodGet) {
			t.Fatalf("person_id should avoid lookup, got %s %s", r.Method, r.URL.Path)
		}
		if strings.Contains(r.URL.Path, "/posts") && r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			postPath = r.URL.Path
			postedBody, _ = body["text"].(string)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "p1"})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer replySrv.Close()

	replyClient := ringcentral.NewBotClient(replySrv.URL, "bot-token")
	actions := []AgentAction{{
		Type: "MESSAGE",
		Params: map[string]string{
			"to_role_id": "role-clinical-bot",
		},
		Body: "Please review the clinical handoff.",
	}}

	results := ExecuteAgentActions(context.Background(), replyClient, nil, "origin-chat", actions, ActionContext{
		OriginIsOwner: true,
		RolePeers: map[string]RolePeer{
			"role-clinical-bot": {
				RoleID:        "role-clinical-bot",
				DisplayName:   "clinical-bot",
				ExtensionID:   "20762293004",
				PersonID:      "87368474627",
				SharedChatIDs: []string{"shared-admin-chat"},
			},
		},
	})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(postPath, "/chats/shared-admin-chat/") {
		t.Fatalf("expected post to shared chat, got %q", postPath)
	}
	if !strings.HasPrefix(postedBody, "![:Person](87368474627) ") {
		t.Fatalf("expected configured person_id mention prefix, got %q", postedBody)
	}
}

func TestExecuteAgentActions_NonOwnerForcesOriginChat(t *testing.T) {
	var mu sync.Mutex
	var postPaths []string
	var postedBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/posts") && r.Method == "POST" {
			mu.Lock()
			postPaths = append(postPaths, r.URL.Path)
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			postedBody, _ = body["text"].(string)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "p-non-owner"})
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "bot-token")

	actionClient := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	actionClient.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type:   "MESSAGE",
		Params: map[string]string{"chatid": "99999"}, // attacker-supplied target
		Body:   "exfiltrated note",
	}}

	results := ExecuteAgentActions(context.Background(), client, actionClient, "origin-chat", actions, ActionContext{OriginIsOwner: false})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(postPaths) != 1 {
		t.Fatalf("expected exactly 1 POST, got %v", postPaths)
	}
	if !strings.Contains(postPaths[0], "/chats/origin-chat/") {
		t.Errorf("expected POST to origin chat, got %q", postPaths[0])
	}
	if strings.Contains(postPaths[0], "/chats/99999/") {
		t.Errorf("attacker chatid was honored in path: %q", postPaths[0])
	}
	if !strings.Contains(postedBody, "exfiltrated note") {
		t.Errorf("expected body to be sent, got %q", postedBody)
	}
}

func TestExecuteAgentActions_OwnerHonorsCrossChat(t *testing.T) {
	var mu sync.Mutex
	var postPaths []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/posts") && r.Method == "POST" {
			mu.Lock()
			postPaths = append(postPaths, r.URL.Path)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "p-owner"})
	}))
	defer srv.Close()

	// Reply client and action client both target the same server: the
	// fail-closed cross-chat gate sends the pre-notice through the
	// reply client, so it must be reachable.
	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actionClient := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	actionClient.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type:   "MESSAGE",
		Params: map[string]string{"chatid": "77777"},
		Body:   "owner cross-chat",
	}}

	// Fail-closed cross-chat: the pre-dispatch audit notice must be
	// delivered to OwnerDMChat first, then the action lands on the
	// owner-chosen target chat. Expect exactly 2 POSTs — notice then
	// action — and the order matters.
	results := ExecuteAgentActions(context.Background(), client, actionClient, "origin-chat", actions, ActionContext{
		OriginIsOwner: true,
		OwnerDMChat:   "dm-owner",
		RequesterID:   "owner-user",
	})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(postPaths) != 2 {
		t.Fatalf("expected exactly 2 POSTs (audit notice + action), got %v", postPaths)
	}
	if !strings.Contains(postPaths[0], "/chats/dm-owner/") {
		t.Errorf("expected first POST to owner DM, got %q", postPaths[0])
	}
	if !strings.Contains(postPaths[1], "/chats/77777/") {
		t.Errorf("owner chatid was not honored on second POST: %q", postPaths[1])
	}
}

func TestExecuteAgentActions_UnknownTypeFallback(t *testing.T) {
	var mu sync.Mutex
	var postedBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/posts") && r.Method == "POST" {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			postedBody, _ = body["text"].(string)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "p1"})
	}))
	defer srv.Close()

	replyClient := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	replyClient.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type: "UNKNOWN",
		Body: "some content",
	}}

	results := ExecuteAgentActions(context.Background(), replyClient, replyClient, "chat1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 1 || !strings.Contains(results[0], "Unknown action type") {
		t.Errorf("expected unknown action warning, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(postedBody, "UNKNOWN") || !strings.Contains(postedBody, "some content") {
		t.Errorf("expected unknown action content sent as message, got %q", postedBody)
	}
}

func TestExecuteAgentActions_ClinicalRefillBlocksTaskAndSendsCard(t *testing.T) {
	t.Setenv("BOT_ID", "personal-ava-test")
	var taskCalls int
	var cardCalls int
	var postedCard map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/tasks"):
			taskCalls++
			t.Fatalf("unexpected task creation for initial refill request")
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/adaptive-cards"):
			cardCalls++
			if err := json.NewDecoder(r.Body).Decode(&postedCard); err != nil {
				t.Fatalf("decode card: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "card-1", "type": "AdaptiveCard"})
		default:
			json.NewEncoder(w).Encode(map[string]any{"id": "ok"})
		}
	}))
	defer srv.Close()

	botClient := ringcentral.NewBotClient(srv.URL, "bot-token")
	privateClient := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	privateClient.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{
		{Type: "CARD", Body: `{"type":"AdaptiveCard","version":"1.3","body":[{"type":"TextBlock","text":"Refill Routed"}]}`},
		{Type: "TASK", Params: map[string]string{"subject": "Send refill to pharmacy"}},
		{Type: "SMS", Params: map[string]string{"to": "+17205550102"}, Body: "approved"},
	}
	results := ExecuteAgentActions(context.Background(), botClient, privateClient, "chat-1", actions, ActionContext{
		OriginIsOwner: true,
		RequesterID:   "20762292004",
		OriginalText:  "refill AX-2847 Sertraline 100mg Andrew Wenner",
	})

	if len(results) != 0 {
		t.Fatalf("expected no user-visible action errors, got %v", results)
	}
	if taskCalls != 0 {
		t.Fatalf("expected no task calls, got %d", taskCalls)
	}
	if cardCalls != 1 {
		t.Fatalf("expected only one replacement card call, got %d", cardCalls)
	}
	actionsRaw, ok := postedCard["actions"].([]any)
	if !ok || len(actionsRaw) != 3 {
		t.Fatalf("expected three submit actions in card, got %#v", postedCard["actions"])
	}
	first, ok := actionsRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected first action shape: %#v", actionsRaw[0])
	}
	data, ok := first["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected submit data, got %#v", first["data"])
	}
	if data["bot_id"] != "personal-ava-test" || data["patient_id"] != "AX-2847" || data["action"] != "approve" {
		t.Fatalf("unexpected submit data: %#v", data)
	}
}

func TestShouldForceClinicalRefillApproval_SkipsWhenCardPresent(t *testing.T) {
	actions := []AgentAction{{Type: "CARD", Body: `{
		"type":"AdaptiveCard",
		"version":"1.3",
		"actions":[
			{"type":"Action.Submit","title":"Approve","data":{"action":"approve"}},
			{"type":"Action.Submit","title":"Need follow-up","data":{"action":"followup"}},
			{"type":"Action.Submit","title":"Deny","data":{"action":"deny"}}
		]
	}`}}
	if shouldForceClinicalRefillApproval("refill AX-2847 Sertraline 100mg Andrew Wenner", actions) {
		t.Fatal("expected no fallback when agent already emitted a submit approval card")
	}
}

func TestShouldForceClinicalRefillApproval_ReplacesCardWithoutSubmitActions(t *testing.T) {
	actions := []AgentAction{{Type: "CARD", Body: `{"type":"AdaptiveCard","version":"1.3","body":[]}`}}
	if !shouldForceClinicalRefillApproval("refill AX-2847 Sertraline 100mg Andrew Wenner", actions) {
		t.Fatal("expected fallback when agent card lacks approve/followup/deny submit actions")
	}
}

type meshTaskCreatorActionStub struct {
	requests []MeshRuntimeTaskCreateRequest
}

func (s *meshTaskCreatorActionStub) CreateMeshTask(_ context.Context, req MeshRuntimeTaskCreateRequest) (MeshRuntimeTask, error) {
	s.requests = append(s.requests, req)
	return MeshRuntimeTask{
		ID:       "mesh-task-1",
		Intent:   req.Intent,
		ToRoleID: req.ToRoleID,
		Title:    req.Title,
	}, nil
}

func TestExecuteAgentActions_MeshTaskCreatesDelegatedTask(t *testing.T) {
	creator := &meshTaskCreatorActionStub{}
	actions := []AgentAction{{
		Type: "MESH_TASK",
		Params: map[string]string{
			"to_role_id":       "role-nursecoord-bot",
			"intent":           "coverage.transfer",
			"title":            "Alexis absence coverage",
			"context_summary":  "Alexis is absent today.",
			"context_task_cnt": "7",
		},
		Body: "Transfer Alexis task queue and report completion.",
	}}

	results := ExecuteAgentActions(context.Background(), nil, nil, "origin-chat", actions, ActionContext{
		OriginIsOwner:   true,
		MeshTaskCreator: creator,
	})
	if len(results) != 0 {
		t.Fatalf("results = %#v", results)
	}
	if len(creator.requests) != 1 {
		t.Fatalf("mesh task requests = %#v", creator.requests)
	}
	req := creator.requests[0]
	if req.ToRoleID != "role-nursecoord-bot" || req.Intent != "coverage.transfer" || req.Title != "Alexis absence coverage" {
		t.Fatalf("mesh task request = %#v", req)
	}
	if req.Instructions != "Transfer Alexis task queue and report completion." || req.Context.Summary != "Alexis is absent today." {
		t.Fatalf("mesh task context = %#v", req)
	}
}

func TestExecuteAgentActions_MeshTaskAddsStableSourceContext(t *testing.T) {
	creator := &meshTaskCreatorActionStub{}
	actions := []AgentAction{{
		Type: "MESH_TASK",
		Params: map[string]string{
			"to_role_id":      "role-nursecoord-bot",
			"intent":          "coverage.transfer",
			"title":           "Coverage handoff",
			"context_summary": "Alexis 今日缺勤，需要 nursecoord-bot 接续任务。",
		},
		Body: "请处理 Alexis 今日缺勤交接。",
	}}

	results := ExecuteAgentActions(context.Background(), nil, nil, "origin-chat", actions, ActionContext{
		OriginIsOwner:   true,
		RequesterID:     "alexis-user",
		OriginalText:    "今天身体不舒服，缺勤，帮我处理交接",
		SourcePostID:    "post-123",
		SourceAgentID:   "mesh-agent-alexis",
		MeshTaskCreator: creator,
	})
	if len(results) != 0 {
		t.Fatalf("results = %#v", results)
	}
	if len(creator.requests) != 1 {
		t.Fatalf("mesh task requests = %#v", creator.requests)
	}
	data := creator.requests[0].Context.Data
	want := map[string]string{
		"origin_chat_id":  "origin-chat",
		"source_post_id":  "post-123",
		"source_agent_id": "mesh-agent-alexis",
		"requester_id":    "alexis-user",
		"to_role_id":      "role-nursecoord-bot",
		"intent":          "coverage.transfer",
	}
	for key, value := range want {
		if got, _ := data[key].(string); got != value {
			t.Fatalf("context data[%s] = %#v, want %q; data=%#v", key, data[key], value, data)
		}
	}
}

func TestExecuteAgentActions_MeshTaskNotifiesRolePeerWithMention(t *testing.T) {
	creator := &meshTaskCreatorActionStub{}
	var mu sync.Mutex
	var postPath string
	var postedBody string
	replySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/directory/entries/search") && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ringcentral.DirectorySearchResult{Records: []ringcentral.DirectoryEntry{{
				ID:        "nursecoord-person",
				FirstName: "Nursecoord",
				LastName:  "Department",
			}}})
			return
		}
		if strings.Contains(r.URL.Path, "/posts") && r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			postPath = r.URL.Path
			postedBody, _ = body["text"].(string)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "p1"})
	}))
	defer replySrv.Close()

	replyClient := ringcentral.NewBotClient(replySrv.URL, "bot-token")
	replyClient.SetOwnerID("alexis-ext")
	actions := []AgentAction{{
		Type: "MESH_TASK",
		Params: map[string]string{
			"to_role_id":      "role-nursecoord-bot",
			"intent":          "coverage.transfer",
			"title":           "Coverage handoff",
			"context_summary": "Alexis 今日缺勤，需要 nursecoord-bot 接手续剂队列。",
		},
		Body: "Alexis 今日缺勤，请接手续剂队列和跟进项。",
	}}

	results := ExecuteAgentActions(context.Background(), replyClient, nil, "origin-chat", actions, ActionContext{
		OriginIsOwner:   true,
		MeshTaskCreator: creator,
		RolePeers: map[string]RolePeer{
			"role-nursecoord-bot": {
				RoleID:        "role-nursecoord-bot",
				DisplayName:   "Nursecoord Department",
				ExtensionID:   "nursecoord-ext",
				SharedChatIDs: []string{"shared-admin-chat"},
			},
		},
	})
	if len(results) != 0 {
		t.Fatalf("expected no user-visible internal mesh task result, got %v", results)
	}
	if len(creator.requests) != 1 {
		t.Fatalf("mesh task requests = %#v", creator.requests)
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(postPath, "/chats/shared-admin-chat/") {
		t.Fatalf("expected role peer notification in shared chat, got path %q", postPath)
	}
	if !strings.HasPrefix(postedBody, "![:Person](nursecoord-person) ") {
		t.Fatalf("expected role peer mention prefix, got %q", postedBody)
	}
	if !strings.Contains(postedBody, "续剂队列") {
		t.Fatalf("expected handoff details in notification, got %q", postedBody)
	}
}

func TestExecuteAgentActions_MeshTaskNotifiesRolePeerWithSharedChatMemberMention(t *testing.T) {
	creator := &meshTaskCreatorActionStub{}
	var mu sync.Mutex
	var postPath string
	var postedBody string
	replySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/team-messaging/v1/chats/shared-admin-chat") && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(ringcentral.Chat{
				ID: "shared-admin-chat", Type: "Team",
				Members: []ringcentral.ChatMember{
					{ID: "person-alexis", FirstName: "Alexis", LastName: "Gonzalez"},
					{ID: "person-86468591619", Name: "Nursecoord Department", ExtensionID: "20762295004"},
				},
			})
			return
		case strings.Contains(r.URL.Path, "/directory/entries/search") && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(ringcentral.DirectorySearchResult{})
			return
		case strings.Contains(r.URL.Path, "/posts") && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			postPath = r.URL.Path
			postedBody, _ = body["text"].(string)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "p1"})
			return
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer replySrv.Close()

	replyClient := ringcentral.NewBotClient(replySrv.URL, "bot-token")
	actions := []AgentAction{{
		Type: "MESH_TASK",
		Params: map[string]string{
			"to_role_id":      "role-nursecoord-bot",
			"intent":          "coverage.transfer",
			"title":           "Coverage handoff",
			"context_summary": "Alexis 今日缺勤，需要 nursecoord-bot 接手续剂队列。",
		},
		Body: "Alexis 今日缺勤，请接手续剂队列和跟进项。",
	}}

	results := ExecuteAgentActions(context.Background(), replyClient, nil, "origin-chat", actions, ActionContext{
		OriginIsOwner:   true,
		MeshTaskCreator: creator,
		RolePeers: map[string]RolePeer{
			"role-nursecoord-bot": {
				RoleID:        "role-nursecoord-bot",
				DisplayName:   "nursecoord-bot",
				ExtensionID:   "20762295004",
				SharedChatIDs: []string{"shared-admin-chat"},
			},
		},
	})
	if len(results) != 0 {
		t.Fatalf("expected no user-visible internal mesh task result, got %v", results)
	}
	if len(creator.requests) != 1 {
		t.Fatalf("mesh task requests = %#v", creator.requests)
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(postPath, "/chats/shared-admin-chat/") {
		t.Fatalf("expected role peer notification in shared chat, got path %q", postPath)
	}
	if !strings.HasPrefix(postedBody, "![:Person](person-86468591619) ") {
		t.Fatalf("expected chat member mention prefix, got %q", postedBody)
	}
	if strings.Contains(postedBody, "![:Person](20762295004)") {
		t.Fatalf("extension ID fallback should not be used as mention, got %q", postedBody)
	}
}

func TestExecuteAgentActions_MeshTaskNotifiesRolePeerByDirectChatWhenNoSharedChat(t *testing.T) {
	creator := &meshTaskCreatorActionStub{}
	var mu sync.Mutex
	var conversationMember string
	var postPath string
	var postedBody string
	replySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/directory/entries/search") && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(ringcentral.DirectorySearchResult{Records: []ringcentral.DirectoryEntry{{
				ID:        "nursecoord-person",
				FirstName: "Nursecoord",
				LastName:  "Department",
			}}})
			return
		}
		if strings.Contains(r.URL.Path, "/team-messaging/v1/conversations") && r.Method == http.MethodPost {
			var body ringcentral.CreateChatRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			if len(body.Members) > 0 {
				conversationMember = body.Members[0].ID
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(ringcentral.Chat{ID: "nursecoord-direct-chat", Type: "Direct"})
			return
		}
		if strings.Contains(r.URL.Path, "/posts") && r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			postPath = r.URL.Path
			postedBody, _ = body["text"].(string)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "p1"})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer replySrv.Close()

	replyClient := ringcentral.NewBotClient(replySrv.URL, "bot-token")
	actions := []AgentAction{{
		Type: "MESH_TASK",
		Params: map[string]string{
			"to_role_id":      "role-nursecoord-bot",
			"intent":          "coverage.transfer",
			"title":           "Coverage handoff",
			"context_summary": "Alexis 今日缺勤，需要 nursecoord-bot 接手续剂队列。",
		},
		Body: "Alexis 今日缺勤，请接手续剂队列和跟进项。",
	}}

	results := ExecuteAgentActions(context.Background(), replyClient, nil, "origin-chat", actions, ActionContext{
		OriginIsOwner:   true,
		MeshTaskCreator: creator,
		RolePeers: map[string]RolePeer{
			"role-nursecoord-bot": {
				RoleID:      "role-nursecoord-bot",
				DisplayName: "Nursecoord Department",
				ExtensionID: "nursecoord-ext",
			},
		},
	})
	if len(results) != 0 {
		t.Fatalf("expected no user-visible internal mesh task result, got %v", results)
	}
	if len(creator.requests) != 1 {
		t.Fatalf("mesh task requests = %#v", creator.requests)
	}

	mu.Lock()
	defer mu.Unlock()
	if conversationMember != "nursecoord-person" {
		t.Fatalf("conversation member = %q", conversationMember)
	}
	if !strings.Contains(postPath, "/chats/nursecoord-direct-chat/") {
		t.Fatalf("expected direct chat notification, got path %q", postPath)
	}
	if !strings.HasPrefix(postedBody, "![:Person](nursecoord-person) ") {
		t.Fatalf("expected role peer mention prefix, got %q", postedBody)
	}
}

func TestExecuteAgentActions_MeshTaskNotifyFailureIsAuditOnly(t *testing.T) {
	creator := &meshTaskCreatorActionStub{}
	var events []ActionEvent
	restore := SetActionEventRecorder(func(_ context.Context, event ActionEvent) {
		events = append(events, event)
	})
	defer restore()

	actions := []AgentAction{{
		Type: "MESH_TASK",
		Params: map[string]string{
			"to_role_id": "role-nursecoord-bot",
			"intent":     "coverage.transfer",
		},
		Body: "Alexis 今日缺勤，请接手续剂队列和跟进项。",
	}}
	results := ExecuteAgentActions(context.Background(), ringcentral.NewBotClient("http://127.0.0.1", "bot-token"), nil, "origin-chat", actions, ActionContext{
		OriginIsOwner:   true,
		MeshTaskCreator: creator,
		RolePeers: map[string]RolePeer{
			"role-nursecoord-bot": {
				RoleID:      "role-nursecoord-bot",
				ExtensionID: "nursecoord-ext",
			},
		},
	})
	if len(results) != 0 {
		t.Fatalf("mesh task notification failure should not be user-visible, got %v", results)
	}
	if len(creator.requests) != 1 {
		t.Fatalf("mesh task requests = %#v", creator.requests)
	}
	var sawNotifyFailure bool
	for _, event := range events {
		if event.Type == "MESSAGE" && event.Status == "failed" && event.Details["reason"] == "mesh_task_role_peer_notify_failed" {
			sawNotifyFailure = true
			break
		}
	}
	if !sawNotifyFailure {
		t.Fatalf("expected audit-only notify failure event, got %#v", events)
	}
}

func TestExecuteAgentActions_LegacyAdminHandoffMessageCreatesMeshTask(t *testing.T) {
	creator := &meshTaskCreatorActionStub{}
	actions := []AgentAction{{
		Type: "MESSAGE",
		Params: map[string]string{
			"chatid": "#admin",
		},
		Body: "TASK_HANDOFF_REQUEST\n来源：alexis-bot\n任务摘要：Alexis 今日缺勤，需要 nursecoord-bot 协调覆盖。",
	}}

	results := ExecuteAgentActions(context.Background(), nil, nil, "origin-chat", actions, ActionContext{
		OriginIsOwner:   true,
		OriginalText:    "今天身体不舒服，缺勤，帮我处理交接",
		MeshTaskCreator: creator,
	})

	if len(results) != 0 {
		t.Fatalf("results = %#v", results)
	}
	if len(creator.requests) != 1 {
		t.Fatalf("mesh task requests = %#v", creator.requests)
	}
	req := creator.requests[0]
	if req.ToRoleID != "role-nursecoord-bot" || req.Intent != "coverage.transfer" {
		t.Fatalf("mesh task request = %#v", req)
	}
	if !strings.Contains(req.Instructions, "TASK_HANDOFF_REQUEST") || req.Context.Summary != "TASK_HANDOFF_REQUEST" {
		t.Fatalf("mesh task context = %#v", req)
	}
}

func TestExecuteAgentActions_LegacyAdminHandoffNoteCreatesMeshTask(t *testing.T) {
	creator := &meshTaskCreatorActionStub{}
	actionSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/notes") && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(ringcentral.Note{ID: "handoff-note", Title: "Alexis absence handoff"})
			return
		}
		if strings.Contains(r.URL.Path, "/notes/handoff-note/publish") && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer actionSrv.Close()
	actionClient := ringcentral.NewBotClient(actionSrv.URL, "bot-token")
	actions := []AgentAction{{
		Type: "NOTE",
		Params: map[string]string{
			"chatid": "admin",
			"title":  "Alexis absence handoff",
		},
		Body: "今日缺勤交接文档：续剂队列和跟进项需要 nursecoord-bot 协调覆盖。",
	}}

	results := ExecuteAgentActions(context.Background(), actionClient, actionClient, "origin-chat", actions, ActionContext{
		OriginIsOwner:   true,
		OriginalText:    "今天身体不舒服，缺勤，帮我处理交接",
		MeshTaskCreator: creator,
	})

	if len(results) != 0 {
		t.Fatalf("results = %#v", results)
	}
	if len(creator.requests) != 1 {
		t.Fatalf("mesh task requests = %#v", creator.requests)
	}
	req := creator.requests[0]
	if req.ToRoleID != "role-nursecoord-bot" || req.Intent != "coverage.transfer" {
		t.Fatalf("mesh task request = %#v", req)
	}
	if !strings.Contains(req.Instructions, "缺勤交接文档") {
		t.Fatalf("mesh task instructions = %q", req.Instructions)
	}
}

func TestExecuteAgentActions_LegacyAdminHandoffNotePreservesCurrentChatDocument(t *testing.T) {
	creator := &meshTaskCreatorActionStub{}
	var mu sync.Mutex
	var notePath string
	var noteBody ringcentral.CreateNoteRequest
	actionSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/notes") && !strings.Contains(r.URL.Path, "/publish") && r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&noteBody)
			mu.Lock()
			notePath = r.URL.Path
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(ringcentral.Note{ID: "handoff-note", Title: noteBody.Title})
			return
		}
		if strings.Contains(r.URL.Path, "/notes/handoff-note/publish") && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer actionSrv.Close()

	actionClient := ringcentral.NewBotClient(actionSrv.URL, "bot-token")
	actions := []AgentAction{{
		Type: "NOTE",
		Params: map[string]string{
			"chatid": "admin",
			"title":  "Alexis 缺勤交接文档 - 2026-06-06",
		},
		Body: "今日缺勤交接文档：续剂队列和跟进项需要 nursecoord-bot 协调覆盖。",
	}}

	results := ExecuteAgentActions(context.Background(), actionClient, actionClient, "origin-chat", actions, ActionContext{
		OriginIsOwner:   true,
		OriginalText:    "今天身体不舒服，缺勤，帮我处理交接",
		MeshTaskCreator: creator,
	})

	if len(results) != 0 {
		t.Fatalf("results = %#v", results)
	}
	if len(creator.requests) != 1 {
		t.Fatalf("mesh task requests = %#v", creator.requests)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(notePath, "/chats/origin-chat/notes") {
		t.Fatalf("expected handoff note in origin chat, got path %q", notePath)
	}
	if noteBody.Title != "Alexis 缺勤交接文档 - 2026-06-06" || !strings.Contains(noteBody.Body, "续剂队列") {
		t.Fatalf("note body = %#v", noteBody)
	}
}

func TestExecuteAgentActions_CoverageMeshTaskCreatesHandoffNoteWhenMissing(t *testing.T) {
	creator := &meshTaskCreatorActionStub{}
	var mu sync.Mutex
	var notePath string
	var noteBody ringcentral.CreateNoteRequest
	actionSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/notes") && !strings.Contains(r.URL.Path, "/publish") && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&noteBody)
			mu.Lock()
			notePath = r.URL.Path
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(ringcentral.Note{ID: "handoff-note", Title: noteBody.Title})
		case strings.Contains(r.URL.Path, "/notes/handoff-note/publish") && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer actionSrv.Close()

	actionClient := ringcentral.NewBotClient(actionSrv.URL, "bot-token")
	actions := []AgentAction{{
		Type: "MESH_TASK",
		Params: map[string]string{
			"to_role_id":      "role-nursecoord-bot",
			"intent":          "coverage.transfer",
			"title":           "Coverage handoff",
			"context_summary": "Alexis 今日缺勤，需要 nursecoord-bot 接续任务。",
		},
		Body: "Alexis 今日缺勤，请接手续剂队列和跟进项。",
	}}

	results := ExecuteAgentActions(context.Background(), actionClient, actionClient, "origin-chat", actions, ActionContext{
		OriginIsOwner:   true,
		OriginalText:    "今天身体不舒服，缺勤，帮我处理交接",
		MeshTaskCreator: creator,
	})
	if len(results) != 0 {
		t.Fatalf("results = %#v", results)
	}
	if len(creator.requests) != 1 {
		t.Fatalf("mesh task requests = %#v", creator.requests)
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(notePath, "/chats/origin-chat/notes") {
		t.Fatalf("expected handoff note in origin chat, got path %q", notePath)
	}
	if !strings.Contains(noteBody.Title, "缺勤交接文档") || !strings.Contains(noteBody.Body, "续剂队列") {
		t.Fatalf("note body = %#v", noteBody)
	}
}

func TestExecuteAgentActions_LegacyAdminMessageWithoutMeshStillResolvesNormally(t *testing.T) {
	creator := &meshTaskCreatorActionStub{}
	actions := []AgentAction{{
		Type: "MESSAGE",
		Params: map[string]string{
			"chatid": "#admin",
		},
		Body: "Please review this admin announcement.",
	}}

	got := normalizeLegacyMeshDelegationActions(actions, ActionContext{
		OriginIsOwner:   true,
		OriginalText:    "send announcement",
		MeshTaskCreator: creator,
	})

	if len(creator.requests) != 0 {
		t.Fatalf("mesh task requests = %#v", creator.requests)
	}
	if len(got) != 1 || got[0].Type != "MESSAGE" {
		t.Fatalf("expected normal message action to remain unchanged, got %#v", got)
	}
}

func TestBestDirectoryMatch_ExactOverFuzzy(t *testing.T) {
	records := []ringcentral.DirectoryEntry{
		{ID: "1", FirstName: "John", LastName: "Linaza"},
		{ID: "2", FirstName: "John", LastName: "Lin"},
		{ID: "3", FirstName: "Johnny", LastName: "Lin"},
	}
	best := bestDirectoryMatch(records, "John Lin")
	if best == nil {
		t.Fatal("expected a match")
	}
	if best.ID != "2" {
		t.Errorf("expected exact match ID=2 (John Lin), got ID=%s (%s %s)", best.ID, best.FirstName, best.LastName)
	}
}

func TestBestDirectoryMatch_FuzzyShortestWins(t *testing.T) {
	records := []ringcentral.DirectoryEntry{
		{ID: "1", FirstName: "John", LastName: "Linaza Rodriguez"},
		{ID: "2", FirstName: "John", LastName: "Linaza"},
	}
	best := bestDirectoryMatch(records, "John Lin")
	if best == nil {
		t.Fatal("expected a match")
	}
	// "John Linaza" (11 chars) is shorter than "John Linaza Rodriguez" (21 chars), both fuzzy-match
	if best.ID != "2" {
		t.Errorf("expected shorter fuzzy match ID=2 (John Linaza), got ID=%s (%s %s)", best.ID, best.FirstName, best.LastName)
	}
}

func TestBestDirectoryMatch_NoMatch(t *testing.T) {
	records := []ringcentral.DirectoryEntry{
		{ID: "1", FirstName: "Alice", LastName: "Smith"},
	}
	best := bestDirectoryMatch(records, "Bob Jones")
	if best != nil {
		t.Errorf("expected no match, got %s %s", best.FirstName, best.LastName)
	}
}
