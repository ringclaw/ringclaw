package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/messaging/oob"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// loadTestOOB returns an OOB manager backed by a tempdir and the
// plaintext PIN. Each test call gets its own dir so the per-process
// rate limiter does not bleed between tests.
func loadTestOOB(t *testing.T) (*oob.Manager, string) {
	t.Helper()
	mgr, pin, err := oob.Load(oob.LoadOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("oob.Load: %v", err)
	}
	return mgr, pin
}

func newCrossChatActionServer(t *testing.T) (*httptest.Server, *[]string, *sync.Mutex) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/adaptive-cards"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "card-1", "type": "Adaptive", "version": "1.3",
			})
			return
		case strings.Contains(r.URL.Path, "/posts") && r.Method == "POST":
			mu.Lock()
			paths = append(paths, r.URL.Path)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "post-1"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "x"})
	}))
	t.Cleanup(srv.Close)
	return srv, &paths, &mu
}

// newOOBTestClients points BOTH the reply client (used to post the OOB
// card) and the action client (used to execute the action against the
// target chat) at the same httptest server so the OOB round-trip and
// the post both get valid JSON back.
func newOOBTestClients(serverURL string) (replyClient, actionClient *ringcentral.Client) {
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

// TestExecuteAgentActions_OwnerCrossChatGatedByOOB_ApprovedFromCache
// verifies that when the OOB approval cache already contains the
// (requester, intent) pair, the cross-chat action proceeds without
// posting a new challenge card. This is the fast path that prevents
// prompt fatigue when the AI emits a burst of similar actions.
func TestExecuteAgentActions_OwnerCrossChatGatedByOOB_ApprovedFromCache(t *testing.T) {
	srv, paths, mu := newCrossChatActionServer(t)
	client, actionClient := newOOBTestClients(srv.URL)

	mgr, _ := loadTestOOB(t)
	intent := fmt.Sprintf("MESSAGE cross-chat from %s to %s", "origin-chat", "77777")
	mgr.MarkApprovedForTest("user-7", intent, time.Minute)

	actions := []AgentAction{{
		Type:   "MESSAGE",
		Params: map[string]string{"chatid": "77777"},
		Body:   "owner cross-chat (cached approval)",
	}}
	results := ExecuteAgentActions(context.Background(), client, actionClient, "origin-chat", actions, ActionContext{
		OriginIsOwner: true,
		OOB:           mgr,
		OwnerDMChat:   "dm-chat",
		RequesterID:   "user-7",
	})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*paths) != 1 {
		t.Fatalf("expected exactly 1 POST to target chat, got %v", *paths)
	}
	if !strings.Contains((*paths)[0], "/chats/77777/") {
		t.Errorf("expected POST to target chat, got %q", (*paths)[0])
	}
}

// TestExecuteAgentActions_OwnerCrossChatBlockedOnDeny verifies that an
// explicit /deny reply to the OOB challenge prevents the cross-chat
// action and surfaces a result line back to the user. The action MUST
// NOT be executed against the target chat.
func TestExecuteAgentActions_OwnerCrossChatBlockedOnDeny(t *testing.T) {
	srv, paths, mu := newCrossChatActionServer(t)
	client, actionClient := newOOBTestClients(srv.URL)

	prevTTL := crossChatApprovalTTL
	crossChatApprovalTTL = 2 * time.Second
	t.Cleanup(func() { crossChatApprovalTTL = prevTTL })

	mgr, _ := loadTestOOB(t)

	actions := []AgentAction{{
		Type:   "MESSAGE",
		Params: map[string]string{"chatid": "88888"},
		Body:   "should not be delivered",
	}}

	doneCh := make(chan []string, 1)
	go func() {
		doneCh <- ExecuteAgentActions(context.Background(), client, actionClient, "origin-chat", actions, ActionContext{
			OriginIsOwner: true,
			OOB:           mgr,
			OwnerDMChat:   "dm-chat",
			RequesterID:   "user-7",
		})
	}()

	// Wait for the challenge to land in the manager, then /deny it.
	pending := waitForPending(t, mgr, "user-7", 1, 2*time.Second)
	mgr.Deny(pending[0].ID)

	select {
	case results := <-doneCh:
		if len(results) != 1 || !strings.Contains(results[0], "PIN approval") {
			t.Fatalf("expected 1 PIN-denied result, got %v", results)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ExecuteAgentActions did not return after /deny")
	}

	mu.Lock()
	defer mu.Unlock()
	for _, p := range *paths {
		if strings.Contains(p, "/chats/88888/") {
			t.Errorf("denied cross-chat MESSAGE was still delivered: %q", p)
		}
	}
}

// TestExecuteAgentActions_OwnerCrossChatBlockedOnExpiry verifies that a
// challenge that nobody answers within TTL returns a denial result and
// does not execute the action.
func TestExecuteAgentActions_OwnerCrossChatBlockedOnExpiry(t *testing.T) {
	srv, paths, mu := newCrossChatActionServer(t)
	client, actionClient := newOOBTestClients(srv.URL)

	prevTTL := crossChatApprovalTTL
	crossChatApprovalTTL = 200 * time.Millisecond
	t.Cleanup(func() { crossChatApprovalTTL = prevTTL })

	mgr, _ := loadTestOOB(t)

	actions := []AgentAction{{
		Type:   "MESSAGE",
		Params: map[string]string{"chatid": "55555"},
		Body:   "should expire",
	}}
	results := ExecuteAgentActions(context.Background(), client, actionClient, "origin-chat", actions, ActionContext{
		OriginIsOwner: true,
		OOB:           mgr,
		OwnerDMChat:   "dm-chat",
		RequesterID:   "user-7",
	})
	if len(results) != 1 || !strings.Contains(results[0], "PIN approval") {
		t.Fatalf("expected 1 PIN-denied result on expiry, got %v", results)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, p := range *paths {
		if strings.Contains(p, "/chats/55555/") {
			t.Errorf("expired cross-chat MESSAGE was still delivered: %q", p)
		}
	}
}

// TestExecuteAgentActions_OwnerCrossChatNoOOBPreservesPhase1 verifies
// that callers that have not wired up OOB still get the Phase 1
// behavior (warn-log + dispatch) so library callers and tests do not
// silently break when they construct ActionContext{OriginIsOwner: true}
// without OOB.
func TestExecuteAgentActions_OwnerCrossChatNoOOBPreservesPhase1(t *testing.T) {
	srv, paths, mu := newCrossChatActionServer(t)
	client, actionClient := newOOBTestClients(srv.URL)

	actions := []AgentAction{{
		Type:   "MESSAGE",
		Params: map[string]string{"chatid": "44444"},
		Body:   "owner cross-chat with no OOB configured",
	}}
	results := ExecuteAgentActions(context.Background(), client, actionClient, "origin-chat", actions, ActionContext{
		OriginIsOwner: true,
	})
	if len(results) != 0 {
		t.Fatalf("expected no error results in Phase 1 fallback, got %v", results)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*paths) != 1 || !strings.Contains((*paths)[0], "/chats/44444/") {
		t.Fatalf("expected exactly 1 POST to target chat in Phase 1 fallback, got %v", *paths)
	}
}

// waitForPending polls the manager until at least n challenges are
// outstanding for the given requester (or the deadline elapses).
func waitForPending(t *testing.T, mgr *oob.Manager, requesterID string, n int, timeout time.Duration) []*oob.Challenge {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		pending := mgr.PendingFor(requesterID)
		if len(pending) >= n {
			return pending
		}
		if time.Now().After(deadline) {
			t.Fatalf("waitForPending: expected %d pending for %s, have %d", n, requesterID, len(pending))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
