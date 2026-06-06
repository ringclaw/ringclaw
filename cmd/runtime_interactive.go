package cmd

import (
	"context"
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
	post := ringcentral.Post{
		ID:           "interactive-" + event.ID,
		GroupID:      event.ConversationID,
		Type:         "TextMessage",
		Text:         text,
		CreatorID:    event.UserID,
		CreationTime: firstNonEmptyString(event.EventTimestamp, time.Now().UTC().Format(time.RFC3339Nano)),
		EventType:    "PostAdded",
	}
	slog.Info("dispatching runtime interactive event", "component", "runtime_interactive", "eventID", event.ID, "creatorID", event.UserID, "chatID", event.ConversationID, "text", text)
	handler.HandleMessage(ctx, c.bot, c.lookupClient(), post)
	ackRuntimeInteractiveEvent(ctx, controlPlaneURL, botID, bootstrapToken, event.ID, "acknowledged", "")
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
