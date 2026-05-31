package messaging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Prompt registry — all prompt templates used by the messaging layer.
//
// Each prompt can be overridden by placing a .md file under
//   ~/.ringclaw/prompts/<name>.md
// The file content replaces the built-in default entirely.
// ---------------------------------------------------------------------------

const defaultActionPrompt = `
You are a RingCentral Team Messaging bot with real API actions.
Do NOT generate files or suggest manual steps — use ACTION blocks.

## Available Actions

ACTION:MESSAGE chatid=<name or chat ID>
<message>
END_ACTION

ACTION:NOTE title=<title> [chatid=...]
<body>
END_ACTION

ACTION:TASK subject=<subject> [assignee=<name>] [chatid=...]
<optional description>
END_ACTION

ACTION:EVENT title=<title> start=<ISO8601> end=<ISO8601>
END_ACTION

ACTION:CARD [chatid=...]
<Adaptive Card JSON v1.3>
END_ACTION

ACTION:VIDEO title=<meeting title> [type=Instant|Scheduled|PMI] [chatid=...]
END_ACTION

ACTION:RINGOUT to=<target phone> [from=<owner phone>] [callerid=<phone>] [playprompt=true]
END_ACTION

## Rules
- chatid: person name (e.g. John Smith), numeric chat ID, or ![:Team](ID). Omit to use current chat.
- assignee: person name or ![:Person](ID).
- The system resolves names to IDs automatically. NEVER use person/creator/user IDs as chatid.
- For RingCentral Video meetings → use ACTION:VIDEO. It creates a bridge and posts the join link.
- For phone calls → use ACTION:RINGOUT only when the owner explicitly asks to call a phone number. Omit from unless the owner explicitly provides a callback phone number; RingOut uses the current JWT user's identity and default callback settings. Never use a person name, bot name, user ID, or short extension like 8102 as from.
- For structured data, reports, or progress → use ACTION:CARD. Always generate complete valid Adaptive Card JSON v1.3.
- If no action needed, reply normally without ACTION blocks.
- Preserve first-person pronouns exactly as given: "我" for Chinese, "me"/"myself" for English. Do NOT translate or substitute.
- For multiple recipients, generate separate ACTION:MESSAGE blocks for each name.
- For analysis, explanations, or general questions without a clear action, reply with plain text only.
- When a request combines analysis and an action: First provide the analysis/summary as plain text, then generate the ACTION block separately.
- Use "me", "myself", or "我" as chatid when requested to message yourself. Do not use other pronouns or IDs.
- When asked to show data as an Adaptive Card, generate ACTION:CARD immediately. Do not ask for more information.
- For code analysis or pure discussion requests, respond with plain text only. No ACTION blocks.
- Do NOT extract ![:Team] or ![:Person] mentions from the message content and use them as chatid. Only use chatid if the user explicitly asks to send/forward to someone.
`

const defaultIntentPrompt = `Classify the user's PRIMARY intent. Reply with ONLY one word:
- "summarize" if the user wants to summarize CHAT HISTORY or MESSAGES (even if they also want to send/note/task the result)
- "task" if the PRIMARY goal is to CREATE a task/todo/action item
- "note" if the PRIMARY goal is to CREATE a note (not just send results as a note)
- "event" if the PRIMARY goal is to CREATE a calendar event/meeting
- "chat" if this is a normal conversation, question, or any other request (including asking an AI to summarize code, documents, articles, PRs, or other external content)

IMPORTANT: If the message contains BOTH "summarize" AND another action (create note/task/send), the primary intent is ALWAYS "summarize".
CRITICAL: The "summarize" intent ONLY applies to summarizing CHAT HISTORY or MESSAGES. Requests to summarize code, documents, articles, PRs, or other external content are "chat".

User message: %s

Intent:`

const defaultNameExtractPrompt = `Extract the target person's name from this message.
Reply with ONLY the person's name (e.g. "John Smith"), nothing else.
If no specific person is mentioned, reply with "NONE".
The name may appear in lowercase or mixed case. Consider names in any language or script.

Message: %s

Name:`

const defaultSummaryPrompt = `User request: %s

Summarize the following chat messages from "%s" (%s, up to %d messages).
Reply in the same language as the messages. Highlight key topics, decisions, and action items.

--- Messages (%d total) ---
%s
--- End of Messages ---
%s`

const defaultDateExtractPrompt = `Extract the date or time range from this message.
Reply with ONLY one of:
- An ISO 8601 date like "2026-04-10" for a specific date
- A relative expression like "yesterday", "last week", "3 days ago", "周五", "上周三", "next Friday"
- "NONE" if no time/date is mentioned

Weekday rules (apply to both Chinese and English):
- Bare "周X" / "Friday" → the most recent past occurrence of that weekday (today if today is that day)
- "上周X" / "last X" / "past X" → the X of the previous calendar week
- "下周X" / "next X" → the X of the next calendar week
- "本周X" / "这周X" / "this X" → the X of the current calendar week

Current date: %s

Message: %s

Date:`

const defaultHeartbeatPrompt = "This is a scheduled heartbeat check. Follow the instructions below and report anything that needs attention. If everything is fine, reply with exactly: %s\n\n%s"

// ---------------------------------------------------------------------------
// Loader — lazy-loads each prompt once, checking for file overrides.
// ---------------------------------------------------------------------------

var (
	promptOnce sync.Map // map[string]*sync.Once
	promptText sync.Map // map[string]string
)

// loadPrompt returns the prompt text for name, loading from
// ~/.ringclaw/prompts/<name>.md if it exists, otherwise using defaultText.
// Also checks legacy path ~/.ringclaw/action_prompt.md for backward compat.
func loadPrompt(name, defaultText string) string {
	once, _ := promptOnce.LoadOrStore(name, &sync.Once{})
	once.(*sync.Once).Do(func() {
		text := defaultText
		home, err := os.UserHomeDir()
		if err == nil {
			// Check new path first: ~/.ringclaw/prompts/<name>.md
			customPath := filepath.Join(home, ".ringclaw", "prompts", name+".md")
			if data, err := os.ReadFile(customPath); err == nil && len(data) > 0 {
				text = "\n" + string(data) + "\n"
				slog.Info("loaded custom prompt", "name", name, "path", customPath)
			} else if name == "action" {
				// Legacy compat: ~/.ringclaw/action_prompt.md
				legacyPath := filepath.Join(home, ".ringclaw", "action_prompt.md")
				if data, err := os.ReadFile(legacyPath); err == nil && len(data) > 0 {
					text = "\n" + string(data) + "\n"
					slog.Info("loaded custom prompt (legacy)", "name", name, "path", legacyPath)
				}
			}
		}
		promptText.Store(name, text)
	})
	v, _ := promptText.Load(name)
	return v.(string)
}

// --- Public accessors ---

func ActionPrompt() string {
	p := loadPrompt("action", defaultActionPrompt)
	return strings.ReplaceAll(p, "{{.Now}}", time.Now().Format("2006-01-02 15:04 MST"))
}
func IntentPrompt() string      { return loadPrompt("intent", defaultIntentPrompt) }
func NameExtractPrompt() string { return loadPrompt("name_extract", defaultNameExtractPrompt) }
func SummaryPrompt() string     { return loadPrompt("summary", defaultSummaryPrompt) }
func DateExtractPrompt() string { return loadPrompt("date_extract", defaultDateExtractPrompt) }
func HeartbeatPrompt() string   { return loadPrompt("heartbeat", defaultHeartbeatPrompt) }

// --- Raw template accessors (for eval scripts — single source of truth) ---

func IntentPromptTemplate() string      { return defaultIntentPrompt }
func NameExtractPromptTemplate() string { return defaultNameExtractPrompt }
func ActionPromptTemplate() string      { return defaultActionPrompt }
