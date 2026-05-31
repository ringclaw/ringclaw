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

// --- Task command tests ---

func TestHandleActionCommand_TaskGet(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "t1", "subject": "Review PR", "status": "InProgress",
			"description": "Review the new API", "dueDate": "2026-04-15",
		})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task get t1")
	if !strings.Contains(result, "t1") || !strings.Contains(result, "Review PR") {
		t.Errorf("unexpected result: %s", result)
	}
	if !strings.Contains(result, "InProgress") {
		t.Errorf("expected status in result: %s", result)
	}
}

func TestHandleActionCommand_TaskGetMissingID(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task get")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

func TestHandleActionCommand_TaskUpdate(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "t1", "subject": "Updated"})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task update t1 subject=Updated")
	if !strings.Contains(result, "updated") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_TaskUpdateMissingArgs(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task update t1")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

func TestHandleActionCommand_TaskDelete(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task delete t1")
	if !strings.Contains(result, "deleted") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_TaskDeleteMissingID(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task delete")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

func TestHandleActionCommand_TaskComplete(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task complete t1")
	if !strings.Contains(result, "completed") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_TaskCompleteMissingID(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task complete")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

func TestHandleActionCommand_TaskCreateMissingSubject(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task create")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

func TestHandleActionCommand_TaskListEmpty(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"records": []interface{}{}})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task list")
	if !strings.Contains(result, "No tasks") {
		t.Errorf("expected 'No tasks', got: %s", result)
	}
}

// --- Note command tests ---

func TestHandleActionCommand_NoteList(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"records": []map[string]string{
				{"id": "n1", "title": "Meeting Notes", "status": "Active", "preview": "discussed stuff", "creationTime": time.Now().Format(time.RFC3339)},
			},
		})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/note list")
	if !strings.Contains(result, "n1") || !strings.Contains(result, "Meeting Notes") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_NoteListEmpty(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"records": []interface{}{}})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/note list")
	if !strings.Contains(result, "No notes") {
		t.Errorf("expected 'No notes', got: %s", result)
	}
}

func TestHandleActionCommand_NoteGet(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id": "n1", "title": "My Note", "status": "Active", "preview": "some text", "creationTime": time.Now().Format(time.RFC3339),
		})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/note get n1")
	if !strings.Contains(result, "n1") || !strings.Contains(result, "My Note") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_NoteGetMissingID(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/note get")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

func TestHandleActionCommand_NoteUpdate(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "n1", "title": "Updated Title"})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/note update n1 title=Updated Title")
	if !strings.Contains(result, "updated") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_NoteUpdateMissingArgs(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/note update n1")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

func TestHandleActionCommand_NoteDelete(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/note delete n1")
	if !strings.Contains(result, "deleted") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_NoteDeleteMissingID(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/note delete")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

func TestHandleActionCommand_NoteCreateMissingContent(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/note create")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

func TestHandleActionCommand_NoteUnlockMissingID(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/note unlock")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

// --- Event command tests ---

func TestHandleActionCommand_EventList(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"records": []map[string]string{
				{"id": "e1", "title": "Standup", "startTime": time.Now().Format(time.RFC3339)},
			},
		})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/event list")
	if !strings.Contains(result, "e1") || !strings.Contains(result, "Standup") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_EventListEmpty(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"records": []interface{}{}})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/event list")
	if !strings.Contains(result, "No events") {
		t.Errorf("expected 'No events', got: %s", result)
	}
}

func TestHandleActionCommand_EventGet(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "e1", "title": "Sprint Review", "startTime": "2026-04-10T10:00:00Z",
			"endTime": "2026-04-10T11:00:00Z", "location": "Room A", "description": "Sprint review", "color": "Blue",
		})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/event get e1")
	if !strings.Contains(result, "Sprint Review") || !strings.Contains(result, "Room A") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_EventGetMissingID(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/event get")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

func TestHandleActionCommand_EventUpdate(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "e1", "title": "Updated Event"})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/event update e1 title=Updated Event")
	if !strings.Contains(result, "updated") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_EventUpdateMissingArgs(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/event update e1")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

func TestHandleActionCommand_EventDelete(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/event delete e1")
	if !strings.Contains(result, "deleted") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_EventDeleteMissingID(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/event delete")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

func TestHandleActionCommand_EventCreateMissingArgs(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/event create Meeting")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

// --- Card command tests ---

func TestHandleActionCommand_CardGet(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "ac1", "type": "AdaptiveCard", "version": "1.3",
			"creationTime": time.Now().Format(time.RFC3339),
			"chatIds":      []string{"c1", "c2"},
		})
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/card get ac1")
	if !strings.Contains(result, "ac1") || !strings.Contains(result, "AdaptiveCard") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_CardGetMissingID(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/card get")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

func TestHandleActionCommand_CardDelete(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/card delete ac1")
	if !strings.Contains(result, "deleted") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleActionCommand_CardDeleteMissingID(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/card delete")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

func TestHandleActionCommand_CardUnknownAction(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/card unknown")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage, got: %s", result)
	}
}

// --- Unknown resource tests ---

func TestHandleActionCommand_UnknownResource(t *testing.T) {
	result := HandleActionCommand(context.Background(), nil, "c1", "/unknown list")
	if !strings.Contains(result, "Unknown") {
		t.Errorf("expected 'Unknown', got: %s", result)
	}
}

// --- API error tests ---

func TestHandleActionCommand_TaskListError(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task list")
	if !strings.Contains(result, "Error") {
		t.Errorf("expected error message, got: %s", result)
	}
}

func TestHandleActionCommand_TaskCreateError(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/task create Some Task")
	if !strings.Contains(result, "Error") {
		t.Errorf("expected error message, got: %s", result)
	}
}

func TestHandleActionCommand_NoteCreateError(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/note create My Note | body")
	if !strings.Contains(result, "Error") {
		t.Errorf("expected error message, got: %s", result)
	}
}

func TestHandleActionCommand_EventCreateError(t *testing.T) {
	client, srv := newTestActionClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	})
	defer srv.Close()

	result := HandleActionCommand(context.Background(), client, "c1", "/event create Meeting 2026-04-10T10:00:00Z 2026-04-10T11:00:00Z")
	if !strings.Contains(result, "Error") {
		t.Errorf("expected error message, got: %s", result)
	}
}

// --- Helper function tests ---

func TestSplitKeyValueParts(t *testing.T) {
	tests := []struct {
		input string
		want  int // number of parts
	}{
		{"subject=hello world", 1},
		{"title=hello description=world", 2},
		{"", 0},
	}
	for _, tt := range tests {
		got := splitKeyValueParts(tt.input)
		if len(got) != tt.want {
			t.Errorf("splitKeyValueParts(%q) returned %d parts, want %d", tt.input, len(got), tt.want)
		}
	}
}

func TestRecentCutoff(t *testing.T) {
	cutoff := recentCutoff()
	if cutoff == "" {
		t.Error("recentCutoff() returned empty string")
	}
	// Should be parseable as RFC3339
	_, err := time.Parse(time.RFC3339, cutoff)
	if err != nil {
		t.Errorf("recentCutoff() not valid RFC3339: %v", err)
	}
}

// --- ExecuteAgentActions additional tests ---

func TestExecuteAgentActions_NoteSuccess(t *testing.T) {
	var createdTitle string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/notes") {
			if strings.HasSuffix(r.URL.Path, "/publish") {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			createdTitle = req["title"]
			json.NewEncoder(w).Encode(map[string]string{"id": "n1", "title": req["title"]})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type:   "NOTE",
		Params: map[string]string{"title": "Sprint Summary"},
		Body:   "Key points from the sprint.",
	}}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Errorf("expected no errors, got %v", results)
	}
	if createdTitle != "Sprint Summary" {
		t.Errorf("expected title 'Sprint Summary', got %q", createdTitle)
	}
}

func TestExecuteAgentActions_NoteDefaultTitle(t *testing.T) {
	var createdTitle string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/notes") {
			if strings.HasSuffix(r.URL.Path, "/publish") {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			createdTitle = req["title"]
			json.NewEncoder(w).Encode(map[string]string{"id": "n1", "title": req["title"]})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type:   "NOTE",
		Params: map[string]string{},
		Body:   "No title provided.",
	}}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Errorf("expected no errors, got %v", results)
	}
	if createdTitle != "Note" {
		t.Errorf("expected default title 'Note', got %q", createdTitle)
	}
}

func TestExecuteAgentActions_TaskSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "t1", "subject": "Follow up"})
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type:   "TASK",
		Params: map[string]string{"subject": "Follow up"},
	}}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Errorf("expected no errors, got %v", results)
	}
}

func TestExecuteAgentActions_TaskSkippedNoSubject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make any request for task with no subject")
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type:   "TASK",
		Params: map[string]string{},
	}}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Errorf("expected no errors for skipped task, got %v", results)
	}
}

func TestExecuteAgentActions_EventSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "e1", "title": "Standup"})
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type: "EVENT",
		Params: map[string]string{
			"title": "Standup",
			"start": "2026-04-10T09:00:00Z",
			"end":   "2026-04-10T09:30:00Z",
		},
	}}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Errorf("expected no errors, got %v", results)
	}
}

func TestExecuteAgentActions_EventNormalizesLocalDateTimeToRFC3339(t *testing.T) {
	var got ringcentral.CreateEventRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode event request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "e1", "title": got.Title})
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type: "EVENT",
		Params: map[string]string{
			"title": "明天12点会议",
			"start": "2026-06-01 12:00",
			"end":   "2026-06-01 13:00",
		},
	}}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}

	wantStart := time.Date(2026, 6, 1, 12, 0, 0, 0, time.Local).Format(time.RFC3339)
	wantEnd := time.Date(2026, 6, 1, 13, 0, 0, 0, time.Local).Format(time.RFC3339)
	if got.StartTime != wantStart || got.EndTime != wantEnd {
		t.Fatalf("event time = %q ~ %q, want %q ~ %q", got.StartTime, got.EndTime, wantStart, wantEnd)
	}
}

func TestExecuteAgentActions_EventSkippedMissingFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make any request for event with missing fields")
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	// Missing end time
	actions := []AgentAction{{
		Type:   "EVENT",
		Params: map[string]string{"title": "Meeting", "start": "2026-04-10T10:00:00Z"},
	}}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Errorf("expected no errors for skipped event, got %v", results)
	}
}

func TestExecuteAgentActions_CardInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make request for invalid card JSON")
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type: "CARD",
		Body: "this is not valid json",
	}}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 1 || !strings.Contains(results[0], "invalid JSON") {
		t.Errorf("expected invalid JSON error, got %v", results)
	}
}

func TestExecuteAgentActions_CardEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make request for empty card body")
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type: "CARD",
		Body: "",
	}}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Errorf("expected no errors for skipped empty card, got %v", results)
	}
}

func TestExecuteAgentActions_MessageEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make request for empty message body")
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type: "MESSAGE",
		Body: "   ",
	}}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Errorf("expected no errors for empty message, got %v", results)
	}
}

// --- parseActionParams tests ---

func TestParseActionParams(t *testing.T) {
	tests := []struct {
		input string
		want  map[string]string
	}{
		{
			"title=Team Meeting start=2026-04-10T10:00:00Z end=2026-04-10T11:00:00Z",
			map[string]string{"title": "Team Meeting", "start": "2026-04-10T10:00:00Z", "end": "2026-04-10T11:00:00Z"},
		},
		{
			"subject=Review PR #6",
			map[string]string{"subject": "Review PR #6"},
		},
		{
			"title=Hello World",
			map[string]string{"title": "Hello World"},
		},
	}

	for _, tt := range tests {
		got := parseActionParams(tt.input)
		gotMap := make(map[string]string)
		for _, kv := range got {
			gotMap[kv.key] = kv.value
		}
		for k, v := range tt.want {
			if gotMap[k] != v {
				t.Errorf("parseActionParams(%q)[%q] = %q, want %q", tt.input, k, gotMap[k], v)
			}
		}
	}
}

// --- selectCardClient tests ---
//
// Empirically (spike against the live RingCentral instance) the Private
// App is allowed to POST adaptive cards into the bot's own DM. We
// therefore prefer the Private App across the board, so the card's
// creator matches the human owner — the same identity NOTE / TASK /
// EVENT actions already use. Bot is only used as a last-resort fallback
// when no Private App is configured.

func TestSelectCardClient_PrefersPrivateAppInBotDM(t *testing.T) {
	botClient := ringcentral.NewBotClient("http://localhost", "bot-token")
	botClient.SetDMChatID("dm-1")
	privateClient := ringcentral.NewBotClient("http://localhost", "private-token")

	selected := selectCardClient(botClient, privateClient, "dm-1")
	if selected != privateClient {
		t.Error("expected private client even for bot DM (Private App may post cards there)")
	}
}

func TestSelectCardClient_PrefersPrivateAppInGroup(t *testing.T) {
	botClient := ringcentral.NewBotClient("http://localhost", "bot-token")
	botClient.SetDMChatID("dm-1")
	privateClient := ringcentral.NewBotClient("http://localhost", "private-token")

	selected := selectCardClient(botClient, privateClient, "group-1")
	if selected != privateClient {
		t.Error("expected private client for non-DM chat")
	}
}

func TestSelectCardClient_FallsBackToBotWhenNoPrivateApp(t *testing.T) {
	botClient := ringcentral.NewBotClient("http://localhost", "bot-token")
	botClient.SetDMChatID("dm-1")

	if got := selectCardClient(botClient, nil, "dm-1"); got != botClient {
		t.Error("expected bot client in DM when action client is nil")
	}
	if got := selectCardClient(botClient, nil, "group-1"); got != botClient {
		t.Error("expected bot client in group when action client is nil")
	}
}

// --- IsSelfPronoun tests ---

func TestIsSelfPronoun(t *testing.T) {
	pronouns := []string{"我", "me", "myself", "私", "自分", "나", "저", "moi", "yo", "ich", "я"}
	for _, p := range pronouns {
		if !isSelfPronoun(p) {
			t.Errorf("expected %q to be a self pronoun", p)
		}
	}

	nonPronouns := []string{"John", "team", "everyone", ""}
	for _, p := range nonPronouns {
		if isSelfPronoun(p) {
			t.Errorf("expected %q to NOT be a self pronoun", p)
		}
	}
}
