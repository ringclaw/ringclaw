package ringcentral

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		serverURL:  srv.URL,
		auth:       auth,
		httpClient: &http.Client{},
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
	if _, err := client.ListNotes(ctx, "c1"); err == nil {
		t.Error("ListNotes: expected error")
	}
	if _, err := client.CreateNote(ctx, "c1", &CreateNoteRequest{Title: "x"}); err == nil {
		t.Error("CreateNote: expected error")
	}
	if _, err := client.ListEvents(ctx); err == nil {
		t.Error("ListEvents: expected error")
	}
	if _, err := client.CreateEvent(ctx, &CreateEventRequest{Title: "x"}); err == nil {
		t.Error("CreateEvent: expected error")
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
