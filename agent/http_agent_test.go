package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPAgent_Chat_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "response text"}},
			},
		})
	}))
	defer srv.Close()

	ag := NewHTTPAgent(HTTPAgentConfig{
		Endpoint: srv.URL,
		Model:    "test-model",
	})

	reply, err := ag.Chat(context.Background(), "conv1", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "response text" {
		t.Errorf("expected 'response text', got %q", reply)
	}
}

func TestHTTPAgent_Chat_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	ag := NewHTTPAgent(HTTPAgentConfig{Endpoint: srv.URL})
	_, err := ag.Chat(context.Background(), "conv1", "hello")
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
}

func TestHTTPAgent_Chat_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"choices": []interface{}{}})
	}))
	defer srv.Close()

	ag := NewHTTPAgent(HTTPAgentConfig{Endpoint: srv.URL})
	_, err := ag.Chat(context.Background(), "conv1", "hello")
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestHTTPAgent_HistoryTrimming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "ok"}},
			},
		})
	}))
	defer srv.Close()

	ag := NewHTTPAgent(HTTPAgentConfig{
		Endpoint:   srv.URL,
		MaxHistory: 3,
	})

	// Send enough messages to trigger trimming
	for i := 0; i < 5; i++ {
		_, err := ag.Chat(context.Background(), "conv1", "msg")
		if err != nil {
			t.Fatalf("unexpected error on msg %d: %v", i, err)
		}
	}

	ag.mu.Lock()
	histLen := len(ag.history["conv1"])
	ag.mu.Unlock()

	// maxHistory=3 means 3*2=6 entries max (user+assistant pairs)
	if histLen > 6 {
		t.Errorf("history not trimmed: got %d entries, max should be 6", histLen)
	}
}

func TestHTTPAgent_BuildMessages_WithSystemPrompt(t *testing.T) {
	ag := NewHTTPAgent(HTTPAgentConfig{
		SystemPrompt: "you are a bot",
	})

	ag.mu.Lock()
	msgs := ag.buildMessages("conv1", "hello")
	ag.mu.Unlock()

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Content != "you are a bot" {
		t.Errorf("unexpected system message: %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "hello" {
		t.Errorf("unexpected user message: %+v", msgs[1])
	}
}

func TestHTTPAgent_BuildMessages_WithHistory(t *testing.T) {
	ag := NewHTTPAgent(HTTPAgentConfig{})

	ag.mu.Lock()
	ag.history["conv1"] = []ChatMessage{
		{Role: "user", Content: "prev"},
		{Role: "assistant", Content: "prev reply"},
	}
	msgs := ag.buildMessages("conv1", "new msg")
	ag.mu.Unlock()

	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "prev" {
		t.Errorf("unexpected history[0]: %+v", msgs[0])
	}
	if msgs[2].Role != "user" || msgs[2].Content != "new msg" {
		t.Errorf("unexpected last msg: %+v", msgs[2])
	}
}

// --- NanoClaw format tests ---

func TestHTTPAgent_NanoClaw_Chat_JSONReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		var req struct {
			ConversationID string `json:"conversation_id"`
			GroupJID       string `json:"group_jid"`
			Sender         string `json:"sender"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ConversationID != "conv-1" {
			t.Fatalf("unexpected conversation ID: %q", req.ConversationID)
		}
		if req.GroupJID != "rc-group" {
			t.Fatalf("unexpected group JID: %q", req.GroupJID)
		}
		json.NewEncoder(w).Encode(map[string]string{"reply": "hello from andy"})
	}))
	defer srv.Close()

	ag := NewHTTPAgent(HTTPAgentConfig{
		Name:        "andy",
		Endpoint:    srv.URL,
		APIKey:      "secret",
		Format:      "nanoclaw",
		GroupJID:    "rc-group",
		Sender:      "Andy",
		ContextMode: "group",
	})

	reply, err := ag.Chat(context.Background(), "conv-1", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "hello from andy" {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestHTTPAgent_NanoClaw_Chat_PlainTextReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("plain text reply"))
	}))
	defer srv.Close()

	ag := NewHTTPAgent(HTTPAgentConfig{Endpoint: srv.URL, Format: "nanoclaw"})
	reply, err := ag.Chat(context.Background(), "conv-1", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "plain text reply" {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestHTTPAgent_NanoClaw_SetCwd(t *testing.T) {
	ag := NewHTTPAgent(HTTPAgentConfig{Format: "nanoclaw"})
	ag.SetCwd("/tmp/project")
	if ag.cwd != "/tmp/project" {
		t.Fatalf("unexpected cwd: %q", ag.cwd)
	}
}

func TestHTTPAgent_NanoClaw_ServerManagesHistory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"reply": "ok"})
	}))
	defer srv.Close()

	ag := NewHTTPAgent(HTTPAgentConfig{Endpoint: srv.URL, Format: "nanoclaw"})
	for i := 0; i < 5; i++ {
		if _, err := ag.Chat(context.Background(), "conv-1", "msg"); err != nil {
			t.Fatalf("unexpected error on msg %d: %v", i, err)
		}
	}

	ag.mu.Lock()
	histLen := len(ag.history["conv-1"])
	ag.mu.Unlock()

	if histLen != 0 {
		t.Errorf("nanoclaw format should not store local history, got %d entries", histLen)
	}
}

func TestHTTPAgent_OpenAI_SetCwd_Noop(t *testing.T) {
	ag := NewHTTPAgent(HTTPAgentConfig{})
	ag.cwd = "/original"
	ag.SetCwd("/new/path")
	if ag.cwd != "/original" {
		t.Fatalf("openai format should not change cwd, got %q", ag.cwd)
	}
}

func TestHTTPAgent_NanoClaw_ResetSession_Noop(t *testing.T) {
	ag := NewHTTPAgent(HTTPAgentConfig{Format: "nanoclaw"})
	ag.mu.Lock()
	ag.history["conv-1"] = []ChatMessage{{Role: "user", Content: "test"}}
	ag.mu.Unlock()

	ag.ResetSession(context.Background(), "conv-1")

	ag.mu.Lock()
	histLen := len(ag.history["conv-1"])
	ag.mu.Unlock()

	if histLen != 1 {
		t.Fatalf("nanoclaw reset should not clear local history (server manages it), got %d", histLen)
	}
}

func TestHTTPAgent_Info_WithName(t *testing.T) {
	ag := NewHTTPAgent(HTTPAgentConfig{Name: "andy", Format: "nanoclaw", Model: "test"})
	info := ag.Info()
	if info.Name != "andy" {
		t.Fatalf("expected name 'andy', got %q", info.Name)
	}
	if info.Type != "http" {
		t.Fatalf("expected type 'http', got %q", info.Type)
	}
}
