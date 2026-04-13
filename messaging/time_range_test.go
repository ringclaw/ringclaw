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
