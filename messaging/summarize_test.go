package messaging

import (
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
)

func TestIsSummarizeCommand(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"总结一下跟张三的聊天", true},
		{"summarize last 3 days", true},
		{"summary of this week", true},
		{"hello world", false},
		{"  总结 today", true},
		{"SUMMARIZE chat", true},
	}
	for _, tt := range tests {
		got := IsSummarizeCommand(tt.text)
		if got != tt.want {
			t.Errorf("IsSummarizeCommand(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestParseTimeRange_LastNDays(t *testing.T) {
	now := time.Now()
	result := parseTimeRange("最近3天的消息")
	diff := now.Sub(result)
	if diff < 2*24*time.Hour || diff > 4*24*time.Hour {
		t.Errorf("expected ~3 days ago, got %v ago", diff)
	}
}

func TestParseTimeRange_LastNCNDays(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		text string
		minD time.Duration
		maxD time.Duration
	}{
		{"总结我跟 Yuki Chen 最近十天的聊天", 9 * 24 * time.Hour, 11 * 24 * time.Hour},
		{"最近两天的消息", 1 * 24 * time.Hour, 3 * 24 * time.Hour},
		{"过去十五天", 14 * 24 * time.Hour, 16 * 24 * time.Hour},
		{"最近二十三天", 22 * 24 * time.Hour, 24 * 24 * time.Hour},
	} {
		result := parseTimeRange(tc.text)
		diff := now.Sub(result)
		if diff < tc.minD || diff > tc.maxD {
			t.Errorf("parseTimeRange(%q): diff %v want between %v and %v", tc.text, diff, tc.minD, tc.maxD)
		}
	}
}

func TestChineseNumeralToInt(t *testing.T) {
	tests := []struct {
		s    string
		want int
		ok   bool
	}{
		{"十", 10, true},
		{"两", 2, true},
		{"十一", 11, true},
		{"二十三", 23, true},
		{"一百", 100, true},
		{"", 0, false},
	}
	for _, tt := range tests {
		got, ok := chineseNumeralToInt(tt.s)
		if ok != tt.ok || got != tt.want {
			t.Errorf("chineseNumeralToInt(%q) = (%d, %v), want (%d, %v)", tt.s, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParseTimeRange_LastNHours(t *testing.T) {
	now := time.Now()
	result := parseTimeRange("last 2 hours")
	diff := now.Sub(result)
	if diff < 1*time.Hour || diff > 3*time.Hour {
		t.Errorf("expected ~2 hours ago, got %v ago", diff)
	}
}

func TestParseTimeRange_ThisWeek(t *testing.T) {
	result := parseTimeRange("本周的消息")
	if result.Weekday() != time.Monday {
		// Could be Sunday depending on locale, just check it's within this week
		now := time.Now()
		if now.Sub(result) > 7*24*time.Hour {
			t.Errorf("expected within this week, got %v", result)
		}
	}
}

func TestParseTimeRange_Yesterday(t *testing.T) {
	now := time.Now()
	result := parseTimeRange("昨天的消息")
	diff := now.Sub(result)
	if diff < 12*time.Hour || diff > 48*time.Hour {
		t.Errorf("expected ~1 day ago, got %v ago", diff)
	}
}

func TestParseTimeRange_Default(t *testing.T) {
	result := parseTimeRange("some random text")
	today := todayStart()
	if !result.Equal(today) {
		t.Errorf("expected today start %v, got %v", today, result)
	}
}

func TestExtractNameFromText(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{"总结一下跟张三的聊天", "张三"},
		{"summarize chat with John", "john"},
		// Logged failures: "记录" left after removing 聊天; English name + 今天的聊天记录
		{"总结下我和 Strong Luo 今天的聊天记录", "strong luo"},
		{"总结和 Holgie Wei 最近2天的聊天记录", "holgie wei"},
		{"总结和 holgie.wei@ringcentral.com 最近2天的聊天记录", "holgie.wei@ringcentral.com"},
		// Team name without mention: strip 这个群 / 天 / 聊天 debris
		{"总结GSP/Partners Teams DEV+QA+SDET 这个群最近2天的聊天", "gsp/partners teams dev+qa+sdet"},
		// Chinese numeral duration: "两天" must not leave trailing "两"
		{"总结我和 Strong Luo 最近两天的聊天记录", "strong luo"},
	}
	for _, tt := range tests {
		got := extractNameFromText(tt.text)
		if got != tt.want {
			t.Errorf("extractNameFromText(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

func TestNameTokensAllPresent(t *testing.T) {
	if !nameTokensAllPresent("Yuki Chen", "yuki chen") {
		t.Error("expected both tokens to match as words")
	}
	if nameTokensAllPresent("Yukio Chen", "yuki chen") {
		t.Error("yukio is not the whole word yuki")
	}
	if !nameTokensAllPresent("Yuki Chen", "chen") {
		t.Error("single surname token")
	}
}

func TestDirectorySearchQueries(t *testing.T) {
	q := directorySearchQueries("yuki chen")
	if len(q) < 2 {
		t.Fatalf("expected full name + tokens, got %v", q)
	}
	if q[0] != "yuki chen" {
		t.Errorf("want full name first, got %v", q)
	}
}

func TestPickBestDirectoryEntry(t *testing.T) {
	entries := []ringcentral.DirectoryEntry{
		{ID: "1", FirstName: "Yukio", LastName: "Test", Email: "a@a.com"},
		{ID: "2", FirstName: "Yuki", LastName: "Chen", Email: "y@c.com"},
	}
	best := pickBestDirectoryEntry(entries, "yuki chen")
	if best == nil || best.ID != "2" {
		t.Fatalf("got %+v", best)
	}
	if pickBestDirectoryEntry(entries, "nobody here") != nil {
		t.Fatal("expected no match")
	}
}

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		haystack string
		needle   string
		want     bool
	}{
		{"John Smith", "john", true},
		{"John Smith", "smith", true},
		{"张三", "张三", true},
		{"hello", "world", false},
		{"hello", "", false},
	}
	for _, tt := range tests {
		got := fuzzyMatch(tt.haystack, tt.needle)
		if got != tt.want {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", tt.haystack, tt.needle, got, tt.want)
		}
	}
}

func TestFormatTimeDesc(t *testing.T) {
	today := todayStart()
	if got := formatTimeDesc(today); got != "today" {
		t.Errorf("formatTimeDesc(today) = %q, want %q", got, "today")
	}

	yesterday := time.Now().Add(-36 * time.Hour)
	got := formatTimeDesc(yesterday)
	if got != "since yesterday" {
		t.Errorf("formatTimeDesc(yesterday) = %q, want %q", got, "since yesterday")
	}

	threeDaysAgo := time.Now().Add(-72 * time.Hour)
	got = formatTimeDesc(threeDaysAgo)
	if got != "last 3 days" {
		t.Errorf("formatTimeDesc(3 days ago) = %q, want %q", got, "last 3 days")
	}
}
