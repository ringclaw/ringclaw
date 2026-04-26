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

// finalizeReplyServer captures every PATCH/POST/DELETE the helper makes
// against a mock server so tests can assert on the exact RingCentral API
// calls FinalizeReply emits.
type finalizeReplyServer struct {
	patchedTexts   []string
	postedTexts    []string
	deletedPostIDs []string
	failPatch      bool
	failPost       bool
	failDelete     bool
}

func newFinalizeReplyServer(s *finalizeReplyServer) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPatch:
			if s.failPatch {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			s.patchedTexts = append(s.patchedTexts, req["text"])
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
		case r.Method == http.MethodPost:
			if s.failPost {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			s.postedTexts = append(s.postedTexts, req["text"])
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "new-1"})
		case r.Method == http.MethodDelete:
			if s.failDelete {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			parts := strings.Split(r.URL.Path, "/posts/")
			if len(parts) > 1 {
				s.deletedPostIDs = append(s.deletedPostIDs, parts[1])
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
}

func TestFinalizeReply_EmptyReplyDeletesPlaceholder(t *testing.T) {
	mock := &finalizeReplyServer{}
	srv := newFinalizeReplyServer(mock)
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	FinalizeReply(context.Background(), client, "chat-1", "ph-1", "", "test")

	if got, want := len(mock.deletedPostIDs), 1; got != want {
		t.Fatalf("expected 1 DELETE, got %d (deletes=%v patches=%v posts=%v)",
			got, mock.deletedPostIDs, mock.patchedTexts, mock.postedTexts)
	}
	if mock.deletedPostIDs[0] != "ph-1" {
		t.Errorf("expected DELETE on 'ph-1', got %q", mock.deletedPostIDs[0])
	}
	if len(mock.patchedTexts) != 0 {
		t.Errorf("expected no PATCH for empty reply, got %v", mock.patchedTexts)
	}
	if len(mock.postedTexts) != 0 {
		t.Errorf("expected no POST for empty reply, got %v", mock.postedTexts)
	}
}

func TestFinalizeReply_WhitespaceOnlyTreatedAsEmpty(t *testing.T) {
	mock := &finalizeReplyServer{}
	srv := newFinalizeReplyServer(mock)
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	FinalizeReply(context.Background(), client, "chat-1", "ph-1", "   \n\t  ", "test")

	if len(mock.deletedPostIDs) != 1 {
		t.Errorf("expected 1 DELETE for whitespace-only reply, got deletes=%v patches=%v",
			mock.deletedPostIDs, mock.patchedTexts)
	}
}

func TestFinalizeReply_EmptyReplyNoPlaceholderIsNoop(t *testing.T) {
	mock := &finalizeReplyServer{}
	srv := newFinalizeReplyServer(mock)
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	FinalizeReply(context.Background(), client, "chat-1", "", "", "test")

	if len(mock.deletedPostIDs) != 0 || len(mock.patchedTexts) != 0 || len(mock.postedTexts) != 0 {
		t.Errorf("expected no API calls for empty reply + no placeholder, got deletes=%v patches=%v posts=%v",
			mock.deletedPostIDs, mock.patchedTexts, mock.postedTexts)
	}
}

func TestFinalizeReply_PatchesPlaceholderWhenReplyHasContent(t *testing.T) {
	mock := &finalizeReplyServer{}
	srv := newFinalizeReplyServer(mock)
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	FinalizeReply(context.Background(), client, "chat-1", "ph-1", "hello", "test")

	if len(mock.patchedTexts) != 1 || mock.patchedTexts[0] != "hello" {
		t.Errorf("expected one PATCH with 'hello', got %v", mock.patchedTexts)
	}
	if len(mock.postedTexts) != 0 {
		t.Errorf("expected no POST when PATCH succeeds, got %v", mock.postedTexts)
	}
	if len(mock.deletedPostIDs) != 0 {
		t.Errorf("expected no DELETE for non-empty reply, got %v", mock.deletedPostIDs)
	}
}

func TestFinalizeReply_FallsBackToPostWhenPatchFails(t *testing.T) {
	mock := &finalizeReplyServer{failPatch: true}
	srv := newFinalizeReplyServer(mock)
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	FinalizeReply(context.Background(), client, "chat-1", "ph-1", "hello", "test")

	if len(mock.postedTexts) != 1 || mock.postedTexts[0] != "hello" {
		t.Errorf("expected fallback POST with 'hello' after PATCH failure, got %v", mock.postedTexts)
	}
}

func TestFinalizeReply_NoPlaceholderSendsFreshPost(t *testing.T) {
	mock := &finalizeReplyServer{}
	srv := newFinalizeReplyServer(mock)
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	FinalizeReply(context.Background(), client, "chat-1", "", "hello", "test")

	if len(mock.postedTexts) != 1 || mock.postedTexts[0] != "hello" {
		t.Errorf("expected one POST with 'hello', got %v", mock.postedTexts)
	}
	if len(mock.patchedTexts) != 0 {
		t.Errorf("expected no PATCH when no placeholder, got %v", mock.patchedTexts)
	}
}

func TestFinalizeReply_SwallowsPostErrors(t *testing.T) {
	mock := &finalizeReplyServer{failPost: true}
	srv := newFinalizeReplyServer(mock)
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	// Should not panic — error is logged, not returned
	FinalizeReply(context.Background(), client, "chat-1", "", "hello", "test")
}

func TestFinalizeReply_SwallowsDeleteErrors(t *testing.T) {
	mock := &finalizeReplyServer{failDelete: true}
	srv := newFinalizeReplyServer(mock)
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	// Should not panic — error is logged, not returned
	FinalizeReply(context.Background(), client, "chat-1", "ph-1", "", "test")
}
