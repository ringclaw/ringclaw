package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/messaging/oob"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// newCrossChatServer records the POST /posts destinations (chatID
// extracted from the URL path) and message bodies so tests can assert
// the cross-chat behavior: the action lands in the target chat AND
// the owner DM receives a metadata-only notice.
func newCrossChatServer(t *testing.T) (*httptest.Server, *[]cspost, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var posts []cspost
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/posts") && r.Method == "POST":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			text, _ := body["text"].(string)
			chat := extractChatFromPath(r.URL.Path)
			mu.Lock()
			posts = append(posts, cspost{ChatID: chat, Text: text, Path: r.URL.Path})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "post-1"})
			return
		case strings.Contains(r.URL.Path, "/adaptive-cards"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "card-1", "type": "Adaptive", "version": "1.3"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "x"})
	}))
	t.Cleanup(srv.Close)
	return srv, &posts, &mu
}

type cspost struct {
	ChatID string
	Text   string
	Path   string
}

func extractChatFromPath(path string) string {
	// /team-messaging/v1/chats/{chatID}/posts
	idx := strings.Index(path, "/chats/")
	if idx < 0 {
		return ""
	}
	tail := path[idx+len("/chats/"):]
	end := strings.Index(tail, "/")
	if end < 0 {
		return tail
	}
	return tail[:end]
}

func newCrossChatClients(serverURL string) (replyClient, actionClient *ringcentral.Client) {
	replyClient = ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: serverURL,
	})
	replyClient.Auth().SetTokenForTest("test-token", time.Now().Add(time.Hour))
	actionClient = ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: serverURL,
	})
	actionClient.Auth().SetTokenForTest("test-token", time.Now().Add(time.Hour))
	return replyClient, actionClient
}

// waitForNotice polls the captured posts until a notice is delivered
// to ownerDMChat or the deadline elapses.
func waitForNotice(t *testing.T, posts *[]cspost, mu *sync.Mutex, ownerDMChat string, timeout time.Duration) (cspost, bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mu.Lock()
		snap := append([]cspost(nil), (*posts)...)
		mu.Unlock()
		for _, p := range snap {
			if p.ChatID == ownerDMChat && strings.HasPrefix(strings.TrimSpace(p.Text), "[notice]") {
				return p, true
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cspost{}, false
}

// TestCrossChatMessage_ExecutesAndNotifiesOwner verifies the Phase 2b
// contract for an owner-initiated cross-chat MESSAGE: the action
// lands immediately in the target chat (no gating) AND a metadata-
// only notice is posted to the owner DM.
func TestCrossChatMessage_ExecutesAndNotifiesOwner(t *testing.T) {
	srv, posts, mu := newCrossChatServer(t)
	reply, action := newCrossChatClients(srv.URL)

	actions := []AgentAction{{
		Type:   "MESSAGE",
		Params: map[string]string{"chatid": "77777"},
		Body:   "cross-chat payload",
	}}
	results := ExecuteAgentActions(context.Background(), reply, action, "origin-chat", actions, ActionContext{
		OriginIsOwner: true,
		OOB:           oob.New(oob.Options{}),
		OwnerDMChat:   "dm-1",
		RequesterID:   "user-7",
	})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}

	// The action must have been delivered to 77777 immediately.
	mu.Lock()
	snapEarly := append([]cspost(nil), (*posts)...)
	mu.Unlock()
	var targetHit bool
	for _, p := range snapEarly {
		if p.ChatID == "77777" && strings.Contains(p.Text, "cross-chat payload") {
			targetHit = true
		}
	}
	if !targetHit {
		t.Fatalf("expected cross-chat MESSAGE to be delivered to target chat, got %v", snapEarly)
	}

	notice, ok := waitForNotice(t, posts, mu, "dm-1", 2*time.Second)
	if !ok {
		t.Fatalf("expected metadata notice to owner DM, none observed")
	}
	if !strings.Contains(notice.Text, "MESSAGE") ||
		!strings.Contains(notice.Text, "user-7") ||
		!strings.Contains(notice.Text, "origin=origin-chat") ||
		!strings.Contains(notice.Text, "target=77777") {
		t.Fatalf("notice missing required metadata fields: %q", notice.Text)
	}
	if strings.Contains(notice.Text, "cross-chat payload") {
		t.Fatalf("notice must NOT include action body: %q", notice.Text)
	}
}

// TestCrossChatMessage_NoNoticeWhenTargetIsOwnerDM checks that when
// the cross-chat target IS the owner's own DM, no duplicate notice is
// posted (the operator already sees the action land there).
func TestCrossChatMessage_NoNoticeWhenTargetIsOwnerDM(t *testing.T) {
	srv, posts, mu := newCrossChatServer(t)
	reply, action := newCrossChatClients(srv.URL)

	actions := []AgentAction{{
		Type:   "MESSAGE",
		Params: map[string]string{"chatid": "99"},
		Body:   "to owner DM",
	}}
	results := ExecuteAgentActions(context.Background(), reply, action, "origin-chat", actions, ActionContext{
		OriginIsOwner: true,
		OwnerDMChat:   "99",
		RequesterID:   "user-7",
	})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}
	// Give the (skipped) notice goroutine a moment; there should be
	// none.
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for _, p := range *posts {
		if p.ChatID == "99" && strings.HasPrefix(strings.TrimSpace(p.Text), "[notice]") {
			t.Fatalf("did not expect a notice when target == ownerDM, got %q", p.Text)
		}
	}
}

// TestSameChatMessage_NoNotice confirms that a MESSAGE that stays in
// the origin chat does not trigger a notice.
func TestSameChatMessage_NoNotice(t *testing.T) {
	srv, posts, mu := newCrossChatServer(t)
	reply, action := newCrossChatClients(srv.URL)

	actions := []AgentAction{{
		Type: "MESSAGE",
		Body: "same-chat only",
	}}
	results := ExecuteAgentActions(context.Background(), reply, action, "origin-chat", actions, ActionContext{
		OriginIsOwner: true,
		OwnerDMChat:   "99",
		RequesterID:   "user-7",
	})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for _, p := range *posts {
		if p.ChatID == "99" && strings.HasPrefix(strings.TrimSpace(p.Text), "[notice]") {
			t.Fatalf("did not expect a notice when target == origin, got %q", p.Text)
		}
	}
}

// TestCrossChatMessage_NoOwnerDMSkipsNotice confirms that when OOB is
// not wired (no OwnerDMChat), the cross-chat action still executes
// without any notice being attempted.
func TestCrossChatMessage_NoOwnerDMSkipsNotice(t *testing.T) {
	srv, posts, mu := newCrossChatServer(t)
	reply, action := newCrossChatClients(srv.URL)

	actions := []AgentAction{{
		Type:   "MESSAGE",
		Params: map[string]string{"chatid": "44444"},
		Body:   "owner cross-chat no OOB",
	}}
	results := ExecuteAgentActions(context.Background(), reply, action, "origin-chat", actions, ActionContext{
		OriginIsOwner: true,
	})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	var targetHit bool
	for _, p := range *posts {
		if p.ChatID == "44444" && strings.Contains(p.Text, "owner cross-chat no OOB") {
			targetHit = true
		}
		if strings.HasPrefix(strings.TrimSpace(p.Text), "[notice]") {
			t.Fatalf("no notice should be attempted when OwnerDMChat is empty, got %q", p.Text)
		}
	}
	if !targetHit {
		t.Fatalf("expected action to be delivered to 44444, got %v", *posts)
	}
}
