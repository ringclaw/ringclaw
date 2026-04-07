package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
)

func newReactionTestClient(handler http.HandlerFunc) (*ringcentral.Client, *httptest.Server) {
	srv := httptest.NewServer(handler)
	creds := &ringcentral.Credentials{
		ClientID:     "id",
		ClientSecret: "secret",
		JWTToken:     "jwt",
		ServerURL:    srv.URL,
	}
	client := ringcentral.NewClient(creds)
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))
	return client, srv
}

func TestStartThinkingReaction_ImmediateStop(t *testing.T) {
	var mu sync.Mutex
	var puts, deletes []string

	client, srv := newReactionTestClient(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		if r.Method == http.MethodPut {
			puts = append(puts, body["code"])
		} else if r.Method == http.MethodDelete {
			deletes = append(deletes, body["code"])
		}
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	stop := StartThinkingReaction(context.Background(), client, "chat1", "post1")
	// Stop immediately with success
	stop(true)

	// Wait a bit for async operations
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should have PUT first emoji, then DELETE it and PUT done emoji
	if len(puts) < 2 {
		t.Fatalf("expected at least 2 PUTs (first emoji + done), got %d: %v", len(puts), puts)
	}
	if puts[0] != thinkingEmojis[0] {
		t.Errorf("first PUT should be %s, got %s", thinkingEmojis[0], puts[0])
	}
	if puts[len(puts)-1] != doneEmoji {
		t.Errorf("last PUT should be done emoji %s, got %s", doneEmoji, puts[len(puts)-1])
	}
	if len(deletes) < 1 || deletes[0] != thinkingEmojis[0] {
		t.Errorf("should DELETE first emoji, got %v", deletes)
	}
}

func TestStartThinkingReaction_StopFalse(t *testing.T) {
	var mu sync.Mutex
	var puts []string

	client, srv := newReactionTestClient(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		if r.Method == http.MethodPut {
			puts = append(puts, body["code"])
		}
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	stop := StartThinkingReaction(context.Background(), client, "chat1", "post1")
	stop(false)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should NOT have done emoji
	for _, p := range puts {
		if p == doneEmoji {
			t.Errorf("should not PUT done emoji on failure stop")
		}
	}
}

func TestStartThinkingReaction_DoubleStop(t *testing.T) {
	client, srv := newReactionTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	stop := StartThinkingReaction(context.Background(), client, "chat1", "post1")
	stop(true)
	stop(true) // should not panic
}
