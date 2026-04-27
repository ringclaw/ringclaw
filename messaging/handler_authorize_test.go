package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/messaging/oob"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// fakeAuthorizeMonitor records AddChatUserAllow calls so tests can
// confirm that an approved grant flowed back into the monitor as
// well as the handler.
type fakeAuthorizeMonitor struct {
	mu      sync.Mutex
	entries []string
}

func (f *fakeAuthorizeMonitor) AddChatUserAllow(chatID, userID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, chatID+"|"+userID)
}

func (f *fakeAuthorizeMonitor) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.entries))
	copy(out, f.entries)
	return out
}

// authorizeFakeServer captures POSTed reply bodies and answers
// /persons/<id> + /chats/<id> with stub data so the rich prompt
// builder produces a realistic owner-DM message during tests.
func authorizeFakeServer(t *testing.T, person ringcentral.PersonInfo, chat ringcentral.Chat) (*httptest.Server, *[]string, *sync.Mutex) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/team-messaging/v1/persons/") && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(person)
		case strings.HasPrefix(r.URL.Path, "/team-messaging/v1/chats/") && !strings.Contains(r.URL.Path, "/posts") && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(chat)
		case strings.Contains(r.URL.Path, "/posts") && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if t, _ := body["text"].(string); t != "" {
				mu.Lock()
				bodies = append(bodies, t)
				mu.Unlock()
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "post-1"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "x"})
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies, &mu
}

func newAuthorizeBotClient(serverURL string) *ringcentral.Client {
	bot := ringcentral.NewBotClient(serverURL, "bot-token")
	bot.SetDMChatID("dm-1")
	bot.Auth().SetTokenForTest("bot-token", time.Now().Add(time.Hour))
	return bot
}

func waitFor(t *testing.T, cond func() bool, deadline time.Duration) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", deadline)
}

func waitForBody(t *testing.T, bodies *[]string, mu *sync.Mutex, substr string, deadline time.Duration) string {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		mu.Lock()
		for _, b := range *bodies {
			if strings.Contains(b, substr) {
				mu.Unlock()
				return b
			}
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("did not see body containing %q within %s", substr, deadline)
	return ""
}

func TestAuthorizeMention_NoOOB_DropsSilently(t *testing.T) {
	srv, bodies, mu := authorizeFakeServer(t,
		ringcentral.PersonInfo{ID: "user-9", Email: "u@example.com"},
		ringcentral.Chat{ID: "group-1", Name: "Eng", Type: "Team"})
	bot := newAuthorizeBotClient(srv.URL)
	read := newAuthorizeBotClient(srv.URL)

	h := newTestHandler()
	post := ringcentral.Post{ID: "p1", GroupID: "group-1", CreatorID: "user-9", Text: "@bot hi"}
	h.AuthorizeMention(context.Background(), bot, read, post)

	mu.Lock()
	defer mu.Unlock()
	if len(*bodies) != 0 {
		t.Fatalf("expected no DM posts when OOB is not configured, got %v", *bodies)
	}
}

func TestAuthorizeMention_PostsRichPromptToOwnerDM(t *testing.T) {
	srv, bodies, mu := authorizeFakeServer(t,
		ringcentral.PersonInfo{ID: "user-9", FirstName: "Eve", LastName: "Doe", Email: "eve@example.com"},
		ringcentral.Chat{ID: "group-1", Name: "Engineering", Type: "Team"})
	bot := newAuthorizeBotClient(srv.URL)
	read := newAuthorizeBotClient(srv.URL)

	h := newTestHandler()
	mgr := oob.New(oob.Options{})
	h.SetOOBManager(mgr, "dm-1")

	post := ringcentral.Post{ID: "p1", GroupID: "group-1", CreatorID: "user-9", Text: "![:Person](bot-1) please summarize"}
	h.AuthorizeMention(context.Background(), bot, read, post)

	body := waitForBody(t, bodies, mu, "Pending authorization", time.Second)
	for _, want := range []string{"Engineering", "group-1", "user-9", "Eve Doe", "eve@example.com", "Mention:", "ringclaw approval"} {
		if !strings.Contains(body, want) {
			t.Errorf("prompt missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestAuthorizeMention_DedupesPendingChallenge(t *testing.T) {
	srv, bodies, mu := authorizeFakeServer(t,
		ringcentral.PersonInfo{ID: "user-9", Email: "eve@example.com"},
		ringcentral.Chat{ID: "group-1", Name: "Eng", Type: "Team"})
	bot := newAuthorizeBotClient(srv.URL)
	read := newAuthorizeBotClient(srv.URL)

	h := newTestHandler()
	mgr := oob.New(oob.Options{})
	h.SetOOBManager(mgr, "dm-1")

	post := ringcentral.Post{ID: "p1", GroupID: "group-1", CreatorID: "user-9", Text: "@bot ping"}
	h.AuthorizeMention(context.Background(), bot, read, post)
	waitForBody(t, bodies, mu, "Pending authorization", time.Second)

	mu.Lock()
	first := len(*bodies)
	mu.Unlock()

	// Second mention while first is pending → should be silently dropped.
	post2 := ringcentral.Post{ID: "p2", GroupID: "group-1", CreatorID: "user-9", Text: "@bot still here?"}
	h.AuthorizeMention(context.Background(), bot, read, post2)

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	second := len(*bodies)
	mu.Unlock()
	if second != first {
		t.Fatalf("expected dedupe to suppress the second prompt: before=%d after=%d", first, second)
	}
	pending := mgr.Pending()
	if len(pending) != 1 {
		t.Fatalf("expected exactly 1 outstanding challenge, got %d", len(pending))
	}
}

func TestAuthorizeMention_ApproveAppliesGrantAndPersists(t *testing.T) {
	srv, bodies, mu := authorizeFakeServer(t,
		ringcentral.PersonInfo{ID: "user-9", FirstName: "Eve", LastName: "Doe", Email: "eve@example.com"},
		ringcentral.Chat{ID: "group-1", Name: "Eng", Type: "Team"})
	bot := newAuthorizeBotClient(srv.URL)
	read := newAuthorizeBotClient(srv.URL)

	h := newTestHandler()
	mgr := oob.New(oob.Options{})
	h.SetOOBManager(mgr, "dm-1")

	mon := &fakeAuthorizeMonitor{}
	var persisted atomic.Value // (chatID, identifier)
	persist := func(chatID, identifier string) error {
		persisted.Store([2]string{chatID, identifier})
		return nil
	}
	h.SetAuthorizeMention(persist, mon)

	post := ringcentral.Post{ID: "p1", GroupID: "group-1", CreatorID: "user-9", Text: "@bot hi"}
	h.AuthorizeMention(context.Background(), bot, read, post)

	prompt := waitForBody(t, bodies, mu, "Pending authorization", time.Second)
	id := extractChallengeID(t, prompt)

	if _, err := mgr.Approve(id); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	waitForBody(t, bodies, mu, "Authorized `eve@example.com`", time.Second)

	if !h.isChatUserAllowed("group-1", "user-9") {
		t.Errorf("handler did not record chat_user_allow for the approved user")
	}
	got := mon.snapshot()
	if len(got) != 1 || got[0] != "group-1|user-9" {
		t.Errorf("monitor.AddChatUserAllow not called as expected: %v", got)
	}
	saved, _ := persisted.Load().([2]string)
	if saved[0] != "group-1" || saved[1] != "eve@example.com" {
		t.Errorf("persisted = %v, want [group-1 eve@example.com]", saved)
	}
}

func TestAuthorizeMention_NoEmailFallbackToNumeric(t *testing.T) {
	srv, bodies, mu := authorizeFakeServer(t,
		ringcentral.PersonInfo{ID: "user-9", FirstName: "Eve", LastName: "Doe"}, // no email
		ringcentral.Chat{ID: "group-1", Name: "Eng", Type: "Team"})
	bot := newAuthorizeBotClient(srv.URL)
	read := newAuthorizeBotClient(srv.URL)

	h := newTestHandler()
	mgr := oob.New(oob.Options{})
	h.SetOOBManager(mgr, "dm-1")
	var persisted atomic.Value
	h.SetAuthorizeMention(func(chatID, identifier string) error {
		persisted.Store([2]string{chatID, identifier})
		return nil
	}, &fakeAuthorizeMonitor{})

	post := ringcentral.Post{ID: "p1", GroupID: "group-1", CreatorID: "user-9", Text: "@bot hi"}
	h.AuthorizeMention(context.Background(), bot, read, post)
	prompt := waitForBody(t, bodies, mu, "Pending authorization", time.Second)
	id := extractChallengeID(t, prompt)

	if _, err := mgr.Approve(id); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	waitForBody(t, bodies, mu, "Authorized `user-9`", time.Second)

	saved, _ := persisted.Load().([2]string)
	if saved[1] != "user-9" {
		t.Errorf("expected numeric ID fallback, got identifier=%q", saved[1])
	}
}

func TestAuthorizeMention_DenyNotifiesAndDoesNotPersist(t *testing.T) {
	srv, bodies, mu := authorizeFakeServer(t,
		ringcentral.PersonInfo{ID: "user-9", Email: "eve@example.com"},
		ringcentral.Chat{ID: "group-1", Name: "Eng", Type: "Team"})
	bot := newAuthorizeBotClient(srv.URL)
	read := newAuthorizeBotClient(srv.URL)

	h := newTestHandler()
	mgr := oob.New(oob.Options{})
	h.SetOOBManager(mgr, "dm-1")
	var persistCalls int32
	h.SetAuthorizeMention(func(chatID, identifier string) error {
		atomic.AddInt32(&persistCalls, 1)
		return nil
	}, &fakeAuthorizeMonitor{})

	post := ringcentral.Post{ID: "p1", GroupID: "group-1", CreatorID: "user-9", Text: "@bot hi"}
	h.AuthorizeMention(context.Background(), bot, read, post)
	prompt := waitForBody(t, bodies, mu, "Pending authorization", time.Second)
	id := extractChallengeID(t, prompt)

	if !mgr.Deny(id) {
		t.Fatalf("Deny returned false")
	}
	waitForBody(t, bodies, mu, "Denied authorization", time.Second)

	if atomic.LoadInt32(&persistCalls) != 0 {
		t.Errorf("persist must not be called on deny")
	}
	if h.isChatUserAllowed("group-1", "user-9") {
		t.Errorf("denied user must not gain chat-allow trust")
	}
	// pending should be released
	waitFor(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return !h.pendingAuthorize[authorizePendingKey("group-1", "user-9")]
	}, time.Second)
}

func TestAuthorizeMention_ExpiryNotifiesAndReleasesPending(t *testing.T) {
	srv, bodies, mu := authorizeFakeServer(t,
		ringcentral.PersonInfo{ID: "user-9", Email: "eve@example.com"},
		ringcentral.Chat{ID: "group-1", Name: "Eng", Type: "Team"})
	bot := newAuthorizeBotClient(srv.URL)

	h := newTestHandler()
	mgr := oob.New(oob.Options{})
	h.SetOOBManager(mgr, "dm-1")
	h.SetAuthorizeMention(func(chatID, identifier string) error { return nil }, &fakeAuthorizeMonitor{})

	// Issue a custom-TTL challenge directly so the test doesn't need 5 minutes.
	c, err := mgr.Issue("user-9", "test", "group-1", "dm-1", oob.IssueOptions{TTL: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// Pre-stake the pending key + meta so awaitAuthorizeMention has the
	// state it would have after a real AuthorizeMention call.
	h.tryReservePending(authorizePendingKey("group-1", "user-9"))
	h.storeAuthorizeMeta(c.ID, authorizeMeta{Email: "eve@example.com"})

	go h.awaitAuthorizeMention(bot, c, "group-1", "user-9", authorizePendingKey("group-1", "user-9"))

	waitForBody(t, bodies, mu, "expired", 2*time.Second)
	waitFor(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return !h.pendingAuthorize[authorizePendingKey("group-1", "user-9")]
	}, time.Second)
	if h.isChatUserAllowed("group-1", "user-9") {
		t.Errorf("expired challenge must not grant chat allow")
	}
}

func extractChallengeID(t *testing.T, prompt string) string {
	t.Helper()
	// Look for "challenge `<id>`" segment.
	const marker = "challenge `"
	idx := strings.Index(prompt, marker)
	if idx < 0 {
		t.Fatalf("could not locate challenge id in prompt: %s", prompt)
	}
	rest := prompt[idx+len(marker):]
	end := strings.IndexByte(rest, '`')
	if end < 0 {
		t.Fatalf("malformed prompt: %s", prompt)
	}
	return rest[:end]
}

func TestAddChatUserAllow_DedupesAndIsolatesByChat(t *testing.T) {
	h := newTestHandler()
	h.AddChatUserAllow("c1", "u1")
	h.AddChatUserAllow("c1", "u1")
	h.AddChatUserAllow("c1", "u2")
	h.AddChatUserAllow("c2", "u1")

	if !h.isChatUserAllowed("c1", "u1") || !h.isChatUserAllowed("c1", "u2") {
		t.Fatalf("missing entries in c1")
	}
	if !h.isChatUserAllowed("c2", "u1") {
		t.Fatalf("missing c2/u1")
	}
	if h.isChatUserAllowed("c2", "u2") {
		t.Fatalf("c2/u2 should not exist")
	}
}

// TestPostCrossChatPrompt_RichContent verifies that the cross-chat
// OOB challenge prompt sent to the owner DM surfaces the action
// type, requester display + email, origin and target chat names
// (when resolvable), the relevant action params, and the body
// preview.
func TestPostCrossChatPrompt_RichContent(t *testing.T) {
	// authorizeFakeServer answers /persons and /chats with the same
	// stub for any ID; for this test we want different responses per
	// chat ID, so use a hand-rolled server.
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/team-messaging/v1/persons/"):
			_ = json.NewEncoder(w).Encode(ringcentral.PersonInfo{
				ID: "user-7", FirstName: "Alice", LastName: "Cross", Email: "alice@example.com",
			})
		case strings.HasSuffix(r.URL.Path, "/team-messaging/v1/chats/origin-1"):
			_ = json.NewEncoder(w).Encode(ringcentral.Chat{ID: "origin-1", Name: "Engineering", Type: "Team"})
		case strings.HasSuffix(r.URL.Path, "/team-messaging/v1/chats/target-9"):
			_ = json.NewEncoder(w).Encode(ringcentral.Chat{ID: "target-9", Name: "Customer Support", Type: "Team"})
		case strings.Contains(r.URL.Path, "/posts") && r.Method == http.MethodPost:
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if t, _ := b["text"].(string); t != "" {
				mu.Lock()
				bodies = append(bodies, t)
				mu.Unlock()
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "post-1"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "x"})
		}
	}))
	t.Cleanup(srv.Close)

	actionClient := newAuthorizeBotClient(srv.URL)
	mgr := oob.New(oob.Options{})
	c, err := mgr.Issue("user-7", "cross-chat NOTE", "origin-1", "dm-1", oob.IssueOptions{TTL: time.Minute})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	a := AgentAction{
		Type:   "NOTE",
		Params: map[string]string{"title": "Quarterly review notes"},
		Body:   "Highlights for the next quarter and follow-ups for the team.",
	}
	opts := ActionContext{
		OOB:         mgr,
		OwnerDMChat: "dm-1",
		RequesterID: "user-7",
	}

	if err := postCrossChatPrompt(context.Background(), actionClient, c, a, "origin-1", "target-9", opts); err != nil {
		t.Fatalf("postCrossChatPrompt: %v", err)
	}

	body := waitForBody(t, &bodies, &mu, "Pending approval", time.Second)
	for _, want := range []string{
		"Action: Cross-chat NOTE",
		"Alice Cross",
		"alice@example.com",
		"Origin chat: Engineering",
		"Target chat: Customer Support",
		"Title: Quarterly review notes",
		"Body: Highlights",
		"Effect: bot will write a NOTE",
		"ringclaw approval",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("cross-chat prompt missing %q\nbody:\n%s", want, body)
		}
	}
}
