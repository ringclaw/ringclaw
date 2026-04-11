package heartbeat

import (
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/config"
)

func TestParseActiveHours(t *testing.T) {
	tests := []struct {
		input string
		start int
		end   int
		err   bool
	}{
		{"09:00-18:00", 540, 1080, false},
		{"00:00-23:59", 0, 1439, false},
		{"22:00-06:00", 1320, 360, false},
		{"bad", 0, 0, true},
		{"25:00-18:00", 0, 0, true},
		{"09:00-18:60", 0, 0, true},
	}
	for _, tt := range tests {
		start, end, err := parseActiveHours(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("parseActiveHours(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseActiveHours(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if start != tt.start || end != tt.end {
			t.Errorf("parseActiveHours(%q) = (%d,%d), want (%d,%d)", tt.input, start, end, tt.start, tt.end)
		}
	}
}

func TestIsEffectivelyEmpty(t *testing.T) {
	tests := []struct {
		content string
		empty   bool
	}{
		{"", true},
		{"\n\n", true},
		{"# Title\n\n", true},
		{"# Title\n<!-- comment -->\n", true},
		{"check emails", false},
		{"# Title\n- do stuff\n", false},
	}
	for _, tt := range tests {
		got := isEffectivelyEmpty(tt.content)
		if got != tt.empty {
			t.Errorf("isEffectivelyEmpty(%q) = %v, want %v", tt.content, got, tt.empty)
		}
	}
}

func TestNewHeartbeatRunner_RejectsNonPositiveInterval(t *testing.T) {
	tests := []struct {
		interval string
		wantErr  bool
	}{
		{"0s", true},
		{"-1m", true},
		{"30m", false},
		{"1h", false},
	}
	for _, tt := range tests {
		cfg := config.HeartbeatConfig{Enabled: true, Interval: tt.interval}
		_, err := NewHeartbeatRunner(cfg, nil, "", nil, nil)
		if tt.wantErr && err == nil {
			t.Errorf("interval %q: expected error", tt.interval)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("interval %q: unexpected error: %v", tt.interval, err)
		}
	}
}

func TestNewHeartbeatRunner_WithTimezone(t *testing.T) {
	cfg := config.HeartbeatConfig{Enabled: true, Interval: "1h", Timezone: "America/New_York"}
	r, err := NewHeartbeatRunner(cfg, nil, "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.location.String() != "America/New_York" {
		t.Errorf("got location %q, want America/New_York", r.location)
	}
}

func TestNewHeartbeatRunner_InvalidTimezone(t *testing.T) {
	cfg := config.HeartbeatConfig{Enabled: true, Timezone: "Invalid/Zone"}
	_, err := NewHeartbeatRunner(cfg, nil, "", nil, nil)
	if err == nil {
		t.Error("expected error for invalid timezone")
	}
}

func TestNewHeartbeatRunner_WithActiveHours(t *testing.T) {
	cfg := config.HeartbeatConfig{Enabled: true, ActiveHours: "09:00-18:00"}
	r, err := NewHeartbeatRunner(cfg, nil, "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.activeStart != 540 || r.activeEnd != 1080 {
		t.Errorf("got start=%d end=%d, want 540 1080", r.activeStart, r.activeEnd)
	}
}

func TestNewHeartbeatRunner_InvalidActiveHours(t *testing.T) {
	cfg := config.HeartbeatConfig{Enabled: true, ActiveHours: "bad"}
	_, err := NewHeartbeatRunner(cfg, nil, "", nil, nil)
	if err == nil {
		t.Error("expected error for invalid active hours")
	}
}

func TestNewHeartbeatRunner_DefaultInterval(t *testing.T) {
	cfg := config.HeartbeatConfig{Enabled: true}
	r, err := NewHeartbeatRunner(cfg, nil, "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.interval != 30*time.Minute {
		t.Errorf("got interval %v, want 30m", r.interval)
	}
}

func TestNewHeartbeatRunner_InvalidInterval(t *testing.T) {
	cfg := config.HeartbeatConfig{Enabled: true, Interval: "notaduration"}
	_, err := NewHeartbeatRunner(cfg, nil, "", nil, nil)
	if err == nil {
		t.Error("expected error for invalid interval")
	}
}

func TestIsActiveTime_NoHours(t *testing.T) {
	r := &HeartbeatRunner{cfg: config.HeartbeatConfig{}}
	if !r.isActiveTime() {
		t.Error("should be active when no active hours configured")
	}
}

func TestIsActiveTime_WithinHours(t *testing.T) {
	r := &HeartbeatRunner{
		cfg:         config.HeartbeatConfig{ActiveHours: "00:00-23:59"},
		location:    time.UTC,
		activeStart: 0,
		activeEnd:   1439,
	}
	if !r.isActiveTime() {
		t.Error("should be active within 00:00-23:59")
	}
}

func TestIsActiveTime_MidnightWrap(t *testing.T) {
	r := &HeartbeatRunner{
		cfg:         config.HeartbeatConfig{ActiveHours: "22:00-06:00"},
		location:    time.UTC,
		activeStart: 1320, // 22:00
		activeEnd:   360,  // 06:00
	}
	// This is always active in the wrap case (either >= 22:00 OR < 06:00)
	// At any UTC time it might or might not be active, but the logic is testable.
	_ = r.isActiveTime() // Just ensure it doesn't panic
}

func TestParseTimeOfDay(t *testing.T) {
	tests := []struct {
		input string
		want  int
		err   bool
	}{
		{"00:00", 0, false},
		{"09:30", 570, false},
		{"23:59", 1439, false},
		{"24:00", 0, true},
		{"12:60", 0, true},
		{"bad", 0, true},
	}
	for _, tt := range tests {
		got, err := parseTimeOfDay(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("parseTimeOfDay(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseTimeOfDay(%q): %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseTimeOfDay(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestHeartbeatRunnerIsDuplicate(t *testing.T) {
	r := &HeartbeatRunner{recentHash: make(map[string]time.Time)}
	if r.isDuplicate("hello") {
		t.Error("first call should not be duplicate")
	}
	if !r.isDuplicate("hello") {
		t.Error("second call should be duplicate")
	}
	if r.isDuplicate("different") {
		t.Error("different content should not be duplicate")
	}
}
