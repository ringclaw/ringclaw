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

const testAPIToken = "test-api-token-123"

func newTestServer() *Server {
	creds := &ringcentral.Credentials{
		ClientID:     "id",
		ClientSecret: "secret",
		JWTToken:     "jwt",
		ServerURL:    "https://example.com",
	}
	client := ringcentral.NewClient(creds)
	s, err := NewServer(client, "127.0.0.1:0", "default-chat", testAPIToken)
	if err != nil {
		panic(err)
	}
	return s
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
	s, err := NewServer(client, "127.0.0.1:0", "default-chat", testAPIToken)
	if err != nil {
		panic(err)
	}
	return s
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

// --- Middleware chain tests ---

func TestMiddleware_FullChain(t *testing.T) {
	s := newTestServer()
	handler := s.middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Valid request with token and loopback host
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-RingClaw-Token", testAPIToken)
	req.Host = "127.0.0.1:18011"
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMiddleware_RateLimited(t *testing.T) {
	s, _ := NewServer(nil, "127.0.0.1:0", "", testAPIToken)
	s.limiter = newRateLimiter(1, time.Minute)
	handler := s.middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	makeReq := func() int {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-RingClaw-Token", testAPIToken)
		req.Host = "127.0.0.1:18011"
		req.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		handler(w, req)
		return w.Code
	}

	if code := makeReq(); code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", code)
	}
	if code := makeReq(); code != http.StatusTooManyRequests {
		t.Errorf("second request: expected 429, got %d", code)
	}
}

func TestRateLimitMiddleware_ExtractsIP(t *testing.T) {
	s := newTestServer()
	s.limiter = newRateLimiter(1, time.Minute)
	handler := s.rateLimitMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Same IP, different ports — should share rate limit bucket
	for _, port := range []string{"11111", "22222"} {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "1.2.3.4:" + port
		w := httptest.NewRecorder()
		handler(w, req)
	}
	// Third from same IP should be blocked (rate=1)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "1.2.3.4:33333"
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 for rate-limited IP, got %d", w.Code)
	}
}

// --- Rate limiter tests ---

func TestRateLimiter_AllowAndDeny(t *testing.T) {
	rl := newRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !rl.allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if rl.allow("1.2.3.4") {
		t.Error("4th request should be denied")
	}
	// Different IP should still be allowed
	if !rl.allow("5.6.7.8") {
		t.Error("different IP should be allowed")
	}
}

func TestRateLimiter_WindowReset(t *testing.T) {
	rl := newRateLimiter(1, 1*time.Millisecond)
	if !rl.allow("1.2.3.4") {
		t.Fatal("first request should be allowed")
	}
	if rl.allow("1.2.3.4") {
		t.Fatal("second request should be denied")
	}
	time.Sleep(5 * time.Millisecond)
	if !rl.allow("1.2.3.4") {
		t.Error("request after window reset should be allowed")
	}
}

// --- Task API: Patch ---

func TestHandleTaskByID_Patch(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "t1", "subject": "Updated"})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/", s.handleTaskByID)

	body, _ := json.Marshal(map[string]string{"subject": "Updated"})
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/t1", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleTaskByID_InvalidMethod(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/", s.handleTaskByID)

	req := httptest.NewRequest(http.MethodPut, "/api/tasks/t1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		// jsonError writes 405 via response body
		var resp map[string]string
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] != "method not allowed" {
			t.Errorf("expected method not allowed error, got %v", resp)
		}
	}
}

// --- Note API: Patch, Delete ---

func TestHandleNoteByID_Patch(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "n1", "title": "Updated"})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes/", s.handleNoteByID)

	body, _ := json.Marshal(map[string]string{"title": "Updated"})
	req := httptest.NewRequest(http.MethodPatch, "/api/notes/n1", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleNoteByID_Delete(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes/", s.handleNoteByID)

	req := httptest.NewRequest(http.MethodDelete, "/api/notes/n1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
}

// --- Event API: Get, Put ---

func TestHandleEventByID_Get(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "e1", "title": "Meeting"})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events/", s.handleEventByID)

	req := httptest.NewRequest(http.MethodGet, "/api/events/e1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleEventByID_Put(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "e1", "title": "Updated"})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events/", s.handleEventByID)

	body, _ := json.Marshal(map[string]string{"title": "Updated"})
	req := httptest.NewRequest(http.MethodPut, "/api/events/e1", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Card API ---

func TestHandleCards_Create(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "card1"})
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

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCards_InvalidMethod(t *testing.T) {
	s := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cards", s.handleCards)

	req := httptest.NewRequest(http.MethodGet, "/api/cards", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "method not allowed" {
		t.Errorf("expected method not allowed, got %v", resp)
	}
}

func TestHandleCards_NoChatID(t *testing.T) {
	s, _ := NewServer(nil, "127.0.0.1:0", "", testAPIToken)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cards", s.handleCards)

	body, _ := json.Marshal(map[string]interface{}{
		"card": map[string]string{"type": "AdaptiveCard"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/cards", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCardByID_Get(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "card1"})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cards/", s.handleCardByID)

	req := httptest.NewRequest(http.MethodGet, "/api/cards/card1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCardByID_Put(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "card1"})
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cards/", s.handleCardByID)

	body, _ := json.Marshal(map[string]string{"type": "AdaptiveCard"})
	req := httptest.NewRequest(http.MethodPut, "/api/cards/card1", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCardByID_Delete(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	s := newTestServerWithBackend(backend)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cards/", s.handleCardByID)

	req := httptest.NewRequest(http.MethodDelete, "/api/cards/card1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
}

// --- Missing chat_id tests ---

func TestHandleTasks_ListNoChatID(t *testing.T) {
	s, _ := NewServer(nil, "127.0.0.1:0", "", testAPIToken)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks", s.handleTasks)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleNotes_ListNoChatID(t *testing.T) {
	s, _ := NewServer(nil, "127.0.0.1:0", "", testAPIToken)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes", s.handleNotes)

	req := httptest.NewRequest(http.MethodGet, "/api/notes", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleTasks_CreateNoChatID(t *testing.T) {
	s, _ := NewServer(nil, "127.0.0.1:0", "", testAPIToken)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks", s.handleTasks)

	body, _ := json.Marshal(map[string]string{"subject": "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleNotes_CreateNoChatID(t *testing.T) {
	s, _ := NewServer(nil, "127.0.0.1:0", "", testAPIToken)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notes", s.handleNotes)

	body, _ := json.Marshal(map[string]string{"title": "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/notes", bytes.NewBuffer(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestNewServer_LoopbackOnly(t *testing.T) {
	creds := &ringcentral.Credentials{
		ClientID:     "id",
		ClientSecret: "secret",
		JWTToken:     "jwt",
		ServerURL:    "https://example.com",
	}
	client := ringcentral.NewClient(creds)

	tests := []struct {
		addr    string
		wantErr bool
	}{
		{"127.0.0.1:18011", false},
		{"localhost:18011", false},
		{"[::1]:18011", false},
		{"127.0.0.1:0", false},
		{"", false}, // defaults to 127.0.0.1:18011
		{"0.0.0.0:18011", true},
		{"192.168.1.1:18011", true},
		{"10.0.0.1:8080", true},
		{"example.com:18011", true},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			_, err := NewServer(client, tt.addr, "chat", "token")
			if tt.wantErr && err == nil {
				t.Errorf("NewServer(%q) should have returned error", tt.addr)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("NewServer(%q) unexpected error: %v", tt.addr, err)
			}
		})
	}
}
