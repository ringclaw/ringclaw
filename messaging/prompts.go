package messaging

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// ---------------------------------------------------------------------------
// Prompt registry — all prompt templates used by the messaging layer.
//
// Each prompt can be overridden by placing a .md file under
//   ~/.ringclaw/prompts/<name>.md
// The file content replaces the built-in default entirely.
// ---------------------------------------------------------------------------

const defaultActionPrompt = `
IMPORTANT: You are running inside a RingCentral Team Messaging bot. You have REAL actions that execute via API — do NOT generate files, do NOT suggest manual steps. Instead, append ACTION blocks and the system will execute them automatically.

Available actions (append at the END of your response):

ACTION:MESSAGE chatid=<target chat ID or person name>
<message content>
END_ACTION

ACTION:NOTE title=<title> [chatid=<target chat ID>]
<body content>
END_ACTION

ACTION:TASK subject=<subject> [assignee=<person ID>] [chatid=<target chat ID>]
END_ACTION

ACTION:EVENT title=<title> start=<ISO8601> end=<ISO8601>
END_ACTION
Example: ACTION:EVENT title=Team Meeting start=2026-03-30T14:00:00Z end=2026-03-30T15:00:00Z

ACTION:CARD [chatid=<target chat ID>]
<Adaptive Card JSON, version 1.3>
END_ACTION

Adaptive Card example:
{"type":"AdaptiveCard","version":"1.3","body":[{"type":"TextBlock","text":"Title","weight":"bolder","size":"medium"},{"type":"FactSet","facts":[{"title":"Key","value":"Value"}]}]}

Card elements: TextBlock, FactSet, ColumnSet/Column, Image, Container, Action.OpenUrl, Action.Submit

Rules:
- Your text reply comes FIRST, then ACTION blocks at the end.
- When the user asks to send a message to someone → use ACTION:MESSAGE with the person's name as chatid.
- NEVER use a person ID, creatorId, or userId as chatid — it is NOT a chat ID. Always use the person's NAME instead (e.g., chatid=John Lin). The system resolves names to chat IDs automatically.
- If you want to reply in the current chat, omit the chatid parameter entirely.
- When the user asks for cards, rich display, progress, reports, or structured data → use ACTION:CARD.
- When the user asks to create notes/tasks/events → use the corresponding ACTION block.
- chatid accepts a numeric Chat ID, a ![:Team](ID) mention, OR a person's name (e.g., John Test). The system will automatically resolve names to chat IDs via directory search.
- assignee accepts a numeric Person ID, a ![:Person](ID) mention, OR a person's name (e.g., assignee=John Test). The system resolves names automatically.
- If no chatid is specified, the action executes in the current chat.
- Do NOT create files. Do NOT output raw JSON in your reply. Use ACTION blocks so the system executes them.
- If no action is needed, reply normally without ACTION blocks.
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
Reply with ONLY the person's name (e.g. "John Lin"), nothing else.
If no specific person is mentioned, reply with "NONE".

Message: %s

Name:`

const defaultSummaryPrompt = `User request: %s

Please summarize the following chat messages from "%s" (%s).
These are the most recent %d messages fetched from the chat before time filtering.
Provide a concise summary in the same language as the messages. 
Highlight key topics, decisions, and action items if any.
%s
--- Messages (%d total) ---
%s
--- End of Messages ---`

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

func ActionPrompt() string      { return loadPrompt("action", defaultActionPrompt) }
func IntentPrompt() string      { return loadPrompt("intent", defaultIntentPrompt) }
func NameExtractPrompt() string { return loadPrompt("name_extract", defaultNameExtractPrompt) }
func SummaryPrompt() string     { return loadPrompt("summary", defaultSummaryPrompt) }
func HeartbeatPrompt() string   { return loadPrompt("heartbeat", defaultHeartbeatPrompt) }
