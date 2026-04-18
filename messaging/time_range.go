package messaging

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	// Relative durations: "最近一周", "过去 7 天", "last 3 days", "past two weeks",
	// "最近一个月", "last month" (handled by keyword), etc.
	// Capture 1: count (digits or simple Chinese numerals, optional → defaults to 1)
	// Capture 2: unit token (天/days/周/weeks/星期/个月/months)
	// Capture 1: count (digits or simple Chinese numerals). Required so that
	// bare phrases like "last week" / "上个月" stay with their calendar-aware
	// keyword resolvers further down.
	reLastDuration = regexp.MustCompile(`(?:最近|过去|last|past)\s*的?\s*(\d+|一|两|二|三|四|五|六|七|八|九|十)\s*(个月|天|周|星期|days?|weeks?|months?)`)
	reLastHours    = regexp.MustCompile(`(?:最近|过去|last|past)\s*的?\s*(\d+|一|两|二|三|四|五|六|七|八|九|十)\s*(个小时|小时|hours?|hrs?)`)

	reAbsDateZH    = regexp.MustCompile(`(\d{1,2})\s*月\s*(\d{1,2})\s*[日号]`)
	reAbsDateEN    = regexp.MustCompile(`(?i)(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:tember)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\s+(\d{1,2})(?:st|nd|rd|th)?`)
	reAbsDateSlash = regexp.MustCompile(`(\d{1,2})/(\d{1,2})`)
	reAbsDateISO   = regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})`)
)

var cnSmallNum = map[string]int{
	"一": 1, "两": 2, "二": 2, "三": 3, "四": 4, "五": 5,
	"六": 6, "七": 7, "八": 8, "九": 9, "十": 10,
}

var monthNames = map[string]time.Month{
	"jan": time.January, "january": time.January,
	"feb": time.February, "february": time.February,
	"mar": time.March, "march": time.March,
	"apr": time.April, "april": time.April,
	"may": time.May,
	"jun": time.June, "june": time.June,
	"jul": time.July, "july": time.July,
	"aug": time.August, "august": time.August,
	"sep": time.September, "september": time.September,
	"oct": time.October, "october": time.October,
	"nov": time.November, "november": time.November,
	"dec": time.December, "december": time.December,
}

// timeRule maps multilingual keywords to a time resolver.
type timeRule struct {
	keywords []string
	resolve  func(now time.Time) time.Time
}

var timeRules = []timeRule{
	{
		keywords: []string{"上周", "上星期", "last week", "先週", "지난주"},
		resolve: func(now time.Time) time.Time {
			wd := int(now.Weekday())
			if wd == 0 {
				wd = 7
			}
			d := now.AddDate(0, 0, -(wd - 1 + 7))
			return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, now.Location())
		},
	},
	{
		keywords: []string{"本周", "这周", "这星期", "this week", "今週", "이번주"},
		resolve: func(now time.Time) time.Time {
			wd := int(now.Weekday())
			if wd == 0 {
				wd = 7
			}
			d := now.AddDate(0, 0, -(wd - 1))
			return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, now.Location())
		},
	},
	{
		keywords: []string{"上个月", "上月", "last month", "先月", "지난달"},
		resolve: func(now time.Time) time.Time {
			return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -1, 0)
		},
	},
	{
		keywords: []string{"本月", "这个月", "this month", "今月", "이번달"},
		resolve: func(now time.Time) time.Time {
			return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		},
	},
	{
		keywords: []string{"前天", "day before yesterday", "一昨日", "그저께", "avant-hier", "anteayer", "vorgestern"},
		resolve: func(now time.Time) time.Time {
			d := now.AddDate(0, 0, -2)
			return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, now.Location())
		},
	},
	{
		keywords: []string{"昨天", "yesterday", "昨日"},
		resolve: func(now time.Time) time.Time {
			d := now.AddDate(0, 0, -1)
			return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, now.Location())
		},
	},
	{
		keywords: []string{"这几天", "近期", "最近", "recently", "past few days"},
		resolve: func(now time.Time) time.Time {
			return now.AddDate(0, 0, -3)
		},
	},
}

func parseTimeRange(text string) time.Time {
	lower := strings.ToLower(text)
	now := time.Now()

	// Absolute dates (most specific, check first)
	if t, ok := parseAbsoluteDate(lower, now); ok {
		return t
	}

	// "最近 N 天/周/星期/个月" / "last N days/weeks/months" — must run before
	// the bare-keyword loop so "最近一周" doesn't get swallowed by the broad
	// "最近 → -3d" rule below.
	if t, ok := parseRelativeDuration(lower, now); ok {
		return t
	}
	if t, ok := parseRelativeHours(lower, now); ok {
		return t
	}

	for _, r := range timeRules {
		if containsAny(lower, r.keywords...) {
			return r.resolve(now)
		}
	}

	return todayStart()
}

// parseRelativeDuration matches "最近一周", "过去 7 天", "last 3 weeks",
// "past month", "最近一个月" and returns the corresponding time offset.
func parseRelativeDuration(lower string, now time.Time) (time.Time, bool) {
	m := reLastDuration.FindStringSubmatch(lower)
	if len(m) != 3 {
		return time.Time{}, false
	}
	count := parseDurationCount(m[1])
	if count <= 0 {
		return time.Time{}, false
	}
	per := unitToDays(m[2])
	if per <= 0 {
		return time.Time{}, false
	}
	return now.AddDate(0, 0, -count*per), true
}

// parseRelativeHours matches "最近 2 小时", "last 5 hours", "过去三个小时".
func parseRelativeHours(lower string, now time.Time) (time.Time, bool) {
	m := reLastHours.FindStringSubmatch(lower)
	if len(m) != 3 {
		return time.Time{}, false
	}
	count := parseDurationCount(m[1])
	if count <= 0 {
		return time.Time{}, false
	}
	return now.Add(-time.Duration(count) * time.Hour), true
}

// parseDurationCount accepts an optional digit string ("3", "10"), a small
// Chinese numeral ("一", "两", …, "十"), or empty (defaults to 1, e.g.
// "past week").
func parseDurationCount(s string) int {
	if s == "" {
		return 1
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	if n, ok := cnSmallNum[s]; ok {
		return n
	}
	return 0
}

// unitToDays maps a duration unit token to days. "个月" / "month(s)" is
// approximated as 30 days (good enough for chat-summary windows).
func unitToDays(unit string) int {
	switch unit {
	case "天", "day", "days":
		return 1
	case "周", "星期", "week", "weeks":
		return 7
	case "个月", "month", "months":
		return 30
	}
	return 0
}

func parseAbsoluteDate(lower string, now time.Time) (time.Time, bool) {
	loc := now.Location()

	// ISO 8601: "2026-04-10"
	if m := reAbsDateISO.FindStringSubmatch(lower); len(m) == 4 {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if mo >= 1 && mo <= 12 && d >= 1 && d <= 31 {
			return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, loc), true
		}
	}

	// Chinese: "4月10日", "4 月 10 号"
	if m := reAbsDateZH.FindStringSubmatch(lower); len(m) == 3 {
		mo, _ := strconv.Atoi(m[1])
		d, _ := strconv.Atoi(m[2])
		if mo >= 1 && mo <= 12 && d >= 1 && d <= 31 {
			return time.Date(now.Year(), time.Month(mo), d, 0, 0, 0, 0, loc), true
		}
	}

	// English: "April 10", "Apr 10th"
	if m := reAbsDateEN.FindStringSubmatch(lower); len(m) >= 3 {
		mo, ok := monthNames[strings.ToLower(m[1])]
		if ok {
			d, _ := strconv.Atoi(m[2])
			if d >= 1 && d <= 31 {
				return time.Date(now.Year(), mo, d, 0, 0, 0, 0, loc), true
			}
		}
	}

	// Slash: "4/10", "04/10" (month/day)
	if m := reAbsDateSlash.FindStringSubmatch(lower); len(m) == 3 {
		mo, _ := strconv.Atoi(m[1])
		d, _ := strconv.Atoi(m[2])
		if mo >= 1 && mo <= 12 && d >= 1 && d <= 31 {
			return time.Date(now.Year(), time.Month(mo), d, 0, 0, 0, 0, loc), true
		}
	}

	return time.Time{}, false
}

func formatTimeDesc(from time.Time) string {
	now := time.Now()
	today := todayStart()
	if from.Equal(today) {
		return "today"
	}
	diff := now.Sub(from)
	if diff < 48*time.Hour {
		return "since yesterday"
	}
	days := int(diff.Hours() / 24)
	return fmt.Sprintf("last %d days", days)
}

func containsAny(s string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

// flattenLang merges all values from a lang→words map into a single slice,
// sorted by length descending so longer phrases are replaced first.
func flattenLang(m map[string][]string) []string {
	var all []string
	for _, words := range m {
		all = append(all, words...)
	}
	sort.Slice(all, func(i, j int) bool { return len(all[i]) > len(all[j]) })
	return all
}

// --- Word lists for extractNameFromText (grouped by language) ---

var timeWords = flattenLang(map[string][]string{
	"zh": {"上星期", "这星期", "上个月", "这段时间", "这几天",
		"上周", "这周", "上月", "本月", "前天", "近期",
		"今天", "昨天", "本周", "最近", "过去"},
	"en": {"last week", "last month", "this week", "this month", "past week", "past month",
		"today", "yesterday", "recently", "last"},
	"ja": {"一昨日", "先週", "今週", "先月", "今月", "最近"},
	"ko": {"지난주", "이번주", "지난달", "이번달", "그저께", "최근"},
	"fr": {"la semaine dernière", "cette semaine", "le mois dernier", "ce mois", "avant-hier", "récemment"},
	"es": {"la semana pasada", "esta semana", "el mes pasado", "este mes", "anteayer", "recientemente"},
	"de": {"letzte woche", "diese woche", "letzten monat", "diesen monat", "vorgestern", "kürzlich"},
	"ru": {"на прошлой неделе", "на этой неделе", "в прошлом месяце", "в этом месяце", "позавчера", "недавно"},
})
