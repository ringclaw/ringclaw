package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/ringclaw/ringclaw/config"
	"github.com/ringclaw/ringclaw/ringcentral"
)

func TestVideoPhoneCLIEndToEndWithJWTClient(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}

	record := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		calls[name]++
	}
	requireBearer := func(t *testing.T, r *http.Request) {
		t.Helper()
		if got := r.Header.Get("Authorization"); got != "Bearer e2e-token" {
			t.Fatalf("Authorization = %q, want Bearer e2e-token", got)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/oauth/token":
			record("token")
			if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Basic ") {
				t.Fatalf("expected Basic auth token request, got %q", got)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm() error = %v", err)
			}
			if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || r.Form.Get("assertion") != "jwt" {
				t.Fatalf("unexpected token form: %s", r.Form.Encode())
			}
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "e2e-token",
				"expires_in":   3600,
			})

		case r.Method == http.MethodPost && r.URL.Path == "/rcvideo/v2/account/~/extension/~/bridges":
			record("video-create")
			requireBearer(t, r)
			var body ringcentral.CreateVideoBridgeRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode video create request: %v", err)
			}
			if body.Name != "E2E Bridge" || body.Type != "Scheduled" {
				t.Fatalf("unexpected video create body: %+v", body)
			}
			json.NewEncoder(w).Encode(ringcentral.VideoBridge{
				ID:   "bridge-e2e",
				Name: body.Name,
				Type: body.Type,
				Discovery: ringcentral.VideoBridgeDiscovery{
					Web: "https://v.ringcentral.com/join/e2e",
				},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/rcvideo/v2/account/~/extension/~/bridges":
			record("video-list")
			requireBearer(t, r)
			json.NewEncoder(w).Encode(ringcentral.VideoBridgeList{
				Records: []ringcentral.VideoBridge{{
					ID:   "bridge-e2e",
					Name: "E2E Bridge",
					Type: "Scheduled",
					Discovery: ringcentral.VideoBridgeDiscovery{
						Web: "https://v.ringcentral.com/join/e2e",
					},
				}},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/rcvideo/v2/bridges/bridge-e2e":
			record("video-get")
			requireBearer(t, r)
			json.NewEncoder(w).Encode(ringcentral.VideoBridge{
				ID:   "bridge-e2e",
				Name: "E2E Bridge",
				Type: "Scheduled",
				Discovery: ringcentral.VideoBridgeDiscovery{
					Web: "https://v.ringcentral.com/join/e2e",
				},
			})

		case r.Method == http.MethodDelete && r.URL.Path == "/rcvideo/v2/bridges/bridge-e2e":
			record("video-delete")
			requireBearer(t, r)
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPost && r.URL.Path == "/restapi/v1.0/account/~/extension/~/ring-out":
			record("ringout-create")
			requireBearer(t, r)
			var body ringcentral.CreateRingOutRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode ringout request: %v", err)
			}
			if body.From != nil || body.To.PhoneNumber != "+14155550199" || body.CallerID == nil || body.CallerID.PhoneNumber != "+14155550100" || !body.PlayPrompt {
				t.Fatalf("unexpected ringout body: %+v", body)
			}
			json.NewEncoder(w).Encode(ringcentral.RingOut{
				ID:     "ringout-e2e",
				Status: ringcentral.RingOutStatus{CallStatus: "InProgress", CallerStatus: "InProgress", CalleeStatus: "InProgress"},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/ring-out/ringout-e2e":
			record("ringout-get")
			requireBearer(t, r)
			json.NewEncoder(w).Encode(ringcentral.RingOut{
				ID:     "ringout-e2e",
				Status: ringcentral.RingOutStatus{CallStatus: "Success", CallerStatus: "Success", CalleeStatus: "Success"},
			})

		case r.Method == http.MethodDelete && r.URL.Path == "/restapi/v1.0/account/~/extension/~/ring-out/ringout-e2e":
			record("ringout-delete")
			requireBearer(t, r)
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/call-log":
			record("calllog")
			requireBearer(t, r)
			query, _ := url.ParseQuery(r.URL.RawQuery)
			if query.Get("recordCount") != "1" || query.Get("direction") != "Outbound" || query.Get("view") != "Detailed" {
				t.Fatalf("unexpected call log query: %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(ringcentral.CallLogList{
				Records: []ringcentral.CallLogRecord{{
					ID:        "call-e2e",
					StartTime: "2026-05-26T10:00:00Z",
					Direction: "Outbound",
					Result:    "Missed",
					From:      ringcentral.CallLogParty{PhoneNumber: "+14155550100"},
					To:        ringcentral.CallLogParty{PhoneNumber: "+14155550199"},
					Duration:  12,
				}},
			})

		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	cfgPath := t.TempDir() + "/config.json"
	t.Setenv("RINGCLAW_CONFIG", cfgPath)
	if err := config.SaveTo(cfgPath, &config.Config{
		RC: config.RCConfig{
			ServerURL:    srv.URL,
			ClientID:     "client",
			ClientSecret: "secret",
			JWTToken:     "jwt",
		},
	}); err != nil {
		t.Fatalf("SaveTo() error = %v", err)
	}

	oldJSON := jsonOutput
	oldVideoType := videoBridgeType
	oldCallerID := phoneCallerID
	oldPlayPrompt := phonePlayPrompt
	oldCallLogView := callLogView
	oldCallLogDirection := callLogDirection
	oldCallLogResult := callLogResult
	oldCallLogLimit := callLogLimit
	defer func() {
		jsonOutput = oldJSON
		videoBridgeType = oldVideoType
		phoneCallerID = oldCallerID
		phonePlayPrompt = oldPlayPrompt
		callLogView = oldCallLogView
		callLogDirection = oldCallLogDirection
		callLogResult = oldCallLogResult
		callLogLimit = oldCallLogLimit
	}()

	jsonOutput = false
	videoBridgeType = "Scheduled"
	if err := videoListCmd.RunE(videoListCmd, nil); err != nil {
		t.Fatalf("video list RunE() error = %v", err)
	}
	if err := videoCreateCmd.RunE(videoCreateCmd, []string{"E2E", "Bridge"}); err != nil {
		t.Fatalf("video create RunE() error = %v", err)
	}
	if err := videoGetCmd.RunE(videoGetCmd, []string{"bridge-e2e"}); err != nil {
		t.Fatalf("video get RunE() error = %v", err)
	}
	if err := videoDeleteCmd.RunE(videoDeleteCmd, []string{"bridge-e2e"}); err != nil {
		t.Fatalf("video delete RunE() error = %v", err)
	}

	phoneCallerID = "+14155550100"
	phoneFrom = ""
	phonePlayPrompt = true
	if err := phoneRingOutCmd.RunE(phoneRingOutCmd, []string{"+14155550199"}); err != nil {
		t.Fatalf("phone ringout RunE() error = %v", err)
	}
	if err := phoneStatusCmd.RunE(phoneStatusCmd, []string{"ringout-e2e"}); err != nil {
		t.Fatalf("phone status RunE() error = %v", err)
	}
	if err := phoneCancelCmd.RunE(phoneCancelCmd, []string{"ringout-e2e"}); err != nil {
		t.Fatalf("phone cancel RunE() error = %v", err)
	}

	callLogView = "Detailed"
	callLogDirection = "Outbound"
	callLogResult = "Missed"
	callLogLimit = 1
	if err := phoneCallLogCmd.RunE(phoneCallLogCmd, nil); err != nil {
		t.Fatalf("phone calllog RunE() error = %v", err)
	}

	for _, name := range []string{"video-list", "video-create", "video-get", "video-delete", "ringout-create", "ringout-get", "ringout-delete", "calllog"} {
		if calls[name] != 1 {
			t.Fatalf("calls[%s] = %d, want 1; all calls=%#v", name, calls[name], calls)
		}
	}
	if calls["token"] != 8 {
		t.Fatalf("token calls = %d, want 8; all calls=%#v", calls["token"], calls)
	}
}

func TestPhoneCallLogJSONAppliesResultFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/restapi/oauth/token":
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "e2e-token",
				"expires_in":   3600,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/restapi/v1.0/account/~/extension/~/call-log":
			json.NewEncoder(w).Encode(ringcentral.CallLogList{
				Records: []ringcentral.CallLogRecord{
					{ID: "missed-1", Result: "Missed"},
					{ID: "answered-1", Result: "Call connected"},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	cfgPath := t.TempDir() + "/config.json"
	t.Setenv("RINGCLAW_CONFIG", cfgPath)
	if err := config.SaveTo(cfgPath, &config.Config{
		RC: config.RCConfig{
			ServerURL:    srv.URL,
			ClientID:     "client",
			ClientSecret: "secret",
			JWTToken:     "jwt",
		},
	}); err != nil {
		t.Fatalf("SaveTo() error = %v", err)
	}

	oldJSON := jsonOutput
	oldCallLogView := callLogView
	oldCallLogDirection := callLogDirection
	oldCallLogType := callLogType
	oldCallLogResult := callLogResult
	oldCallLogLimit := callLogLimit
	defer func() {
		jsonOutput = oldJSON
		callLogView = oldCallLogView
		callLogDirection = oldCallLogDirection
		callLogType = oldCallLogType
		callLogResult = oldCallLogResult
		callLogLimit = oldCallLogLimit
	}()

	jsonOutput = true
	callLogView = "Simple"
	callLogDirection = ""
	callLogType = ""
	callLogResult = "Missed"
	callLogLimit = 10

	output := captureStdout(t, func() error {
		return phoneCallLogCmd.RunE(phoneCallLogCmd, nil)
	})
	var got ringcentral.CallLogList
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, output)
	}
	if len(got.Records) != 1 || got.Records[0].ID != "missed-1" {
		t.Fatalf("expected JSON output to include only missed calls, got %+v", got.Records)
	}
}

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = old
	}()

	runErr := fn()
	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("close stdout pipe: %v", closeErr)
	}
	if runErr != nil {
		t.Fatalf("captured function error = %v", runErr)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	return string(data)
}
