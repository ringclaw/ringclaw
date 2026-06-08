package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/messaging"
	"github.com/ringclaw/ringclaw/ringcentral"
)

const runtimeInteractivePollInterval = 2 * time.Second

type runtimeInteractiveEventsRequest struct {
	BotID          string `json:"bot_id"`
	BootstrapToken string `json:"bootstrap_token"`
	Limit          int    `json:"limit,omitempty"`
}

type runtimeInteractiveEventsResult struct {
	Events []runtimeInteractiveEvent `json:"events"`
}

type runtimeInteractiveEventAckRequest struct {
	BotID          string `json:"bot_id"`
	BootstrapToken string `json:"bootstrap_token"`
	Status         string `json:"status,omitempty"`
	StatusReason   string `json:"status_reason,omitempty"`
}

type runtimeInteractiveEvent struct {
	ID             string         `json:"id"`
	UserID         string         `json:"user_id"`
	UserFirstName  string         `json:"user_first_name,omitempty"`
	UserLastName   string         `json:"user_last_name,omitempty"`
	ConversationID string         `json:"conversation_id"`
	PostID         string         `json:"post_id,omitempty"`
	CardID         string         `json:"card_id,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
	EventTimestamp string         `json:"event_timestamp,omitempty"`
}

func startRuntimeInteractiveEventPoller(ctx context.Context, c *clients, handler *messaging.Handler) func() {
	controlPlaneURL := firstEnv("AVA_CONTROL_PLANE_URL", "CONTROL_PLANE_URL")
	botID := firstEnv("RINGCLAW_BOT_ID", "BOT_ID")
	bootstrapToken := firstEnv("RINGCLAW_BOOTSTRAP_TOKEN", "BOOTSTRAP_TOKEN")
	if strings.TrimSpace(controlPlaneURL) == "" || strings.TrimSpace(botID) == "" || strings.TrimSpace(bootstrapToken) == "" {
		return func() {}
	}
	if c == nil || c.bot == nil || handler == nil {
		return func() {}
	}
	pollCtx, cancel := context.WithCancel(ctx)
	go runRuntimeInteractiveEventPoller(pollCtx, controlPlaneURL, botID, bootstrapToken, c, handler)
	return cancel
}

func runRuntimeInteractiveEventPoller(ctx context.Context, controlPlaneURL, botID, bootstrapToken string, c *clients, handler *messaging.Handler) {
	slog.Info("runtime interactive event poller started", "component", "runtime_interactive")
	ticker := time.NewTicker(runtimeInteractivePollInterval)
	defer ticker.Stop()
	for {
		pollRuntimeInteractiveEvents(ctx, controlPlaneURL, botID, bootstrapToken, c, handler)
		select {
		case <-ctx.Done():
			slog.Info("runtime interactive event poller stopped", "component", "runtime_interactive")
			return
		case <-ticker.C:
		}
	}
}

func pollRuntimeInteractiveEvents(ctx context.Context, controlPlaneURL, botID, bootstrapToken string, c *clients, handler *messaging.Handler) {
	var result runtimeInteractiveEventsResult
	req := runtimeInteractiveEventsRequest{BotID: botID, BootstrapToken: bootstrapToken, Limit: 10}
	if err := postJSON(ctx, controlPlaneURL, "/runtime/v1/interactive-events", req, 200, &result); err != nil {
		slog.Warn("runtime interactive event poll failed", "component", "runtime_interactive", "error", err)
		return
	}
	for _, event := range result.Events {
		processRuntimeInteractiveEvent(ctx, controlPlaneURL, botID, bootstrapToken, c, handler, event)
	}
}

func processRuntimeInteractiveEvent(ctx context.Context, controlPlaneURL, botID, bootstrapToken string, c *clients, handler *messaging.Handler, event runtimeInteractiveEvent) {
	text := runtimeInteractiveEventText(event)
	if text == "" {
		ackRuntimeInteractiveEvent(ctx, controlPlaneURL, botID, bootstrapToken, event.ID, "failed", "interactive event did not contain an action or command")
		return
	}
	executorID := runtimeInteractiveExecutorID(c, event)
	post := ringcentral.Post{
		ID:           "interactive-" + event.ID,
		GroupID:      event.ConversationID,
		Type:         "TextMessage",
		Text:         text,
		CreatorID:    executorID,
		CreationTime: firstNonEmptyString(event.EventTimestamp, time.Now().UTC().Format(time.RFC3339Nano)),
		EventType:    "PostAdded",
		RuntimeMetadata: map[string]string{
			"origin_chat_id": interactiveString(event.Data, "origin_chat_id"),
		},
	}
	updateRuntimeInteractiveDecisionCard(ctx, c.bot, event)
	slog.Info("dispatching runtime interactive event", "component", "runtime_interactive", "eventID", event.ID, "creatorID", executorID, "submitterID", event.UserID, "submitter", runtimeInteractiveSubmitter(event), "chatID", event.ConversationID, "text", text)
	handler.HandleMessage(ctx, c.bot, c.lookupClient(), post)
	ackRuntimeInteractiveEvent(ctx, controlPlaneURL, botID, bootstrapToken, event.ID, "acknowledged", "")
}

func runtimeInteractiveExecutorID(c *clients, event runtimeInteractiveEvent) string {
	if c != nil {
		if lookup := c.lookupClient(); lookup != nil {
			if ownerID := strings.TrimSpace(lookup.OwnerID()); ownerID != "" {
				return ownerID
			}
		}
		if c.bot != nil {
			if ownerID := strings.TrimSpace(c.bot.OwnerID()); ownerID != "" {
				return ownerID
			}
		}
	}
	return strings.TrimSpace(event.UserID)
}

func updateRuntimeInteractiveDecisionCard(ctx context.Context, client *ringcentral.Client, event runtimeInteractiveEvent) {
	if client == nil || strings.TrimSpace(event.CardID) == "" {
		return
	}
	card := runtimeInteractiveDecisionCard(event)
	if len(card) == 0 {
		return
	}
	if _, err := client.UpdateAdaptiveCard(ctx, event.CardID, card); err != nil {
		slog.Warn("runtime interactive card update failed", "component", "runtime_interactive", "eventID", event.ID, "cardID", event.CardID, "error", err)
		return
	}
	slog.Info("runtime interactive card updated", "component", "runtime_interactive", "eventID", event.ID, "cardID", event.CardID, "action", runtimeInteractiveAction(event))
}

func ackRuntimeInteractiveEvent(ctx context.Context, controlPlaneURL, botID, bootstrapToken, eventID, status, reason string) {
	if strings.TrimSpace(eventID) == "" {
		return
	}
	req := runtimeInteractiveEventAckRequest{
		BotID:          botID,
		BootstrapToken: bootstrapToken,
		Status:         status,
		StatusReason:   reason,
	}
	if err := postJSON(ctx, controlPlaneURL, "/runtime/v1/interactive-events/"+eventID+"/ack", req, 200, nil); err != nil {
		slog.Warn("runtime interactive event ack failed", "component", "runtime_interactive", "eventID", eventID, "status", status, "error", err)
	}
}

func runtimeInteractiveEventText(event runtimeInteractiveEvent) string {
	if text := interactiveString(event.Data, "text"); text != "" {
		return text
	}
	if command := interactiveString(event.Data, "command"); command != "" {
		return command
	}
	action := strings.ToLower(strings.TrimSpace(interactiveString(event.Data, "action")))
	rxID := strings.TrimSpace(interactiveString(event.Data, "rx_id"))
	if rxID == "" {
		rxID = strings.TrimSpace(interactiveString(event.Data, "rxId"))
	}
	switch action {
	case "approve", "approved":
		return strings.TrimSpace("approve " + rxID)
	case "followup", "follow-up", "follow_up":
		return strings.TrimSpace("followup " + rxID)
	case "deny", "denied", "reject", "rejected":
		return strings.TrimSpace("deny " + rxID)
	case "":
		return ""
	default:
		if rxID != "" {
			return fmt.Sprintf("%s %s", action, rxID)
		}
		return action
	}
}

func runtimeInteractiveDecisionCard(event runtimeInteractiveEvent) json.RawMessage {
	action := runtimeInteractiveAction(event)
	if action == "" {
		return nil
	}
	switch action {
	case "coverage_confirm", "coverage_followup", "coverage_decline":
		return runtimeInteractiveCoverageDecisionCard(event, action)
	}
	return runtimeInteractiveRefillDecisionCard(event, action)
}

func runtimeInteractiveRefillDecisionCard(event runtimeInteractiveEvent, action string) json.RawMessage {
	rxID := firstNonEmptyString(interactiveString(event.Data, "rx_id"), interactiveString(event.Data, "rxId"), "Refill request")
	patientID := firstNonEmptyString(interactiveString(event.Data, "patient_id"), interactiveString(event.Data, "patientId"), "Unknown")
	medication := firstNonEmptyString(interactiveString(event.Data, "medication"), "See request")
	provider := firstNonEmptyString(interactiveString(event.Data, "provider_name"), interactiveString(event.Data, "providerName"), "provider")
	submitter := runtimeInteractiveSubmitter(event)

	header := "Refill decision recorded"
	status := "Submitted by " + submitter
	color := "Accent"
	message := "Decision recorded. Runtime will continue the refill workflow."
	switch action {
	case "approve", "approved":
		header = "Refill approved"
		status = "Approved by " + submitter
		color = "Good"
		message = "Provider approval has been recorded. Runtime will continue the refill workflow."
	case "followup", "follow-up", "follow_up":
		header = "Follow-up requested"
		status = "Follow-up requested by " + submitter
		color = "Warning"
		message = "Follow-up is required before this refill can continue."
	case "deny", "denied", "reject", "rejected":
		header = "Refill denied"
		status = "Denied by " + submitter
		color = "Attention"
		message = "Provider denial has been recorded. Runtime will stop the refill workflow."
	}

	facts := []map[string]string{
		{"title": "Provider", "value": provider},
		{"title": "Patient", "value": patientID},
		{"title": "Medication", "value": medication},
		{"title": "Status", "value": status},
	}
	if event.EventTimestamp != "" {
		facts = append(facts, map[string]string{"title": "Submitted", "value": event.EventTimestamp})
	}
	card := map[string]any{
		"$schema":      "http://adaptivecards.io/schemas/adaptive-card.json",
		"type":         "AdaptiveCard",
		"version":      "1.3",
		"fallbackText": fmt.Sprintf("%s: %s", header, rxID),
		"body": []map[string]any{
			{
				"type":   "TextBlock",
				"text":   header,
				"size":   "Small",
				"color":  color,
				"weight": "Bolder",
			},
			{
				"type":   "TextBlock",
				"text":   strings.TrimSpace(fmt.Sprintf("%s - %s %s", rxID, patientID, medication)),
				"size":   "Large",
				"color":  "Accent",
				"weight": "Bolder",
				"wrap":   true,
			},
			{
				"type":  "FactSet",
				"facts": facts,
			},
			{
				"type":      "TextBlock",
				"text":      message,
				"wrap":      true,
				"separator": true,
			},
		},
	}
	body, err := json.Marshal(card)
	if err != nil {
		return nil
	}
	return body
}

func runtimeInteractiveCoverageDecisionCard(event runtimeInteractiveEvent, action string) json.RawMessage {
	coverageID := firstNonEmptyString(interactiveString(event.Data, "coverage_id"), interactiveString(event.Data, "coverageId"), "Coverage request")
	candidate := firstNonEmptyString(interactiveString(event.Data, "candidate_name"), interactiveString(event.Data, "candidateName"), runtimeInteractiveSubmitter(event))
	date := firstNonEmptyString(interactiveString(event.Data, "date"), "See request")
	shift := firstNonEmptyString(interactiveString(event.Data, "shift"), "See request")
	workload := firstNonEmptyString(interactiveString(event.Data, "workload"), interactiveString(event.Data, "transferred_workload"), "See request")
	submitter := runtimeInteractiveSubmitter(event)

	header := "Coverage decision recorded"
	status := "Submitted by " + submitter
	color := "Accent"
	message := "Coverage decision recorded. Runtime will continue the coverage workflow."
	switch action {
	case "coverage_confirm":
		header = "Coverage confirmed"
		status = "Confirmed by " + submitter
		color = "Good"
		message = "Coverage confirmation has been recorded. Runtime will continue the coverage workflow."
	case "coverage_followup":
		header = "Coverage follow-up requested"
		status = "Follow-up requested by " + submitter
		color = "Warning"
		message = "Follow-up is required before this coverage handoff can continue."
	case "coverage_decline":
		header = "Coverage declined"
		status = "Declined by " + submitter
		color = "Attention"
		message = "Coverage decline has been recorded. Runtime will continue the fallback coverage workflow."
	}

	facts := []map[string]string{
		{"title": "Coverage ID", "value": coverageID},
		{"title": "Candidate", "value": candidate},
		{"title": "Date", "value": date},
		{"title": "Shift", "value": shift},
		{"title": "Status", "value": status},
	}
	if action == "coverage_confirm" {
		facts = append(facts, map[string]string{"title": "Transferred workload", "value": workload})
	}
	if event.EventTimestamp != "" {
		facts = append(facts, map[string]string{"title": "Submitted", "value": event.EventTimestamp})
	}
	card := map[string]any{
		"$schema":      "http://adaptivecards.io/schemas/adaptive-card.json",
		"type":         "AdaptiveCard",
		"version":      "1.3",
		"fallbackText": fmt.Sprintf("%s: %s", header, coverageID),
		"body": []map[string]any{
			{
				"type":   "TextBlock",
				"text":   header,
				"size":   "Small",
				"color":  color,
				"weight": "Bolder",
			},
			{
				"type":   "TextBlock",
				"text":   strings.TrimSpace(fmt.Sprintf("%s - %s", coverageID, candidate)),
				"size":   "Large",
				"color":  "Accent",
				"weight": "Bolder",
				"wrap":   true,
			},
			{
				"type":  "FactSet",
				"facts": facts,
			},
			{
				"type":      "TextBlock",
				"text":      message,
				"wrap":      true,
				"separator": true,
			},
		},
	}
	body, err := json.Marshal(card)
	if err != nil {
		return nil
	}
	return body
}

func runtimeInteractiveAction(event runtimeInteractiveEvent) string {
	return strings.ToLower(strings.TrimSpace(interactiveString(event.Data, "action")))
}

func runtimeInteractiveSubmitter(event runtimeInteractiveEvent) string {
	name := strings.TrimSpace(strings.Join([]string{event.UserFirstName, event.UserLastName}, " "))
	if name != "" {
		return name
	}
	if event.UserID != "" {
		return event.UserID
	}
	return "submitter"
}

func interactiveString(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	switch value := data[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}
