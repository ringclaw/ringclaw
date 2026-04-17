package oob

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ----- test helpers ---------------------------------------------------

type fakeCard struct{ id string }

func (f *fakeCard) GetID() string { return f.id }

type fakeClient struct {
	mu        sync.Mutex
	cards     []json.RawMessage
	cardChats []string
	texts     []string
	textChats []string
	failCard  bool
	failText  bool
}

func (c *fakeClient) CreateAdaptiveCard(_ context.Context, chatID string, card json.RawMessage) (Card, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failCard {
		return nil, errors.New("rate limited")
	}
	c.cards = append(c.cards, card)
	c.cardChats = append(c.cardChats, chatID)
	return &fakeCard{id: "card-1"}, nil
}

func (c *fakeClient) SendText(_ context.Context, chatID, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failText {
		return errors.New("send failed")
	}
	c.texts = append(c.texts, text)
	c.textChats = append(c.textChats, chatID)
	return nil
}

func (c *fakeClient) snapshotCards() []json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]json.RawMessage, len(c.cards))
	copy(out, c.cards)
	return out
}

func (c *fakeClient) snapshotTexts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.texts))
	copy(out, c.texts)
	return out
}

// loadTestManager returns a Manager backed by a fresh tempdir and the
// plaintext PIN that was generated. Each test gets its own dir so
// rate-limit windows don't bleed across tests.
func loadTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m, pin, err := Load(LoadOptions{Dir: dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pin == "" {
		t.Fatalf("expected a fresh PIN to be generated")
	}
	if len(pin) != pinDigits {
		t.Fatalf("PIN length = %d, want %d", len(pin), pinDigits)
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			t.Fatalf("PIN contains non-digit %q", pin)
		}
	}
	if _, statErr := readPinFile(filepath.Join(dir, PinFileName)); statErr != nil {
		t.Fatalf("PIN file not written: %v", statErr)
	}
	return m, pin
}

func readPinFile(path string) (pinFile, error) {
	var pf pinFile
	data, err := readFile(path)
	if err != nil {
		return pf, err
	}
	if err := json.Unmarshal(data, &pf); err != nil {
		return pf, err
	}
	return pf, nil
}

// ----- tests ----------------------------------------------------------

func TestLoad_GeneratesPersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	m1, pin, err := Load(LoadOptions{Dir: dir})
	if err != nil {
		t.Fatalf("Load fresh: %v", err)
	}
	if !m1.VerifyPIN(pin) {
		t.Fatalf("VerifyPIN failed for newly generated PIN")
	}
	m2, newPIN, err := Load(LoadOptions{Dir: dir})
	if err != nil {
		t.Fatalf("Load existing: %v", err)
	}
	if newPIN != "" {
		t.Fatalf("expected no new PIN on reload, got %q", newPIN)
	}
	if !m2.VerifyPIN(pin) {
		t.Fatalf("reloaded manager rejected the original PIN")
	}
}

func TestVerifyPIN_RejectsWrongAndIsRateLimited(t *testing.T) {
	m, pin := loadTestManager(t)
	if m.VerifyPIN("000000") && pin != "000000" {
		t.Fatalf("VerifyPIN accepted a wrong PIN")
	}
	for i := 0; i < 6; i++ {
		_ = m.VerifyPIN("000000")
	}
	if m.VerifyPIN(pin) {
		t.Fatalf("rate limiter should reject correct PIN after >5 failures in 1 min")
	}
}

func TestParseApprovalReply(t *testing.T) {
	cases := []struct {
		in       string
		want     ApprovalReply
		wantKind ApprovalReplyKind
	}{
		{in: "/approve aabbccdd 123456", want: ApprovalReply{Kind: ReplyApprove, ChallengeID: "aabbccdd", PIN: "123456"}, wantKind: ReplyApprove},
		{in: "AABBCCDD 654321", want: ApprovalReply{Kind: ReplyApprove, ChallengeID: "aabbccdd", PIN: "654321"}, wantKind: ReplyApprove},
		{in: "654321", want: ApprovalReply{Kind: ReplyApprove, PIN: "654321"}, wantKind: ReplyApprove},
		{in: "/deny aabbccdd", want: ApprovalReply{Kind: ReplyDeny, ChallengeID: "aabbccdd"}, wantKind: ReplyDeny},
		{in: "/approve aabbccdd badpin", want: ApprovalReply{}, wantKind: ReplyNone},
		{in: "hi 123456 there", want: ApprovalReply{}, wantKind: ReplyNone},
		{in: "12345", want: ApprovalReply{}, wantKind: ReplyNone},
		{in: "", want: ApprovalReply{}, wantKind: ReplyNone},
	}
	for _, tc := range cases {
		got := ParseApprovalReply(tc.in)
		if got != tc.want {
			t.Errorf("ParseApprovalReply(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestAuthorize_PostsCardAndApprovesOnReply(t *testing.T) {
	m, pin := loadTestManager(t)
	client := &fakeClient{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resultCh := make(chan struct {
		approved bool
		err      error
	}, 1)
	go func() {
		approved, err := m.Authorize(ctx, AuthorizeOptions{
			RequesterID:  "user-1",
			Intent:       "test action",
			OriginChatID: "origin",
			OwnerDMChat:  "dm-1",
			Client:       client,
			TTL:          2 * time.Second,
		})
		resultCh <- struct {
			approved bool
			err      error
		}{approved, err}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(client.snapshotCards()) >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("challenge card never posted")
		}
		time.Sleep(10 * time.Millisecond)
	}

	pending := m.PendingFor("user-1")
	if len(pending) != 1 {
		t.Fatalf("PendingFor returned %d challenges, want 1", len(pending))
	}
	cid := pending[0].ID
	if !m.HandleApprovalReply(ctx, client, "dm-1", "user-1", cid+" "+pin) {
		t.Fatalf("HandleApprovalReply did not consume the message")
	}

	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("Authorize err = %v", res.err)
		}
		if !res.approved {
			t.Fatalf("expected approval")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Authorize did not return after PIN reply")
	}

	if !m.CachedApproval("user-1", "test action") {
		t.Fatalf("expected approval cache to be populated")
	}
}

func TestAuthorize_CachedApprovalSkipsCard(t *testing.T) {
	m, _ := loadTestManager(t)
	m.cacheApproval("user-1", "intent-x", time.Minute)
	client := &fakeClient{}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	approved, err := m.Authorize(ctx, AuthorizeOptions{
		RequesterID: "user-1",
		Intent:      "intent-x",
		OwnerDMChat: "dm",
		Client:      client,
	})
	if err != nil || !approved {
		t.Fatalf("cached path should approve immediately, got approved=%v err=%v", approved, err)
	}
	if cards := client.snapshotCards(); len(cards) != 0 {
		t.Fatalf("expected zero cards on cache hit, got %d", len(cards))
	}
}

func TestAuthorize_DenyResolvesFalse(t *testing.T) {
	m, _ := loadTestManager(t)
	client := &fakeClient{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	doneCh := make(chan struct {
		approved bool
		err      error
	}, 1)
	go func() {
		approved, err := m.Authorize(ctx, AuthorizeOptions{
			RequesterID: "user-1",
			Intent:      "deny intent",
			OwnerDMChat: "dm",
			Client:      client,
			TTL:         2 * time.Second,
		})
		doneCh <- struct {
			approved bool
			err      error
		}{approved, err}
	}()
	pending := waitPending(t, m, "user-1", 1)
	if !m.HandleApprovalReply(ctx, client, "dm", "user-1", "/deny "+pending[0].ID) {
		t.Fatalf("HandleApprovalReply did not consume /deny")
	}
	select {
	case res := <-doneCh:
		if res.err != nil {
			t.Fatalf("Authorize err = %v", res.err)
		}
		if res.approved {
			t.Fatalf("expected denial, got approved")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Authorize did not return after /deny")
	}
}

func TestAuthorize_ExpiresWhenIgnored(t *testing.T) {
	m, _ := loadTestManager(t)
	client := &fakeClient{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	approved, err := m.Authorize(ctx, AuthorizeOptions{
		RequesterID: "user-1",
		Intent:      "expiry test",
		OwnerDMChat: "dm",
		Client:      client,
		TTL:         200 * time.Millisecond,
	})
	if approved {
		t.Fatalf("expected expiry, got approved")
	}
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("expected ErrChallengeExpired, got %v", err)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatalf("Authorize returned faster than TTL")
	}
}

func TestAuthorize_FallbackToTextWhenCardFails(t *testing.T) {
	m, pin := loadTestManager(t)
	client := &fakeClient{failCard: true}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	doneCh := make(chan bool, 1)
	go func() {
		approved, _ := m.Authorize(ctx, AuthorizeOptions{
			RequesterID: "user-1",
			Intent:      "fallback test",
			OwnerDMChat: "dm",
			Client:      client,
			TTL:         2 * time.Second,
		})
		doneCh <- approved
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(client.snapshotTexts()) >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fallback text never sent")
		}
		time.Sleep(10 * time.Millisecond)
	}
	pending := waitPending(t, m, "user-1", 1)
	if !m.HandleApprovalReply(ctx, client, "dm", "user-1", pending[0].ID+" "+pin) {
		t.Fatalf("approval reply not consumed")
	}
	select {
	case ok := <-doneCh:
		if !ok {
			t.Fatalf("expected approval after fallback")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Authorize did not return")
	}
}

func TestHandleApprovalReply_BarePinRequiresExactlyOnePending(t *testing.T) {
	m, pin := loadTestManager(t)
	client := &fakeClient{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Zero pending challenges: bare PIN should fall through.
	if m.HandleApprovalReply(ctx, client, "dm", "user-1", pin) {
		t.Fatalf("bare PIN with zero pending should fall through")
	}

	// Two pending: bare PIN should be consumed but not approve.
	c1, _ := m.Issue("user-1", "intent-a", "origin", "dm", IssueOptions{TTL: time.Second})
	c2, _ := m.Issue("user-1", "intent-b", "origin", "dm", IssueOptions{TTL: time.Second})
	if !m.HandleApprovalReply(ctx, client, "dm", "user-1", pin) {
		t.Fatalf("bare PIN with multiple pending should be consumed (with disambiguation reply)")
	}
	// Both challenges should still be pending.
	if len(m.PendingFor("user-1")) != 2 {
		t.Fatalf("challenges should remain pending after ambiguous bare-PIN reply")
	}
	m.removeChallenge(c1.ID)
	m.removeChallenge(c2.ID)
}

func TestDeny_RefusedForDifferentRequester(t *testing.T) {
	m, _ := loadTestManager(t)
	client := &fakeClient{}
	ctx := context.Background()
	c, _ := m.Issue("owner", "intent", "origin", "dm", IssueOptions{TTL: time.Second})
	handled := m.HandleApprovalReply(ctx, client, "dm", "intruder", "/deny "+c.ID)
	if !handled {
		t.Fatalf("expected /deny to be consumed (and rejected)")
	}
	if _, ok := m.lookupChallenge(c.ID); !ok {
		t.Fatalf("challenge should not be removed by non-requester /deny")
	}
}

func TestFullAccessGrantAndExpire(t *testing.T) {
	m, _ := loadTestManager(t)
	if m.FullAccessActive() {
		t.Fatalf("fresh manager should not have full-access active")
	}
	m.GrantFullAccess(50 * time.Millisecond)
	if !m.FullAccessActive() {
		t.Fatalf("expected full-access active right after grant")
	}
	time.Sleep(80 * time.Millisecond)
	if m.FullAccessActive() {
		t.Fatalf("expected full-access to expire")
	}
	m.GrantFullAccess(time.Minute)
	m.RevokeFullAccess()
	if m.FullAccessActive() {
		t.Fatalf("Revoke should clear grant")
	}
}

func waitPending(t *testing.T, m *Manager, requesterID string, want int) []*Challenge {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		pending := m.PendingFor(requesterID)
		if len(pending) >= want {
			return pending
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d pending challenges for %s, have %d", want, requesterID, len(pending))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func init() {
	// Sanity: ensure approval cache key changes when intent does.
	a := approvalCacheKey("u", "x")
	b := approvalCacheKey("u", "y")
	if a == b {
		panic("approvalCacheKey collision")
	}
	if !strings.HasPrefix(approvalCacheKey("u", "x"), a[:8]) {
		panic("approvalCacheKey not deterministic")
	}
}
