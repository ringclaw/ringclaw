package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Send handler edge cases ---

func TestHandleSend_NoDefaultChat_NoTo(t *testing.T) {
	s, _ := NewServer(nil, "127.0.0.1:0", "", testAPIToken)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/send", s.handleSend)

	body, _ := json.Marshal(SendRequest{Text: "hello"})
	req := httptest.NewRequest(http.MethodPost, "/api/send", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when no to and no default, got %d", w.Code)
	}
}

func TestHandleSend_WithMediaURL(t *testing.T) {
	var receivedPaths []string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPaths = append(receivedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "post-1"})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/send", s.handleSend)

	body, _ := json.Marshal(SendRequest{To: "chat1", MediaURL: backend.URL + "/image.png"})
	req := httptest.NewRequest(http.MethodPost, "/api/send", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// It tries to download the media and upload; expect 200 or 500 depending on mock
	// The key point is it doesn't return 400
	if w.Code == http.StatusBadRequest {
		t.Errorf("expected request to be processed, got 400: %s", w.Body.String())
	}
}

func TestHandleSend_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/send", s.handleSend)

	body, _ := json.Marshal(SendRequest{To: "chat1", Text: "hello"})
	req := httptest.NewRequest(http.MethodPost, "/api/send", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on backend error, got %d", w.Code)
	}
}

func TestHandleSend_UsesDefaultChat(t *testing.T) {
	var receivedChatID string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract chat ID from URL path
		receivedChatID = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "post-1"})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/send", s.handleSend)

	// No "to" field — should use default chat
	body, _ := json.Marshal(SendRequest{Text: "hello"})
	req := httptest.NewRequest(http.MethodPost, "/api/send", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(receivedChatID, "default-chat") {
		t.Errorf("expected default-chat in path, got %q", receivedChatID)
	}
}

// --- Tasks handler error paths ---

func TestHandleTasks_List_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks", s.handleTasks)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks?chat_id=c1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleTasks_Create_InvalidJSON(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks", s.handleTasks)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewBufferString("not json"))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleTasks_Create_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks", s.handleTasks)

	body, _ := json.Marshal(map[string]string{"subject": "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// --- TaskByID error paths ---

func TestHandleTaskByID_Get_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/", s.handleTaskByID)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/t1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleTaskByID_Patch_InvalidJSON(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/", s.handleTaskByID)

	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/t1", bytes.NewBufferString("{bad"))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleTaskByID_Patch_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/", s.handleTaskByID)

	body, _ := json.Marshal(map[string]string{"subject": "updated"})
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/t1", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleTaskByID_Delete_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/", s.handleTaskByID)

	req := httptest.NewRequest(http.MethodDelete, "/api/tasks/t1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleTaskByID_Complete_InvalidMethod(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/", s.handleTaskByID)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/t1/complete", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleTaskByID_Complete_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/", s.handleTaskByID)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/t1/complete", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// --- Notes handler error paths ---

func TestHandleNotes_List_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes", s.handleNotes)

	req := httptest.NewRequest(http.MethodGet, "/api/notes?chat_id=c1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleNotes_Create_InvalidJSON(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes", s.handleNotes)

	req := httptest.NewRequest(http.MethodPost, "/api/notes", bytes.NewBufferString("{bad"))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleNotes_Create_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes", s.handleNotes)

	body, _ := json.Marshal(map[string]string{"title": "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/notes", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleNotes_InvalidMethod(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes", s.handleNotes)

	req := httptest.NewRequest(http.MethodDelete, "/api/notes", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "method not allowed" {
		t.Errorf("expected method not allowed, got %v", resp)
	}
}

// --- NoteByID error paths ---

func TestHandleNoteByID_Get_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes/", s.handleNoteByID)

	req := httptest.NewRequest(http.MethodGet, "/api/notes/n1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleNoteByID_Patch_InvalidJSON(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes/", s.handleNoteByID)

	req := httptest.NewRequest(http.MethodPatch, "/api/notes/n1", bytes.NewBufferString("{bad"))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleNoteByID_Patch_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes/", s.handleNoteByID)

	body, _ := json.Marshal(map[string]string{"title": "up"})
	req := httptest.NewRequest(http.MethodPatch, "/api/notes/n1", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleNoteByID_Delete_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes/", s.handleNoteByID)

	req := httptest.NewRequest(http.MethodDelete, "/api/notes/n1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleNoteByID_InvalidMethod(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes/", s.handleNoteByID)

	req := httptest.NewRequest(http.MethodPut, "/api/notes/n1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "method not allowed" {
		t.Errorf("expected method not allowed, got %v", resp)
	}
}

// --- Events handler error paths ---

func TestHandleEvents_List_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events", s.handleEvents)

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleEvents_Create_InvalidJSON(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events", s.handleEvents)

	req := httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewBufferString("{bad"))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleEvents_Create_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events", s.handleEvents)

	body, _ := json.Marshal(map[string]string{"title": "event"})
	req := httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleEvents_InvalidMethod(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events", s.handleEvents)

	req := httptest.NewRequest(http.MethodDelete, "/api/events", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "method not allowed" {
		t.Errorf("expected method not allowed, got %v", resp)
	}
}

// --- EventByID error paths ---

func TestHandleEventByID_Get_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events/", s.handleEventByID)

	req := httptest.NewRequest(http.MethodGet, "/api/events/e1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleEventByID_Put_InvalidJSON(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events/", s.handleEventByID)

	req := httptest.NewRequest(http.MethodPut, "/api/events/e1", bytes.NewBufferString("{bad"))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleEventByID_Put_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events/", s.handleEventByID)

	body, _ := json.Marshal(map[string]string{"title": "up"})
	req := httptest.NewRequest(http.MethodPut, "/api/events/e1", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleEventByID_Delete_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events/", s.handleEventByID)

	req := httptest.NewRequest(http.MethodDelete, "/api/events/e1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleEventByID_InvalidMethod(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events/", s.handleEventByID)

	req := httptest.NewRequest(http.MethodPatch, "/api/events/e1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "method not allowed" {
		t.Errorf("expected method not allowed, got %v", resp)
	}
}

// --- CardByID error paths ---

func TestHandleCardByID_Get_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cards/", s.handleCardByID)

	req := httptest.NewRequest(http.MethodGet, "/api/cards/c1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleCardByID_Put_InvalidJSON(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cards/", s.handleCardByID)

	req := httptest.NewRequest(http.MethodPut, "/api/cards/c1", bytes.NewBufferString("{bad"))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleCardByID_Put_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cards/", s.handleCardByID)

	body, _ := json.Marshal(map[string]string{"type": "AdaptiveCard"})
	req := httptest.NewRequest(http.MethodPut, "/api/cards/c1", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleCardByID_Delete_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cards/", s.handleCardByID)

	req := httptest.NewRequest(http.MethodDelete, "/api/cards/c1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleCardByID_InvalidMethod(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cards/", s.handleCardByID)

	req := httptest.NewRequest(http.MethodPatch, "/api/cards/c1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "method not allowed" {
		t.Errorf("expected method not allowed, got %v", resp)
	}
}

func TestHandleCards_Create_InvalidJSON(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cards", s.handleCards)

	req := httptest.NewRequest(http.MethodPost, "/api/cards", bytes.NewBufferString("{bad"))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleCards_Create_BackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cards", s.handleCards)

	body, _ := json.Marshal(map[string]interface{}{
		"chat_id": "c1",
		"card":    map[string]string{"type": "AdaptiveCard"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/cards", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// --- Rate limiter cleanup logic ---

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := newRateLimiter(1000, 1*time.Millisecond)

	// Fill with many different IPs to trigger cleanup at 100 calls
	for i := 0; i < 110; i++ {
		ip := "1.2.3." + string(rune('0'+i%10))
		rl.allow(ip)
	}
	// After window expires, call again to trigger cleanup
	time.Sleep(5 * time.Millisecond)
	rl.allow("cleanup-trigger")

	// Should be cleaned up now
	rl.mu.Lock()
	count := len(rl.visitors)
	rl.mu.Unlock()
	if count > 20 {
		t.Errorf("expected cleanup to reduce visitors, got %d", count)
	}
}

// --- extractID ---

func TestExtractID(t *testing.T) {
	tests := []struct {
		path   string
		prefix string
		want   string
	}{
		{"/api/tasks/123", "/api/tasks/", "123"},
		{"/api/notes/abc", "/api/notes/", "abc"},
		{"/api/events/e-1", "/api/events/", "e-1"},
		{"/api/tasks/", "/api/tasks/", ""},
	}
	for _, tt := range tests {
		got := extractID(tt.path, tt.prefix)
		if got != tt.want {
			t.Errorf("extractID(%q, %q) = %q, want %q", tt.path, tt.prefix, got, tt.want)
		}
	}
}

// --- resolveChatID ---

func TestResolveChatID(t *testing.T) {
	s := &Server{defaultChatID: "default-1"}
	chatID, ok := s.resolveChatID("explicit")
	if chatID != "explicit" || !ok {
		t.Errorf("expected explicit chat ID, got %q, %v", chatID, ok)
	}
	chatID, ok = s.resolveChatID("")
	if chatID != "default-1" || !ok {
		t.Errorf("expected default chat ID, got %q, %v", chatID, ok)
	}
}

func TestResolveChatID_NoDefault(t *testing.T) {
	s := &Server{defaultChatID: ""}
	chatID, ok := s.resolveChatID("")
	if chatID != "" || ok {
		t.Errorf("expected empty and false, got %q, %v", chatID, ok)
	}
}

// --- NewServer validation ---

func TestNewServer_InvalidAddr(t *testing.T) {
	_, err := NewServer(nil, "not-a-host-port", "", "token")
	if err == nil {
		t.Error("expected error for invalid addr")
	}
}

func TestNewServer_DefaultAddr(t *testing.T) {
	s, err := NewServer(nil, "", "", "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.addr != "127.0.0.1:18011" {
		t.Errorf("expected default addr, got %q", s.addr)
	}
}

// --- decodeJSON ---

func TestDecodeJSON_ValidBody(t *testing.T) {
	body := bytes.NewBufferString(`{"text":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/", body)
	w := httptest.NewRecorder()

	var result SendRequest
	ok := decodeJSON(w, req, &result)
	if !ok {
		t.Error("expected success")
	}
	if result.Text != "hello" {
		t.Errorf("expected hello, got %q", result.Text)
	}
}

func TestDecodeJSON_InvalidBody(t *testing.T) {
	body := bytes.NewBufferString("not json")
	req := httptest.NewRequest(http.MethodPost, "/", body)
	w := httptest.NewRecorder()

	var result SendRequest
	ok := decodeJSON(w, req, &result)
	if ok {
		t.Error("expected failure for invalid JSON")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- Health endpoint in full server mux ---

func TestHealthEndpoint_FullMux(t *testing.T) {
	s := newTestServer()

	// Replicate the mux setup from Run
	mux := http.NewServeMux()
	mux.HandleFunc("/health", hostGuard(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
	}))
	mux.HandleFunc("/api/send", s.middleware(s.handleSend))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Host = "127.0.0.1:18011"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ok") {
		t.Errorf("expected 'ok' in body, got %q", w.Body.String())
	}
}

func TestHealthEndpoint_ForbiddenHost(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", hostGuard(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Host = "evil.com"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

// --- Middleware: no auth token configured ---

func TestMiddleware_NoTokenRequired(t *testing.T) {
	s, _ := NewServer(nil, "127.0.0.1:0", "", "")
	handler := s.middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Host = "127.0.0.1:18011"
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when no token required, got %d", w.Code)
	}
}

// --- isLoopbackHost ---

func TestIsLoopbackHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"localhost:9090", true},
		{"[::1]:18011", true},
		{"127.0.0.1", true},
		{"localhost", true},
		{"::1", true},
		{"example.com:8080", false},
		{"192.168.1.1:8080", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isLoopbackHost(tt.host)
		if got != tt.want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

// --- LoadOrCreateToken edge cases ---

func TestLoadOrCreateToken_ExistingToken(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "api_token"), []byte("my-existing-token\n"), 0o600)

	token, err := LoadOrCreateToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "my-existing-token" {
		t.Errorf("expected my-existing-token, got %q", token)
	}
}

func TestLoadOrCreateToken_EmptyTokenFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "api_token"), []byte(""), 0o600)

	token, err := LoadOrCreateToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty generated token")
	}
	if len(token) != 64 {
		t.Errorf("expected 64-char token, got %d chars", len(token))
	}
}

// --- Rate limiter with RemoteAddr without port ---

func TestRateLimitMiddleware_RemoteAddrNoPort(t *testing.T) {
	s := newTestServer()
	s.limiter = newRateLimiter(1, time.Minute)
	handler := s.rateLimitMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// RemoteAddr without port (unusual but possible)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "1.2.3.4"
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- jsonReply / jsonError ---

func TestJsonReply(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	s.jsonReply(w, map[string]string{"key": "val"})

	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected json content type, got %q", w.Header().Get("Content-Type"))
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["key"] != "val" {
		t.Errorf("expected val, got %q", resp["key"])
	}
}

func TestJsonError(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	s.jsonError(w, "test error", http.StatusTeapot)

	if w.Code != http.StatusTeapot {
		t.Errorf("expected 418, got %d", w.Code)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "test error" {
		t.Errorf("expected test error, got %q", resp["error"])
	}
}


