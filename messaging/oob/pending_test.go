package oob

import (
	"context"
	"testing"
	"time"
)

func TestPendingFor_FiltersByRequester(t *testing.T) {
	m := New(Options{})
	a, err := m.Issue("alice", "intent-A", "chat-a", "dm", IssueOptions{TTL: time.Second})
	if err != nil {
		t.Fatalf("issue alice: %v", err)
	}
	if _, err := m.Issue("bob", "intent-B", "chat-b", "dm", IssueOptions{TTL: time.Second}); err != nil {
		t.Fatalf("issue bob: %v", err)
	}

	got := m.PendingFor("alice")
	if len(got) != 1 {
		t.Fatalf("PendingFor(alice) len=%d, want 1", len(got))
	}
	if got[0].ID != a.ID {
		t.Errorf("PendingFor(alice)[0].ID = %q, want %q", got[0].ID, a.ID)
	}

	if got := m.PendingFor("nobody"); len(got) != 0 {
		t.Errorf("PendingFor(nobody) len=%d, want 0", len(got))
	}
}

func TestPendingFor_ExcludesExpired(t *testing.T) {
	m := New(Options{})
	if _, err := m.Issue("alice", "soon", "c", "dm", IssueOptions{TTL: 10 * time.Millisecond}); err != nil {
		t.Fatalf("issue: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if got := m.PendingFor("alice"); len(got) != 0 {
		t.Errorf("expired challenge still listed: %v", got)
	}
}

func TestPending_ReturnsAllRequesters(t *testing.T) {
	m := New(Options{})
	if _, err := m.Issue("alice", "intent-A", "chat-a", "dm", IssueOptions{TTL: time.Second}); err != nil {
		t.Fatalf("issue alice: %v", err)
	}
	if _, err := m.Issue("bob", "intent-B", "chat-b", "dm", IssueOptions{TTL: time.Second}); err != nil {
		t.Fatalf("issue bob: %v", err)
	}
	got := m.Pending()
	if len(got) != 2 {
		t.Errorf("Pending() len=%d, want 2", len(got))
	}
}

func TestPending_ExcludesExpired(t *testing.T) {
	m := New(Options{})
	if _, err := m.Issue("alice", "fast", "c", "dm", IssueOptions{TTL: 10 * time.Millisecond}); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := m.Issue("bob", "slow", "c", "dm", IssueOptions{TTL: time.Second}); err != nil {
		t.Fatalf("issue: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	got := m.Pending()
	if len(got) != 1 {
		t.Errorf("Pending() len=%d, want 1 (alice expired)", len(got))
	}
	if len(got) == 1 && got[0].RequesterID != "bob" {
		t.Errorf("expected bob as the only survivor, got %q", got[0].RequesterID)
	}
}

func TestIssue_RejectsEmptyRequester(t *testing.T) {
	m := New(Options{})
	if _, err := m.Issue("   ", "intent", "c", "dm", IssueOptions{}); err == nil {
		t.Error("blank requesterID should fail")
	}
}

func TestIssue_RejectsEmptyIntent(t *testing.T) {
	m := New(Options{})
	if _, err := m.Issue("alice", " \t\n", "c", "dm", IssueOptions{}); err == nil {
		t.Error("blank intent should fail")
	}
}

func TestApprove_UnknownChallengeFails(t *testing.T) {
	m := New(Options{})
	if _, err := m.Approve("ffffffff"); err == nil {
		t.Error("Approve unknown challenge should fail")
	}
}

func TestApprove_AfterExpiryReturnsExpired(t *testing.T) {
	m := New(Options{})
	c, err := m.Issue("alice", "i", "c", "dm", IssueOptions{TTL: 5 * time.Millisecond})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := m.Approve(c.ID); err == nil {
		t.Error("Approve(expired) should fail")
	}
}

func TestDeny_UnknownChallengeReturnsFalse(t *testing.T) {
	m := New(Options{})
	if m.Deny("ffffffff") {
		t.Error("Deny unknown should return false")
	}
}

func TestPostChallengePrompt_NilGuards(t *testing.T) {
	if err := PostChallengePrompt(context.Background(), nil, &Challenge{}, "1h"); err == nil {
		t.Error("nil client should fail")
	}
	if err := PostChallengePrompt(context.Background(), &fakeClient{}, nil, "1h"); err == nil {
		t.Error("nil challenge should fail")
	}
}

func TestPostChallengePrompt_DeliversText(t *testing.T) {
	m := New(Options{})
	c, err := m.Issue("alice", "intent", "chat-x", "dm-1", IssueOptions{TTL: time.Second})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	client := &fakeClient{}
	if err := PostChallengePrompt(context.Background(), client, c, "24h"); err != nil {
		t.Fatalf("PostChallengePrompt: %v", err)
	}
	got := client.snapshot()
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if got[0] == "" {
		t.Errorf("delivered message should be non-empty")
	}
}

func TestPostChallengePrompt_PropagatesSendError(t *testing.T) {
	m := New(Options{})
	c, err := m.Issue("alice", "intent", "chat-x", "dm-1", IssueOptions{TTL: time.Second})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	client := &fakeClient{failText: true}
	if err := PostChallengePrompt(context.Background(), client, c, ""); err == nil {
		t.Error("PostChallengePrompt should propagate SendText error")
	}
}

func TestParseApprovalReply_ChallengeIDLengths(t *testing.T) {
	// Boundary cases for isChallengeID via ParseApprovalReply.
	cases := []struct {
		in   string
		want ApprovalReplyKind
	}{
		{"/approval 1234567", ReplyNone},        // 7 hex
		{"/approval 12345678", ReplyApprove},    // 8 hex
		{"/approval 123456789", ReplyNone},      // 9 hex
		{"/approval 12345g78", ReplyNone},       // non-hex char
		{"/approval deny ABCDEF12", ReplyDeny},  // mixed case allowed
		{"/approval ABCDEF12", ReplyApprove},    // mixed case allowed
	}
	for _, tc := range cases {
		got := ParseApprovalReply(tc.in)
		if got.Kind != tc.want {
			t.Errorf("ParseApprovalReply(%q).Kind = %v, want %v", tc.in, got.Kind, tc.want)
		}
	}
}
