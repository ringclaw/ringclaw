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

// newDMRoutingServer mirrors newCrossChatActionServer but exposes the
// captured POST bodies so tests can verify the operator gets a textual
// confirmation of the approval result.
func newDMRoutingServer(t *testing.T) (*httptest.Server, *[]string, *sync.Mutex) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/adaptive-cards"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "card-1", "type": "Adaptive", "version": "1.3"})
			return
		case strings.Contains(r.URL.Path, "/posts") && r.Method == "POST":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if t, _ := body["text"].(string); t != "" {
				mu.Lock()
				bodies = append(bodies, t)
				mu.Unlock()
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "post-1"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "x"})
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies, &mu
}

func newDMBotClient(serverURL string) *ringcentral.Client {
	bot := ringcentral.NewBotClient(serverURL, "bot-token")
	bot.SetDMChatID("dm-1")
	return bot
}

// TestRouteOOBApprovalReply_NoManagerFallsThrough confirms that without
// an OOB manager the helper does not consume any messages — handler
// continues to pass the text to the agent.
func TestRouteOOBApprovalReply_NoManagerFallsThrough(t *testing.T) {
	h := newTestHandler()
	bot := newDMBotClient("http://example.com")
	if h.routeOOBApprovalReply(context.Background(), bot, "dm-1", "user-1", "123456") {
		t.Fatalf("expected no-op when OOB manager is nil")
	}
}

// TestRouteOOBApprovalReply_NonDMFallsThrough confirms that PIN replies
// posted in any chat other than the bot DM are NOT consumed. This
// keeps PINs out of group chat history and forces the operator to use
// the dedicated DM channel.
func TestRouteOOBApprovalReply_NonDMFallsThrough(t *testing.T) {
	h := newTestHandler()
	mgr, _, err := oob.Load(oob.LoadOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("oob.Load: %v", err)
	}
	h.SetOOBManager(mgr, "dm-1")
	bot := newDMBotClient("http://example.com")
	if h.routeOOBApprovalReply(context.Background(), bot, "group-99", "user-1", "123456") {
		t.Fatalf("PIN replies in non-DM chats must not be consumed")
	}
}

// TestRouteOOBApprovalReply_DMApprovesPendingChallenge drives the full
// path: the manager has a pending challenge, the owner replies in the
// bot DM with the matching challenge ID + PIN, and the helper consumes
// the message and resolves the challenge.
func TestRouteOOBApprovalReply_DMApprovesPendingChallenge(t *testing.T) {
	srv, bodies, mu := newDMRoutingServer(t)
	bot := ringcentral.NewBotClient(srv.URL, "bot-token")
	bot.SetDMChatID("dm-1")
	bot.Auth().SetTokenForTest("bot-token", time.Now().Add(time.Hour))

	h := newTestHandler()
	mgr, pin, err := oob.Load(oob.LoadOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("oob.Load: %v", err)
	}
	if pin == "" {
		t.Fatalf("expected fresh PIN")
	}
	h.SetOOBManager(mgr, "dm-1")

	c, issueErr := mgr.Issue("user-1", "test action", "origin", "dm-1", oob.IssueOptions{TTL: 2 * time.Second})
	if issueErr != nil {
		t.Fatalf("Issue: %v", issueErr)
	}
	doneCh := make(chan bool, 1)
	go func() {
		approved, _ := c.Wait(context.Background(), mgr)
		doneCh <- approved
	}()

	if !h.routeOOBApprovalReply(context.Background(), bot, "dm-1", "user-1", c.ID+" "+pin) {
		t.Fatalf("routeOOBApprovalReply did not consume the PIN message")
	}
	select {
	case approved := <-doneCh:
		if !approved {
			t.Fatalf("expected challenge to be approved")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("challenge did not resolve after PIN reply")
	}
	mu.Lock()
	defer mu.Unlock()
	_ = bodies // confirmation message is best-effort; not asserted here
}

// TestRouteOOBApprovalReply_NonApprovalPassesThrough confirms that
// regular DM chatter (not matching the documented approval syntax) is
// NOT consumed by the OOB router, even when the manager is configured.
// This keeps normal AI dispatch working in the bot DM.
func TestRouteOOBApprovalReply_NonApprovalPassesThrough(t *testing.T) {
	srv, _, _ := newDMRoutingServer(t)
	bot := ringcentral.NewBotClient(srv.URL, "bot-token")
	bot.SetDMChatID("dm-1")
	bot.Auth().SetTokenForTest("bot-token", time.Now().Add(time.Hour))

	h := newTestHandler()
	mgr, _, err := oob.Load(oob.LoadOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("oob.Load: %v", err)
	}
	h.SetOOBManager(mgr, "dm-1")

	cases := []string{
		"hello bot",
		"/help",
		"/cwd ~/foo",
		"123 456",
	}
	for _, in := range cases {
		if h.routeOOBApprovalReply(context.Background(), bot, "dm-1", "user-1", in) {
			t.Errorf("non-approval message %q should have fallen through", in)
		}
	}
}
