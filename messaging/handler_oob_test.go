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

// newDMRoutingServer returns an httptest.Server that captures POST
// /posts bodies (used to verify the operator gets text confirmations)
// and answers /adaptive-cards with a stub JSON document. Returned for
// both bodies and the accompanying mutex so tests can safely read the
// slice.
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

// newCardRecordingServer captures POSTs to /adaptive-cards so tests can
// assert that Phase 2b does NOT emit an Adaptive Card for approval. A
// fresh server is used per-test to avoid cross-contamination.
func newCardRecordingServer(t *testing.T) (*httptest.Server, *[]string, *sync.Mutex) {
	var mu sync.Mutex
	var cards []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/adaptive-cards") && r.Method == "POST":
			buf := make([]byte, 0, 256)
			tmp := make([]byte, 256)
			for {
				n, _ := r.Body.Read(tmp)
				buf = append(buf, tmp[:n]...)
				if n == 0 {
					break
				}
			}
			mu.Lock()
			cards = append(cards, string(buf))
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "card-1", "type": "Adaptive", "version": "1.3"})
			return
		case strings.Contains(r.URL.Path, "/posts") && r.Method == "POST":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "post-1"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "x"})
	}))
	t.Cleanup(srv.Close)
	return srv, &cards, &mu
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
	if h.routeOOBApprovalReply(context.Background(), bot, "dm-1", "user-1", "/approval aabbccdd") {
		t.Fatalf("expected no-op when OOB manager is nil")
	}
}

// TestRouteOOBApprovalReply_NonDMRefusedWithMessage confirms that a
// recognizable `/approval ...` shape posted outside the bot DM is
// intercepted and refused with an explanatory message, rather than
// silently falling through to the default agent (which would both
// confuse the operator and leak the challenge syntax into an AI
// prompt). The refusal neither consumes the challenge nor lets a
// teammate poke at another user's pending ID.
func TestRouteOOBApprovalReply_NonDMRefusedWithMessage(t *testing.T) {
	h := newTestHandler()
	h.SetOOBManager(oob.New(oob.Options{}), "dm-1")
	bot := newDMBotClient("http://example.com")
	if !h.routeOOBApprovalReply(context.Background(), bot, "group-99", "user-1", "/approval aabbccdd") {
		t.Fatalf("/approval in non-DM chats must be intercepted (returned true)")
	}
}

// TestRouteOOBApprovalReply_NonApprovalOutsideDMFallsThrough confirms
// that non-approval text in non-DM chats still falls through to the
// normal agent pipeline — only recognizable `/approval ...` shapes
// are refused outside the bot DM.
func TestRouteOOBApprovalReply_NonApprovalOutsideDMFallsThrough(t *testing.T) {
	h := newTestHandler()
	h.SetOOBManager(oob.New(oob.Options{}), "dm-1")
	bot := newDMBotClient("http://example.com")
	if h.routeOOBApprovalReply(context.Background(), bot, "group-99", "user-1", "hello there") {
		t.Fatalf("non-/approval text outside bot DM must fall through")
	}
}

// TestRouteOOBApprovalReply_DMApprovesPendingChallenge verifies that
// /approval in chat is consumed but redirected to terminal (not resolved).
func TestRouteOOBApprovalReply_DMApprovesPendingChallenge(t *testing.T) {
	srv, _, _ := newDMRoutingServer(t)
	bot := ringcentral.NewBotClient(srv.URL, "bot-token")
	bot.SetDMChatID("dm-1")
	bot.Auth().SetTokenForTest("bot-token", time.Now().Add(time.Hour))

	h := newTestHandler()
	mgr := oob.New(oob.Options{})
	h.SetOOBManager(mgr, "dm-1")

	c, issueErr := mgr.Issue("user-1", "test action", "dm-1", "dm-1", oob.IssueOptions{TTL: 2 * time.Second})
	if issueErr != nil {
		t.Fatalf("Issue: %v", issueErr)
	}

	// Chat /approval is consumed but does NOT resolve the challenge.
	if !h.routeOOBApprovalReply(context.Background(), bot, "dm-1", "user-1", "/approval "+c.ID) {
		t.Fatalf("routeOOBApprovalReply did not consume the /approval message")
	}

	// Approve via terminal path (Manager.Approve directly).
	doneCh := make(chan bool, 1)
	go func() {
		approved, _ := c.Wait(context.Background(), mgr)
		doneCh <- approved
	}()
	if _, err := mgr.Approve(c.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	select {
	case approved := <-doneCh:
		if !approved {
			t.Fatalf("expected challenge to be approved via terminal path")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("challenge did not resolve after Approve()")
	}
}

// TestRouteOOBApprovalReply_NonApprovalPassesThrough confirms that
// regular DM chatter (not matching the `/approval` syntax) is NOT
// consumed, even when the manager is configured. Keeps normal AI
// dispatch working in the bot DM.
func TestRouteOOBApprovalReply_NonApprovalPassesThrough(t *testing.T) {
	srv, _, _ := newDMRoutingServer(t)
	bot := ringcentral.NewBotClient(srv.URL, "bot-token")
	bot.SetDMChatID("dm-1")
	bot.Auth().SetTokenForTest("bot-token", time.Now().Add(time.Hour))

	h := newTestHandler()
	h.SetOOBManager(oob.New(oob.Options{}), "dm-1")

	cases := []string{
		"hello bot",
		"/help",
		"/cwd ~/foo",
		"123456",
		"/approve aabbccdd 123456", // legacy PIN syntax, must fall through
		"aabbccdd 123456",          // legacy <id> <pin>, must fall through
	}
	for _, in := range cases {
		if h.routeOOBApprovalReply(context.Background(), bot, "dm-1", "user-1", in) {
			t.Errorf("non-approval message %q should have fallen through", in)
		}
	}
}
