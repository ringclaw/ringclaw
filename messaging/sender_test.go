package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ringclaw/ringclaw/ringcentral"
)

func TestSendTypingPlaceholder_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["text"] != "Thinking..." {
			t.Errorf("expected 'Thinking...', got %q", req["text"])
		}
		_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "placeholder-123"})
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	id, err := SendTypingPlaceholder(context.Background(), client, "chat-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "placeholder-123" {
		t.Errorf("expected 'placeholder-123', got %q", id)
	}
}

func TestSendTypingPlaceholder_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	_, err := SendTypingPlaceholder(context.Background(), client, "chat-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "typing placeholder") {
		t.Errorf("expected 'typing placeholder' in error, got %q", err.Error())
	}
}

func TestUpdatePostText_Success(t *testing.T) {
	var updatedText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		updatedText = req["text"]
		_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1", Text: updatedText})
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	err := UpdatePostText(context.Background(), client, "chat-1", "post-1", "new text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updatedText != "new text" {
		t.Errorf("expected 'new text', got %q", updatedText)
	}
}

func TestUpdatePostText_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	err := UpdatePostText(context.Background(), client, "chat-1", "post-1", "text")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "update post") {
		t.Errorf("expected 'update post' in error, got %q", err.Error())
	}
}

func TestSendTextReply_Success(t *testing.T) {
	var sentText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		sentText = req["text"]
		_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	err := SendTextReply(context.Background(), client, "chat-1", "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sentText != "hello world" {
		t.Errorf("expected 'hello world', got %q", sentText)
	}
}

func TestSendTextReply_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	err := SendTextReply(context.Background(), client, "chat-1", "hello")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "send message") {
		t.Errorf("expected 'send message' in error, got %q", err.Error())
	}
}

func TestLogSendError_NilError(t *testing.T) {
	// Should not panic
	logSendError(nil)
}

func TestLogSendError_NonNilError(t *testing.T) {
	// Should not panic
	logSendError(fmt.Errorf("test error"))
}
