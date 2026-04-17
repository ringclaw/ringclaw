package messaging

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/messaging/oob"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// ---------- pure helpers ----------------------------------------------

func TestParseGrantDuration_DefaultsAndCaps(t *testing.T) {
	d, err := parseGrantDuration("")
	if err != nil || d != fullAccessDefaultGrant {
		t.Fatalf("empty input → %v %v, want default %v", d, err, fullAccessDefaultGrant)
	}
	d, err = parseGrantDuration("15m")
	if err != nil || d != 15*time.Minute {
		t.Fatalf("15m → %v %v", d, err)
	}
	d, err = parseGrantDuration("720h")
	if err != nil || d != fullAccessMaxGrant {
		t.Fatalf("720h → %v %v, want cap %v", d, err, fullAccessMaxGrant)
	}
	d, err = parseGrantDuration("999h")
	if err != nil || d != fullAccessMaxGrant {
		t.Fatalf("999h should clamp to %v, got %v (err %v)", fullAccessMaxGrant, d, err)
	}
	if _, err := parseGrantDuration("nonsense"); err == nil {
		t.Fatalf("expected parse error for 'nonsense'")
	}
	if _, err := parseGrantDuration("-1m"); err == nil {
		t.Fatalf("expected error for negative duration")
	}
}

func TestFullAccessDefaultIsOneDay(t *testing.T) {
	if fullAccessDefaultGrant != 24*time.Hour {
		t.Fatalf("default grant = %v, want 24h", fullAccessDefaultGrant)
	}
	if fullAccessMaxGrant != 30*24*time.Hour {
		t.Fatalf("max grant = %v, want 30d", fullAccessMaxGrant)
	}
}

func TestSplitFirstWord(t *testing.T) {
	cases := []struct{ in, a, b string }{
		{"", "", ""},
		{"  status  ", "status", ""},
		{"grant 30m", "grant", "30m"},
		{"grant\t1h", "grant", "1h"},
		{"revoke now please", "revoke", "now please"},
	}
	for _, c := range cases {
		a, b := splitFirstWord(c.in)
		if a != c.a || b != c.b {
			t.Errorf("splitFirstWord(%q) = (%q,%q), want (%q,%q)", c.in, a, b, c.a, c.b)
		}
	}
}

func TestIsFullAccessCommand(t *testing.T) {
	yes := []string{"/full-access", "/full-access status", "/full-access grant 5m"}
	no := []string{"", "/full", "/full-access-grant", "full-access"}
	for _, in := range yes {
		if !IsFullAccessCommand(in) {
			t.Errorf("expected %q to be a /full-access command", in)
		}
	}
	for _, in := range no {
		if IsFullAccessCommand(in) {
			t.Errorf("expected %q NOT to be a /full-access command", in)
		}
	}
}

func TestFormatFullAccessStatus(t *testing.T) {
	mgr := oob.New(oob.Options{})
	off := formatFullAccessStatus(mgr)
	if !strings.Contains(off, "off") {
		t.Errorf("off status missing 'off': %q", off)
	}
	mgr.GrantFullAccess(time.Minute)
	on := formatFullAccessStatus(mgr)
	if !strings.Contains(on, "on") {
		t.Errorf("on status missing 'on': %q", on)
	}
	if !strings.Contains(on, "remaining") {
		t.Errorf("on status missing 'remaining': %q", on)
	}
}

// ---------- handler integration ---------------------------------------

func TestHandleFullAccess_NoOOBManager(t *testing.T) {
	srv, bodies, mu := newDMRoutingServer(t)
	bot := newDMBotClient(srv.URL)
	bot.Auth().SetTokenForTest("bot-token", time.Now().Add(time.Hour))

	h := newTestHandler()
	h.handleFullAccess(context.Background(), bot, "dm-1", "user-1", "/full-access status")

	mu.Lock()
	defer mu.Unlock()
	if len(*bodies) == 0 || !strings.Contains((*bodies)[0], "not configured") {
		t.Fatalf("expected 'not configured' reply, got %v", *bodies)
	}
}

func TestHandleFullAccess_RejectsOutsideOwnerDM(t *testing.T) {
	srv, bodies, mu := newDMRoutingServer(t)
	bot := newDMBotClient(srv.URL)
	bot.Auth().SetTokenForTest("bot-token", time.Now().Add(time.Hour))

	h := newTestHandler()
	h.SetOOBManager(oob.New(oob.Options{}), "dm-1")
	h.handleFullAccess(context.Background(), bot, "group-99", "user-1", "/full-access status")

	mu.Lock()
	defer mu.Unlock()
	if len(*bodies) == 0 || !strings.Contains((*bodies)[0], "owner") {
		t.Fatalf("expected owner-DM-only reply, got %v", *bodies)
	}
}

func TestHandleFullAccess_StatusAndRevoke(t *testing.T) {
	srv, bodies, mu := newDMRoutingServer(t)
	bot := newDMBotClient(srv.URL)
	bot.Auth().SetTokenForTest("bot-token", time.Now().Add(time.Hour))

	h := newTestHandler()
	mgr := oob.New(oob.Options{})
	h.SetOOBManager(mgr, "dm-1")

	// Install the revoke hook to assert that /full-access revoke
	// actually triggers the live-session demotion path. In
	// production the hook is wired to agent.DemoteAllACPFullAccess
	// via cmd.initOOBManager; here we just capture the call.
	hookFired := make(chan struct{}, 1)
	mgr.SetFullAccessRevokeHook(func() {
		select {
		case hookFired <- struct{}{}:
		default:
		}
	})

	mgr.GrantFullAccess(time.Minute)
	h.handleFullAccess(context.Background(), bot, "dm-1", "user-1", "/full-access status")
	h.handleFullAccess(context.Background(), bot, "dm-1", "user-1", "/full-access revoke")
	h.handleFullAccess(context.Background(), bot, "dm-1", "user-1", "/full-access")

	mu.Lock()
	got := append([]string(nil), (*bodies)...)
	mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("expected 3 replies, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "on") {
		t.Fatalf("first status should report 'on', got %q", got[0])
	}
	if !strings.Contains(got[1], "revoked") {
		t.Fatalf("expected 'revoked' confirmation, got %q", got[1])
	}
	if !strings.Contains(got[2], "off") {
		t.Fatalf("post-revoke status should be 'off', got %q", got[2])
	}
	if mgr.FullAccessActive() {
		t.Fatalf("manager should be off after revoke")
	}
	select {
	case <-hookFired:
	case <-time.After(time.Second):
		t.Fatalf("/full-access revoke did not invoke the live-session demotion hook")
	}
}

// TestHandleFullAccess_GrantPathActivatesOnApproval drives the two-step
// /approval flow: handleFullAccess posts the challenge prompt; the test
// replies with `/approval <id>` via routeOOBApprovalReply and verifies
// that the manager flips to FullAccessActive.
func TestHandleFullAccess_GrantPathActivatesOnApproval(t *testing.T) {
	srv, bodies, mu := newDMRoutingServer(t)
	bot := ringcentral.NewBotClient(srv.URL, "bot-token")
	bot.SetDMChatID("dm-1")
	bot.Auth().SetTokenForTest("bot-token", time.Now().Add(time.Hour))

	h := newTestHandler()
	mgr := oob.New(oob.Options{})
	h.SetOOBManager(mgr, "dm-1")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	h.handleFullAccess(ctx, bot, "dm-1", "user-1", "/full-access grant 30s")

	pending := waitForPending(t, mgr, "user-1", 1, 2*time.Second)
	if !h.routeOOBApprovalReply(ctx, bot, "dm-1", "user-1", "/approval "+pending[0].ID) {
		t.Fatalf("/approval reply was not consumed")
	}

	deadline := time.Now().Add(2 * time.Second)
	for !mgr.FullAccessActive() {
		if time.Now().After(deadline) {
			t.Fatalf("FullAccess never activated after approval")
		}
		time.Sleep(20 * time.Millisecond)
	}
	exp := mgr.FullAccessExpiresAt()
	if exp.IsZero() || time.Until(exp) > 35*time.Second || time.Until(exp) < 10*time.Second {
		t.Fatalf("expected ~30s grant, got expiry %v (in %v)", exp, time.Until(exp))
	}

	deadline = time.Now().Add(1 * time.Second)
	var grantedSeen bool
	for time.Now().Before(deadline) {
		mu.Lock()
		snap := append([]string(nil), (*bodies)...)
		mu.Unlock()
		for _, b := range snap {
			if strings.Contains(b, "Full-access granted until") {
				grantedSeen = true
				break
			}
		}
		if grantedSeen {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !grantedSeen {
		mu.Lock()
		t.Fatalf("expected granted confirmation, got %v", *bodies)
	}
}

// TestHandleFullAccess_GrantPathDenialKeepsLocked checks that `/approval
// deny <id>` leaves the manager locked.
func TestHandleFullAccess_GrantPathDenialKeepsLocked(t *testing.T) {
	srv, _, _ := newDMRoutingServer(t)
	bot := ringcentral.NewBotClient(srv.URL, "bot-token")
	bot.SetDMChatID("dm-1")
	bot.Auth().SetTokenForTest("bot-token", time.Now().Add(time.Hour))

	h := newTestHandler()
	mgr := oob.New(oob.Options{})
	h.SetOOBManager(mgr, "dm-1")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	h.handleFullAccess(ctx, bot, "dm-1", "user-1", "/full-access grant 1m")

	pending := waitForPending(t, mgr, "user-1", 1, 2*time.Second)
	if !h.routeOOBApprovalReply(ctx, bot, "dm-1", "user-1", "/approval deny "+pending[0].ID) {
		t.Fatalf("/approval deny was not consumed")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.FullAccessActive() {
			t.Fatalf("denied grant must not flip FullAccess on")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestHandleFullAccess_GrantPromptIsTextOnly verifies that the
// /full-access grant path emits a plain text `/approval` prompt and
// does NOT post an Adaptive Card. This is the Phase 2b invariant that
// replaces the old bcrypt-PIN card.
func TestHandleFullAccess_GrantPromptIsTextOnly(t *testing.T) {
	srv, cardBodies, mu := newCardRecordingServer(t)
	bot := ringcentral.NewBotClient(srv.URL, "bot-token")
	bot.SetDMChatID("dm-1")
	bot.Auth().SetTokenForTest("bot-token", time.Now().Add(time.Hour))

	h := newTestHandler()
	mgr := oob.New(oob.Options{})
	h.SetOOBManager(mgr, "dm-1")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	h.handleFullAccess(ctx, bot, "dm-1", "user-1", "/full-access grant 30s")

	// Wait for the pending challenge to appear so we know the grant
	// path executed and posted its prompt.
	_ = waitForPending(t, mgr, "user-1", 1, 2*time.Second)

	mu.Lock()
	defer mu.Unlock()
	if len(*cardBodies) != 0 {
		t.Fatalf("expected no Adaptive Card posts from /full-access grant, got %d: %v", len(*cardBodies), *cardBodies)
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
