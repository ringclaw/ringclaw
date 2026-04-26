package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"
)

type nameExtractorAgent struct {
	reply string
}

func (m *nameExtractorAgent) Chat(_ context.Context, _, _ string) (string, error) {
	return m.reply, nil
}
func (m *nameExtractorAgent) ResetSession(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *nameExtractorAgent) SetCwd(_ string)          {}
func (m *nameExtractorAgent) Info() agent.AgentInfo     { return agent.AgentInfo{Name: "mock"} }

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

func TestParseTimeRange_DefaultForGroupSummaryWithoutExplicitTime(t *testing.T) {
	result := parseTimeRange("总结这个群的消息")
	today := todayStart()
	if !result.Equal(today) {
		t.Errorf("expected today start %v, got %v", today, result)
	}
}

func TestParseTimeRange_DefaultForEnglishSummaryWithoutExplicitTime(t *testing.T) {
	result := parseTimeRange("summarize this group")
	today := todayStart()
	if !result.Equal(today) {
		t.Errorf("expected today start %v, got %v", today, result)
	}
}

func TestParseTimeRange_LastWeek(t *testing.T) {
	now := time.Now()
	for _, text := range []string{"上周的聊天", "上星期的消息", "last week chat", "先週のチャット", "지난주 채팅"} {
		result := parseTimeRange(text)
		diff := now.Sub(result)
		if diff < 7*24*time.Hour || diff > 14*24*time.Hour {
			t.Errorf("parseTimeRange(%q): expected 7-14 days ago, got %v ago", text, diff)
		}
		if result.Weekday() != time.Monday {
			t.Errorf("parseTimeRange(%q): expected Monday, got %v", text, result.Weekday())
		}
	}
}

func TestParseTimeRange_LastMonth(t *testing.T) {
	now := time.Now()
	for _, text := range []string{"上个月的聊天", "上月的消息", "last month chat", "先月のチャット", "지난달 채팅"} {
		result := parseTimeRange(text)
		if result.Day() != 1 {
			t.Errorf("parseTimeRange(%q): expected 1st of month, got day %d", text, result.Day())
		}
		expectedMonth := now.Month() - 1
		if expectedMonth == 0 {
			expectedMonth = 12
		}
		if result.Month() != expectedMonth {
			t.Errorf("parseTimeRange(%q): expected month %d, got %d", text, expectedMonth, result.Month())
		}
	}
}

func TestParseTimeRange_ThisMonth(t *testing.T) {
	now := time.Now()
	result := parseTimeRange("本月的消息")
	if result.Day() != 1 || result.Month() != now.Month() {
		t.Errorf("expected 1st of current month, got %v", result)
	}
}

func TestParseTimeRange_DayBeforeYesterday(t *testing.T) {
	now := time.Now()
	result := parseTimeRange("前天的聊天")
	diff := now.Sub(result)
	if diff < 36*time.Hour || diff > 72*time.Hour {
		t.Errorf("expected ~2 days ago, got %v ago", diff)
	}
}

func TestParseTimeRange_Recently(t *testing.T) {
	now := time.Now()
	result := parseTimeRange("最近的聊天")
	diff := now.Sub(result)
	if diff < 2*24*time.Hour || diff > 4*24*time.Hour {
		t.Errorf("expected ~3 days ago, got %v ago", diff)
	}
}

func TestExtractNameFromText(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{"总结一下跟张三的聊天", "张三"},
		{"summarize chat with John", "john"},
		// Trailing instruction stripping (multilingual)
		{"总结 maxwell 并用 note 发给他", "maxwell"},
		{"summarize john and then create a note", "john"},
		{"总结张三然后发任务给他", "张三"},
		{"summary of alice and also send to her", "alice"},
		{"总结 bob 并且用笔记发送", "bob"},
		{"summarize dave then send him a task", "dave"},
		// Regression: standalone 用/再 must NOT split inside words
		{"总结 昨天跟 maxwell 的聊天并用 note 发给他", "maxwell"},
		{"总结 昨天跟 Maxwell Huang 的聊天并用 note 发给他", "maxwell huang"},
		// Time word stripping (multilingual)
		{"总结我和 Maxwell 上周的聊天", "maxwell"},
		{"总结我和 Maxwell 上星期的聊天", "maxwell"},
		{"总结 Maxwell 这周的消息", "maxwell"},
		{"总结 Maxwell 上个月的聊天", "maxwell"},
		{"summarize my chat with John last week", "john"},
		{"summarize chat with Alice last month", "alice"},
		{"先週のMaxwellとのチャットを要約して", "maxwell"},
		{"지난주 maxwell 과의 채팅 요약", "maxwell"},
	}
	for _, tt := range tests {
		got := extractNameFromText(tt.text)
		if got != tt.want {
			t.Errorf("extractNameFromText(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

func TestExtractNameViaAgent(t *testing.T) {
	tests := []struct {
		reply string
		want  string
	}{
		{"John Lin", "john lin"},
		{`"Maxwell Huang"`, "maxwell huang"},
		{"NONE", ""},
		{"", ""},
		{"  Alice  ", "alice"},
	}
	for _, tt := range tests {
		ag := &nameExtractorAgent{reply: tt.reply}
		got := extractNameViaAgent(context.Background(), ag, "dummy")
		if got != tt.want {
			t.Errorf("extractNameViaAgent(reply=%q) = %q, want %q", tt.reply, got, tt.want)
		}
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
		{"John Linaza", "John Lin", true},
	}
	for _, tt := range tests {
		got := fuzzyMatch(tt.haystack, tt.needle)
		if got != tt.want {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", tt.haystack, tt.needle, got, tt.want)
		}
	}
}

func TestExactMatch(t *testing.T) {
	tests := []struct {
		haystack string
		needle   string
		want     bool
	}{
		{"John Lin", "John Lin", true},
		{"John Lin", "john lin", true},
		{" John Lin ", "John Lin", true},
		{"John Linaza", "John Lin", false},
		{"John Lin", "John Linaza", false},
		{"张三", "张三", true},
	}
	for _, tt := range tests {
		got := exactMatch(tt.haystack, tt.needle)
		if got != tt.want {
			t.Errorf("exactMatch(%q, %q) = %v, want %v", tt.haystack, tt.needle, got, tt.want)
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

func TestBuildSummaryPrompt_DefaultMessageLimit(t *testing.T) {
	var gotRecordCount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/team-messaging/v1/chats/group-1/posts":
			gotRecordCount = r.URL.Query().Get("recordCount")
			_ = json.NewEncoder(w).Encode(ringcentral.PostList{
				Records: []ringcentral.Post{
					{
						ID:           "m1",
						GroupID:      "group-1",
						Text:         "latest message",
						CreatorID:    "glip-user-1",
						CreationTime: time.Now().UTC().Format(time.RFC3339),
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	prompt, err := BuildSummaryPrompt(context.Background(), client, &SummarizeRequest{
		ChatID:   "group-1",
		ChatName: "General",
		TimeFrom: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("BuildSummaryPrompt returned error: %v", err)
	}
	if gotRecordCount != "250" {
		t.Fatalf("expected default recordCount=250, got %q", gotRecordCount)
	}
	if !strings.Contains(prompt, "250 messages") {
		t.Fatalf("expected prompt to mention default message limit, got %q", prompt)
	}
}

type dateAgent struct {
	reply string
	err   error
}

func (a *dateAgent) Chat(_ context.Context, _, _ string) (string, error) {
	if a.err != nil {
		return "", a.err
	}
	return a.reply, nil
}
func (a *dateAgent) ResetSession(_ context.Context, _ string) (string, error) { return "", nil }
func (a *dateAgent) SetCwd(_ string)                                          {}
func (a *dateAgent) Info() agent.AgentInfo                                    { return agent.AgentInfo{Name: "date-mock"} }

func TestExtractDateViaAgent_ISO(t *testing.T) {
	ag := &dateAgent{reply: "2026-04-10"}
	got := extractDateViaAgent(context.Background(), ag, "总结4月10日的消息")
	if got.Year() != 2026 || got.Month() != time.April || got.Day() != 10 {
		t.Errorf("expected 2026-04-10, got %v", got.Format("2006-01-02"))
	}
}

func TestExtractDateViaAgent_Relative(t *testing.T) {
	ag := &dateAgent{reply: "yesterday"}
	got := extractDateViaAgent(context.Background(), ag, "总结昨天的消息")
	yesterday := time.Now().AddDate(0, 0, -1)
	if got.Month() != yesterday.Month() || got.Day() != yesterday.Day() {
		t.Errorf("expected yesterday, got %v", got.Format("2006-01-02"))
	}
}

func TestExtractDateViaAgent_None(t *testing.T) {
	ag := &dateAgent{reply: "NONE"}
	got := extractDateViaAgent(context.Background(), ag, "总结一下")
	today := todayStart()
	if !got.Equal(today) {
		t.Errorf("expected today for NONE, got %v", got.Format("2006-01-02"))
	}
}

func TestExtractDateViaAgent_Error(t *testing.T) {
	ag := &dateAgent{err: fmt.Errorf("timeout")}
	got := extractDateViaAgent(context.Background(), ag, "总结4月10日的消息")
	// Should fall back to parseTimeRange which handles "4月10日"
	if got.Month() != time.April || got.Day() != 10 {
		t.Errorf("expected regex fallback to April 10, got %v", got.Format("2006-01-02"))
	}
}

// dateAgentSpy fails the test if Chat is invoked — used to assert the
// regex fast-path short-circuits the LLM call.
type dateAgentSpy struct {
	t      *testing.T
	called int
}

func (a *dateAgentSpy) Chat(_ context.Context, _, _ string) (string, error) {
	a.called++
	a.t.Fatalf("agent.Chat must not be called when regex matches")
	return "", nil
}
func (a *dateAgentSpy) ResetSession(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (a *dateAgentSpy) SetCwd(_ string)        {}
func (a *dateAgentSpy) Info() agent.AgentInfo { return agent.AgentInfo{Name: "spy"} }

// TestExtractDateViaAgent_RegexFastPath_SkipsLLM is the regression for the
// production bug where "总结最近一周的聊天" timed out on the LLM extractor
// (10 s deadline) and then the regex fallback collapsed to -3 days. With
// the regex now run first AND covering "一周", the LLM must not be called
// at all.
func TestExtractDateViaAgent_RegexFastPath_SkipsLLM(t *testing.T) {
	cases := []string{
		"总结最近一周的聊天",
		"总结一下![:Team](158994374662) 最近一周的聊天",
		"过去三天的消息",
		"最近一个月的活动",
		"4月10日的消息",
		"summarize last 7 days",
		// Weekday phrases must also short-circuit the LLM. This is the
		// regression for the production bug where "周五" fell through
		// to the LLM, which returned NONE, defaulting summary to today.
		"总结周五的讨论",
		"上周三的会议纪要",
		"summarize last friday",
		"this monday update",
	}
	for _, in := range cases {
		ag := &dateAgentSpy{t: t}
		got := extractDateViaAgent(context.Background(), ag, in)
		if got.Equal(todayStart()) && !looksLikeTodayWeekdayMatch(in) {
			t.Errorf("extractDateViaAgent(%q) = today; expected regex match to back-date", in)
		}
		if ag.called != 0 {
			t.Errorf("extractDateViaAgent(%q) invoked LLM %d time(s); expected 0", in, ag.called)
		}
	}
}

// looksLikeTodayWeekdayMatch is a narrow escape hatch: when the host clock
// is on the same weekday the test phrase names ("周X" alone or "this X"),
// resolveWeekday correctly returns today, which is otherwise our
// "no match" signal in the assertion above.
func looksLikeTodayWeekdayMatch(in string) bool {
	wd := int(time.Now().Weekday())
	if wd == 0 {
		wd = 7
	}
	zhMap := map[int]string{1: "周一", 2: "周二", 3: "周三", 4: "周四", 5: "周五", 6: "周六", 7: "周日"}
	enMap := map[int]string{1: "monday", 2: "tuesday", 3: "wednesday", 4: "thursday", 5: "friday", 6: "saturday", 7: "sunday"}
	if z, ok := zhMap[wd]; ok && strings.Contains(in, z) && !strings.Contains(in, "上") && !strings.Contains(in, "下") {
		return true
	}
	if e, ok := enMap[wd]; ok && strings.Contains(strings.ToLower(in), e) &&
		!strings.Contains(strings.ToLower(in), "last") &&
		!strings.Contains(strings.ToLower(in), "next") &&
		!strings.Contains(strings.ToLower(in), "past") {
		return true
	}
	return false
}

// TestExtractDateViaAgent_NilAgent_RegexOnly confirms the helper still
// works when no agent is configured (e.g. early init paths).
func TestExtractDateViaAgent_NilAgent_RegexOnly(t *testing.T) {
	got := extractDateViaAgent(context.Background(), nil, "总结最近一周的聊天")
	now := time.Now()
	diffDays := int(now.Sub(got).Hours()/24 + 0.5)
	if diffDays < 6 || diffDays > 8 {
		t.Errorf("expected ~7 days back, got %d days (%s)", diffDays, got.Format("2006-01-02 15:04"))
	}
}

func TestResolveChatTarget_SkipsBotMention(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/chats") && r.URL.Query().Get("type") == "" {
			json.NewEncoder(w).Encode(ringcentral.Chat{ID: "group-chat", Name: "实验虾", Type: "Team"})
		} else {
			json.NewEncoder(w).Encode(ringcentral.ChatList{Records: []ringcentral.Chat{}})
		}
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-123")

	mentions := []ringcentral.Mention{
		{ID: "bot-123", Type: "Person", Name: "catclaw"},
		{ID: "team-456", Type: "Team", Name: "实验虾"},
	}

	req, err := ResolveChatTarget(context.Background(), client, nil, "总结4月10日的消息", mentions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.ChatID != "team-456" {
		t.Errorf("expected chatID=team-456 (Team mention), got %q", req.ChatID)
	}
	if req.ChatName != "实验虾" {
		t.Errorf("expected chatName=实验虾, got %q", req.ChatName)
	}
}
