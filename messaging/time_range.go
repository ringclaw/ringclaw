package messaging

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
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

	// Weekday names with optional direction modifier.
	// ZH:  "周五" / "上周五" / "下周五" / "本周五" / "这周五"
	//      also accepts 星期X / 礼拜X and 上个/下个/这个 prefixes.
	// EN:  "Friday" / "last Friday" / "next Friday" / "this Friday" / abbreviations.
	// Bare weekday → most recent past occurrence (or today if today is that day).
	// 上/last  → previous calendar week's X (always 7+ days back).
	// 下/next  → next calendar week's X (always 1-7 days forward).
	// 本/这/this → current calendar week's X (may be past, today, or future).
	reWeekdayZH = regexp.MustCompile(`(上个?|下个?|这个?|本)?\s*(?:周|星期|礼拜)\s*([一二三四五六日天])`)
	reWeekdayEN = regexp.MustCompile(`(?i)(?:\b(this|last|next|past)\s+)?\b(monday|tuesday|wednesday|thursday|friday|saturday|sunday|mon|tue|wed|thu|fri|sat|sun)\b\.?`)
)

var cnSmallNum = map[string]int{
	"一": 1, "两": 2, "二": 2, "三": 3, "四": 4, "五": 5,
	"六": 6, "七": 7, "八": 8, "九": 9, "十": 10,
}

// Weekday lookup tables. Values use ISO numbering (Mon=1 … Sun=7) so the
// resolver math stays uniform across the (Mon-first) Chinese calendar
// convention and Go's (Sun=0) time.Weekday.
var zhWeekdayMap = map[string]int{
	"一": 1, "二": 2, "三": 3, "四": 4, "五": 5, "六": 6,
	"日": 7, "天": 7,
}

var enWeekdayMap = map[string]int{
	"monday": 1, "mon": 1,
	"tuesday": 2, "tue": 2,
	"wednesday": 3, "wed": 3,
	"thursday": 4, "thu": 4,
	"friday": 5, "fri": 5,
	"saturday": 6, "sat": 6,
	"sunday": 7, "sun": 7,
}

// weekdayPrefix encodes the calendar-week direction modifier that may
// precede a weekday name.
type weekdayPrefix int

const (
	wkpNone weekdayPrefix = iota // 周五  / Friday
	wkpThis                      // 本周五 / 这周五 / this Friday
	wkpLast                      // 上周五 / last Friday / past Friday
	wkpNext                      // 下周五 / next Friday
)

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
	t, _ := parseTimeRangeOK(text)
	return t
}

// parseTimeRangeOK is the underlying parser that exposes whether any
// rule actually matched, so callers (e.g. extractDateViaAgent's
// regex fast-path) can distinguish "matched and resolved to today"
// from "no match → fall back to today". Returning ok=false signals
// the caller may want to consult an LLM extractor.
func parseTimeRangeOK(text string) (time.Time, bool) {
	lower := strings.ToLower(text)
	now := time.Now()

	// Absolute dates (most specific, check first)
	if t, ok := parseAbsoluteDate(lower, now); ok {
		return t, true
	}

	// "最近 N 天/周/星期/个月" / "last N days/weeks/months" — must run before
	// the bare-keyword loop so "最近一周" doesn't get swallowed by the broad
	// "最近 → -3d" rule below.
	if t, ok := parseRelativeDuration(lower, now); ok {
		return t, true
	}
	if t, ok := parseRelativeHours(lower, now); ok {
		return t, true
	}

	// Weekday names ("周五", "上周五", "Friday", "last Friday"). Must run
	// before the timeRules loop so "上周五" is not swallowed by the broader
	// "上周 → start of last week" rule.
	if t, ok := parseWeekday(lower, now); ok {
		return t, true
	}

	for _, r := range timeRules {
		if containsAny(lower, r.keywords...) {
			return r.resolve(now), true
		}
	}

	return todayStart(), false
}

// parseWeekday matches "周X" / "上周X" / "下周X" / "本周X" and the English
// equivalents (Friday, last Friday, next Monday, etc.) and returns the
// resolved date with the configured semantics.
//
// For Chinese, the directional prefix (上/下/本/这) is only honored when it
// sits at a natural word boundary (start of input, ASCII whitespace /
// punctuation, or CJK punctuation). Otherwise we treat the weekday as a
// bare match — this avoids false positives like "一下周五" (a bit of
// Friday's stuff) being parsed as "next Friday", or "晚上周五" being
// parsed as "last Friday".
func parseWeekday(lower string, now time.Time) (time.Time, bool) {
	if loc := reWeekdayZH.FindStringSubmatchIndex(lower); len(loc) == 6 {
		target, ok := zhWeekdayMap[lower[loc[4]:loc[5]]]
		if !ok {
			return time.Time{}, false
		}
		prefix := wkpNone
		if loc[2] >= 0 && isCJKWordBoundaryBefore(lower, loc[2]) {
			prefix = zhWeekdayPrefix(lower[loc[2]:loc[3]])
		}
		return resolveWeekday(prefix, target, now), true
	}
	if m := reWeekdayEN.FindStringSubmatch(lower); len(m) == 3 {
		if target, ok := enWeekdayMap[strings.ToLower(m[2])]; ok {
			return resolveWeekday(enWeekdayPrefix(strings.ToLower(m[1])), target, now), true
		}
	}
	return time.Time{}, false
}

// isCJKWordBoundaryBefore reports whether the byte position pos in s sits
// at a word boundary suitable for a Chinese directional prefix.
// True when pos is the start of the string, or the rune immediately before
// pos is ASCII (space, punctuation, alphanumeric) or CJK punctuation.
// False when the preceding rune is another CJK ideograph / hiragana /
// katakana / hangul, since those typically form compound words with 上/下
// (e.g. 一下, 晚上) that shouldn't be reinterpreted as directional prefixes.
func isCJKWordBoundaryBefore(s string, pos int) bool {
	if pos <= 0 {
		return true
	}
	r, size := utf8.DecodeLastRuneInString(s[:pos])
	if r == utf8.RuneError && size == 0 {
		return true
	}
	if r < 0x80 { // ASCII
		return true
	}
	// CJK Symbols and Punctuation (U+3000–U+303F): includes 、。「」 etc.
	if r >= 0x3000 && r <= 0x303F {
		return true
	}
	// Fullwidth ASCII punctuation segments inside U+FF00–U+FFEF.
	if (r >= 0xFF00 && r <= 0xFF20) || (r >= 0xFF3B && r <= 0xFF40) || (r >= 0xFF5B && r <= 0xFF65) {
		return true
	}
	return false
}

func zhWeekdayPrefix(s string) weekdayPrefix {
	switch {
	case strings.HasPrefix(s, "上"):
		return wkpLast
	case strings.HasPrefix(s, "下"):
		return wkpNext
	case strings.HasPrefix(s, "这"), strings.HasPrefix(s, "本"):
		return wkpThis
	}
	return wkpNone
}

func enWeekdayPrefix(s string) weekdayPrefix {
	switch s {
	case "last", "past":
		return wkpLast
	case "next":
		return wkpNext
	case "this":
		return wkpThis
	}
	return wkpNone
}

// resolveWeekday computes the date for the requested weekday using the
// agreed semantics:
//   - bare ("周五" / "Friday"): most recent past occurrence; today if today
//     is that weekday.
//   - "上周X" / "last X" / "past X": always at least one full week back,
//     i.e. the most-recent past X minus 7 days. This guarantees a
//     7–13-day-back range regardless of today's weekday so the phrase
//     reads as "the X from a week ago" rather than colloquially
//     swallowing "this past X" (e.g. on a Monday, "上周五" must not
//     resolve to last Friday's 3-days-back date).
//   - "下周X" / "next X": the X of the next calendar week (1–13 days
//     forward, depending on today's position).
//   - "本周X" / "这周X" / "this X": the X of the current calendar week
//     (may be past, today, or future).
//
// target uses ISO weekday numbering (Mon=1 … Sun=7).
func resolveWeekday(prefix weekdayPrefix, target int, now time.Time) time.Time {
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7 // Sunday → 7 to match ISO numbering
	}

	// barePast is the bare-weekday delta: the most recent past
	// occurrence of `target`, including today (range −6..0).
	barePast := -((wd - target + 7) % 7)

	var deltaDays int
	switch prefix {
	case wkpLast:
		// One full week earlier than the bare delta → range −13..−7,
		// matching the "always at least 7 days back" contract above.
		deltaDays = barePast - 7
	case wkpNext:
		deltaDays = target - wd + 7
	case wkpThis:
		deltaDays = target - wd
	default: // wkpNone
		deltaDays = barePast
	}

	d := now.AddDate(0, 0, deltaDays)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, now.Location())
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
		"上周一", "上周二", "上周三", "上周四", "上周五", "上周六", "上周日", "上周天",
		"下周一", "下周二", "下周三", "下周四", "下周五", "下周六", "下周日", "下周天",
		"本周一", "本周二", "本周三", "本周四", "本周五", "本周六", "本周日", "本周天",
		"这周一", "这周二", "这周三", "这周四", "这周五", "这周六", "这周日", "这周天",
		"周一", "周二", "周三", "周四", "周五", "周六", "周日", "周天",
		"星期一", "星期二", "星期三", "星期四", "星期五", "星期六", "星期日", "星期天",
		"礼拜一", "礼拜二", "礼拜三", "礼拜四", "礼拜五", "礼拜六", "礼拜日", "礼拜天",
		"上周", "这周", "上月", "本月", "前天", "近期",
		"今天", "昨天", "本周", "最近", "过去"},
	"en": {"last week", "last month", "this week", "this month", "past week", "past month",
		"last monday", "last tuesday", "last wednesday", "last thursday", "last friday", "last saturday", "last sunday",
		"next monday", "next tuesday", "next wednesday", "next thursday", "next friday", "next saturday", "next sunday",
		"this monday", "this tuesday", "this wednesday", "this thursday", "this friday", "this saturday", "this sunday",
		"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday",
		"today", "yesterday", "recently", "last"},
	"ja": {"一昨日", "先週", "今週", "先月", "今月", "最近"},
	"ko": {"지난주", "이번주", "지난달", "이번달", "그저께", "최근"},
	"fr": {"la semaine dernière", "cette semaine", "le mois dernier", "ce mois", "avant-hier", "récemment"},
	"es": {"la semana pasada", "esta semana", "el mes pasado", "este mes", "anteayer", "recientemente"},
	"de": {"letzte woche", "diese woche", "letzten monat", "diesen monat", "vorgestern", "kürzlich"},
	"ru": {"на прошлой неделе", "на этой неделе", "в прошлом месяце", "в этом месяце", "позавчера", "недавно"},
})
