package oob

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClient is a minimal oob.Client implementation that records the
// outgoing text messages so assertions can look at what the operator
// would have seen in the owner DM.
type fakeClient struct {
	mu        sync.Mutex
	texts     []string
	textChats []string
	failText  bool
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

func (c *fakeClient) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.texts))
	copy(out, c.texts)
	return out
}

// waitPending polls until at least `want` challenges are outstanding
// for the given requester (or the 2s deadline elapses). Kept in the
// test file so the production API is not leaking a polling primitive.
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

func TestIssueApproveFlow(t *testing.T) {
	m := New(Options{})
	client := &fakeClient{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := m.Issue("user-1", "grant full-access", "dm-1", "dm-1", IssueOptions{TTL: 2 * time.Second})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := PostChallengePrompt(ctx, client, c, "24h"); err != nil {
		t.Fatalf("PostChallengePrompt: %v", err)
	}
	if got := client.snapshot(); len(got) != 1 || !strings.Contains(got[0], c.ID) {
		t.Fatalf("expected challenge prompt with challenge ID, got %v", got)
	}

	type res struct {
		approved bool
		err      error
	}
	ch := make(chan res, 1)
	go func() {
		ok, werr := c.Wait(ctx, m)
		ch <- res{ok, werr}
	}()

	if !m.HandleApprovalReply(ctx, client, "dm-1", "user-1", "/approval "+c.ID) {
		t.Fatalf("HandleApprovalReply did not consume the approval")
	}
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("Wait err = %v", r.err)
		}
		if !r.approved {
			t.Fatalf("expected approval")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("challenge did not resolve after /approval reply")
	}
}

func TestIssueDenyFlow(t *testing.T) {
	m := New(Options{})
	client := &fakeClient{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := m.Issue("user-1", "grant", "dm", "dm", IssueOptions{TTL: 2 * time.Second})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	ch := make(chan bool, 1)
	go func() {
		approved, _ := c.Wait(ctx, m)
		ch <- approved
	}()
	if !m.HandleApprovalReply(ctx, client, "dm", "user-1", "/approval deny "+c.ID) {
		t.Fatalf("HandleApprovalReply did not consume the deny")
	}
	select {
	case ok := <-ch:
		if ok {
			t.Fatalf("expected denial, got approved")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("deny did not resolve challenge")
	}
}

func TestChallengeExpiry(t *testing.T) {
	m := New(Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := m.Issue("user-1", "expire", "dm", "dm", IssueOptions{TTL: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	start := time.Now()
	approved, werr := c.Wait(ctx, m)
	if approved {
		t.Fatalf("expected expiry, got approved")
	}
	if !errors.Is(werr, ErrChallengeExpired) {
		t.Fatalf("expected ErrChallengeExpired, got %v", werr)
	}
	if time.Since(start) < 40*time.Millisecond {
		t.Fatalf("Wait returned faster than TTL")
	}
	// Post-expiry lookup must not find the challenge.
	if _, ok := m.lookupChallenge(c.ID); ok {
		t.Fatalf("expected challenge to be removed after expiry")
	}
}

func TestApproveRefusedForNonRequester(t *testing.T) {
	m := New(Options{})
	client := &fakeClient{}
	ctx := context.Background()
	c, err := m.Issue("owner", "grant", "dm", "dm", IssueOptions{TTL: time.Second})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	handled := m.HandleApprovalReply(ctx, client, "dm", "intruder", "/approval "+c.ID)
	if !handled {
		t.Fatalf("expected /approval reply to be consumed (and rejected)")
	}
	if _, ok := m.lookupChallenge(c.ID); !ok {
		t.Fatalf("challenge should not be removed by non-requester approval")
	}
	got := client.snapshot()
	if len(got) != 1 || !strings.Contains(got[0], "different user") {
		t.Fatalf("expected 'different user' rejection text, got %v", got)
	}
}

func TestDenyRefusedForNonRequester(t *testing.T) {
	m := New(Options{})
	client := &fakeClient{}
	ctx := context.Background()
	c, err := m.Issue("owner", "grant", "dm", "dm", IssueOptions{TTL: time.Second})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	handled := m.HandleApprovalReply(ctx, client, "dm", "intruder", "/approval deny "+c.ID)
	if !handled {
		t.Fatalf("expected /approval deny to be consumed (and rejected)")
	}
	if _, ok := m.lookupChallenge(c.ID); !ok {
		t.Fatalf("challenge should not be removed by non-requester /approval deny")
	}
}

func TestParseApprovalReply(t *testing.T) {
	cases := []struct {
		in   string
		want ApprovalReply
	}{
		{in: "/approval aabbccdd", want: ApprovalReply{Kind: ReplyApprove, ChallengeID: "aabbccdd"}},
		{in: "  /approval  AABBCCDD  ", want: ApprovalReply{Kind: ReplyApprove, ChallengeID: "aabbccdd"}},
		{in: "/APPROVAL aabbccdd", want: ApprovalReply{Kind: ReplyApprove, ChallengeID: "aabbccdd"}},
		{in: "/approval deny aabbccdd", want: ApprovalReply{Kind: ReplyDeny, ChallengeID: "aabbccdd"}},
		{in: "/approval Deny AABBCCDD", want: ApprovalReply{Kind: ReplyDeny, ChallengeID: "aabbccdd"}},

		// Legacy PIN-ish / bare-PIN forms must no longer be recognised.
		{in: "/approve aabbccdd 123456", want: ApprovalReply{}},
		{in: "aabbccdd 123456", want: ApprovalReply{}},
		{in: "123456", want: ApprovalReply{}},
		{in: "/deny aabbccdd", want: ApprovalReply{}},

		// Malformed /approval calls fall through.
		{in: "/approval", want: ApprovalReply{}},
		{in: "/approval garbage", want: ApprovalReply{}},
		{in: "/approval aabbccdd extra", want: ApprovalReply{}},
		{in: "/approval deny", want: ApprovalReply{}},
		{in: "/approval deny garbage", want: ApprovalReply{}},
		{in: "", want: ApprovalReply{}},
	}
	for _, tc := range cases {
		got := ParseApprovalReply(tc.in)
		if got != tc.want {
			t.Errorf("ParseApprovalReply(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestFullAccessGrantAndExpire(t *testing.T) {
	m := New(Options{})
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
	if !m.FullAccessExpiresAt().IsZero() {
		t.Fatalf("expected FullAccessExpiresAt to be zero after expiry")
	}
	m.GrantFullAccess(time.Minute)
	m.RevokeFullAccess()
	if m.FullAccessActive() {
		t.Fatalf("Revoke should clear grant")
	}
}

func TestGrantFullAccessNegativeIsNoop(t *testing.T) {
	m := New(Options{})
	m.GrantFullAccess(0)
	m.GrantFullAccess(-time.Second)
	if m.FullAccessActive() {
		t.Fatalf("non-positive TTL should not activate full-access")
	}
}

func waitPendingSink(t *testing.T, m *Manager, requester string) {
	t.Helper()
	_ = waitPending(t, m, requester, 1)
}

// smoke: keep splitFirstField deterministic under tab/space mixes.
func TestSplitFirstField(t *testing.T) {
	cases := []struct{ in, a, b string }{
		{"", "", ""},
		{"  foo  ", "foo", ""},
		{"foo bar", "foo", "bar"},
		{"foo\tbar baz", "foo", "bar baz"},
	}
	for _, c := range cases {
		a, b := splitFirstField(c.in)
		if a != c.a || b != c.b {
			t.Errorf("splitFirstField(%q) = (%q,%q), want (%q,%q)", c.in, a, b, c.a, c.b)
		}
	}
}

var _ = waitPendingSink // keep helper referenced so future edits can't silently drop it
