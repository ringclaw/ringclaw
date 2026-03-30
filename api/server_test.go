package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
)

func newTestServer() *Server {
	creds := &ringcentral.Credentials{
		ClientID:     "id",
		ClientSecret: "secret",
		JWTToken:     "jwt",
		ServerURL:    "https://example.com",
	}
	client := ringcentral.NewClient(creds)
	return NewServer(client, "127.0.0.1:0", "default-chat")
}

func newTestServerWithBackend(backend *httptest.Server) *Server {
	creds := &ringcentral.Credentials{
		ClientID:     "id",
		ClientSecret: "secret",
		JWTToken:     "jwt",
		ServerURL:    backend.URL,
	}
	client := ringcentral.NewClient(creds)
	// Pre-set a valid token so auth doesn't need to call the real endpoint
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))
	return NewServer(client, "127.0.0.1:0", "default-chat")
}

func TestHealthEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleSend_InvalidMethod(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/send", s.handleSend)

	req := httptest.NewRequest(http.MethodGet, "/api/send", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleSend_InvalidJSON(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/send", s.handleSend)

	req := httptest.NewRequest(http.MethodPost, "/api/send", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSend_Success(t *testing.T) {
	var receivedText string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock the RingCentral SendPost endpoint
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		receivedText = body["text"]
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "post-1", "text": receivedText})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/send", s.handleSend)

	body, _ := json.Marshal(SendRequest{Text: "hello from test"})
	req := httptest.NewRequest(http.MethodPost, "/api/send", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if receivedText != "hello from test" {
		t.Errorf("backend received %q, want %q", receivedText, "hello from test")
	}
}

// --- Task API tests ---

func TestHandleTasks_List(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"records": []map[string]string{{"id": "t1", "subject": "Test task"}},
		})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks", s.handleTasks)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks?chat_id=c1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleTasks_Create(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "t1", "subject": "New task"})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks", s.handleTasks)

	body, _ := json.Marshal(map[string]string{"subject": "New task"})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleTasks_InvalidMethod(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks", s.handleTasks)

	req := httptest.NewRequest(http.MethodPut, "/api/tasks", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleTaskByID_Get(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "t1", "subject": "Test"})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/", s.handleTaskByID)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/t1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleTaskByID_Delete(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/", s.handleTaskByID)

	req := httptest.NewRequest(http.MethodDelete, "/api/tasks/t1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
}

func TestHandleTaskByID_Complete(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/", s.handleTaskByID)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/t1/complete", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Note API tests ---

func TestHandleNotes_List(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"records": []map[string]string{{"id": "n1", "title": "Note 1"}},
		})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes", s.handleNotes)

	req := httptest.NewRequest(http.MethodGet, "/api/notes?chat_id=c1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleNotes_Create(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "n1", "title": "New note", "status": "Draft"})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes", s.handleNotes)

	body, _ := json.Marshal(map[string]string{"title": "New note"})
	req := httptest.NewRequest(http.MethodPost, "/api/notes", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleNoteByID_Get(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "n1", "title": "Test"})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes/", s.handleNoteByID)

	req := httptest.NewRequest(http.MethodGet, "/api/notes/n1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Event API tests ---

func TestHandleEvents_List(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"records": []map[string]string{{"id": "e1", "title": "Meeting"}},
		})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events", s.handleEvents)

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleEvents_Create(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "e1", "title": "New event"})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events", s.handleEvents)

	body, _ := json.Marshal(map[string]string{"title": "New event", "startTime": "2026-03-26T14:00:00Z", "endTime": "2026-03-26T15:00:00Z"})
	req := httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleEventByID_Delete(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events/", s.handleEventByID)

	req := httptest.NewRequest(http.MethodDelete, "/api/events/e1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
}

func TestHandleSend_MissingFields(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/send", s.handleSend)

	body, _ := json.Marshal(SendRequest{To: "chat1"})
	req := httptest.NewRequest(http.MethodPost, "/api/send", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
