package ringcentral

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestListVideoBridges(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/rcvideo/v2/account/~/extension/~/bridges" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(VideoBridgeList{
			Records: []VideoBridge{{
				ID:   "bridge-1",
				Name: "Design review",
				Type: "Scheduled",
				Discovery: VideoBridgeDiscovery{
					Web: "https://v.ringcentral.com/join/123",
				},
			}},
		})
	})
	defer srv.Close()

	list, err := client.ListVideoBridges(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 1 || list.Records[0].ID != "bridge-1" {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestDefaultVideoServerURL(t *testing.T) {
	if got := defaultVideoServerURL("https://platform.ringcentral.com"); got != "https://api-meet.ringcentral.com" {
		t.Fatalf("defaultVideoServerURL production = %q", got)
	}
	if got := defaultVideoServerURL("http://127.0.0.1:8080"); got != "http://127.0.0.1:8080" {
		t.Fatalf("defaultVideoServerURL local = %q", got)
	}
}

func TestListVideoMeetingHistory(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/rcvideo/v1/history/meetings" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("type") != "All" || q.Get("perPage") != "25" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(VideoMeetingHistoryList{
			Meetings: []VideoMeetingHistory{{
				ID:          "meeting-1",
				DisplayName: "Customer sync",
				StartTime:   "2026-06-01T10:00:00Z",
				Status:      "Done",
				Duration:    1800,
				HostInfo:    VideoMeetingParticipant{DisplayName: "John Doe"},
			}},
		})
	})
	defer srv.Close()

	list, err := client.ListVideoMeetingHistory(context.Background(), VideoMeetingHistoryOptions{
		Type:    "All",
		PerPage: 25,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Meetings) != 1 || list.Meetings[0].ID != "meeting-1" {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestListCloudCalendars(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/restapi/v1.0/account/~/extension/~/cloud-calendars/ucc" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("sync") != "true" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CloudCalendarList{
			Records: []CloudCalendar{{
				ID:         "cal-1",
				ProviderID: "office365",
				CalendarID: "cal-1",
				Name:       "Work",
				Connected:  true,
				Primary:    true,
			}},
		})
	})
	defer srv.Close()

	list, err := client.ListCloudCalendars(context.Background(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 1 || list.Records[0].ProviderID != "office365" {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestListCloudCalendarEvents(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/restapi/v1.0/account/~/extension/~/cloud-calendars/ucc/office365/~/cal-1/events" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("startTimeFrom") != "2026-06-01T00:00:00Z" ||
			q.Get("startTimeTo") != "2026-06-02T00:00:00Z" ||
			q.Get("includeNonRcEvents") != "true" ||
			q.Get("perPage") != "100" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CloudCalendarEventList{
			Records: []CloudCalendarEvent{{
				ID:          "event-1",
				Subject:     "Customer escalation review",
				Description: "Review blockers.",
				StartTime:   "2026-06-01T10:00:00Z",
				EndTime:     "2026-06-01T10:30:00Z",
			}},
		})
	})
	defer srv.Close()

	list, err := client.ListCloudCalendarEvents(context.Background(), "office365", "cal-1", CloudCalendarEventOptions{
		StartTimeFrom:      "2026-06-01T00:00:00Z",
		StartTimeTo:        "2026-06-02T00:00:00Z",
		IncludeNonRCEvents: true,
		PerPage:            100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Records) != 1 || list.Records[0].Subject != "Customer escalation review" {
		t.Fatalf("unexpected list: %+v", list)
	}
}

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
