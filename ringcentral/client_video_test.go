package ringcentral

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCreateVideoBridge_RequestAndResponse(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/rcvideo/v2/account/~/extension/~/bridges" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body CreateVideoBridgeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Name != "Design review" || body.Type != "Scheduled" {
			t.Fatalf("unexpected request body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(VideoBridge{
			ID:   "bridge-1",
			Name: "Design review",
			Type: "Scheduled",
			Discovery: VideoBridgeDiscovery{
				Web: "https://v.ringcentral.com/join/123",
			},
		})
	})
	defer srv.Close()

	bridge, err := client.CreateVideoBridge(context.Background(), &CreateVideoBridgeRequest{
		Name: "Design review",
		Type: "Scheduled",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bridge.ID != "bridge-1" || bridge.Discovery.Web == "" {
		t.Fatalf("unexpected bridge: %+v", bridge)
	}
}

func TestGetAndDeleteVideoBridge(t *testing.T) {
	var sawDelete bool
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rcvideo/v2/bridges/bridge-1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(VideoBridge{ID: "bridge-1", Name: "Standup"})
		case http.MethodDelete:
			sawDelete = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	})
	defer srv.Close()

	bridge, err := client.GetVideoBridge(context.Background(), "bridge-1")
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if bridge.Name != "Standup" {
		t.Fatalf("unexpected bridge name: %s", bridge.Name)
	}
	if err := client.DeleteVideoBridge(context.Background(), "bridge-1"); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if !sawDelete {
		t.Fatal("expected DELETE request")
	}
}
