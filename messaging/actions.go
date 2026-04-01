package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ringclaw/ringclaw/ringcentral"
)

// IsActionCommand checks if text starts with /task, /note, or /event.
func IsActionCommand(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, cmd := range []string{"/task", "/note", "/event", "/card"} {
		if lower == cmd || strings.HasPrefix(lower, cmd+" ") {
			return true
		}
	}
	return false
}

// HandleActionCommand routes /task, /note, /event commands.
func HandleActionCommand(ctx context.Context, client *ringcentral.Client, chatID, text string) string {
	parts := strings.Fields(text)
	if len(parts) < 2 {
		return formatActionHelp(parts[0])
	}

	resource := strings.ToLower(parts[0]) // /task, /note, /event
	action := strings.ToLower(parts[1])   // list, create, get, update, delete, complete
	args := parts[2:]

	switch resource {
	case "/task":
		return handleTask(ctx, client, chatID, action, args, text)
	case "/note":
		return handleNote(ctx, client, chatID, action, args, text)
	case "/event":
		return handleEvent(ctx, client, chatID, action, args, text)
	case "/card":
		return handleCard(ctx, client, chatID, action, args)
	default:
		return "Unknown command. Use /task, /note, /event, or /card."
	}
}

// --- Task handlers ---

func handleTask(ctx context.Context, client *ringcentral.Client, chatID, action string, args []string, raw string) string {
	switch action {
	case "list":
		return taskList(ctx, client, chatID)
	case "create":
		subject := extractAfter(raw, "create")
		if subject == "" {
			return "Usage: /task create <subject>"
		}
		return taskCreate(ctx, client, chatID, subject)
	case "get":
		if len(args) == 0 {
			return "Usage: /task get <id>"
		}
		return taskGet(ctx, client, args[0])
	case "update":
		if len(args) < 2 {
			return "Usage: /task update <id> subject=<new subject>"
		}
		return taskUpdate(ctx, client, args[0], strings.Join(args[1:], " "))
	case "delete":
		if len(args) == 0 {
			return "Usage: /task delete <id>"
		}
		return taskDelete(ctx, client, args[0])
	case "complete":
		if len(args) == 0 {
			return "Usage: /task complete <id>"
		}
		return taskComplete(ctx, client, args[0])
	default:
		return formatActionHelp("/task")
	}
}

func taskList(ctx context.Context, client *ringcentral.Client, chatID string) string {
	list, err := client.ListTasks(ctx, chatID)
	if err != nil {
		slog.Error("list tasks failed", "error", err)
		return fmt.Sprintf("Error: %v", err)
	}
	if len(list.Records) == 0 {
		return "No tasks found in this chat."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Tasks** (%d)\n", len(list.Records)))
	for _, t := range list.Records {
		status := statusEmoji(t.Status)
		due := ""
		if t.DueDate != "" {
			due = fmt.Sprintf(" | due: %s", t.DueDate[:10])
		}
		sb.WriteString(fmt.Sprintf("- %s `%s` %s%s\n", status, t.ID, t.Subject, due))
	}
	return sb.String()
}

func taskCreate(ctx context.Context, client *ringcentral.Client, chatID, subject string) string {
	task, err := client.CreateTask(ctx, chatID, &ringcentral.CreateTaskRequest{Subject: subject})
	if err != nil {
		slog.Error("create task failed", "error", err)
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Task created: `%s` — %s", task.ID, task.Subject)
}

func taskGet(ctx context.Context, client *ringcentral.Client, taskID string) string {
	t, err := client.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Task** `%s`\n", t.ID))
	sb.WriteString(fmt.Sprintf("- Subject: %s\n", t.Subject))
	sb.WriteString(fmt.Sprintf("- Status: %s %s\n", statusEmoji(t.Status), t.Status))
	if t.Description != "" {
		sb.WriteString(fmt.Sprintf("- Description: %s\n", t.Description))
	}
	if t.DueDate != "" {
		sb.WriteString(fmt.Sprintf("- Due: %s\n", t.DueDate))
	}
	if len(t.Assignees) > 0 {
		ids := make([]string, len(t.Assignees))
		for i, a := range t.Assignees {
			ids[i] = fmt.Sprintf("%s(%s)", a.ID, a.Status)
		}
		sb.WriteString(fmt.Sprintf("- Assignees: %s\n", strings.Join(ids, ", ")))
	}
	return sb.String()
}

func taskUpdate(ctx context.Context, client *ringcentral.Client, taskID, fieldsRaw string) string {
	req := &ringcentral.UpdateTaskRequest{}
	for _, pair := range parseKeyValues(fieldsRaw) {
		switch pair.key {
		case "subject":
			req.Subject = pair.value
		case "description":
			req.Description = pair.value
		case "duedate", "due_date":
			req.DueDate = pair.value
		case "color":
			req.Color = pair.value
		case "status":
			req.Status = pair.value
		}
	}
	task, err := client.UpdateTask(ctx, taskID, req)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Task updated: `%s` — %s", task.ID, task.Subject)
}

func taskDelete(ctx context.Context, client *ringcentral.Client, taskID string) string {
	if err := client.DeleteTask(ctx, taskID); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Task `%s` deleted.", taskID)
}

func taskComplete(ctx context.Context, client *ringcentral.Client, taskID string) string {
	if err := client.CompleteTask(ctx, taskID); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Task `%s` marked as completed.", taskID)
}

// --- Note handlers ---

func handleNote(ctx context.Context, client *ringcentral.Client, chatID, action string, args []string, raw string) string {
	switch action {
	case "list":
		return noteList(ctx, client, chatID)
	case "create":
		content := extractAfter(raw, "create")
		if content == "" {
			return "Usage: /note create <title> | <body>"
		}
		return noteCreate(ctx, client, chatID, content)
	case "get":
		if len(args) == 0 {
			return "Usage: /note get <id>"
		}
		return noteGet(ctx, client, args[0])
	case "update":
		if len(args) < 2 {
			return "Usage: /note update <id> title=<new title>"
		}
		return noteUpdate(ctx, client, args[0], strings.Join(args[1:], " "))
	case "delete":
		if len(args) == 0 {
			return "Usage: /note delete <id>"
		}
		return noteDelete(ctx, client, args[0])
	case "lock":
		if len(args) == 0 {
			return "Usage: /note lock <id>"
		}
		return noteLock(ctx, client, args[0])
	case "unlock":
		if len(args) == 0 {
			return "Usage: /note unlock <id>"
		}
		return noteUnlock(ctx, client, args[0])
	default:
		return formatActionHelp("/note")
	}
}

func noteList(ctx context.Context, client *ringcentral.Client, chatID string) string {
	list, err := client.ListNotes(ctx, chatID)
	if err != nil {
		slog.Error("list notes failed", "error", err)
		return fmt.Sprintf("Error: %v", err)
	}
	if len(list.Records) == 0 {
		return "No notes found in this chat."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Notes** (%d)\n", len(list.Records)))
	for _, n := range list.Records {
		preview := n.Preview
		if len(preview) > 60 {
			preview = preview[:60] + "..."
		}
		sb.WriteString(fmt.Sprintf("- `%s` **%s** [%s] %s\n", n.ID, n.Title, n.Status, preview))
	}
	return sb.String()
}

func noteCreate(ctx context.Context, client *ringcentral.Client, chatID, content string) string {
	title, body := splitNoteTitleBody(content)
	note, err := client.CreateNote(ctx, chatID, &ringcentral.CreateNoteRequest{Title: title, Body: body})
	if err != nil {
		slog.Error("create note failed", "error", err)
		return fmt.Sprintf("Error: %v", err)
	}
	// Auto-publish
	if err := client.PublishNote(ctx, note.ID); err != nil {
		slog.Error("publish note failed", "error", err)
		return fmt.Sprintf("Note created (`%s`) but publish failed: %v", note.ID, err)
	}
	return fmt.Sprintf("Note created and published: `%s` — %s", note.ID, note.Title)
}

func noteGet(ctx context.Context, client *ringcentral.Client, noteID string) string {
	n, err := client.GetNote(ctx, noteID)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Note** `%s`\n", n.ID))
	sb.WriteString(fmt.Sprintf("- Title: %s\n", n.Title))
	sb.WriteString(fmt.Sprintf("- Status: %s\n", n.Status))
	if n.Preview != "" {
		sb.WriteString(fmt.Sprintf("- Preview: %s\n", n.Preview))
	}
	sb.WriteString(fmt.Sprintf("- Created: %s\n", n.CreationTime))
	return sb.String()
}

func noteUpdate(ctx context.Context, client *ringcentral.Client, noteID, fieldsRaw string) string {
	req := &ringcentral.UpdateNoteRequest{}
	for _, pair := range parseKeyValues(fieldsRaw) {
		switch pair.key {
		case "title":
			req.Title = pair.value
		case "body":
			req.Body = pair.value
		}
	}
	note, err := client.UpdateNote(ctx, noteID, req)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Note updated: `%s` — %s", note.ID, note.Title)
}

func noteDelete(ctx context.Context, client *ringcentral.Client, noteID string) string {
	if err := client.DeleteNote(ctx, noteID); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Note `%s` deleted.", noteID)
}

func noteLock(ctx context.Context, client *ringcentral.Client, noteID string) string {
	if err := client.LockNote(ctx, noteID); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Note `%s` locked for editing.", noteID)
}

func noteUnlock(ctx context.Context, client *ringcentral.Client, noteID string) string {
	if err := client.UnlockNote(ctx, noteID); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Note `%s` unlocked.", noteID)
}

// --- Event handlers ---

func handleEvent(ctx context.Context, client *ringcentral.Client, chatID, action string, args []string, raw string) string {
	switch action {
	case "list":
		if len(args) > 0 {
			return eventListGroup(ctx, client, args[0])
		}
		return eventList(ctx, client)
	case "create":
		if len(args) < 3 {
			return "Usage: /event create <title> <startTime> <endTime>\nExample: /event create Team Meeting 2026-03-26T14:00:00Z 2026-03-26T15:00:00Z"
		}
		// Last two args are start/end time, everything before is title
		endTime := args[len(args)-1]
		startTime := args[len(args)-2]
		title := strings.Join(args[:len(args)-2], " ")
		return eventCreate(ctx, client, title, startTime, endTime)
	case "get":
		if len(args) == 0 {
			return "Usage: /event get <id>"
		}
		return eventGet(ctx, client, args[0])
	case "update":
		if len(args) < 2 {
			return "Usage: /event update <id> title=<new title>"
		}
		return eventUpdate(ctx, client, args[0], strings.Join(args[1:], " "))
	case "delete":
		if len(args) == 0 {
			return "Usage: /event delete <id>"
		}
		return eventDelete(ctx, client, args[0])
	default:
		return formatActionHelp("/event")
	}
}

func eventList(ctx context.Context, client *ringcentral.Client) string {
	list, err := client.ListEvents(ctx)
	if err != nil {
		slog.Error("list events failed", "error", err)
		return fmt.Sprintf("Error: %v", err)
	}
	if len(list.Records) == 0 {
		return "No events found."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Events** (%d)\n", len(list.Records)))
	for _, e := range list.Records {
		start := ""
		if len(e.StartTime) >= 16 {
			start = e.StartTime[:16]
		}
		sb.WriteString(fmt.Sprintf("- `%s` **%s** %s\n", e.ID, e.Title, start))
	}
	return sb.String()
}

func eventListGroup(ctx context.Context, client *ringcentral.Client, groupID string) string {
	list, err := client.ListGroupEvents(ctx, groupID)
	if err != nil {
		slog.Error("list group events failed", "error", err)
		return fmt.Sprintf("Error: %v", err)
	}
	if len(list.Records) == 0 {
		return fmt.Sprintf("No events found in chat `%s`.", groupID)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Events in chat %s** (%d)\n", groupID, len(list.Records)))
	for _, e := range list.Records {
		start := ""
		if len(e.StartTime) >= 16 {
			start = e.StartTime[:16]
		}
		sb.WriteString(fmt.Sprintf("- `%s` **%s** %s\n", e.ID, e.Title, start))
	}
	return sb.String()
}

func eventCreate(ctx context.Context, client *ringcentral.Client, title, startTime, endTime string) string {
	event, err := client.CreateEvent(ctx, &ringcentral.CreateEventRequest{
		Title:     title,
		StartTime: startTime,
		EndTime:   endTime,
	})
	if err != nil {
		slog.Error("create event failed", "error", err)
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Event created: `%s` — %s (%s ~ %s)", event.ID, event.Title, event.StartTime, event.EndTime)
}

func eventGet(ctx context.Context, client *ringcentral.Client, eventID string) string {
	e, err := client.GetEvent(ctx, eventID)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Event** `%s`\n", e.ID))
	sb.WriteString(fmt.Sprintf("- Title: %s\n", e.Title))
	sb.WriteString(fmt.Sprintf("- Start: %s\n", e.StartTime))
	sb.WriteString(fmt.Sprintf("- End: %s\n", e.EndTime))
	if e.Location != "" {
		sb.WriteString(fmt.Sprintf("- Location: %s\n", e.Location))
	}
	if e.Description != "" {
		sb.WriteString(fmt.Sprintf("- Description: %s\n", e.Description))
	}
	if e.Color != "" {
		sb.WriteString(fmt.Sprintf("- Color: %s\n", e.Color))
	}
	return sb.String()
}

func eventUpdate(ctx context.Context, client *ringcentral.Client, eventID, fieldsRaw string) string {
	req := &ringcentral.UpdateEventRequest{}
	for _, pair := range parseKeyValues(fieldsRaw) {
		switch pair.key {
		case "title":
			req.Title = pair.value
		case "starttime", "start_time":
			req.StartTime = pair.value
		case "endtime", "end_time":
			req.EndTime = pair.value
		case "location":
			req.Location = pair.value
		case "description":
			req.Description = pair.value
		case "color":
			req.Color = pair.value
		}
	}
	event, err := client.UpdateEvent(ctx, eventID, req)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Event updated: `%s` — %s", event.ID, event.Title)
}

func eventDelete(ctx context.Context, client *ringcentral.Client, eventID string) string {
	if err := client.DeleteEvent(ctx, eventID); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Event `%s` deleted.", eventID)
}

// --- Card handlers ---

func handleCard(ctx context.Context, client *ringcentral.Client, chatID, action string, args []string) string {
	switch action {
	case "get":
		if len(args) == 0 {
			return "Usage: /card get <id>"
		}
		return cardGet(ctx, client, args[0])
	case "delete":
		if len(args) == 0 {
			return "Usage: /card delete <id>"
		}
		return cardDelete(ctx, client, args[0])
	default:
		return formatActionHelp("/card")
	}
}

func cardGet(ctx context.Context, client *ringcentral.Client, cardID string) string {
	card, err := client.GetAdaptiveCard(ctx, cardID)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Adaptive Card** `%s`\n", card.ID))
	sb.WriteString(fmt.Sprintf("- Type: %s\n", card.Type))
	sb.WriteString(fmt.Sprintf("- Version: %s\n", card.Version))
	sb.WriteString(fmt.Sprintf("- Created: %s\n", card.CreationTime))
	if len(card.ChatIDs) > 0 {
		sb.WriteString(fmt.Sprintf("- Chats: %s\n", strings.Join(card.ChatIDs, ", ")))
	}
	return sb.String()
}

func cardDelete(ctx context.Context, client *ringcentral.Client, cardID string) string {
	if err := client.DeleteAdaptiveCard(ctx, cardID); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Card `%s` deleted.", cardID)
}

// --- Helpers ---

type keyValue struct {
	key   string
	value string
}

// parseKeyValues parses "key=value key2=value2" from a string.
func parseKeyValues(s string) []keyValue {
	var result []keyValue
	for _, part := range splitKeyValueParts(s) {
		idx := strings.IndexByte(part, '=')
		if idx > 0 {
			result = append(result, keyValue{
				key:   strings.ToLower(strings.TrimSpace(part[:idx])),
				value: strings.TrimSpace(part[idx+1:]),
			})
		}
	}
	return result
}

// splitKeyValueParts splits "key1=value one key2=value two" into ["key1=value one", "key2=value two"].
func splitKeyValueParts(s string) []string {
	var parts []string
	words := strings.Fields(s)
	var current strings.Builder
	for _, w := range words {
		if strings.Contains(w, "=") && current.Len() > 0 {
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(w)
	}
	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}
	return parts
}

// extractAfter returns the text after the first occurrence of keyword in raw.
func extractAfter(raw, keyword string) string {
	lower := strings.ToLower(raw)
	idx := strings.Index(lower, strings.ToLower(keyword))
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(raw[idx+len(keyword):])
}

// splitNoteTitleBody splits "title | body" or just "title".
func splitNoteTitleBody(content string) (string, string) {
	parts := strings.SplitN(content, "|", 2)
	title := strings.TrimSpace(parts[0])
	body := ""
	if len(parts) == 2 {
		body = strings.TrimSpace(parts[1])
	}
	return title, body
}

func statusEmoji(status string) string {
	switch status {
	case "Completed":
		return "[v]"
	case "InProgress":
		return "[~]"
	default:
		return "[ ]"
	}
}

// ActionPrompt is appended to prompts to enable the AI agent to trigger actions.
const ActionPrompt = `

IMPORTANT: You are running inside a RingCentral Team Messaging bot. You have REAL actions that execute via API — do NOT generate files, do NOT suggest manual steps. Instead, append ACTION blocks and the system will execute them automatically.

Available actions (append at the END of your response):

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
- When the user asks for cards, rich display, progress, reports, or structured data → use ACTION:CARD.
- When the user asks to create notes/tasks/events → use the corresponding ACTION block.
- chatid accepts a numeric Chat ID, a ![:Team](ID) mention, OR a person's name (e.g., chatid=Ian Zhang). The system will automatically resolve names to chat IDs via directory search.
- assignee accepts a numeric Person ID, a ![:Person](ID) mention, OR a person's name (e.g., assignee=Ian Zhang). The system resolves names automatically.
- If no chatid is specified, the action executes in the current chat.
- Do NOT create files. Do NOT output raw JSON in your reply. Use ACTION blocks so the system executes them.
- If no action is needed, reply normally without ACTION blocks.
`

// AgentAction represents a parsed action from the agent's response.
type AgentAction struct {
	Type   string // "NOTE", "TASK", "EVENT", "CARD"
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
	// header: "ACTION:NOTE title=xxx" or "ACTION:TASK subject=xxx"
	if !strings.HasPrefix(header, "ACTION:") {
		return nil
	}
	header = header[len("ACTION:"):]

	parts := strings.SplitN(header, " ", 2)
	actionType := strings.TrimSpace(parts[0])

	params := make(map[string]string)
	if len(parts) > 1 {
		paramStr := parts[1]
		// Parse key=value pairs; handle start= and end= with ISO timestamps
		for _, p := range parseActionParams(paramStr) {
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
	// Split by known keys to handle values with spaces
	keys := []string{"title", "subject", "start", "end", "chatid", "assignee"}
	remaining := s
	for len(remaining) > 0 {
		remaining = strings.TrimSpace(remaining)
		matched := false
		for _, key := range keys {
			prefix := key + "="
			if strings.HasPrefix(remaining, prefix) {
				remaining = remaining[len(prefix):]
				// Find next key= or end of string
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

// resolveNameToChatID resolves a person name to a Direct chat ID via directory search.
func resolveNameToChatID(ctx context.Context, client *ringcentral.Client, name string) (string, error) {
	result, err := client.SearchDirectory(ctx, name)
	if err != nil {
		return "", fmt.Errorf("directory search: %w", err)
	}
	if len(result.Records) == 0 {
		return "", fmt.Errorf("no person found matching '%s'", name)
	}

	var best *ringcentral.DirectoryEntry
	for i := range result.Records {
		e := &result.Records[i]
		fullName := strings.TrimSpace(e.FirstName + " " + e.LastName)
		if fuzzyMatch(fullName, name) || fuzzyMatch(e.Email, name) {
			best = e
			break
		}
	}
	if best == nil {
		return "", fmt.Errorf("no person matched '%s' (got %d results)", name, len(result.Records))
	}

	fullName := strings.TrimSpace(best.FirstName + " " + best.LastName)
	slog.Info("action: resolved person", "name", name, "match", fullName, "id", best.ID)

	chat, err := client.CreateConversation(ctx, []string{best.ID})
	if err != nil {
		return "", fmt.Errorf("create conversation with %s: %w", fullName, err)
	}
	return chat.ID, nil
}

// resolveNameToPersonID resolves a person name to a person ID via directory search.
func resolveNameToPersonID(ctx context.Context, client *ringcentral.Client, name string) (string, error) {
	result, err := client.SearchDirectory(ctx, name)
	if err != nil {
		return "", fmt.Errorf("directory search: %w", err)
	}
	if len(result.Records) == 0 {
		return "", fmt.Errorf("no person found matching '%s'", name)
	}

	var best *ringcentral.DirectoryEntry
	for i := range result.Records {
		e := &result.Records[i]
		fullName := strings.TrimSpace(e.FirstName + " " + e.LastName)
		if fuzzyMatch(fullName, name) || fuzzyMatch(e.Email, name) {
			best = e
			break
		}
	}
	if best == nil {
		return "", fmt.Errorf("no person matched '%s'", name)
	}

	fullName := strings.TrimSpace(best.FirstName + " " + best.LastName)
	slog.Info("action: resolved assignee", "name", name, "match", fullName, "id", best.ID)
	return best.ID, nil
}

func isNumericID(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// resolveChatParam resolves a chatid param: numeric IDs pass through,
// names are resolved via directory search + conversation creation.
func resolveChatParam(ctx context.Context, client *ringcentral.Client, raw string) (string, error) {
	id := extractChatID(raw)
	if isNumericID(id) {
		return id, nil
	}
	return resolveNameToChatID(ctx, client, id)
}

// resolveAssigneeParam resolves an assignee param: numeric IDs pass through,
// names are resolved via directory search.
func resolveAssigneeParam(ctx context.Context, client *ringcentral.Client, raw string) (string, error) {
	id := extractChatID(raw)
	if isNumericID(id) {
		return id, nil
	}
	return resolveNameToPersonID(ctx, client, id)
}

// selectCardClient picks the right client for adaptive card creation.
// Bot DM → bot client (card appears as bot's message).
// Everything else → action client (private app has broader access).
func selectCardClient(replyClient, actionClient *ringcentral.Client, targetChat string) *ringcentral.Client {
	if replyClient != nil && replyClient.IsBot() && replyClient.IsBotDM(targetChat) {
		return replyClient
	}
	if actionClient != nil {
		return actionClient
	}
	return replyClient
}

// ExecuteAgentActions executes parsed actions against the RC API.
func ExecuteAgentActions(ctx context.Context, replyClient, actionClient *ringcentral.Client, chatID string, actions []AgentAction) []string {
	var results []string
	for _, a := range actions {
		targetChat := chatID
		if cid := a.Params["chatid"]; cid != "" {
			resolved, err := resolveChatParam(ctx, actionClient, cid)
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
		}
	}
	return results
}

// extractChatID extracts a numeric chat ID from various formats:
// "12345", "![:Team](12345)", "![:Person](12345)"
func extractChatID(s string) string {
	s = strings.TrimSpace(s)
	// Handle mention format: ![:Team](12345) or ![:Person](12345)
	if idx := strings.Index(s, "("); idx >= 0 {
		end := strings.Index(s[idx:], ")")
		if end > 0 {
			return s[idx+1 : idx+end]
		}
	}
	return s
}

func formatActionHelp(cmd string) string {
	switch cmd {
	case "/task":
		return "Usage:\n- /task list\n- /task create <subject>\n- /task get <id>\n- /task update <id> subject=<value>\n- /task delete <id>\n- /task complete <id>"
	case "/note":
		return "Usage:\n- /note list\n- /note create <title> | <body>\n- /note get <id>\n- /note update <id> title=<value>\n- /note delete <id>\n- /note lock <id>\n- /note unlock <id>"
	case "/event":
		return "Usage:\n- /event list [chatId]\n- /event create <title> <startTime> <endTime>\n- /event get <id>\n- /event update <id> title=<value>\n- /event delete <id>"
	case "/card":
		return "Usage:\n- /card get <id>\n- /card delete <id>"
	default:
		return "Available commands: /task, /note, /event, /card"
	}
}
