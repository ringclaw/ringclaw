package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/internal/util"
)

// Intent represents the classified intent of a user message.
type Intent string

const (
	IntentSummarize Intent = "summarize"
	IntentTask      Intent = "task"
	IntentNote      Intent = "note"
	IntentEvent     Intent = "event"
	IntentChat      Intent = "chat"
)

// intentTriggers are loose multilingual keywords for pre-filtering.
// Substring match (not prefix) — false positives are corrected by the AI stage.
var intentTriggers = []string{
	// Chinese
	"总结", "摘要", "汇总", "概括",
	"创建任务", "添加任务", "新建任务", "加个任务",
	"创建笔记", "添加笔记", "记一下", "记个笔记",
	"创建日程", "添加日程", "创建事件", "安排",
	// English
	"summarize", "summarise", "summary", "recap", "digest",
	"create task", "add task", "new task",
	"create note", "add note", "take note",
	"create event", "add event", "schedule",
	// Japanese
	"まとめ", "要約",
	// Russian
	"резюме", "итог",
	// Spanish
	"resumir", "resumen",
}

const intentConversationID = "intent:classifier"

// matchesIntentTrigger checks if the text contains any loose intent keyword.
func matchesIntentTrigger(text string) bool {
	lower := strings.ToLower(text)
	for _, kw := range intentTriggers {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func matchesVideoMeetingListIntent(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	if matchesAnySubstring(lower, "创建", "新建", "添加", "create", "schedule", "add ") {
		return false
	}
	hasQuery := matchesAnySubstring(lower, "获取", "查询", "查看", "列出", "看看", "show", "list", "get", "find")
	hasMeeting := matchesAnySubstring(lower, "会议", "视频会议", "video meeting", "video meetings", "rcv meeting", "rcv meetings")
	hasVideoOrFuture := matchesAnySubstring(lower, "video", "视频", "rcv", "将来", "未来", "接下来", "后续", "upcoming", "future", "next")
	return hasQuery && hasMeeting && hasVideoOrFuture
}

func matchesAnySubstring(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

// classifyIntent uses the default agent to determine the user's intent.
// Returns IntentChat if the agent is unavailable or returns an unrecognized response.
func classifyIntent(ctx context.Context, ag agent.Agent, text string) Intent {
	prompt := fmt.Sprintf(IntentPrompt(), text)

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	start := time.Now()
	reply, err := ag.Chat(ctx, intentConversationID, prompt)
	elapsed := time.Since(start)

	if err != nil {
		slog.Warn("intent classification failed, falling back to chat", "component", "intent", "error", err, "elapsed", elapsed)
		return IntentChat
	}

	intent := parseIntentReply(reply)
	slog.Info("intent classified", "component", "intent", "text", util.Truncate(text, 60), "intent", string(intent), "elapsed", elapsed)
	return intent
}

// parseIntentReply extracts the intent from the agent's reply.
func parseIntentReply(reply string) Intent {
	cleaned := strings.ToLower(strings.TrimSpace(reply))
	// The agent may reply with extra text; find the first matching keyword
	for _, candidate := range []struct {
		keyword string
		intent  Intent
	}{
		{"summarize", IntentSummarize},
		{"summary", IntentSummarize},
		{"task", IntentTask},
		{"note", IntentNote},
		{"event", IntentEvent},
		{"chat", IntentChat},
	} {
		if strings.Contains(cleaned, candidate.keyword) {
			return candidate.intent
		}
	}
	return IntentChat
}
