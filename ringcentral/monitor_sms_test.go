package ringcentral

import (
	"context"
	"encoding/json"
	"testing"
)

// TestMonitor_SetMessageStoreHandler verifies that the handler is stored
// and retrieved correctly on the Monitor.
func TestMonitor_SetMessageStoreHandler(t *testing.T) {
	m := newTestMonitor("chat-1", func(ctx context.Context, client *Client, _ *Client, post Post) {})

	// Initially nil.
	m.mu.Lock()
	if m.msgStoreHandler != nil {
		t.Error("expected msgStoreHandler to be nil initially")
	}
	m.mu.Unlock()

	var called bool
	h := MessageStoreHandler(func(ctx context.Context, evt MessageStoreEvent) {
		called = true
	})

	m.SetMessageStoreHandler(h)

	m.mu.Lock()
	stored := m.msgStoreHandler
	m.mu.Unlock()

	if stored == nil {
		t.Fatal("expected msgStoreHandler to be non-nil after SetMessageStoreHandler")
	}

	// Invoke the stored handler to confirm it is the one we installed.
	stored(context.Background(), MessageStoreEvent{Type: "SMS"})
	if !called {
		t.Error("stored handler was not the installed handler")
	}

	// Setting nil should clear the handler.
	m.SetMessageStoreHandler(nil)
	m.mu.Lock()
	if m.msgStoreHandler != nil {
		t.Error("expected msgStoreHandler to be nil after SetMessageStoreHandler(nil)")
	}
	m.mu.Unlock()
}

// TestMessageStoreEvent_Parsing verifies that a MessageStoreEvent can be
// correctly decoded from a JSON webhook payload.
func TestMessageStoreEvent_Parsing(t *testing.T) {
	payload := `{
		"id": "msg-001",
		"type": "SMS",
		"from": {"phoneNumber": "+15550001111"},
		"to": [{"phoneNumber": "+15550002222"}],
		"body": "Hello from customer"
	}`

	var evt MessageStoreEvent
	if err := json.Unmarshal([]byte(payload), &evt); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	if evt.ID != "msg-001" {
		t.Errorf("ID: got %q, want %q", evt.ID, "msg-001")
	}
	if evt.Type != "SMS" {
		t.Errorf("Type: got %q, want %q", evt.Type, "SMS")
	}
	if evt.From.PhoneNumber != "+15550001111" {
		t.Errorf("From.PhoneNumber: got %q, want %q", evt.From.PhoneNumber, "+15550001111")
	}
	if len(evt.To) != 1 || evt.To[0].PhoneNumber != "+15550002222" {
		t.Errorf("To: got %+v", evt.To)
	}
	if evt.Body != "Hello from customer" {
		t.Errorf("Body: got %q, want %q", evt.Body, "Hello from customer")
	}
}
