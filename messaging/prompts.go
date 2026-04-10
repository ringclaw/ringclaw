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

Current time: {{.Now}}

## Available Actions

Append at the END of your reply. Text reply comes FIRST.

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

## Rules
- chatid: person name (e.g. John Smith), numeric chat ID, or ![:Team](ID). Omit to use current chat.
- assignee: person name or ![:Person](ID).
- The system resolves names to IDs automatically. NEVER use person/creator/user IDs as chatid.
- For structured data, reports, or progress → use ACTION:CARD.
- If no action needed, reply normally without ACTION blocks.
`

const defaultIntentPrompt = `Classify the user's PRIMARY intent. Reply with ONLY one word:
- "summarize" if the user wants to summarize CHAT HISTORY or MESSAGES (even if they also want to send/note/task the result)
- "task" if the PRIMARY goal is to CREATE a task/todo/action item
- "note" if the PRIMARY goal is to CREATE a note (not just send results as a note)
- "event" if the PRIMARY goal is to CREATE a calendar event/meeting
- "chat" if this is a normal conversation, question, or any other request (including asking an AI to summarize code, documents, or articles)

IMPORTANT: If the message contains BOTH "summarize" AND another action (create note/task/send), the primary intent is ALWAYS "summarize".

User message: %s

Intent:`

const defaultNameExtractPrompt = `Extract the target person's name from this message.
Reply with ONLY the person's name (e.g. "John Smith"), nothing else.
If no specific person is mentioned, reply with "NONE".

Message: %s

Name:`

const defaultSummaryPrompt = `User request: %s

Summarize the following chat messages from "%s" (%s, up to %d messages).
Reply in the same language as the messages. Highlight key topics, decisions, and action items.

--- Messages (%d total) ---
%s
--- End of Messages ---
%s`

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
func HeartbeatPrompt() string   { return loadPrompt("heartbeat", defaultHeartbeatPrompt) }
