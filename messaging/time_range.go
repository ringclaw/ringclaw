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
	reLastNDays  = regexp.MustCompile(`(?:最近|过去|last)\s*(\d+)\s*(?:天|days?)`)
	reLastNHours = regexp.MustCompile(`(?:最近|过去|last)\s*(\d+)\s*(?:小时|个小时|hours?)`)
)

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

	if m := reLastNDays.FindStringSubmatch(lower); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		if n > 0 {
			return now.AddDate(0, 0, -n)
		}
	}
	if m := reLastNHours.FindStringSubmatch(lower); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		if n > 0 {
			return now.Add(-time.Duration(n) * time.Hour)
		}
	}

	for _, r := range timeRules {
		if containsAny(lower, r.keywords...) {
			return r.resolve(now)
		}
	}

	return todayStart()
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
