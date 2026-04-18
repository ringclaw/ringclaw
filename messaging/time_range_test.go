package messaging

import (
	"testing"
	"time"
)

func TestParseAbsoluteDate_Chinese(t *testing.T) {
	now := time.Now()
	tests := []struct {
		input     string
		wantMonth time.Month
		wantDay   int
	}{
		{"总结4月10日的消息", time.April, 10},
		{"总结 4 月 10 日的消息", time.April, 10},
		{"总结4月10号的消息", time.April, 10},
		{"12月25日", time.December, 25},
		{"1月1日", time.January, 1},
	}
	for _, tt := range tests {
		got := parseTimeRange(tt.input)
		if got.Month() != tt.wantMonth || got.Day() != tt.wantDay || got.Year() != now.Year() {
			t.Errorf("parseTimeRange(%q) = %v, want %d-%02d-%02d", tt.input, got.Format("2006-01-02"), now.Year(), tt.wantMonth, tt.wantDay)
		}
	}
}

func TestParseAbsoluteDate_English(t *testing.T) {
	now := time.Now()
	tests := []struct {
		input     string
		wantMonth time.Month
		wantDay   int
	}{
		{"summarize messages from April 10", time.April, 10},
		{"summarize Apr 10th messages", time.April, 10},
		{"messages from January 1st", time.January, 1},
		{"messages from december 25", time.December, 25},
		{"Feb 14th", time.February, 14},
		{"March 3rd", time.March, 3},
	}
	for _, tt := range tests {
		got := parseTimeRange(tt.input)
		if got.Month() != tt.wantMonth || got.Day() != tt.wantDay || got.Year() != now.Year() {
			t.Errorf("parseTimeRange(%q) = %v, want %d-%02d-%02d", tt.input, got.Format("2006-01-02"), now.Year(), tt.wantMonth, tt.wantDay)
		}
	}
}

func TestParseAbsoluteDate_Slash(t *testing.T) {
	now := time.Now()
	tests := []struct {
		input     string
		wantMonth time.Month
		wantDay   int
	}{
		{"4/10", time.April, 10},
		{"04/10", time.April, 10},
		{"12/25", time.December, 25},
	}
	for _, tt := range tests {
		got := parseTimeRange(tt.input)
		if got.Month() != tt.wantMonth || got.Day() != tt.wantDay || got.Year() != now.Year() {
			t.Errorf("parseTimeRange(%q) = %v, want %d-%02d-%02d", tt.input, got.Format("2006-01-02"), now.Year(), tt.wantMonth, tt.wantDay)
		}
	}
}

func TestParseAbsoluteDate_ISO(t *testing.T) {
	got := parseTimeRange("2026-04-10")
	if got.Year() != 2026 || got.Month() != time.April || got.Day() != 10 {
		t.Errorf("parseTimeRange(2026-04-10) = %v, want 2026-04-10", got.Format("2006-01-02"))
	}
}

func TestParseTimeRange_RelativeStillWorks(t *testing.T) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterday := today.AddDate(0, 0, -1)

	got := parseTimeRange("昨天")
	if got.Year() != yesterday.Year() || got.Month() != yesterday.Month() || got.Day() != yesterday.Day() {
		t.Errorf("parseTimeRange(昨天) = %v, want %v", got.Format("2006-01-02"), yesterday.Format("2006-01-02"))
	}

	got2 := parseTimeRange("yesterday")
	if got2.Year() != yesterday.Year() || got2.Month() != yesterday.Month() || got2.Day() != yesterday.Day() {
		t.Errorf("parseTimeRange(yesterday) = %v, want %v", got2.Format("2006-01-02"), yesterday.Format("2006-01-02"))
	}
}

func TestParseTimeRange_NoDate(t *testing.T) {
	got := parseTimeRange("总结一下消息")
	today := todayStart()
	if !got.Equal(today) {
		t.Errorf("parseTimeRange with no date = %v, want today %v", got.Format("2006-01-02"), today.Format("2006-01-02"))
	}
}

// TestParseTimeRange_RelativeDuration covers the Chinese-numeral / 周 / 个月
// durations that previously fell through to the broad "最近 → -3d" rule.
// In particular, "最近一周" must resolve to ~7 days back, not 3.
func TestParseTimeRange_RelativeDuration(t *testing.T) {
	now := time.Now()
	cases := []struct {
		input    string
		wantDays int // expected (now - result) in whole days, ±1 day tolerance
	}{
		{"总结最近一周的聊天", 7},
		{"总结一下![:Team](158994374662) 最近一周的聊天", 7},
		{"过去一周的消息", 7},
		{"最近两周的消息", 14},
		{"过去 7 天的活动", 7},
		{"最近30天", 30},
		{"最近三天", 3},
		{"最近一个月", 30},
		{"过去两个月", 60},
		{"last 3 days summary", 3},
		{"past 2 weeks summary", 14},
		{"最近的一周", 7}, // optional "的" between prefix and count
	}
	for _, tc := range cases {
		got := parseTimeRange(tc.input)
		diffDays := int(now.Sub(got).Hours()/24 + 0.5)
		// Allow ±1 day slack: keyword rules snap to midnight, duration
		// rules preserve time-of-day, and "last week" lands on a Monday.
		if diffDays < tc.wantDays-1 || diffDays > tc.wantDays+1 {
			t.Errorf("parseTimeRange(%q) → %s (≈%d days back), want ≈%d", tc.input, got.Format("2006-01-02 15:04"), diffDays, tc.wantDays)
		}
	}
}

func TestParseTimeRange_RelativeHours(t *testing.T) {
	now := time.Now()
	cases := []struct {
		input     string
		wantHours int
	}{
		{"最近2小时", 2},
		{"过去三个小时", 3},
		{"last 5 hours", 5},
	}
	for _, tc := range cases {
		got := parseTimeRange(tc.input)
		diffHours := int(now.Sub(got).Hours() + 0.5)
		if diffHours < tc.wantHours-1 || diffHours > tc.wantHours+1 {
			t.Errorf("parseTimeRange(%q) → %s (≈%dh back), want ≈%dh", tc.input, got.Format("2006-01-02 15:04"), diffHours, tc.wantHours)
		}
	}
}

// TestParseTimeRange_ZuiJinAlone makes sure the bare "最近 / recently" keyword
// still resolves to ~3 days back when no explicit duration follows.
func TestParseTimeRange_ZuiJinAlone(t *testing.T) {
	now := time.Now()
	got := parseTimeRange("最近的群聊有什么")
	diffDays := int(now.Sub(got).Hours()/24 + 0.5)
	if diffDays < 2 || diffDays > 4 {
		t.Errorf("parseTimeRange(最近的群聊有什么) → %d days back, want ≈3", diffDays)
	}
}
