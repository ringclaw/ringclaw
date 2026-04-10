package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ringclaw/ringclaw/messaging"
	"github.com/ringclaw/ringclaw/ringcentral"
)

const maxRequestBodyBytes = 1 << 20 // 1MB

// Server provides an HTTP API for sending messages.
type Server struct {
	client        *ringcentral.Client
	defaultChatID string
	addr          string
	token         string
	limiter       *rateLimiter
}

// rateLimiter is a simple token bucket per-IP rate limiter.
type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     int
	window   time.Duration
	calls    int
}

type visitor struct {
	count   int
	resetAt time.Time
}

func newRateLimiter(rate int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		window:   window,
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()

	rl.calls++
	if rl.calls%100 == 0 {
		for k, v := range rl.visitors {
			if now.After(v.resetAt) {
				delete(rl.visitors, k)
			}
		}
	}

	v, ok := rl.visitors[ip]
	if !ok || now.After(v.resetAt) {
		rl.visitors[ip] = &visitor{count: 1, resetAt: now.Add(rl.window)}
		return true
	}
	if v.count >= rl.rate {
		return false
	}
	v.count++
	return true
}

// NewServer creates an API server.
func NewServer(client *ringcentral.Client, addr, defaultChatID, token string) (*Server, error) {
	if addr == "" {
		addr = "127.0.0.1:18011"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid api-addr %q: %w", addr, err)
	}
	if !loopbackHosts[host] {
		return nil, fmt.Errorf("api-addr must bind to loopback (127.0.0.1, ::1, localhost), got %q", host)
	}
	return &Server{
		client:        client,
		defaultChatID: defaultChatID,
		addr:          addr,
		token:         token,
		limiter:       newRateLimiter(60, 1*time.Minute),
	}, nil
}

// SendRequest is the JSON body for POST /api/send.
type SendRequest struct {
	To       string `json:"to"`
	Text     string `json:"text,omitempty"`
	MediaURL string `json:"media_url,omitempty"`
}

// --- Middleware ---

// middleware applies rate limiting, host validation, and token auth.
func (s *Server) middleware(next http.HandlerFunc) http.HandlerFunc {
	return hostGuard(s.authMiddleware(s.rateLimitMiddleware(next)))
}

func (s *Server) rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.limiter.allow(r.RemoteAddr) {
			s.jsonError(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// --- JSON helpers ---

func (s *Server) jsonReply(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// decodeJSON reads and decodes a JSON request body with size limiting.
func decodeJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// extractID gets the last path segment: /api/tasks/123 -> 123
func extractID(path, prefix string) string {
	return strings.TrimPrefix(path, prefix)
}

// resolveChatID returns the chat ID from the request, falling back to the server default.
func (s *Server) resolveChatID(chatID string) (string, bool) {
	if chatID == "" {
		chatID = s.defaultChatID
	}
	return chatID, chatID != ""
}

// --- Server lifecycle ---

// Run starts the HTTP server. Blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/send", s.middleware(s.handleSend))

	// Task endpoints
	mux.HandleFunc("/api/tasks", s.middleware(s.handleTasks))
	mux.HandleFunc("/api/tasks/", s.middleware(s.handleTaskByID))

	// Note endpoints
	mux.HandleFunc("/api/notes", s.middleware(s.handleNotes))
	mux.HandleFunc("/api/notes/", s.middleware(s.handleNoteByID))

	// Event endpoints
	mux.HandleFunc("/api/events", s.middleware(s.handleEvents))
	mux.HandleFunc("/api/events/", s.middleware(s.handleEventByID))

	// Adaptive Card endpoints
	mux.HandleFunc("/api/cards", s.middleware(s.handleCards))
	mux.HandleFunc("/api/cards/", s.middleware(s.handleCardByID))

	// /health is exempt from auth and rate limiting but still checks host
	mux.HandleFunc("/health", hostGuard(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	}))

	srv := &http.Server{Addr: s.addr, Handler: mux}

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	slog.Info("listening", "component", "api", "addr", s.addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// --- Send handler ---

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req SendRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	chatID := req.To
	if chatID == "" {
		chatID = s.defaultChatID
	}
	if chatID == "" {
		http.Error(w, `"to" is required (no default chat configured)`, http.StatusBadRequest)
		return
	}
	if req.Text == "" && req.MediaURL == "" {
		http.Error(w, `"text" or "media_url" is required`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	if req.Text != "" {
		if err := messaging.SendTextReply(ctx, s.client, chatID, req.Text); err != nil {
			slog.Error("send text failed", "component", "api", "error", err)
			http.Error(w, "send text failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		slog.Info("sent text", "component", "api", "chatID", chatID, "text", req.Text)

		for _, imgURL := range messaging.ExtractImageURLs(req.Text) {
			if err := messaging.SendMediaFromURL(ctx, s.client, chatID, imgURL); err != nil {
				slog.Error("send extracted image failed", "component", "api", "error", err)
			} else {
				slog.Info("sent extracted image", "component", "api", "chatID", chatID, "url", imgURL)
			}
		}
	}

	if req.MediaURL != "" {
		if err := messaging.SendMediaFromURL(ctx, s.client, chatID, req.MediaURL); err != nil {
			slog.Error("send media failed", "component", "api", "error", err)
			http.Error(w, "send media failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		slog.Info("sent media", "component", "api", "chatID", chatID, "mediaURL", req.MediaURL)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// --- Task handlers ---

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		chatID := r.URL.Query().Get("chat_id")
		if id, ok := s.resolveChatID(chatID); !ok {
			s.jsonError(w, "chat_id required", http.StatusBadRequest)
			return
		} else {
			chatID = id
		}
		list, err := s.client.ListTasks(ctx, chatID)
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonReply(w, list)
	case http.MethodPost:
		var req struct {
			ChatID string `json:"chat_id"`
			ringcentral.CreateTaskRequest
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		chatID, _ := s.resolveChatID(req.ChatID)
		task, err := s.client.CreateTask(ctx, chatID, &req.CreateTaskRequest)
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		s.jsonReply(w, task)
	default:
		s.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path := r.URL.Path

	// /api/tasks/{id}/complete
	if strings.HasSuffix(path, "/complete") {
		taskID := extractID(strings.TrimSuffix(path, "/complete"), "/api/tasks/")
		if r.Method != http.MethodPost {
			s.jsonError(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if err := s.client.CompleteTask(ctx, taskID); err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonReply(w, map[string]string{"status": "completed"})
		return
	}

	taskID := extractID(path, "/api/tasks/")
	switch r.Method {
	case http.MethodGet:
		task, err := s.client.GetTask(ctx, taskID)
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonReply(w, task)
	case http.MethodPatch:
		var req ringcentral.UpdateTaskRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		task, err := s.client.UpdateTask(ctx, taskID, &req)
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonReply(w, task)
	case http.MethodDelete:
		if err := s.client.DeleteTask(ctx, taskID); err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		s.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Note handlers ---

func (s *Server) handleNotes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		chatID := r.URL.Query().Get("chat_id")
		if id, ok := s.resolveChatID(chatID); !ok {
			s.jsonError(w, "chat_id required", http.StatusBadRequest)
			return
		} else {
			chatID = id
		}
		list, err := s.client.ListNotes(ctx, chatID)
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonReply(w, list)
	case http.MethodPost:
		var req struct {
			ChatID string `json:"chat_id"`
			ringcentral.CreateNoteRequest
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		chatID, _ := s.resolveChatID(req.ChatID)
		note, err := s.client.CreateNote(ctx, chatID, &req.CreateNoteRequest)
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Auto-publish
		if pubErr := s.client.PublishNote(ctx, note.ID); pubErr != nil {
			slog.Error("auto-publish note failed", "component", "api", "noteID", note.ID, "error", pubErr)
		}
		w.WriteHeader(http.StatusCreated)
		s.jsonReply(w, note)
	default:
		s.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNoteByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	noteID := extractID(r.URL.Path, "/api/notes/")
	switch r.Method {
	case http.MethodGet:
		note, err := s.client.GetNote(ctx, noteID)
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonReply(w, note)
	case http.MethodPatch:
		var req ringcentral.UpdateNoteRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		note, err := s.client.UpdateNote(ctx, noteID, &req)
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonReply(w, note)
	case http.MethodDelete:
		if err := s.client.DeleteNote(ctx, noteID); err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		s.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Event handlers ---

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		list, err := s.client.ListEvents(ctx)
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonReply(w, list)
	case http.MethodPost:
		var req ringcentral.CreateEventRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		event, err := s.client.CreateEvent(ctx, &req)
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		s.jsonReply(w, event)
	default:
		s.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleEventByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	eventID := extractID(r.URL.Path, "/api/events/")
	switch r.Method {
	case http.MethodGet:
		event, err := s.client.GetEvent(ctx, eventID)
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonReply(w, event)
	case http.MethodPut:
		var req ringcentral.UpdateEventRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		event, err := s.client.UpdateEvent(ctx, eventID, &req)
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonReply(w, event)
	case http.MethodDelete:
		if err := s.client.DeleteEvent(ctx, eventID); err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		s.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Adaptive Card handlers ---

func (s *Server) handleCards(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			ChatID string          `json:"chat_id"`
			Card   json.RawMessage `json:"card"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		chatID, ok := s.resolveChatID(req.ChatID)
		if !ok {
			s.jsonError(w, "chat_id required", http.StatusBadRequest)
			return
		}
		card, err := s.client.CreateAdaptiveCard(r.Context(), chatID, req.Card)
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		s.jsonReply(w, card)
	default:
		s.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCardByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cardID := extractID(r.URL.Path, "/api/cards/")
	switch r.Method {
	case http.MethodGet:
		card, err := s.client.GetAdaptiveCard(ctx, cardID)
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonReply(w, card)
	case http.MethodPut:
		var card json.RawMessage
		if !decodeJSON(w, r, &card) {
			return
		}
		updated, err := s.client.UpdateAdaptiveCard(ctx, cardID, card)
		if err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonReply(w, updated)
	case http.MethodDelete:
		if err := s.client.DeleteAdaptiveCard(ctx, cardID); err != nil {
			s.jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		s.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
