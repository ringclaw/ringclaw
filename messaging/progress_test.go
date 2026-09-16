package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
)

func TestProgressMessage(t *testing.T) {
	tests := []struct {
		name     string
		activity string
		elapsed  time.Duration
		want     string
	}{
		{"with activity", "bash · git status", 45 * time.Second, "⏳ 正在执行：bash · git status（已 45s）"},
		{"no activity", "", 2 * time.Minute, "⏳ 正在处理…（已 2m）"},
		{"minutes and seconds", "read", 130 * time.Second, "⏳ 正在执行：read（已 2m10s）"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := progressMessage(tt.activity, tt.elapsed); got != tt.want {
				t.Errorf("progressMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShortDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{60 * time.Second, "1m"},
		{90 * time.Second, "1m30s"},
		{2 * time.Hour, "120m"},
	}
	for _, tt := range tests {
		if got := shortDuration(tt.in); got != tt.want {
			t.Errorf("shortDuration(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestProgressLoopUpdatesPlaceholder(t *testing.T) {
	var updates int32
	var lastText atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		lastText.Store(req["text"])
		atomic.AddInt32(&updates, 1)
		_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1", Text: req["text"]})
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	h := NewHandler(nil, nil, "test")
	h.SetProgressConfig(true, 20*time.Millisecond)

	var latest atomic.Value
	latest.Store("bash · git status")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.progressLoop(ctx, client, "chat-1", "post-1", &latest, make(chan struct{}))
	}()

	time.Sleep(90 * time.Millisecond)
	cancel()
	<-done

	if got := atomic.LoadInt32(&updates); got == 0 {
		t.Fatal("expected at least one progress update")
	}
	if got, _ := lastText.Load().(string); got == "" {
		t.Fatal("expected placeholder text to be updated")
	}
}

func TestStartProgress_DisabledIsNoop(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	h.SetProgressConfig(false, time.Second)

	var latest atomic.Value
	stop := h.startProgress(context.Background(), nil, "chat-1", "post-1", &latest)
	stop() // must not panic or block
}

func TestStartProgress_StopIsIdempotent(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	h.SetProgressConfig(true, time.Hour) // long interval: nothing fires during the test

	var latest atomic.Value
	stop := h.startProgress(context.Background(), nil, "chat-1", "post-1", &latest)
	stop()
	stop() // second call must be safe
}

func TestProgressActiveDefault(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	if !h.progressActive() {
		t.Fatal("progress should be enabled by default")
	}
	if got := h.progressIntervalDuration(); got != 60*time.Second {
		t.Fatalf("default interval = %v, want 60s", got)
	}
}
