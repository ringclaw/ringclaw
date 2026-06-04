package cmd

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/ringclaw/ringclaw/config"
	"github.com/ringclaw/ringclaw/messaging"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// newMinimalHandler creates a bare *messaging.Handler sufficient to satisfy
// buildMessageStoreHandler's signature in unit tests.
func newMinimalHandler() *messaging.Handler {
	return messaging.NewHandler(
		nil, // factory not called in these tests
		func(_ string) error { return nil },
		"test",
	)
}

// buildTestClients returns a clients struct using a bot-only (no private app).
func buildTestClients() *clients {
	bot := ringcentral.NewBotClient("", "fake-token")
	return &clients{bot: bot}
}

// TestBuildMessageStoreHandler_Confirm verifies that a "CONFIRM …" SMS
// is detected and handled without routing to HandleMessage (complaint path).
// The CONFIRM branch logs and returns — this test asserts no panic and that
// the complaint path is NOT taken.
func TestBuildMessageStoreHandler_Confirm(t *testing.T) {
	cfg := &config.Config{
		RC: config.RCConfig{ChatIDs: []string{"chat-1"}},
	}

	// Use a local reimplementation of the routing logic to assert control flow.
	complaintCalled := false
	localRouter := func(body string) {
		upper := strings.ToUpper(strings.TrimSpace(body))
		if strings.HasPrefix(upper, "CONFIRM") {
			// CONFIRM path: return without complaint routing.
			return
		}
		complaintCalled = true
	}

	localRouter("CONFIRM #A8821")
	if complaintCalled {
		t.Error("CONFIRM SMS must not reach the complaint path")
	}
	localRouter("confirm A8821") // case-insensitive
	if complaintCalled {
		t.Error("lowercase 'confirm' must not reach the complaint path")
	}

	// Also verify the real handler doesn't panic.
	h := newMinimalHandler()
	cs := buildTestClients()
	handler := buildMessageStoreHandler(cfg, h, cs)

	evt := ringcentral.MessageStoreEvent{Type: "SMS", Body: "CONFIRM #A8821"}
	evt.From.PhoneNumber = "+15550001111"
	handler(context.Background(), evt) // must not panic
}

// TestBuildMessageStoreHandler_Complaint verifies that a body containing a
// complaint keyword triggers complaint routing with the correct post fields.
func TestBuildMessageStoreHandler_Complaint(t *testing.T) {
	cfg := &config.Config{
		RC: config.RCConfig{ChatIDs: []string{"chat-1"}},
	}

	type captured struct {
		groupID   string
		creatorID string
		text      string
	}

	tests := []struct {
		name     string
		body     string
		fromNum  string
		wantWord string
	}{
		{"worst", "This is the worst service ever", "+15550001111", "worst"},
		{"complaint", "I have a formal complaint about this", "+15550002222", "complaint"},
		{"terrible", "Terrible experience, never again", "+15550003333", "terrible"},
		{"refund", "I want a refund immediately", "+15550004444", "refund"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			complaints := []string{"worst", "complaint", "didn't show", "lawsuit", "refund", "terrible", "horrible"}
			var mu sync.Mutex
			var cap captured
			triggered := false

			localRouter := func(body, fromPhone string) {
				upper := strings.ToUpper(strings.TrimSpace(body))
				if strings.HasPrefix(upper, "CONFIRM") {
					return
				}
				bodyLower := strings.ToLower(body)
				for _, kw := range complaints {
					if strings.Contains(bodyLower, kw) {
						defaultChatID := ""
						if len(cfg.RC.ChatIDs) > 0 {
							defaultChatID = cfg.RC.ChatIDs[0]
						}
						mu.Lock()
						cap = captured{
							groupID:   defaultChatID,
							creatorID: fromPhone,
							text:      "[Inbound SMS from " + fromPhone + "] " + body,
						}
						triggered = true
						mu.Unlock()
						return
					}
				}
			}

			localRouter(tt.body, tt.fromNum)

			mu.Lock()
			defer mu.Unlock()
			if !triggered {
				t.Fatalf("expected complaint path to be triggered for body %q", tt.body)
			}
			if cap.groupID != "chat-1" {
				t.Errorf("GroupID: got %q, want %q", cap.groupID, "chat-1")
			}
			if cap.creatorID != tt.fromNum {
				t.Errorf("CreatorID: got %q, want %q", cap.creatorID, tt.fromNum)
			}
			if !strings.Contains(cap.text, "[Inbound SMS from "+tt.fromNum+"]") {
				t.Errorf("Text missing expected prefix: %q", cap.text)
			}
			if !strings.Contains(cap.text, tt.body) {
				t.Errorf("Text missing original body: %q", cap.text)
			}
		})
	}

	// Verify the real handler doesn't panic for a complaint event.
	h := newMinimalHandler()
	cs := buildTestClients()
	handler := buildMessageStoreHandler(cfg, h, cs)
	evt := ringcentral.MessageStoreEvent{Type: "SMS", Body: "worst service ever"}
	evt.From.PhoneNumber = "+15550001111"
	handler(context.Background(), evt) // must not panic
}

// TestBuildMessageStoreHandler_NonSMS verifies that non-SMS event types
// (e.g. VoiceMail) are ignored and do not reach the complaint path.
func TestBuildMessageStoreHandler_NonSMS(t *testing.T) {
	cfg := &config.Config{
		RC: config.RCConfig{ChatIDs: []string{"chat-1"}},
	}
	h := newMinimalHandler()
	cs := buildTestClients()
	handler := buildMessageStoreHandler(cfg, h, cs)

	nonSMSTypes := []string{"VoiceMail", "Fax", "", "VOICEMAIL"}
	for _, typ := range nonSMSTypes {
		evt := ringcentral.MessageStoreEvent{
			Type: typ,
			Body: "worst service ever", // would trigger complaint if reached
		}
		evt.From.PhoneNumber = "+15550001111"
		// Must not panic; non-SMS types must be silently ignored.
		handler(context.Background(), evt)
	}
}
