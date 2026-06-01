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
	IntentVideo     Intent = "video"
	IntentPhone     Intent = "phone"
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
	"创建视频会议", "新建视频会议", "发起视频会议", "开视频会议", "视频会议", "rcv会议", "rcv 会议", "开会链接",
	"将来的会议", "未来的会议", "今天的会议", "重要会议", "会议列表",
	"打电话", "拨打", "外呼", "未接电话", "未接来电", "通话记录", "来电记录", "今天calls", "calls的记录", "calls 记录", "call summary",
	// English
	"summarize", "summarise", "summary", "recap", "digest",
	"create task", "add task", "new task",
	"create note", "add note", "take note",
	"create event", "add event", "schedule",
	"video meeting", "video call", "rcv meeting", "start a meeting", "create a meeting link", "meeting list", "meetings today", "important meetings",
	"ringout", "phone call", "call log", "call logs", "missed call", "missed calls", "missing call", "missing calls", "call summary", "dial ", "call +", "call 1",
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

// deterministicIntent classifies high-confidence product intents without using
// the agent classifier. It is intentionally narrow: broad phrases such as
// "schedule a meeting" or "review this video file" still go through the normal
// classifier/chat path.
func deterministicIntent(text string) (Intent, bool) {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return IntentChat, false
	}
	if matchesDeterministicPhoneIntent(lower) {
		return IntentPhone, true
	}
	if matchesDeterministicVideoIntent(lower) {
		return IntentVideo, true
	}
	return IntentChat, false
}

func matchesDeterministicVideoIntent(lower string) bool {
	for _, kw := range []string{
		"创建视频会议", "新建视频会议", "发起视频会议", "开视频会议", "视频会议",
		"rcv会议", "rcv 会议", "开会链接",
		"video meeting", "video call", "rcv meeting", "create a meeting link",
		"meeting list", "meetings today", "important meetings", "upcoming meetings",
	} {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	if strings.Contains(lower, "会议") {
		for _, kw := range []string{
			"今天", "明天", "最近", "本周", "这周", "下周", "未来", "将来",
			"重要", "列表", "有哪些", "有啥", "查询", "查看", "获取",
		} {
			if strings.Contains(lower, kw) {
				return true
			}
		}
	}

	return false
}

func matchesDeterministicPhoneIntent(lower string) bool {
	for _, kw := range []string{
		"打电话", "拨打", "外呼", "未接电话", "未接来电", "通话记录", "来电记录",
		"今天calls", "calls的记录", "calls 记录", "call summary",
		"ringout", "phone call", "call log", "call logs",
		"missed call", "missed calls", "missing call", "missing calls", "missing的call",
		"dial ", "call +", "call 1",
	} {
		if strings.Contains(lower, kw) {
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
		{"video", IntentVideo},
		{"phone", IntentPhone},
		{"chat", IntentChat},
	} {
		if strings.Contains(cleaned, candidate.keyword) {
			return candidate.intent
		}
	}
	return IntentChat
}
