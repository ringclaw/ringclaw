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
	d, err = parseGrantDuration("48h")
	if err != nil || d != fullAccessMaxGrant {
		t.Fatalf("48h should clamp to %v, got %v", fullAccessMaxGrant, d)
	}
	if _, err := parseGrantDuration("nonsense"); err == nil {
		t.Fatalf("expected parse error for 'nonsense'")
	}
	if _, err := parseGrantDuration("-1m"); err == nil {
		t.Fatalf("expected error for negative duration")
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
	mgr, _, err := oob.Load(oob.LoadOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("oob.Load: %v", err)
	}
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
	mgr, _, err := oob.Load(oob.LoadOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("oob.Load: %v", err)
	}
	h.SetOOBManager(mgr, "dm-1")
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
	mgr, _, err := oob.Load(oob.LoadOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("oob.Load: %v", err)
	}
	h.SetOOBManager(mgr, "dm-1")

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
}

// TestHandleFullAccess_GrantPathRequiresPIN drives the full grant
// round-trip: handleFullAccess kicks off the OOB challenge in a
// goroutine; the test responds with the PIN via routeOOBApprovalReply
// and verifies the manager flips to FullAccessActive.
func TestHandleFullAccess_GrantPathRequiresPIN(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	h.handleFullAccess(ctx, bot, "dm-1", "user-1", "/full-access grant 30s")

	pending := waitForPending(t, mgr, "user-1", 1, 2*time.Second)
	if !h.routeOOBApprovalReply(ctx, bot, "dm-1", "user-1", pending[0].ID+" "+pin) {
		t.Fatalf("PIN reply was not consumed")
	}

	deadline := time.Now().Add(2 * time.Second)
	for !mgr.FullAccessActive() {
		if time.Now().After(deadline) {
			t.Fatalf("FullAccess never activated after PIN approval")
		}
		time.Sleep(20 * time.Millisecond)
	}
	exp := mgr.FullAccessExpiresAt()
	if exp.IsZero() || time.Until(exp) > 35*time.Second || time.Until(exp) < 10*time.Second {
		t.Fatalf("expected ~30s grant, got expiry %v (in %v)", exp, time.Until(exp))
	}

	// Also verify the operator received a granted-confirmation message.
	deadline = time.Now().Add(1 * time.Second)
	var grantedSeen bool
	for time.Now().Before(deadline) {
		mu.Lock()
		snap := append([]string(nil), (*bodies)...)
		mu.Unlock()
		for _, b := range snap {
			if strings.Contains(b, "granted") {
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

// TestHandleFullAccess_GrantPathDeniedKeepsLocked makes sure a /deny
// reply leaves the manager locked.
func TestHandleFullAccess_GrantPathDeniedKeepsLocked(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	h.handleFullAccess(ctx, bot, "dm-1", "user-1", "/full-access grant 1m")

	pending := waitForPending(t, mgr, "user-1", 1, 2*time.Second)
	if !h.routeOOBApprovalReply(ctx, bot, "dm-1", "user-1", "/deny "+pending[0].ID) {
		t.Fatalf("/deny was not consumed")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.FullAccessActive() {
			t.Fatalf("denied grant must not flip FullAccess on")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

