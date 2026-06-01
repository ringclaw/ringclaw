package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
)

func recentCutoff() string {
	return time.Now().AddDate(0, -3, 0).Format(time.RFC3339)
}

// IsActionCommand checks if text starts with a RingCentral resource command.
func IsActionCommand(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, cmd := range []string{"/task", "/note", "/event", "/card", "/video", "/phone", "/sms"} {
		if lower == cmd || strings.HasPrefix(lower, cmd+" ") {
			return true
		}
	}
	return false
}

func actionCommandCapability(text string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(text)))
	if len(fields) == 0 {
		return ""
	}
	switch fields[0] {
	case "/video":
		return "video"
	case "/phone":
		if len(fields) > 1 && fields[1] == "calllog" {
			return "call_log"
		}
		return "phone"
	case "/sms":
		return "sms"
	default:
		return ""
	}
}

func capabilityDisabledMessage(capability string) string {
	switch strings.ToLower(strings.TrimSpace(capability)) {
	case "video":
		return "Video capability is not enabled for this AVA bot. Enable Video during onboarding and verify the Private JWT App has the Video scope."
	case "phone":
		return "Phone capability is not enabled for this AVA bot. Enable Phone during onboarding and verify the Private JWT App has RingOut and ReadCallLog scopes."
	case "call_log":
		return "Call log capability is not enabled for this AVA bot. Enable Phone during onboarding or verify the Private JWT App has the ReadCallLog scope."
	case "sms":
		return "SMS capability is not enabled for this AVA bot. Enable SMS during onboarding and verify the Private JWT App has the SMS scope."
	default:
		return fmt.Sprintf("%s capability is not enabled for this AVA bot.", capability)
	}
}

// HandleActionCommand routes RingCentral resource commands.
func HandleActionCommand(ctx context.Context, client *ringcentral.Client, chatID, text string) string {
	return HandleActionCommandWithRequester(ctx, client, chatID, text, "")
}

func HandleActionCommandWithRequester(ctx context.Context, client *ringcentral.Client, chatID, text, requesterID string) string {
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
	case "/video":
		return handleVideo(ctx, client, action, args, text, requesterID)
	case "/phone":
		return handlePhone(ctx, client, action, args, requesterID)
	case "/sms":
		return handleSMS(ctx, client, action, args)
	default:
		return "Unknown command. Use /task, /note, /event, /card, /video, /phone, or /sms."
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
	cutoff := recentCutoff()
	filtered := list.Records[:0]
	for _, t := range list.Records {
		if t.CreationTime >= cutoff {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		return "No tasks found in this chat (last 3 months)."
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreationTime > filtered[j].CreationTime
	})
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Tasks** (%d)\n", len(filtered)))
	for _, t := range filtered {
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
	cutoff := recentCutoff()
	filtered := list.Records[:0]
	for _, n := range list.Records {
		if n.CreationTime >= cutoff {
			filtered = append(filtered, n)
		}
	}
	if len(filtered) == 0 {
		return "No notes found in this chat (last 3 months)."
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreationTime > filtered[j].CreationTime
	})
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Notes** (%d)\n", len(filtered)))
	for _, n := range filtered {
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
	cutoff := recentCutoff()
	filtered := list.Records[:0]
	for _, e := range list.Records {
		if e.StartTime >= cutoff {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == 0 {
		return "No events found (last 3 months)."
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].StartTime > filtered[j].StartTime
	})
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Events** (%d)\n", len(filtered)))
	for _, e := range filtered {
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
	cutoff := recentCutoff()
	filtered := list.Records[:0]
	for _, e := range list.Records {
		if e.StartTime >= cutoff {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == 0 {
		return fmt.Sprintf("No events found in chat `%s` (last 3 months).", groupID)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].StartTime > filtered[j].StartTime
	})
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Events in chat %s** (%d)\n", groupID, len(filtered)))
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
	startTime, endTime, err := normalizeEventDateTimes(startTime, endTime)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
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
		case "start", "starttime", "start_time":
			normalized, err := normalizeEventDateTime(pair.value)
			if err != nil {
				return fmt.Sprintf("Error: invalid event start time: %v", err)
			}
			req.StartTime = normalized
		case "end", "endtime", "end_time":
			normalized, err := normalizeEventDateTime(pair.value)
			if err != nil {
				return fmt.Sprintf("Error: invalid event end time: %v", err)
			}
			req.EndTime = normalized
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

func normalizeEventDateTimes(startTime, endTime string) (string, string, error) {
	normalizedStart, err := normalizeEventDateTime(startTime)
	if err != nil {
		return "", "", fmt.Errorf("invalid event start time: %w", err)
	}
	normalizedEnd, err := normalizeEventDateTime(endTime)
	if err != nil {
		return "", "", fmt.Errorf("invalid event end time: %w", err)
	}
	return normalizedStart, normalizedEnd, nil
}

func normalizeEventDateTime(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("empty value")
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.Format(time.RFC3339), nil
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed.Format(time.RFC3339), nil
		}
	}
	return "", fmt.Errorf("%q must be ISO8601/RFC3339, e.g. 2026-06-01T12:00:00Z", value)
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

// --- Video handlers ---

func handleVideo(ctx context.Context, client *ringcentral.Client, action string, args []string, _ string, requesterID string) string {
	switch action {
	case "list":
		return videoList(ctx, client, requesterID)
	case "create":
		opts := parseVideoCreateArgs(args)
		if opts.Title == "" {
			return "Usage: /video create <title> [type=Instant|Scheduled|PMI] [start=<ISO8601> end=<ISO8601>]"
		}
		return videoCreate(ctx, client, opts)
	case "get":
		if len(args) == 0 {
			return "Usage: /video get <bridgeId>"
		}
		return videoGet(ctx, client, args[0])
	case "delete":
		if len(args) == 0 {
			return "Usage: /video delete <bridgeId>"
		}
		return videoDelete(ctx, client, args[0])
	default:
		return formatActionHelp("/video")
	}
}

type videoCreateOptions struct {
	Title      string
	BridgeType string
	StartTime  string
	EndTime    string
}

func parseVideoCreateArgs(args []string) videoCreateOptions {
	opts := videoCreateOptions{BridgeType: "Instant"}
	var titleParts []string
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if ok {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "type":
				if v := strings.TrimSpace(value); v != "" {
					opts.BridgeType = v
				}
				continue
			case "start":
				opts.StartTime = strings.TrimSpace(value)
				continue
			case "end":
				opts.EndTime = strings.TrimSpace(value)
				continue
			}
		}
		titleParts = append(titleParts, arg)
	}
	opts.Title = strings.TrimSpace(strings.Join(titleParts, " "))
	return opts
}

func videoList(ctx context.Context, client *ringcentral.Client, requesterID string) string {
	if err := ensureAuthenticatedRequester(client, requesterID, "Video meeting history"); err != nil {
		return fmt.Sprintf("Error: %s", err)
	}
	list, err := client.ListVideoMeetingHistory(ctx, ringcentral.VideoMeetingHistoryOptions{
		Type:    "All",
		PerPage: 20,
	})
	if err != nil {
		slog.Error("list video meeting history failed", "error", err)
		return fmt.Sprintf("Error: %s", friendlyVideoAPIError(err))
	}
	if len(list.Meetings) == 0 {
		return "No video meeting records found."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Video meeting records** (%d)\n", len(list.Meetings)))
	for _, meeting := range list.Meetings {
		name := strings.TrimSpace(meeting.DisplayName)
		if name == "" {
			name = "Video meeting"
		}
		status := strings.TrimSpace(meeting.Status)
		if status == "" {
			status = "Unknown"
		}
		sb.WriteString(fmt.Sprintf("- `%s` **%s** [%s]", meeting.ID, name, status))
		if meeting.StartTime != "" {
			sb.WriteString(fmt.Sprintf(" start=%s", meeting.StartTime))
		}
		sb.WriteByte('\n')
	}
	return strings.TrimSpace(sb.String())
}

func videoCreate(ctx context.Context, client *ringcentral.Client, opts videoCreateOptions) string {
	bridge, event, err := createVideoMeeting(ctx, client, opts)
	if err != nil {
		slog.Error("create video bridge failed", "error", err)
		return fmt.Sprintf("Error: %s", friendlyVideoAPIError(err))
	}
	return formatVideoMeetingMessage(bridge, event)
}

func videoGet(ctx context.Context, client *ringcentral.Client, bridgeID string) string {
	bridge, err := client.GetVideoBridge(ctx, bridgeID)
	if err != nil {
		return fmt.Sprintf("Error: %s", friendlyVideoAPIError(err))
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Video bridge** `%s`\n", bridge.ID))
	sb.WriteString(fmt.Sprintf("- Name: %s\n", bridge.Name))
	sb.WriteString(fmt.Sprintf("- Type: %s\n", bridge.Type))
	if bridge.Discovery.Web != "" {
		sb.WriteString(fmt.Sprintf("- Join: %s\n", bridge.Discovery.Web))
	}
	return sb.String()
}

func videoDelete(ctx context.Context, client *ringcentral.Client, bridgeID string) string {
	if err := client.DeleteVideoBridge(ctx, bridgeID); err != nil {
		return fmt.Sprintf("Error: %s", friendlyVideoAPIError(err))
	}
	return fmt.Sprintf("Video bridge `%s` deleted.", bridgeID)
}

func friendlyVideoAPIError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.Contains(msg, "permissionName\":\"Video") ||
		strings.Contains(msg, "[Video] permission") ||
		strings.Contains(msg, "permissionName: Video") {
		return "Video permission is missing. Ask an admin to add `Video` to the Private JWT App, regenerate or rotate the JWT token, then rerun RC JWT preflight/onboarding."
	}
	if strings.Contains(msg, "permissionName\":\"ManageCloudCalendars") ||
		strings.Contains(msg, "[ManageCloudCalendars] permission") ||
		strings.Contains(msg, "permissionName: ManageCloudCalendars") {
		return "ManageCloudCalendars permission is missing. Ask an admin to add `ManageCloudCalendars` to the Private JWT App, regenerate or rotate the JWT token, then rerun RC JWT preflight/onboarding."
	}
	return msg
}

// --- Phone handlers ---

func handlePhone(ctx context.Context, client *ringcentral.Client, action string, args []string, requesterID string) string {
	switch action {
	case "ringout":
		if len(args) == 0 {
			return "Usage: /phone ringout <toPhone> [from=<phone>] [callerid=<phone>] [playprompt=true]"
		}
		params := map[string]string{}
		keyValueStart := 1
		if len(args) >= 2 && !strings.Contains(args[1], "=") {
			params["from"] = args[0]
			params["to"] = args[1]
			keyValueStart = 2
		} else {
			params["to"] = args[0]
		}
		for _, pair := range parseKeyValues(strings.Join(args[keyValueStart:], " ")) {
			params[pair.key] = pair.value
		}
		return phoneRingOut(ctx, client, params)
	case "status":
		if len(args) == 0 {
			return "Usage: /phone status <ringOutId>"
		}
		return phoneRingOutStatus(ctx, client, args[0])
	case "cancel":
		if len(args) == 0 {
			return "Usage: /phone cancel <ringOutId>"
		}
		return phoneRingOutCancel(ctx, client, args[0])
	case "calllog":
		opts := CallLogOptionsFromPairs(parseKeyValues(strings.Join(args, " ")))
		opts.ExtensionID = strings.TrimSpace(requesterID)
		if opts.RecordCount == 0 {
			opts.RecordCount = 10
		}
		return phoneCallLog(ctx, client, opts)
	case "missed":
		opts := CallLogOptionsFromPairs(parseKeyValues(strings.Join(args, " ")))
		opts.ExtensionID = strings.TrimSpace(requesterID)
		if opts.RecordCount == 0 {
			opts.RecordCount = 25
		}
		opts.Direction = "Inbound"
		opts.Result = "Missed"
		return phoneCallLog(ctx, client, opts)
	default:
		return formatActionHelp("/phone")
	}
}

// --- SMS handlers ---

func handleSMS(ctx context.Context, client *ringcentral.Client, action string, args []string) string {
	switch action {
	case "send":
		if len(args) < 2 {
			return "Usage: /sms send <toPhone> <message> [from=<phone>] or /sms send to=<name|phone> text=<message> [from=<phone>]"
		}
		params := map[string]string{}
		keyed := parseKeyValues(strings.Join(args, " "))
		if len(keyed) > 0 {
			for _, pair := range keyed {
				params[pair.key] = pair.value
			}
			if strings.TrimSpace(params["text"]) == "" {
				if text := strings.TrimSpace(params["body"]); text != "" {
					params["text"] = text
				}
			}
		}
		if strings.TrimSpace(params["to"]) == "" {
			params["to"] = args[0]
			var bodyParts []string
			for _, arg := range args[1:] {
				if value, ok := strings.CutPrefix(arg, "from="); ok {
					params["from"] = strings.TrimSpace(value)
					continue
				}
				bodyParts = append(bodyParts, arg)
			}
			params["text"] = strings.TrimSpace(strings.Join(bodyParts, " "))
		}
		return smsSend(ctx, client, params)
	default:
		return formatActionHelp("/sms")
	}
}

func smsSend(ctx context.Context, client *ringcentral.Client, params map[string]string) string {
	to := strings.TrimSpace(params["to"])
	text := strings.TrimSpace(params["text"])
	if to == "" || text == "" {
		return "Usage: /sms send <toPhone> <message> [from=<phone>] or /sms send to=<name|phone> text=<message> [from=<phone>]"
	}
	targetLabel := to
	if !looksLikePhoneNumber(to) {
		number, label, err := resolveNameToPhoneNumber(ctx, client, to)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		slog.Info("action: resolved sms target", "target", to, "match", label, "phone", number)
		to = number
		if strings.TrimSpace(label) != "" {
			targetLabel = label
		}
	}
	from := strings.TrimSpace(params["from"])
	var err error
	if from == "" {
		from, err = defaultSMSSenderNumber(ctx, client)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
	}
	msg, err := client.SendSMS(ctx, &ringcentral.CreateSMSRequest{
		From: ringcentral.PhoneNumberRef{PhoneNumber: from},
		To:   []ringcentral.PhoneNumberRef{{PhoneNumber: to}},
		Text: text,
	})
	if err != nil {
		slog.Error("send sms failed", "error", err)
		return fmt.Sprintf("Error: %s", friendlyPhoneAPIError(err))
	}
	status := strings.TrimSpace(msg.MessageStatus)
	if status == "" {
		status = "Queued"
	}
	messageID := ringcentral.FormatResourceID(msg.ID)
	if targetLabel != "" && !strings.EqualFold(strings.TrimSpace(targetLabel), strings.TrimSpace(to)) {
		return fmt.Sprintf("SMS sent: `%s` — %s (%s -> %s, %s)", messageID, status, from, targetLabel, to)
	}
	return fmt.Sprintf("SMS sent: `%s` — %s (%s -> %s)", messageID, status, from, to)
}

func phoneRingOut(ctx context.Context, client *ringcentral.Client, params map[string]string) string {
	req, err := ringOutRequestFromParams(ctx, client, params)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	ringOut, err := client.CreateRingOut(ctx, req)
	if err != nil {
		slog.Error("create ringout failed", "error", err)
		return fmt.Sprintf("Error: %s", friendlyPhoneAPIError(err))
	}
	return fmt.Sprintf("RingOut started: `%s` — %s", ringOut.ID, ringOut.Status.CallStatus)
}

func phoneRingOutStatus(ctx context.Context, client *ringcentral.Client, ringOutID string) string {
	ringOut, err := client.GetRingOut(ctx, ringOutID)
	if err != nil {
		return fmt.Sprintf("Error: %s", friendlyPhoneAPIError(err))
	}
	return fmt.Sprintf("RingOut `%s`: call=%s caller=%s callee=%s", ringOut.ID, ringOut.Status.CallStatus, ringOut.Status.CallerStatus, ringOut.Status.CalleeStatus)
}

func phoneRingOutCancel(ctx context.Context, client *ringcentral.Client, ringOutID string) string {
	if err := client.DeleteRingOut(ctx, ringOutID); err != nil {
		return fmt.Sprintf("Error: %s", friendlyPhoneAPIError(err))
	}
	return fmt.Sprintf("RingOut `%s` cancelled.", ringOutID)
}

func phoneCallLog(ctx context.Context, client *ringcentral.Client, opts ringcentral.CallLogOptions) string {
	list, err := client.ListExtensionCallLog(ctx, opts)
	if err != nil {
		slog.Error("list call log failed", "error", err)
		return fmt.Sprintf("Error: %s", friendlyPhoneAPIError(err))
	}
	if len(list.Records) == 0 {
		if strings.EqualFold(opts.Result, "Missed") {
			return "No missed call records found."
		}
		return "No call log records found."
	}
	var sb strings.Builder
	records := filterCallLogRecords(list.Records, opts)
	if len(records) == 0 {
		if strings.EqualFold(opts.Result, "Missed") {
			return "No missed call records found."
		}
		return "No call log records found."
	}
	sb.WriteString(fmt.Sprintf("**Call Log** (%d)\n", len(records)))
	for _, rec := range records {
		result := strings.TrimSpace(rec.Result)
		if result == "" {
			result = "Unknown"
		}
		sb.WriteString(fmt.Sprintf("- `%s` %s %s [%s] %s -> %s (%ds)\n",
			rec.ID, rec.StartTime, rec.Direction, result, rec.From.PhoneNumber, rec.To.PhoneNumber, rec.Duration))
	}
	return sb.String()
}

func filterCallLogRecords(records []ringcentral.CallLogRecord, opts ringcentral.CallLogOptions) []ringcentral.CallLogRecord {
	result := strings.TrimSpace(opts.Result)
	if result == "" {
		return records
	}
	filtered := make([]ringcentral.CallLogRecord, 0, len(records))
	for _, rec := range records {
		if strings.EqualFold(strings.TrimSpace(rec.Result), result) {
			filtered = append(filtered, rec)
		}
	}
	return filtered
}

func defaultSMSSenderNumber(ctx context.Context, client *ringcentral.Client) (string, error) {
	list, err := client.ListExtensionPhoneNumbers(ctx)
	if err != nil {
		return "", fmt.Errorf("list current extension phone numbers: %w", err)
	}
	for _, record := range list.Records {
		if phone := strings.TrimSpace(record.PhoneNumber); phone != "" && extensionPhoneNumberActive(record) {
			return phone, nil
		}
	}
	return "", fmt.Errorf("current extension has no active phone number for SMS; pass from=<owned phone number>")
}

func extensionPhoneNumberActive(record ringcentral.ExtensionPhoneNumber) bool {
	status := strings.TrimSpace(record.Status)
	return status == "" || strings.EqualFold(status, "Normal")
}

// --- Helpers ---

type keyValue struct {
	key   string
	value string
}

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

func CallLogOptionsFromPairs(pairs []keyValue) ringcentral.CallLogOptions {
	var opts ringcentral.CallLogOptions
	for _, pair := range pairs {
		switch pair.key {
		case "view":
			opts.View = pair.value
		case "direction":
			opts.Direction = pair.value
		case "type":
			opts.Type = pair.value
		case "result":
			opts.Result = pair.value
		case "datefrom", "date_from":
			opts.DateFrom = pair.value
		case "dateto", "date_to":
			opts.DateTo = pair.value
		case "recordcount", "record_count", "limit":
			var n int
			if _, err := fmt.Sscanf(pair.value, "%d", &n); err == nil && n > 0 {
				opts.RecordCount = n
			}
		}
	}
	return opts
}

func extractAfter(raw, keyword string) string {
	lower := strings.ToLower(raw)
	idx := strings.Index(lower, strings.ToLower(keyword))
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(raw[idx+len(keyword):])
}

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
		return "Usage:\n- /note list\n- /note create <title> | <body>\n- /note get <id>\n- /note update <id> title=<value>\n- /note delete <id>\n- /note lock <id>\n- /note unlock <id>"
	case "/event":
		return "Usage:\n- /event list [chatId]\n- /event create <title> <startTime> <endTime>\n- /event get <id>\n- /event update <id> title=<value>\n- /event delete <id>"
	case "/card":
		return "Usage:\n- /card get <id>\n- /card delete <id>"
	case "/video":
		return "Usage:\n- /video list\n- /video create <title> [type=Instant|Scheduled|PMI] [start=<ISO8601> end=<ISO8601>]\n- /video get <bridgeId>\n- /video delete <bridgeId>"
	case "/phone":
		return "Usage:\n- /phone ringout <toPhone> [from=<phone>] [callerid=<phone>] [playprompt=true]\n- /phone status <ringOutId>\n- /phone cancel <ringOutId>\n- /phone calllog [direction=Inbound|Outbound] [result=Missed] [view=Simple|Detailed] [limit=10]\n- /phone missed [limit=25]"
	case "/sms":
		return "Usage:\n- /sms send <toPhone> <message> [from=<phone>]\n- /sms send to=<name|phone> text=<message> [from=<phone>]"
	default:
		return "Available commands: /task, /note, /event, /card, /video, /phone, /sms"
	}
}
