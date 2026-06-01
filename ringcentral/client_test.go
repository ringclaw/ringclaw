package ringcentral

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClientWithServer(handler http.HandlerFunc) (*Client, *httptest.Server) {
	srv := httptest.NewServer(handler)
	auth := &Auth{
		accessToken: "test-token",
		expiresAt:   time.Now().Add(1 * time.Hour),
		httpClient:  &http.Client{},
		serverURL:   srv.URL,
	}
	client := &Client{
		serverURL:      srv.URL,
		videoServerURL: srv.URL,
		auth:           auth,
		httpClient:     &http.Client{},
	}
	return client, srv
}

func TestSendPost_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Post{ID: "p1", Text: "hello"})
	})
	defer srv.Close()

	post, err := client.SendPost(context.Background(), "chat1", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post.ID != "p1" {
		t.Errorf("expected post ID p1, got %s", post.ID)
	}
}

func TestSendPost_HTTPError(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	})
	defer srv.Close()

	_, err := client.SendPost(context.Background(), "chat1", "hello")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestUpdatePost_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Post{ID: "p1", Text: "updated"})
	})
	defer srv.Close()

	post, err := client.UpdatePost(context.Background(), "chat1", "p1", "updated")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post.Text != "updated" {
		t.Errorf("expected text 'updated', got %q", post.Text)
	}
}

func TestUploadFile_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]FileUploadResponse{{ID: "f1", Name: "test.png"}})
	})
	defer srv.Close()

	resp, err := client.UploadFile(context.Background(), "chat1", "test.png", []byte("data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "f1" {
		t.Errorf("expected file ID f1, got %s", resp.ID)
	}
}

func TestListPosts_Pagination(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		rc := r.URL.Query().Get("recordCount")
		if rc != "50" {
			t.Errorf("expected recordCount=50, got %s", rc)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(PostList{Records: []Post{{ID: "p1"}}})
	})
	defer srv.Close()

	list, err := client.ListPosts(context.Background(), "chat1", ListPostsOpts{RecordCount: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 1 {
		t.Errorf("expected 1 record, got %d", len(list.Records))
	}
}

// --- Task CRUD tests ---

func TestCreateTask_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/team-messaging/v1/chats/chat1/tasks" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Task{ID: "t1", Subject: "Buy milk", Status: "Pending"})
	})
	defer srv.Close()

	task, err := client.CreateTask(context.Background(), "chat1", &CreateTaskRequest{Subject: "Buy milk"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.ID != "t1" || task.Subject != "Buy milk" {
		t.Errorf("got task %+v", task)
	}
}

func TestListTasks_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TaskList{Records: []Task{{ID: "t1"}, {ID: "t2"}}})
	})
	defer srv.Close()

	list, err := client.ListTasks(context.Background(), "chat1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(list.Records))
	}
}

func TestGetTask_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/team-messaging/v1/tasks/t1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Task{ID: "t1", Subject: "Test", Status: "Pending"})
	})
	defer srv.Close()

	task, err := client.GetTask(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.ID != "t1" {
		t.Errorf("expected t1, got %s", task.ID)
	}
}

func TestUpdateTask_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Task{ID: "t1", Subject: "Updated"})
	})
	defer srv.Close()

	task, err := client.UpdateTask(context.Background(), "t1", &UpdateTaskRequest{Subject: "Updated"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Subject != "Updated" {
		t.Errorf("expected 'Updated', got %q", task.Subject)
	}
}

func TestDeleteTask_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	err := client.DeleteTask(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompleteTask_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/team-messaging/v1/tasks/t1/complete" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	err := client.CompleteTask(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Note CRUD tests ---

func TestCreateNote_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Note{ID: "n1", Title: "Meeting Notes", Status: "Draft"})
	})
	defer srv.Close()

	note, err := client.CreateNote(context.Background(), "chat1", &CreateNoteRequest{Title: "Meeting Notes"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if note.ID != "n1" {
		t.Errorf("expected n1, got %s", note.ID)
	}
}

func TestListNotes_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(NoteList{Records: []Note{{ID: "n1"}}})
	})
	defer srv.Close()

	list, err := client.ListNotes(context.Background(), "chat1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 1 {
		t.Errorf("expected 1 note, got %d", len(list.Records))
	}
}

func TestGetNote_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Note{ID: "n1", Title: "Test", Status: "Active"})
	})
	defer srv.Close()

	note, err := client.GetNote(context.Background(), "n1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if note.Title != "Test" {
		t.Errorf("expected 'Test', got %q", note.Title)
	}
}

func TestUpdateNote_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Note{ID: "n1", Title: "Updated"})
	})
	defer srv.Close()

	note, err := client.UpdateNote(context.Background(), "n1", &UpdateNoteRequest{Title: "Updated"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if note.Title != "Updated" {
		t.Errorf("expected 'Updated', got %q", note.Title)
	}
}

func TestDeleteNote_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	if err := client.DeleteNote(context.Background(), "n1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPublishNote_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/team-messaging/v1/notes/n1/publish" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	if err := client.PublishNote(context.Background(), "n1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Event CRUD tests ---

func TestCreateEvent_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Event{ID: "e1", Title: "Team Meeting"})
	})
	defer srv.Close()

	event, err := client.CreateEvent(context.Background(), &CreateEventRequest{
		Title:     "Team Meeting",
		StartTime: "2026-03-26T14:00:00Z",
		EndTime:   "2026-03-26T15:00:00Z",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.ID != "e1" {
		t.Errorf("expected e1, got %s", event.ID)
	}
}

func TestListEvents_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(EventList{Records: []Event{{ID: "e1"}, {ID: "e2"}}})
	})
	defer srv.Close()

	list, err := client.ListEvents(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 2 {
		t.Errorf("expected 2 events, got %d", len(list.Records))
	}
}

func TestGetEvent_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Event{ID: "e1", Title: "Standup"})
	})
	defer srv.Close()

	event, err := client.GetEvent(context.Background(), "e1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Title != "Standup" {
		t.Errorf("expected 'Standup', got %q", event.Title)
	}
}

func TestUpdateEvent_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Event{ID: "e1", Title: "Updated Meeting"})
	})
	defer srv.Close()

	event, err := client.UpdateEvent(context.Background(), "e1", &UpdateEventRequest{Title: "Updated Meeting"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Title != "Updated Meeting" {
		t.Errorf("expected 'Updated Meeting', got %q", event.Title)
	}
}

func TestDeleteEvent_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	if err := client.DeleteEvent(context.Background(), "e1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Error cases ---

func TestCRUD_HTTPError(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	})
	defer srv.Close()

	ctx := context.Background()

	if _, err := client.ListTasks(ctx, "c1"); err == nil {
		t.Error("ListTasks: expected error")
	}
	if _, err := client.CreateTask(ctx, "c1", &CreateTaskRequest{Subject: "x"}); err == nil {
		t.Error("CreateTask: expected error")
	}
	if _, err := client.GetTask(ctx, "t1"); err == nil {
		t.Error("GetTask: expected error")
	}
	if _, err := client.UpdateTask(ctx, "t1", &UpdateTaskRequest{Subject: "x"}); err == nil {
		t.Error("UpdateTask: expected error")
	}
	if err := client.DeleteTask(ctx, "t1"); err == nil {
		t.Error("DeleteTask: expected error")
	}
	if err := client.CompleteTask(ctx, "t1"); err == nil {
		t.Error("CompleteTask: expected error")
	}
	if _, err := client.ListNotes(ctx, "c1"); err == nil {
		t.Error("ListNotes: expected error")
	}
	if _, err := client.CreateNote(ctx, "c1", &CreateNoteRequest{Title: "x"}); err == nil {
		t.Error("CreateNote: expected error")
	}
	if _, err := client.GetNote(ctx, "n1"); err == nil {
		t.Error("GetNote: expected error")
	}
	if _, err := client.UpdateNote(ctx, "n1", &UpdateNoteRequest{Title: "x"}); err == nil {
		t.Error("UpdateNote: expected error")
	}
	if err := client.DeleteNote(ctx, "n1"); err == nil {
		t.Error("DeleteNote: expected error")
	}
	if err := client.PublishNote(ctx, "n1"); err == nil {
		t.Error("PublishNote: expected error")
	}
	if _, err := client.ListEvents(ctx); err == nil {
		t.Error("ListEvents: expected error")
	}
	if _, err := client.CreateEvent(ctx, &CreateEventRequest{Title: "x"}); err == nil {
		t.Error("CreateEvent: expected error")
	}
	if _, err := client.GetEvent(ctx, "e1"); err == nil {
		t.Error("GetEvent: expected error")
	}
	if _, err := client.UpdateEvent(ctx, "e1", &UpdateEventRequest{Title: "x"}); err == nil {
		t.Error("UpdateEvent: expected error")
	}
	if err := client.DeleteEvent(ctx, "e1"); err == nil {
		t.Error("DeleteEvent: expected error")
	}
	if _, err := client.CreateAdaptiveCard(ctx, "c1", json.RawMessage(`{}`)); err == nil {
		t.Error("CreateAdaptiveCard: expected error")
	}
	if _, err := client.GetAdaptiveCard(ctx, "ac1"); err == nil {
		t.Error("GetAdaptiveCard: expected error")
	}
	if _, err := client.UpdateAdaptiveCard(ctx, "ac1", json.RawMessage(`{}`)); err == nil {
		t.Error("UpdateAdaptiveCard: expected error")
	}
	if err := client.DeleteAdaptiveCard(ctx, "ac1"); err == nil {
		t.Error("DeleteAdaptiveCard: expected error")
	}
	if _, err := client.GetChat(ctx, "c1"); err == nil {
		t.Error("GetChat: expected error")
	}
	if _, err := client.GetPost(ctx, "c1", "p1"); err == nil {
		t.Error("GetPost: expected error")
	}
	if err := client.LockNote(ctx, "n1"); err == nil {
		t.Error("LockNote: expected error")
	}
	if err := client.UnlockNote(ctx, "n1"); err == nil {
		t.Error("UnlockNote: expected error")
	}
	if _, err := client.GetPresence(ctx, "ext1"); err == nil {
		t.Error("GetPresence: expected error")
	}
	if _, err := client.ListRecentChats(ctx, "", 0); err == nil {
		t.Error("ListRecentChats: expected error")
	}
	if _, err := client.ListGroupEvents(ctx, "g1"); err == nil {
		t.Error("ListGroupEvents: expected error")
	}
}

// --- Adaptive Card CRUD tests ---

func TestCreateAdaptiveCard_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AdaptiveCard{ID: "ac1", Type: "AdaptiveCard", Version: "1.3"})
	})
	defer srv.Close()

	card, err := client.CreateAdaptiveCard(context.Background(), "chat1", json.RawMessage(`{"type":"AdaptiveCard","version":"1.3","body":[]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if card.ID != "ac1" {
		t.Errorf("expected ac1, got %s", card.ID)
	}
}

func TestGetAdaptiveCard_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AdaptiveCard{ID: "ac1", Type: "AdaptiveCard"})
	})
	defer srv.Close()

	card, err := client.GetAdaptiveCard(context.Background(), "ac1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if card.ID != "ac1" {
		t.Errorf("expected ac1, got %s", card.ID)
	}
}

func TestUpdateAdaptiveCard_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AdaptiveCard{ID: "ac1", Type: "AdaptiveCard"})
	})
	defer srv.Close()

	card, err := client.UpdateAdaptiveCard(context.Background(), "ac1", json.RawMessage(`{"type":"AdaptiveCard","version":"1.3","body":[]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if card.ID != "ac1" {
		t.Errorf("expected ac1, got %s", card.ID)
	}
}

func TestDeleteAdaptiveCard_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	if err := client.DeleteAdaptiveCard(context.Background(), "ac1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInferContentType(t *testing.T) {
	tests := []struct {
		fileName string
		want     string
	}{
		{"photo.png", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"photo.gif", "image/gif"},
		{"video.mp4", "video/mp4"},
		{"doc.pdf", "application/pdf"},
	}
	for _, tt := range tests {
		got := inferContentType(tt.fileName)
		if got != tt.want {
			t.Errorf("inferContentType(%q) = %q, want %q", tt.fileName, got, tt.want)
		}
	}
}

func TestGetChat_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Chat{
			ID: "c1", Name: "Dev Team", Type: "Team",
			Members: []ChatMember{{ID: "u1"}, {ID: "u2"}},
		})
	})
	defer srv.Close()

	chat, err := client.GetChat(context.Background(), "c1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chat.Name != "Dev Team" || chat.Type != "Team" || len(chat.Members) != 2 {
		t.Errorf("unexpected chat: %+v", chat)
	}
}

func TestGetPost_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Post{ID: "p1", Text: "hello world", CreatorID: "u1"})
	})
	defer srv.Close()

	post, err := client.GetPost(context.Background(), "c1", "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post.Text != "hello world" {
		t.Errorf("expected 'hello world', got %q", post.Text)
	}
}

func TestLockNote_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	err := client.LockNote(context.Background(), "n1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnlockNote_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	err := client.UnlockNote(context.Background(), "n1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetPresence_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(PresenceInfo{
			UserStatus:      "Available",
			DndStatus:       "TakeAllCalls",
			TelephonyStatus: "NoCall",
		})
	})
	defer srv.Close()

	info, err := client.GetPresence(context.Background(), "12345")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.UserStatus != "Available" {
		t.Errorf("expected Available, got %s", info.UserStatus)
	}
}

func TestListRecentChats_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatList{
			Records: []Chat{{ID: "c1", Name: "Recent Chat"}},
		})
	})
	defer srv.Close()

	list, err := client.ListRecentChats(context.Background(), "Direct", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 1 || list.Records[0].Name != "Recent Chat" {
		t.Errorf("unexpected result: %+v", list)
	}
}

func TestListGroupEvents_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(EventList{
			Records: []Event{{ID: "e1", Title: "Sprint Review"}},
		})
	})
	defer srv.Close()

	list, err := client.ListGroupEvents(context.Background(), "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 1 || list.Records[0].Title != "Sprint Review" {
		t.Errorf("unexpected result: %+v", list)
	}
}

// --- doRequest tests ---

func TestDoRequest_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("expected Accept application/json, got %q", r.Header.Get("Accept"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	})
	defer srv.Close()

	body, err := client.doRequest(context.Background(), http.MethodGet, "/test", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("unexpected body: %s", string(body))
	}
}

func TestDoRequest_HTTPError(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	})
	defer srv.Close()

	_, err := client.doRequest(context.Background(), http.MethodGet, "/test", "", nil)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if got := err.Error(); got != "HTTP 400: bad request" {
		t.Errorf("unexpected error message: %s", got)
	}
}

func TestDoRequest_ContentType(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", ct)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	})
	defer srv.Close()

	_, err := client.doRequest(context.Background(), http.MethodPost, "/test", "application/json", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDoRequest_NoContentType(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if ct != "" {
			t.Errorf("expected no Content-Type, got %q", ct)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	})
	defer srv.Close()

	_, err := client.doRequest(context.Background(), http.MethodGet, "/test", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- DeletePost tests ---

func TestDeletePost_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/team-messaging/v1/chats/chat1/posts/p1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	err := client.DeletePost(context.Background(), "chat1", "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeletePost_HTTPError(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	})
	defer srv.Close()

	err := client.DeletePost(context.Background(), "chat1", "p1")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

// --- UploadFile error tests ---

func TestUploadFile_HTTPError(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	})
	defer srv.Close()

	_, err := client.UploadFile(context.Background(), "chat1", "test.png", []byte("data"))
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}

func TestUploadFile_EmptyResponse(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]FileUploadResponse{})
	})
	defer srv.Close()

	_, err := client.UploadFile(context.Background(), "chat1", "test.png", []byte("data"))
	if err == nil {
		t.Fatal("expected error for empty upload response")
	}
}

// --- DownloadAttachment tests ---

func TestDownloadAttachment_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected auth header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake-image-data"))
	}))
	defer srv.Close()

	auth := &Auth{
		accessToken: "test-token",
		expiresAt:   time.Now().Add(1 * time.Hour),
		httpClient:  &http.Client{},
		serverURL:   srv.URL,
	}
	client := &Client{
		serverURL:  srv.URL,
		auth:       auth,
		httpClient: &http.Client{},
	}

	data, mediaType, err := client.DownloadAttachment(context.Background(), srv.URL+"/download/file1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "fake-image-data" {
		t.Errorf("unexpected data: %s", string(data))
	}
	if mediaType != "image/png" {
		t.Errorf("expected image/png, got %q", mediaType)
	}
}

func TestDownloadAttachment_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	auth := &Auth{
		accessToken: "test-token",
		expiresAt:   time.Now().Add(1 * time.Hour),
		httpClient:  &http.Client{},
		serverURL:   srv.URL,
	}
	client := &Client{
		serverURL:  srv.URL,
		auth:       auth,
		httpClient: &http.Client{},
	}

	_, _, err := client.DownloadAttachment(context.Background(), srv.URL+"/download/file1")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

// --- Client accessor tests ---

func TestClient_SetOwnerID_OwnerID(t *testing.T) {
	client := &Client{}
	if id := client.OwnerID(); id != "" {
		t.Errorf("expected empty OwnerID, got %q", id)
	}
	client.SetOwnerID("user-123")
	if id := client.OwnerID(); id != "user-123" {
		t.Errorf("expected user-123, got %q", id)
	}
}

func TestClient_ServerURL(t *testing.T) {
	client := &Client{serverURL: "https://example.com"}
	if url := client.ServerURL(); url != "https://example.com" {
		t.Errorf("expected https://example.com, got %q", url)
	}
}

func TestClient_IsBot(t *testing.T) {
	client := &Client{}
	if client.IsBot() {
		t.Error("expected IsBot()=false for non-bot client")
	}
	client.isBot = true
	if !client.IsBot() {
		t.Error("expected IsBot()=true for bot client")
	}
}

func TestClient_SetDMChatID_IsBotDM(t *testing.T) {
	client := &Client{isBot: true}
	client.SetDMChatID("dm-123")

	if !client.IsBotDM("dm-123") {
		t.Error("expected IsBotDM(dm-123)=true")
	}
	if client.IsBotDM("other-chat") {
		t.Error("expected IsBotDM(other-chat)=false")
	}

	// Non-bot client should always return false
	nonBot := &Client{dmChatID: "dm-123"}
	if nonBot.IsBotDM("dm-123") {
		t.Error("expected IsBotDM=false for non-bot client")
	}
}

func TestClient_SetMonitor_MarkSentPost(t *testing.T) {
	m := &Monitor{sentPosts: make(map[string]time.Time)}
	client := &Client{}
	client.SetMonitor(m)
	client.markSentPost("p1")
	if !m.IsSentPost("p1") {
		t.Error("expected post p1 to be marked as sent")
	}
}

func TestClient_MarkSentPost_NilMonitor(t *testing.T) {
	client := &Client{}
	// Should not panic
	client.markSentPost("p1")
}

func TestNewClient(t *testing.T) {
	creds := &Credentials{
		ClientID:     "id",
		ClientSecret: "secret",
		JWTToken:     "jwt",
		ServerURL:    "https://custom.example.com",
	}
	client := NewClient(creds)
	if client.ServerURL() != "https://custom.example.com" {
		t.Errorf("expected custom URL, got %q", client.ServerURL())
	}
	if client.IsBot() {
		t.Error("expected IsBot()=false for NewClient")
	}
}

func TestNewClient_DefaultServerURL(t *testing.T) {
	creds := &Credentials{ClientID: "id", ClientSecret: "secret", JWTToken: "jwt"}
	client := NewClient(creds)
	if client.ServerURL() != defaultServerURL {
		t.Errorf("expected %q, got %q", defaultServerURL, client.ServerURL())
	}
}

func TestListPosts_DefaultRecordCount(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		rc := r.URL.Query().Get("recordCount")
		if rc != "250" {
			t.Errorf("expected default recordCount=250, got %s", rc)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(PostList{Records: []Post{{ID: "p1"}}})
	})
	defer srv.Close()

	_, err := client.ListPosts(context.Background(), "chat1", ListPostsOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListPosts_WithPageToken(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		pt := r.URL.Query().Get("pageToken")
		if pt != "abc123" {
			t.Errorf("expected pageToken=abc123, got %s", pt)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(PostList{Records: []Post{{ID: "p2"}}})
	})
	defer srv.Close()

	list, err := client.ListPosts(context.Background(), "chat1", ListPostsOpts{PageToken: "abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 1 || list.Records[0].ID != "p2" {
		t.Errorf("unexpected result: %+v", list)
	}
}

func TestGetPersonInfo_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/team-messaging/v1/persons/user-1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(PersonInfo{ID: "user-1", FirstName: "John", LastName: "Doe"})
	})
	defer srv.Close()

	info, err := client.GetPersonInfo(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.FirstName != "John" || info.LastName != "Doe" {
		t.Errorf("unexpected person info: %+v", info)
	}
}

func TestGetPersonInfo_HTTPError(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	})
	defer srv.Close()

	_, err := client.GetPersonInfo(context.Background(), "user-1")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestListChats_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatList{Records: []Chat{{ID: "c1"}, {ID: "c2"}}})
	})
	defer srv.Close()

	list, err := client.ListChats(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 2 {
		t.Errorf("expected 2 chats, got %d", len(list.Records))
	}
}

func TestListChats_WithTypeFilter(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		ct := r.URL.Query().Get("type")
		if ct != "Team" {
			t.Errorf("expected type=Team, got %s", ct)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatList{Records: []Chat{{ID: "c1", Type: "Team"}}})
	})
	defer srv.Close()

	list, err := client.ListChats(context.Background(), "Team")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 1 {
		t.Errorf("expected 1 chat, got %d", len(list.Records))
	}
}

func TestInferContentType_Fallback(t *testing.T) {
	got := inferContentType("file.unknown_ext_xyz")
	if got != "application/octet-stream" {
		t.Errorf("expected application/octet-stream for unknown ext, got %q", got)
	}
}

func TestSendPost_MarksSentPost(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Post{ID: "p-sent-1", Text: "hello"})
	})
	defer srv.Close()

	m := &Monitor{sentPosts: make(map[string]time.Time)}
	client.SetMonitor(m)

	_, err := client.SendPost(context.Background(), "chat1", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.IsSentPost("p-sent-1") {
		t.Error("expected post p-sent-1 to be marked as sent after SendPost")
	}
}

// --- Invalid JSON response tests (covers json.Unmarshal error paths) ---

func TestSendPost_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not json`))
	})
	defer srv.Close()

	_, err := client.SendPost(context.Background(), "chat1", "hello")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestUpdatePost_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid`))
	})
	defer srv.Close()

	_, err := client.UpdatePost(context.Background(), "chat1", "p1", "text")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestUploadFile_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not json`))
	})
	defer srv.Close()

	_, err := client.UploadFile(context.Background(), "chat1", "test.png", []byte("data"))
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestListPosts_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{broken`))
	})
	defer srv.Close()

	_, err := client.ListPosts(context.Background(), "chat1", ListPostsOpts{})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestGetPersonInfo_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{broken`))
	})
	defer srv.Close()

	_, err := client.GetPersonInfo(context.Background(), "user-1")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestListChats_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{broken`))
	})
	defer srv.Close()

	_, err := client.ListChats(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestGetChat_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not json`))
	})
	defer srv.Close()

	_, err := client.GetChat(context.Background(), "c1")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestGetPost_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not json`))
	})
	defer srv.Close()

	_, err := client.GetPost(context.Background(), "c1", "p1")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestGetPresence_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not json`))
	})
	defer srv.Close()

	_, err := client.GetPresence(context.Background(), "ext-1")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestListRecentChats_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not json`))
	})
	defer srv.Close()

	_, err := client.ListRecentChats(context.Background(), "", 0)
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestListGroupEvents_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not json`))
	})
	defer srv.Close()

	_, err := client.ListGroupEvents(context.Background(), "g1")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

// --- Resource CRUD Invalid JSON tests ---

func TestListTasks_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.ListTasks(context.Background(), "chat1")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestCreateTask_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.CreateTask(context.Background(), "chat1", &CreateTaskRequest{Subject: "x"})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestGetTask_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.GetTask(context.Background(), "t1")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestUpdateTask_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.UpdateTask(context.Background(), "t1", &UpdateTaskRequest{Subject: "x"})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestListNotes_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.ListNotes(context.Background(), "chat1")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestCreateNote_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.CreateNote(context.Background(), "chat1", &CreateNoteRequest{Title: "x"})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestGetNote_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.GetNote(context.Background(), "n1")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestUpdateNote_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.UpdateNote(context.Background(), "n1", &UpdateNoteRequest{Title: "x"})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestListEvents_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.ListEvents(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestCreateEvent_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.CreateEvent(context.Background(), &CreateEventRequest{Title: "x"})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestGetEvent_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.GetEvent(context.Background(), "e1")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestUpdateEvent_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.UpdateEvent(context.Background(), "e1", &UpdateEventRequest{Title: "x"})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestCreateAdaptiveCard_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.CreateAdaptiveCard(context.Background(), "c1", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestGetAdaptiveCard_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.GetAdaptiveCard(context.Background(), "ac1")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestUpdateAdaptiveCard_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.UpdateAdaptiveCard(context.Background(), "ac1", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

// --- MemberIDs test ---

func TestChat_MemberIDs(t *testing.T) {
	chat := Chat{
		ID:      "c1",
		Members: []ChatMember{{ID: "u1"}, {ID: "u2"}, {ID: "u3"}},
	}
	ids := chat.MemberIDs()
	if len(ids) != 3 {
		t.Fatalf("expected 3 member IDs, got %d", len(ids))
	}
	if ids[0] != "u1" || ids[1] != "u2" || ids[2] != "u3" {
		t.Errorf("unexpected member IDs: %v", ids)
	}
}

func TestChat_MemberIDs_Empty(t *testing.T) {
	chat := Chat{ID: "c1"}
	ids := chat.MemberIDs()
	if len(ids) != 0 {
		t.Errorf("expected 0 member IDs, got %d", len(ids))
	}
}

// --- Authenticate test ---

func TestNewAuth_DefaultServerURL(t *testing.T) {
	auth := NewAuth("id", "secret", "jwt", "")
	if auth.serverURL != defaultServerURL {
		t.Errorf("expected %q, got %q", defaultServerURL, auth.serverURL)
	}
}

func TestNewAuth_CustomServerURL(t *testing.T) {
	auth := NewAuth("id", "secret", "jwt", "https://custom.example.com")
	if auth.serverURL != "https://custom.example.com" {
		t.Errorf("expected custom URL, got %q", auth.serverURL)
	}
}

func TestClient_Authenticate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "auth-token",
			ExpiresIn:   3600,
		})
	}))
	defer srv.Close()

	creds := &Credentials{
		ClientID:     "id",
		ClientSecret: "secret",
		JWTToken:     "jwt",
		ServerURL:    srv.URL,
	}
	client := NewClient(creds)
	client.auth.httpClient = srv.Client()

	if err := client.Authenticate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Auth() == nil {
		t.Fatal("expected non-nil auth")
	}
}

// --- DownloadAttachment too large test ---

func TestDownloadAttachment_TooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		// Write more than 5MB
		data := make([]byte, maxImageSize+100)
		w.Write(data)
	}))
	defer srv.Close()

	auth := &Auth{
		accessToken: "test-token",
		expiresAt:   time.Now().Add(1 * time.Hour),
		httpClient:  &http.Client{},
		serverURL:   srv.URL,
	}
	client := &Client{
		serverURL:  srv.URL,
		auth:       auth,
		httpClient: &http.Client{},
	}

	_, _, err := client.DownloadAttachment(context.Background(), srv.URL+"/download/big")
	if err == nil {
		t.Fatal("expected error for file too large")
	}
}

// --- inferContentType additional coverage ---

func TestInferContentType_KnownExtensions(t *testing.T) {
	// The mime package may handle these directly, but test the fallback switch
	tests := []struct {
		fileName string
		wantType string
	}{
		{"file.txt", "text/plain"},
		{"file.html", "text/html"},
		{"data.json", "application/json"},
		{"photo.jpeg", "image/jpeg"},
		{"archive.zip", "application/zip"},
		{"file.xyz_unknown", "application/octet-stream"},
	}
	for _, tt := range tests {
		got := inferContentType(tt.fileName)
		if got == "" {
			t.Errorf("inferContentType(%q) returned empty", tt.fileName)
		}
	}
}

// --- Network error tests ---

func TestDoRequest_NetworkError(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv.Close() // Close immediately to trigger network error

	_, err := client.doRequest(context.Background(), http.MethodGet, "/test", "", nil)
	if err == nil {
		t.Fatal("expected network error for closed server")
	}
}

func TestDoRequest_CancelledContext(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.doRequest(ctx, http.MethodGet, "/test", "", nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestDownloadAttachment_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.Close() // Close immediately

	auth := &Auth{
		accessToken: "test-token",
		expiresAt:   time.Now().Add(1 * time.Hour),
		httpClient:  &http.Client{},
		serverURL:   srv.URL,
	}
	client := &Client{
		serverURL:  srv.URL,
		auth:       auth,
		httpClient: &http.Client{},
	}

	_, _, err := client.DownloadAttachment(context.Background(), srv.URL+"/download/file1")
	if err == nil {
		t.Fatal("expected network error for closed server")
	}
}

func TestSendPost_ErrorPaths(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	})
	defer srv.Close()

	_, err := client.SendPost(context.Background(), "chat1", "hello")
	if err == nil {
		t.Fatal("expected error for HTTP error response")
	}
}

func TestUpdatePost_ErrorPaths(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	})
	defer srv.Close()

	_, err := client.UpdatePost(context.Background(), "chat1", "p1", "text")
	if err == nil {
		t.Fatal("expected error for HTTP error response")
	}
}

func TestListPosts_HTTPError(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	})
	defer srv.Close()

	_, err := client.ListPosts(context.Background(), "chat1", ListPostsOpts{})
	if err == nil {
		t.Fatal("expected error for HTTP error response")
	}
}

func TestListChats_HTTPError(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	})
	defer srv.Close()

	_, err := client.ListChats(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for HTTP error response")
	}
}

func TestSearchDirectory_RequestBody(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["searchString"] != "test-query" {
			t.Errorf("expected searchString=test-query, got %q", body["searchString"])
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DirectorySearchResult{Records: []DirectoryEntry{}})
	})
	defer srv.Close()

	client.SearchDirectory(context.Background(), "test-query")
}

func TestCreateConversation_RequestBody(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		var body CreateChatRequest
		json.NewDecoder(r.Body).Decode(&body)
		if len(body.Members) != 1 || body.Members[0].ID != "user-x" {
			t.Errorf("unexpected members: %+v", body.Members)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Chat{ID: "c1"})
	})
	defer srv.Close()

	client.CreateConversation(context.Background(), []string{"user-x"})
}

func TestListRecentChats_NoParams(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/team-messaging/v1/recent/chats" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if q := r.URL.RawQuery; q != "" {
			t.Errorf("expected no query params, got %q", q)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatList{Records: []Chat{{ID: "c1"}}})
	})
	defer srv.Close()

	list, err := client.ListRecentChats(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 1 {
		t.Errorf("expected 1 chat, got %d", len(list.Records))
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		header   string
		fallback time.Duration
		want     time.Duration
	}{
		{"", 5 * time.Second, 5 * time.Second},
		{"3", 5 * time.Second, 3 * time.Second},
		{"10", 5 * time.Second, 10 * time.Second},
		{"0", 5 * time.Second, 5 * time.Second},
		{"-1", 5 * time.Second, 5 * time.Second},
		{"abc", 5 * time.Second, 5 * time.Second},
	}
	for _, tt := range tests {
		got := parseRetryAfter(tt.header, tt.fallback)
		if got != tt.want {
			t.Errorf("parseRetryAfter(%q, %v) = %v, want %v", tt.header, tt.fallback, got, tt.want)
		}
	}
}

func TestDoRequest_429Retry(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/restapi/oauth/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(TokenResponse{AccessToken: "tok", ExpiresIn: 3600})
			return
		}
		n := callCount.Add(1)
		if n <= 2 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"errorCode":"CMN-301","message":"Request rate exceeded"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	auth := NewAuth("id", "secret", "jwt", srv.URL)
	auth.httpClient = srv.Client()
	client := &Client{serverURL: srv.URL, auth: auth, httpClient: srv.Client()}

	body, err := client.doRequest(context.Background(), http.MethodGet, "/test", "", nil)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if !strings.Contains(string(body), "ok") {
		t.Errorf("unexpected body: %s", body)
	}
	if callCount.Load() != 3 {
		t.Errorf("expected 3 API calls (2 x 429 + 1 success), got %d", callCount.Load())
	}
}

func TestDoRequest_429ExhaustedRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/restapi/oauth/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(TokenResponse{AccessToken: "tok", ExpiresIn: 3600})
			return
		}
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"errorCode":"CMN-301","message":"Request rate exceeded"}`))
	}))
	defer srv.Close()

	auth := NewAuth("id", "secret", "jwt", srv.URL)
	auth.httpClient = srv.Client()
	client := &Client{serverURL: srv.URL, auth: auth, httpClient: srv.Client()}

	_, err := client.doRequest(context.Background(), http.MethodGet, "/test", "", nil)
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("expected 429 in error, got: %v", err)
	}
}
