package ringcentral

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// TestSendSMS_Success verifies that a successful POST to the SMS endpoint
// returns a populated SMSMessage with correct fields.
func TestSendSMS_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/restapi/v1.0/account/~/extension/~/sms" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		// Verify body contains expected fields
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		from, ok := body["from"].(map[string]interface{})
		if !ok {
			t.Errorf("expected 'from' object in body, got %T", body["from"])
		} else if from["phoneNumber"] != "+14045550001" {
			t.Errorf("expected from.phoneNumber=+14045550001, got %v", from["phoneNumber"])
		}
		toList, ok := body["to"].([]interface{})
		if !ok || len(toList) == 0 {
			t.Errorf("expected non-empty 'to' array, got %v", body["to"])
		} else {
			toEntry, _ := toList[0].(map[string]interface{})
			if toEntry["phoneNumber"] != "+14045550002" {
				t.Errorf("expected to[0].phoneNumber=+14045550002, got %v", toEntry["phoneNumber"])
			}
		}
		if body["text"] != "Hello from test" {
			t.Errorf("expected text='Hello from test', got %v", body["text"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SMSMessage{
			ID:           "sms-1",
			From:         "+14045550001",
			To:           "+14045550002",
			Direction:    "Outbound",
			CreationTime: "2026-06-04T10:00:00Z",
			Subject:      "Hello from test",
		})
	})
	defer srv.Close()

	msg, err := client.SendSMS(context.Background(), "+14045550001", "+14045550002", "Hello from test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ID != "sms-1" {
		t.Errorf("expected ID sms-1, got %s", msg.ID)
	}
	if msg.Direction != "Outbound" {
		t.Errorf("expected Direction=Outbound, got %s", msg.Direction)
	}
	if msg.Subject != "Hello from test" {
		t.Errorf("expected Subject='Hello from test', got %s", msg.Subject)
	}
}

// TestSendSMS_Error verifies that a 4xx response from the SMS endpoint
// results in an error being returned.
func TestSendSMS_Error(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errorCode":"MSG-403","message":"Invalid phone number"}`))
	})
	defer srv.Close()

	_, err := client.SendSMS(context.Background(), "+1invalid", "+14045550002", "test")
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
}

// TestSendSMS_InvalidJSON verifies that an unparseable response body returns an error.
func TestSendSMS_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not valid json`))
	})
	defer srv.Close()

	_, err := client.SendSMS(context.Background(), "+14045550001", "+14045550002", "test")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

// TestListCallLog_Success verifies that query parameters are forwarded correctly
// and that response records are parsed.
func TestListCallLog_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/restapi/v1.0/account/~/extension/~/call-log" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		q := r.URL.Query()
		if q.Get("direction") != "Inbound" {
			t.Errorf("expected direction=Inbound, got %s", q.Get("direction"))
		}
		if q.Get("type") != "Voice" {
			t.Errorf("expected type=Voice, got %s", q.Get("type"))
		}
		if q.Get("dateFrom") != "2026-06-01T00:00:00Z" {
			t.Errorf("expected dateFrom=2026-06-01T00:00:00Z, got %s", q.Get("dateFrom"))
		}
		if q.Get("dateTo") != "2026-06-04T00:00:00Z" {
			t.Errorf("expected dateTo=2026-06-04T00:00:00Z, got %s", q.Get("dateTo"))
		}
		if q.Get("perPage") != "25" {
			t.Errorf("expected perPage=25, got %s", q.Get("perPage"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CallLogList{
			Records: []CallLogEntry{
				{
					ID:        "log-1",
					SessionID: "sess-1",
					Direction: "Inbound",
					Action:    "Phone Call",
					Result:    "Accepted",
					StartTime: "2026-06-02T14:00:00Z",
					Duration:  120,
					From:      struct {
						PhoneNumber string `json:"phoneNumber"`
						Name        string `json:"name"`
					}{PhoneNumber: "+14045550003", Name: "John Doe"},
					To: struct {
						PhoneNumber string `json:"phoneNumber"`
						Name        string `json:"name"`
					}{PhoneNumber: "+14045550001", Name: "Bot"},
				},
			},
		})
	})
	defer srv.Close()

	opts := ListCallLogOpts{
		Direction: "Inbound",
		Type:      "Voice",
		DateFrom:  "2026-06-01T00:00:00Z",
		DateTo:    "2026-06-04T00:00:00Z",
		PerPage:   25,
	}
	list, err := client.ListCallLog(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(list.Records))
	}
	rec := list.Records[0]
	if rec.ID != "log-1" {
		t.Errorf("expected ID=log-1, got %s", rec.ID)
	}
	if rec.Direction != "Inbound" {
		t.Errorf("expected Direction=Inbound, got %s", rec.Direction)
	}
	if rec.From.PhoneNumber != "+14045550003" {
		t.Errorf("expected From.PhoneNumber=+14045550003, got %s", rec.From.PhoneNumber)
	}
	if rec.To.PhoneNumber != "+14045550001" {
		t.Errorf("expected To.PhoneNumber=+14045550001, got %s", rec.To.PhoneNumber)
	}
}

// TestListCallLog_Empty verifies that an empty records list is handled without error.
func TestListCallLog_Empty(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CallLogList{Records: []CallLogEntry{}})
	})
	defer srv.Close()

	list, err := client.ListCallLog(context.Background(), ListCallLogOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 0 {
		t.Errorf("expected 0 records, got %d", len(list.Records))
	}
}

// TestListCallLog_NoOptionalParams verifies that omitted optional params
// are not included in the query string.
func TestListCallLog_NoOptionalParams(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("direction") != "" {
			t.Errorf("expected no direction param, got %s", q.Get("direction"))
		}
		if q.Get("type") != "" {
			t.Errorf("expected no type param, got %s", q.Get("type"))
		}
		if q.Get("perPage") != "" {
			t.Errorf("expected no perPage param, got %s", q.Get("perPage"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CallLogList{Records: []CallLogEntry{}})
	})
	defer srv.Close()

	_, err := client.ListCallLog(context.Background(), ListCallLogOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestListCallLog_HTTPError verifies that a non-2xx response returns an error.
func TestListCallLog_HTTPError(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"errorCode":"OAU-101","message":"Unauthorized"}`))
	})
	defer srv.Close()

	_, err := client.ListCallLog(context.Background(), ListCallLogOpts{})
	if err == nil {
		t.Fatal("expected error for 401 response, got nil")
	}
}

// TestListCallLog_InvalidJSON verifies that an unparseable response body returns an error.
func TestListCallLog_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not valid json`))
	})
	defer srv.Close()

	_, err := client.ListCallLog(context.Background(), ListCallLogOpts{})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}
