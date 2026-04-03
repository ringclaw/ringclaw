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
	"summarize", "summary", "recap", "digest",
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

const intentPrompt = `Classify the user's PRIMARY intent. Reply with ONLY one word:
- "summarize" if the user wants to summarize CHAT HISTORY or MESSAGES (even if they also want to send/note/task the result)
- "task" if the PRIMARY goal is to CREATE a task/todo/action item
- "note" if the PRIMARY goal is to CREATE a note (not just send results as a note)
- "event" if the PRIMARY goal is to CREATE a calendar event/meeting
- "chat" if this is a normal conversation, question, or any other request (including asking an AI to summarize code, documents, or articles)

IMPORTANT: If the message contains BOTH "summarize" AND another action (create note/task/send), the primary intent is ALWAYS "summarize".

User message: %s

Intent:`

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

// classifyIntent uses the default agent to determine the user's intent.
// Returns IntentChat if the agent is unavailable or returns an unrecognized response.
func classifyIntent(ctx context.Context, ag agent.Agent, text string) Intent {
	prompt := fmt.Sprintf(intentPrompt, text)

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
