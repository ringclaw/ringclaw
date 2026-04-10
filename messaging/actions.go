package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ringclaw/ringclaw/internal/util"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// defaultActionPrompt is the built-in prompt for ACTION blocks.
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

var (
	actionPromptOnce sync.Once
	actionPromptText string
)

// ActionPrompt returns the action prompt, loading from ~/.ringclaw/action_prompt.md if it exists.
func ActionPrompt() string {
	actionPromptOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err == nil {
			customPath := filepath.Join(home, ".ringclaw", "action_prompt.md")
			if data, err := os.ReadFile(customPath); err == nil && len(data) > 0 {
				actionPromptText = "\n" + string(data) + "\n"
				slog.Info("loaded custom action prompt", "path", customPath)
				return
			}
		}
		actionPromptText = defaultActionPrompt
	})
	return actionPromptText
}

// AgentAction represents a parsed action from the agent's response.
type AgentAction struct {
	Type   string // "NOTE", "TASK", "EVENT", "CARD", "MESSAGE"
	Params map[string]string
	Body   string
}

// ParseAgentActions extracts ACTION blocks from the agent's response and returns
// the clean reply text (without ACTION blocks) and the parsed actions.
func ParseAgentActions(reply string) (string, []AgentAction) {
	var actions []AgentAction
	clean := reply

	for {
		startIdx := strings.Index(clean, "ACTION:")
		if startIdx < 0 {
			break
		}
		endIdx := strings.Index(clean[startIdx:], "END_ACTION")
		if endIdx < 0 {
			// No END_ACTION: treat the single line as a complete action (e.g. EVENT).
			lineEnd := strings.Index(clean[startIdx:], "\n")
			if lineEnd < 0 {
				lineEnd = len(clean) - startIdx
			}
			block := clean[startIdx : startIdx+lineEnd]
			action := parseActionBlock(block)
			if action != nil {
				actions = append(actions, *action)
			}
			clean = clean[:startIdx] + clean[startIdx+lineEnd:]
			continue
		}
		endIdx += startIdx + len("END_ACTION")

		block := clean[startIdx:endIdx]
		action := parseActionBlock(block)
		if action != nil {
			actions = append(actions, *action)
		}

		clean = clean[:startIdx] + clean[endIdx:]
	}

	clean = strings.TrimSpace(clean)
	return clean, actions
}

func parseActionBlock(block string) *AgentAction {
	lines := strings.SplitN(block, "\n", 2)
	if len(lines) == 0 {
		return nil
	}

	header := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(header, "ACTION:") {
		return nil
	}
	header = header[len("ACTION:"):]

	parts := strings.SplitN(header, " ", 2)
	actionType := strings.TrimSpace(parts[0])

	params := make(map[string]string)
	if len(parts) > 1 {
		for _, p := range parseActionParams(parts[1]) {
			params[p.key] = p.value
		}
	}

	body := ""
	if len(lines) > 1 {
		body = strings.TrimSuffix(lines[1], "END_ACTION")
		body = strings.TrimSpace(body)
	}

	return &AgentAction{
		Type:   actionType,
		Params: params,
		Body:   body,
	}
}

// parseActionParams parses "title=xxx start=2026-01-01T10:00:00Z end=2026-01-01T11:00:00Z"
func parseActionParams(s string) []keyValue {
	var result []keyValue
	keys := []string{"title", "subject", "start", "end", "chatid", "assignee"}
	remaining := s
	for len(remaining) > 0 {
		remaining = strings.TrimSpace(remaining)
		matched := false
		for _, key := range keys {
			prefix := key + "="
			if strings.HasPrefix(remaining, prefix) {
				remaining = remaining[len(prefix):]
				nextIdx := len(remaining)
				for _, k := range keys {
					idx := strings.Index(remaining, " "+k+"=")
					if idx >= 0 && idx < nextIdx {
						nextIdx = idx
					}
				}
				value := strings.TrimSpace(remaining[:nextIdx])
				result = append(result, keyValue{key: key, value: value})
				remaining = remaining[nextIdx:]
				matched = true
				break
			}
		}
		if !matched {
			break
		}
	}
	return result
}

// ExecuteAgentActions executes parsed actions against the RC API.
func ExecuteAgentActions(ctx context.Context, replyClient, actionClient *ringcentral.Client, chatID string, actions []AgentAction) []string {
	var results []string
	for _, a := range actions {
		targetChat := chatID
		if cid := a.Params["chatid"]; cid != "" {
			resolved, err := resolveChatParam(ctx, actionClient, cid, chatID)
			if err != nil {
				slog.Error("action: failed to resolve chatid", "chatid", cid, "error", err)
				results = append(results, fmt.Sprintf("Failed to resolve chat '%s': %v", cid, err))
				continue
			}
			targetChat = resolved
		}

		switch a.Type {
		case "NOTE":
			title := a.Params["title"]
			if title == "" {
				title = "Note"
			}
			note, err := actionClient.CreateNote(ctx, targetChat, &ringcentral.CreateNoteRequest{
				Title: title,
				Body:  a.Body,
			})
			if err != nil {
				slog.Error("action: create note failed", "error", err)
				results = append(results, fmt.Sprintf("Failed to create note: %v", err))
				continue
			}
			if pubErr := actionClient.PublishNote(ctx, note.ID); pubErr != nil {
				slog.Error("action: publish note failed", "noteID", note.ID, "error", pubErr)
			}
			slog.Info("action: created note", "noteID", note.ID, "chatID", targetChat, "title", title)

		case "TASK":
			subject := a.Params["subject"]
			if subject == "" {
				continue
			}
			req := &ringcentral.CreateTaskRequest{Subject: subject}
			if aid := a.Params["assignee"]; aid != "" {
				resolvedID, err := resolveAssigneeParam(ctx, actionClient, aid)
				if err != nil {
					slog.Error("action: failed to resolve assignee", "assignee", aid, "error", err)
					results = append(results, fmt.Sprintf("Failed to resolve assignee '%s': %v", aid, err))
					continue
				}
				req.Assignees = []ringcentral.TaskAssignee{{ID: resolvedID}}
			}
			task, err := actionClient.CreateTask(ctx, targetChat, req)
			if err != nil {
				slog.Error("action: create task failed", "error", err)
				results = append(results, fmt.Sprintf("Failed to create task: %v", err))
				continue
			}
			slog.Info("action: created task", "taskID", task.ID, "chatID", targetChat, "subject", subject)

		case "EVENT":
			title := a.Params["title"]
			startTime := a.Params["start"]
			endTime := a.Params["end"]
			if title == "" || startTime == "" || endTime == "" {
				continue
			}
			event, err := actionClient.CreateEvent(ctx, &ringcentral.CreateEventRequest{
				Title:     title,
				StartTime: startTime,
				EndTime:   endTime,
			})
			if err != nil {
				slog.Error("action: create event failed", "error", err)
				results = append(results, fmt.Sprintf("Failed to create event: %v", err))
				continue
			}
			slog.Info("action: created event", "eventID", event.ID, "title", title)

		case "CARD":
			cardJSON := a.Body
			if cardJSON == "" {
				continue
			}
			if !json.Valid([]byte(cardJSON)) {
				slog.Error("action: invalid adaptive card JSON")
				results = append(results, "Failed to create card: invalid JSON")
				continue
			}
			cardClient := selectCardClient(replyClient, actionClient, targetChat)
			card, err := cardClient.CreateAdaptiveCard(ctx, targetChat, json.RawMessage(cardJSON))
			if err != nil {
				slog.Error("action: create adaptive card failed", "error", err)
				results = append(results, fmt.Sprintf("Failed to create card: %v", err))
				continue
			}
			slog.Info("action: created adaptive card", "cardID", card.ID, "chatID", targetChat)

		case "MESSAGE":
			body := strings.TrimSpace(a.Body)
			if body == "" {
				continue
			}
			if err := SendTextReply(ctx, actionClient, targetChat, body); err != nil {
				slog.Error("action: send message failed", "error", err, "chatID", targetChat)
				results = append(results, fmt.Sprintf("Failed to send message: %v", err))
				continue
			}
			slog.Info("action: sent message", "chatID", targetChat, "text", util.Truncate(body, 60))

		default:
			slog.Warn("action: unknown action type, sending body as message", "type", a.Type)
			body := strings.TrimSpace(a.Body)
			if body != "" {
				if err := SendTextReply(ctx, replyClient, chatID, fmt.Sprintf("[%s] %s", a.Type, body)); err != nil {
					slog.Error("action: failed to send unknown action as message", "error", err)
				}
			}
			results = append(results, fmt.Sprintf("Unknown action type: %s", a.Type))
		}
	}
	return results
}

// extractChatID extracts a numeric chat ID from various formats:
// "12345", "![:Team](12345)", "![:Person](12345)"
func extractChatID(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "("); idx >= 0 {
		end := strings.Index(s[idx:], ")")
		if end > 0 {
			return s[idx+1 : idx+end]
		}
	}
	return s
}
