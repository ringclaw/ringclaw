package ringcentral

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCreateRingOut_RequestAndResponse(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/restapi/v1.0/account/~/extension/~/ring-out" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body CreateRingOutRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.From == nil || body.From.PhoneNumber != "+14155550100" || body.To.PhoneNumber != "+14155550199" {
			t.Fatalf("unexpected request body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RingOut{
			ID: "ringout-1",
			Status: RingOutStatus{
				CallStatus:   "InProgress",
				CallerStatus: "InProgress",
				CalleeStatus: "InProgress",
			},
		})
	})
	defer srv.Close()

	ringOut, err := client.CreateRingOut(context.Background(), &CreateRingOutRequest{
		From: &PhoneNumberRef{PhoneNumber: "+14155550100"},
		To:   PhoneNumberRef{PhoneNumber: "+14155550199"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ringOut.ID != "ringout-1" || ringOut.Status.CallStatus != "InProgress" {
		t.Fatalf("unexpected ringout: %+v", ringOut)
	}
}

func TestCreateRingOut_OmitsFromWhenCurrentTokenIdentityShouldBeUsed(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, ok := body["from"]; ok {
			t.Fatalf("from should be omitted when caller relies on current token identity/default callback: %+v", body["from"])
		}
		to, _ := body["to"].(map[string]any)
		if to["phoneNumber"] != "+14155550199" {
			t.Fatalf("unexpected request body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RingOut{ID: "ringout-1"})
	})
	defer srv.Close()

	if _, err := client.CreateRingOut(context.Background(), &CreateRingOutRequest{
		To: PhoneNumberRef{PhoneNumber: "+14155550199"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListExtensionPhoneNumbers(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/restapi/v1.0/account/~/extension/~/phone-number" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ExtensionPhoneNumberList{
			Records: []ExtensionPhoneNumber{{
				ID:          904688021,
				PhoneNumber: "+14155550100",
				UsageType:   "DirectNumber",
				Type:        "VoiceFax",
				Status:      "Normal",
			}},
		})
	})
	defer srv.Close()

	list, err := client.ListExtensionPhoneNumbers(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 1 || list.Records[0].PhoneNumber != "+14155550100" {
		t.Fatalf("unexpected phone number list: %+v", list)
	}
}

func TestListForwardingNumbers(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/restapi/v1.0/account/~/extension/~/forwarding-number" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ForwardingNumberList{
			Records: []ForwardingNumber{{
				ID:          904688021,
				PhoneNumber: "+14155550100",
				Label:       "My mobile",
				Type:        "Other",
				Features:    []string{"RingOut"},
			}},
		})
	})
	defer srv.Close()

	list, err := client.ListForwardingNumbers(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 1 || list.Records[0].PhoneNumber != "+14155550100" || list.Records[0].Label != "My mobile" {
		t.Fatalf("unexpected forwarding number list: %+v", list)
	}
}

func TestListExtensionCallLog_Query(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/restapi/v1.0/account/~/extension/~/call-log" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("view") != "Detailed" || q.Get("direction") != "Outbound" || q.Get("recordCount") != "10" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CallLogList{
			Records: []CallLogRecord{{
				ID:        "call-1",
				SessionID: "session-1",
				Direction: "Outbound",
				Duration:  42,
			}},
		})
	})
	defer srv.Close()

	logs, err := client.ListExtensionCallLog(context.Background(), CallLogOptions{
		View:        "Detailed",
		Direction:   "Outbound",
		RecordCount: 10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs.Records) != 1 || logs.Records[0].Duration != 42 {
		t.Fatalf("unexpected call logs: %+v", logs)
	}
}

func TestListExtensionCallLog_UsesExplicitExtension(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/restapi/v1.0/account/~/extension/user-1/call-log" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CallLogList{})
	})
	defer srv.Close()

	if _, err := client.ListExtensionCallLog(context.Background(), CallLogOptions{ExtensionID: "user-1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetAndDeleteRingOut(t *testing.T) {
	var sawDelete bool
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/restapi/v1.0/account/~/extension/~/ring-out/ringout-1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(RingOut{ID: "ringout-1", Status: RingOutStatus{CallStatus: "Success"}})
		case http.MethodDelete:
			sawDelete = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	})
	defer srv.Close()

	ringOut, err := client.GetRingOut(context.Background(), "ringout-1")
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if ringOut.Status.CallStatus != "Success" {
		t.Fatalf("unexpected status: %+v", ringOut)
	}
	if err := client.DeleteRingOut(context.Background(), "ringout-1"); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if !sawDelete {
		t.Fatal("expected DELETE request")
	}
}
