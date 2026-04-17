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

// newCrossChatServer records POST destinations and bodies so the
// fail-closed tests can assert ordering (pre-notice before action) or
// the absence of a delivery.
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
			posts = append(posts, cspost{ChatID: chat, Text: text, Path: r.URL.Path, At: time.Now()})
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
	At     time.Time
}

func extractChatFromPath(path string) string {
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

// TestCrossChatMessage_NoticeBeforeAction pins the fail-closed
// contract: the heads-up notice to the owner DM is delivered BEFORE
// the cross-chat ACTION lands on the target chat. If the notice
// succeeds, both posts show up on the server in that order.
func TestCrossChatMessage_NoticeBeforeAction(t *testing.T) {
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

	mu.Lock()
	defer mu.Unlock()
	snap := append([]cspost(nil), (*posts)...)
	if len(snap) != 2 {
		t.Fatalf("expected exactly 2 POSTs (notice + action), got %d: %+v", len(snap), snap)
	}
	if snap[0].ChatID != "dm-1" || !strings.HasPrefix(strings.TrimSpace(snap[0].Text), "[notice]") {
		t.Fatalf("expected first POST to be the notice on dm-1, got %+v", snap[0])
	}
	if snap[1].ChatID != "77777" || !strings.Contains(snap[1].Text, "cross-chat payload") {
		t.Fatalf("expected second POST to be the action on 77777, got %+v", snap[1])
	}
	if !snap[0].At.Before(snap[1].At) && !snap[0].At.Equal(snap[1].At) {
		t.Fatalf("notice timestamp (%s) must not be after action (%s)", snap[0].At, snap[1].At)
	}
	if !strings.Contains(snap[0].Text, "MESSAGE") ||
		!strings.Contains(snap[0].Text, "user-7") ||
		!strings.Contains(snap[0].Text, "origin=origin-chat") ||
		!strings.Contains(snap[0].Text, "target=77777") {
		t.Fatalf("notice missing required metadata fields: %q", snap[0].Text)
	}
	if strings.Contains(snap[0].Text, "cross-chat payload") {
		t.Fatalf("notice must NOT include action body: %q", snap[0].Text)
	}
}

// TestCrossChatMessage_NoNoticeWhenTargetIsOwnerDM confirms that when
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
	mu.Lock()
	defer mu.Unlock()
	for _, p := range *posts {
		if p.ChatID == "99" && strings.HasPrefix(strings.TrimSpace(p.Text), "[notice]") {
			t.Fatalf("did not expect a notice when target == origin, got %q", p.Text)
		}
	}
}

// TestCrossChatMessage_RefusedWhenOwnerDMEmpty is the fail-closed
// regression test: when no owner DM audit channel is wired, owner
// cross-chat ACTIONs must be refused and NOT dispatched to the
// target chat. Previously the action still landed with only a warn
// log, which is the gap flagged by the reviewer.
func TestCrossChatMessage_RefusedWhenOwnerDMEmpty(t *testing.T) {
	srv, posts, mu := newCrossChatServer(t)
	reply, action := newCrossChatClients(srv.URL)

	actions := []AgentAction{{
		Type:   "MESSAGE",
		Params: map[string]string{"chatid": "44444"},
		Body:   "owner cross-chat no audit channel",
	}}
	results := ExecuteAgentActions(context.Background(), reply, action, "origin-chat", actions, ActionContext{
		OriginIsOwner: true,
		// OwnerDMChat intentionally empty
	})
	if len(results) != 1 || !strings.Contains(results[0], "Refused cross-chat MESSAGE") {
		t.Fatalf("expected refusal result, got %v", results)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, p := range *posts {
		if p.ChatID == "44444" {
			t.Fatalf("action must NOT be dispatched when audit channel is unavailable, got %+v", p)
		}
		if strings.HasPrefix(strings.TrimSpace(p.Text), "[notice]") {
			t.Fatalf("no notice should be attempted when OwnerDMChat is empty, got %q", p.Text)
		}
	}
}

// TestCrossChatMessage_RefusedWhenNoticeFails asserts fail-closed
// semantics when the RC backend returns 5xx on the pre-notice POST:
// the action must NOT be dispatched to the target chat. We achieve
// this by running two separate test servers — the "notice" server
// returns 503 when posting to dm-1, and the "action" server records
// whether the cross-chat write sneaks through.
func TestCrossChatMessage_RefusedWhenNoticeFails(t *testing.T) {
	var mu sync.Mutex
	var posts []cspost
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/posts") || r.Method != "POST" {
			w.WriteHeader(http.StatusOK)
			return
		}
		chat := extractChatFromPath(r.URL.Path)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		text, _ := body["text"].(string)
		mu.Lock()
		posts = append(posts, cspost{ChatID: chat, Text: text, Path: r.URL.Path, At: time.Now()})
		mu.Unlock()
		if chat == "dm-failing" {
			http.Error(w, "simulated outage", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "post-1"})
	}))
	t.Cleanup(srv.Close)
	reply, action := newCrossChatClients(srv.URL)

	actions := []AgentAction{{
		Type:   "MESSAGE",
		Params: map[string]string{"chatid": "88888"},
		Body:   "should-not-land",
	}}
	results := ExecuteAgentActions(context.Background(), reply, action, "origin-chat", actions, ActionContext{
		OriginIsOwner: true,
		OwnerDMChat:   "dm-failing",
		RequesterID:   "user-7",
	})
	if len(results) != 1 || !strings.Contains(results[0], "Refused cross-chat MESSAGE") {
		t.Fatalf("expected refusal result, got %v", results)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, p := range posts {
		if p.ChatID == "88888" {
			t.Fatalf("action must NOT be dispatched when pre-notice fails, got %+v", p)
		}
	}
}
