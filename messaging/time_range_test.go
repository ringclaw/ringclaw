package messaging

import (
	"strings"
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

// --- Weekday resolution tests ---
//
// resolveWeekday is exercised directly against synthetic `now` values so the
// math is verified independently of the host clock. parseTimeRange wrappers
// then assert the regex/dispatch glue is wired up correctly.

func TestResolveWeekday_BareUsesMostRecentPast(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	// Sunday 2026-04-26.
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, loc)
	cases := []struct {
		target    int
		wantMonth time.Month
		wantDay   int
	}{
		{1, time.April, 20}, // Mon → 6 days back
		{2, time.April, 21}, // Tue
		{3, time.April, 22}, // Wed
		{4, time.April, 23}, // Thu
		{5, time.April, 24}, // Fri
		{6, time.April, 25}, // Sat
		{7, time.April, 26}, // Sun → today
	}
	for _, c := range cases {
		got := resolveWeekday(wkpNone, c.target, now)
		if got.Month() != c.wantMonth || got.Day() != c.wantDay {
			t.Errorf("resolveWeekday(none, %d, Sun 4/26) = %s, want 2026-%02d-%02d",
				c.target, got.Format("2006-01-02"), c.wantMonth, c.wantDay)
		}
	}
}

func TestResolveWeekday_LastAlwaysPreviousWeek(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, loc) // Sun
	cases := []struct {
		target    int
		wantMonth time.Month
		wantDay   int
	}{
		{1, time.April, 13}, // Mon of last week
		{5, time.April, 17}, // Fri of last week
		{7, time.April, 19}, // Sun of last week
	}
	for _, c := range cases {
		got := resolveWeekday(wkpLast, c.target, now)
		if got.Month() != c.wantMonth || got.Day() != c.wantDay {
			t.Errorf("resolveWeekday(last, %d, Sun 4/26) = %s, want 2026-%02d-%02d",
				c.target, got.Format("2006-01-02"), c.wantMonth, c.wantDay)
		}
	}
}

func TestResolveWeekday_NextAlwaysNextWeek(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, loc) // Sun
	cases := []struct {
		target    int
		wantMonth time.Month
		wantDay   int
	}{
		{1, time.April, 27}, // Mon of next week
		{5, time.May, 1},    // Fri of next week
		{7, time.May, 3},    // Sun of next week
	}
	for _, c := range cases {
		got := resolveWeekday(wkpNext, c.target, now)
		if got.Month() != c.wantMonth || got.Day() != c.wantDay {
			t.Errorf("resolveWeekday(next, %d, Sun 4/26) = %s, want 2026-%02d-%02d",
				c.target, got.Format("2006-01-02"), c.wantMonth, c.wantDay)
		}
	}
}

func TestResolveWeekday_ThisCalendarWeek(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	// Wed 2026-04-22. Week is Mon 4/20 – Sun 4/26.
	now := time.Date(2026, 4, 22, 12, 0, 0, 0, loc)
	cases := []struct {
		target    int
		wantMonth time.Month
		wantDay   int
	}{
		{1, time.April, 20}, // Mon (past)
		{3, time.April, 22}, // Wed (today)
		{5, time.April, 24}, // Fri (future this week)
		{7, time.April, 26}, // Sun (future this week)
	}
	for _, c := range cases {
		got := resolveWeekday(wkpThis, c.target, now)
		if got.Month() != c.wantMonth || got.Day() != c.wantDay {
			t.Errorf("resolveWeekday(this, %d, Wed 4/22) = %s, want 2026-%02d-%02d",
				c.target, got.Format("2006-01-02"), c.wantMonth, c.wantDay)
		}
	}
}

// TestParseWeekday_ChineseDispatch confirms the ZH regex picks the right
// weekday + prefix.
func TestParseWeekday_ChineseDispatch(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, loc) // Sun
	cases := []struct {
		input string
		want  string // expected RFC-3339 date portion
	}{
		{"周五", "2026-04-24"},
		{"周一", "2026-04-20"},
		{"周日", "2026-04-26"}, // today
		{"星期五", "2026-04-24"},
		{"礼拜五", "2026-04-24"},
		{"周天", "2026-04-26"},
		{"上周五", "2026-04-17"},
		{"下周五", "2026-05-01"},
		{"本周五", "2026-04-24"},
		{"这周五", "2026-04-24"},
		{"上个周五", "2026-04-17"},
		{"下个周五", "2026-05-01"},
		{"周 五", "2026-04-24"}, // tolerate whitespace
		{"总结一下周五的讨论", "2026-04-24"},
	}
	for _, c := range cases {
		got, ok := parseWeekday(c.input, now)
		if !ok {
			t.Errorf("parseWeekday(%q) = no match; want %s", c.input, c.want)
			continue
		}
		if g := got.Format("2006-01-02"); g != c.want {
			t.Errorf("parseWeekday(%q) = %s, want %s", c.input, g, c.want)
		}
	}
}

// TestParseWeekday_EnglishDispatch confirms the EN regex picks the right
// weekday + prefix and handles abbreviations / case.
func TestParseWeekday_EnglishDispatch(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, loc) // Sun
	cases := []struct {
		input string
		want  string
	}{
		{"friday", "2026-04-24"},
		{"Friday", "2026-04-24"},
		{"FRI", "2026-04-24"},
		{"summarize Friday's discussion", "2026-04-24"},
		{"last friday", "2026-04-17"},
		{"past Friday", "2026-04-17"},
		{"next friday", "2026-05-01"},
		{"this friday", "2026-04-24"},
		{"sunday", "2026-04-26"}, // today
		{"sun", "2026-04-26"},
	}
	for _, c := range cases {
		got, ok := parseWeekday(strings.ToLower(c.input), now)
		if !ok {
			t.Errorf("parseWeekday(%q) = no match; want %s", c.input, c.want)
			continue
		}
		if g := got.Format("2006-01-02"); g != c.want {
			t.Errorf("parseWeekday(%q) = %s, want %s", c.input, g, c.want)
		}
	}
}

// TestParseTimeRange_WeekdayBeatsLastWeek ensures the weekday rule runs
// before the bare "上周" rule so "上周五" resolves to last Friday rather
// than the Monday that "上周" returns.
func TestParseTimeRange_WeekdayBeatsLastWeek(t *testing.T) {
	now := time.Now()
	got := parseTimeRange("上周五的总结")
	if got.Weekday() != time.Friday {
		t.Errorf("parseTimeRange(上周五的总结) → %s, want Friday", got.Weekday())
	}
	diff := int(now.Sub(got).Hours() / 24)
	if diff < 7 || diff > 14 {
		t.Errorf("parseTimeRange(上周五的总结) → %d days back, want 7-14", diff)
	}
}

// TestParseTimeRange_BareWeekdaySmoke runs against the host clock so any
// regression in the regex / dispatch glue surfaces in CI even without a
// pinned `now`.
func TestParseTimeRange_BareWeekdaySmoke(t *testing.T) {
	now := time.Now()
	got := parseTimeRange("周五的讨论")
	if got.Weekday() != time.Friday {
		t.Errorf("parseTimeRange(周五的讨论) → %s, want Friday", got.Weekday())
	}
	diff := int(now.Sub(got).Hours() / 24)
	if diff < 0 || diff > 7 {
		t.Errorf("parseTimeRange(周五的讨论) → %d days back, want 0-6 (most recent past Friday)", diff)
	}
}

// TestParseTimeRange_WeekdayDoesNotShadowAbsoluteDate ensures absolute
// dates still win over weekday matches when both are present.
func TestParseTimeRange_WeekdayDoesNotShadowAbsoluteDate(t *testing.T) {
	got := parseTimeRange("周五开会,具体看 2026-04-10 的纪要")
	if got.Year() != 2026 || got.Month() != time.April || got.Day() != 10 {
		t.Errorf("parseTimeRange(...) = %s, want 2026-04-10 (absolute date wins)", got.Format("2006-01-02"))
	}
}

// TestParseWeekday_PrefixBoundary covers the regression where greedy regex
// matching against "总结一下周五的讨论" picked "下" as a "next" prefix and
// resolved to next Friday instead of the most recent past Friday.
// The boundary check should also reject "晚上周五" → last Friday and
// preserve the "this/last/next" semantics when the prefix is at a real
// word boundary.
func TestParseWeekday_PrefixBoundary(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, loc) // Sun
	cases := []struct {
		input string
		want  string
	}{
		// CJK char immediately before prefix → drop prefix, treat as bare.
		{"一下周五", "2026-04-24"},
		{"总结一下周五的讨论", "2026-04-24"},
		{"晚上周五", "2026-04-24"},
		{"明天上周五", "2026-04-24"},
		// Boundary present → prefix honored.
		{"上周五", "2026-04-17"},
		{"下周五", "2026-05-01"},
		{" 上周五", "2026-04-17"},
		{",下周五", "2026-05-01"},
		{"。上周五", "2026-04-17"},
		{"开会:上周五的纪要", "2026-04-17"},
	}
	for _, c := range cases {
		got, ok := parseWeekday(c.input, now)
		if !ok {
			t.Errorf("parseWeekday(%q) = no match", c.input)
			continue
		}
		if g := got.Format("2006-01-02"); g != c.want {
			t.Errorf("parseWeekday(%q) = %s, want %s", c.input, g, c.want)
		}
	}
}
