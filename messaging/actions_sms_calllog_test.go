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

	"github.com/ringclaw/ringclaw/ringcentral"
)

// TestExecuteAgentActions_SMS_Success verifies that a well-formed SMS action
// calls SendSMS with the correct recipient and message body.
func TestExecuteAgentActions_SMS_Success(t *testing.T) {
	var mu sync.Mutex
	var smsTo, smsText string
	smsCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/sms") && r.Method == http.MethodPost {
			mu.Lock()
			smsCalled = true
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if tos, ok := body["to"].([]interface{}); ok && len(tos) > 0 {
				if m, ok := tos[0].(map[string]interface{}); ok {
					smsTo, _ = m["phoneNumber"].(string)
				}
			}
			smsText, _ = body["text"].(string)
			mu.Unlock()
			json.NewEncoder(w).Encode(map[string]string{"id": "msg-1", "messageStatus": "Sent"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"id": "p1"})
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type:   "SMS",
		Params: map[string]string{"to": "+16505551234", "from": "+14155559999"},
		Body:   "Hello from the bot",
	}}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if !smsCalled {
		t.Fatal("expected SendSMS to be called, but it was not")
	}
	if smsTo != "+16505551234" {
		t.Errorf("expected to=+16505551234, got %q", smsTo)
	}
	if !strings.Contains(smsText, "Hello from the bot") {
		t.Errorf("expected body to contain 'Hello from the bot', got %q", smsText)
	}
}

// TestExecuteAgentActions_SMS_MissingTo verifies that an SMS action without
// a 'to' parameter yields an error result and does not call SendSMS.
func TestExecuteAgentActions_SMS_MissingTo(t *testing.T) {
	var mu sync.Mutex
	smsCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/sms") {
			mu.Lock()
			smsCalled = true
			mu.Unlock()
		}
		json.NewEncoder(w).Encode(map[string]string{"id": "p1"})
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type:   "SMS",
		Params: map[string]string{},
		Body:   "Hello",
	}}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 1 {
		t.Fatalf("expected 1 error result, got %v", results)
	}
	if !strings.Contains(results[0], "missing") {
		t.Errorf("expected error about missing 'to', got %q", results[0])
	}

	mu.Lock()
	defer mu.Unlock()
	if smsCalled {
		t.Error("SendSMS should not be called when 'to' is missing")
	}
}

// TestExecuteAgentActions_SMS_MissingBody verifies that an SMS action with
// an empty body yields an error result and does not call SendSMS.
func TestExecuteAgentActions_SMS_MissingBody(t *testing.T) {
	var mu sync.Mutex
	smsCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/sms") {
			mu.Lock()
			smsCalled = true
			mu.Unlock()
		}
		json.NewEncoder(w).Encode(map[string]string{"id": "p1"})
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type:   "SMS",
		Params: map[string]string{"to": "+16505551234"},
		Body:   "   ", // whitespace-only body
	}}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 1 {
		t.Fatalf("expected 1 error result, got %v", results)
	}
	if !strings.Contains(results[0], "missing") {
		t.Errorf("expected error about missing body, got %q", results[0])
	}

	mu.Lock()
	defer mu.Unlock()
	if smsCalled {
		t.Error("SendSMS should not be called when body is empty")
	}
}

// TestExecuteAgentActions_PHONE_CALLLOG_Today verifies that PHONE_CALLLOG
// with scope=today calls ListCallLog and sends a formatted reply with records.
func TestExecuteAgentActions_PHONE_CALLLOG_Today(t *testing.T) {
	var mu sync.Mutex
	var callLogPath string
	var postedText string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/call-log") {
			mu.Lock()
			callLogPath = r.URL.String()
			mu.Unlock()
			json.NewEncoder(w).Encode(map[string]interface{}{
				"records": []map[string]interface{}{
					{
						"id":        "cl-1",
						"startTime": "2026-06-04T10:00:00Z",
						"duration":  120,
						"result":    "Call connected",
						"from":      map[string]string{"phoneNumber": "+14155559999", "name": "Alice"},
						"to":        map[string]string{"phoneNumber": "+16505551234", "name": "Bob"},
					},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/posts") && r.Method == http.MethodPost {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			postedText, _ = body["text"].(string)
			mu.Unlock()
		}
		json.NewEncoder(w).Encode(map[string]string{"id": "p1"})
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type:   "PHONE_CALLLOG",
		Params: map[string]string{"scope": "today"},
	}}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if callLogPath == "" {
		t.Fatal("expected ListCallLog to be called")
	}
	if !strings.Contains(callLogPath, "dateFrom") {
		t.Errorf("expected dateFrom in call log query for scope=today, got %q", callLogPath)
	}
	if !strings.Contains(postedText, "Call Log") {
		t.Errorf("expected reply to contain 'Call Log', got %q", postedText)
	}
	if !strings.Contains(postedText, "+14155559999") {
		t.Errorf("expected reply to contain from number, got %q", postedText)
	}
	if !strings.Contains(postedText, "+16505551234") {
		t.Errorf("expected reply to contain to number, got %q", postedText)
	}
}

// TestExecuteAgentActions_PHONE_CALLLOG_Empty verifies that PHONE_CALLLOG
// with no records sends a "(no calls)" reply.
func TestExecuteAgentActions_PHONE_CALLLOG_Empty(t *testing.T) {
	var mu sync.Mutex
	var postedText string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/call-log") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"records": []interface{}{},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/posts") && r.Method == http.MethodPost {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			postedText, _ = body["text"].(string)
			mu.Unlock()
		}
		json.NewEncoder(w).Encode(map[string]string{"id": "p1"})
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	actions := []AgentAction{{
		Type:   "PHONE_CALLLOG",
		Params: map[string]string{},
	}}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(postedText, "(no calls)") {
		t.Errorf("expected '(no calls)' in reply for empty call log, got %q", postedText)
	}
}
