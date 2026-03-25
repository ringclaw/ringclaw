package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ringclaw/ringclaw/ringcentral"
)

// IsActionCommand checks if text starts with /task, /note, or /event.
func IsActionCommand(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, cmd := range []string{"/task", "/note", "/event"} {
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
	default:
		return "Unknown command. Use /task, /note, or /event."
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

// --- Event handlers ---

func handleEvent(ctx context.Context, client *ringcentral.Client, chatID, action string, args []string, raw string) string {
	switch action {
	case "list":
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

func formatActionHelp(cmd string) string {
	switch cmd {
	case "/task":
		return "Usage:\n- /task list\n- /task create <subject>\n- /task get <id>\n- /task update <id> subject=<value>\n- /task delete <id>\n- /task complete <id>"
	case "/note":
		return "Usage:\n- /note list\n- /note create <title> | <body>\n- /note get <id>\n- /note update <id> title=<value>\n- /note delete <id>"
	case "/event":
		return "Usage:\n- /event list\n- /event create <title> <startTime> <endTime>\n- /event get <id>\n- /event update <id> title=<value>\n- /event delete <id>"
	default:
		return "Available commands: /task, /note, /event"
	}
}
