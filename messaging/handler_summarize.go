package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
)

// classifyAndRoute uses AI to classify the user's intent and routes accordingly.
// Returns true if the message was handled, false to continue normal routing.
func (h *Handler) classifyAndRoute(ctx context.Context, client *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post, text string, isBotGroup bool) bool {
	// Fast-path: messages starting with summarize keywords skip AI classification.
	// The summarize flow already injects ActionPrompt, so the agent can still
	// produce ACTION blocks (note/task/message) for compound requests like
	// "总结 maxwell 并用 note 发给他".
	if isSummarizeKeyword(text) {
		return h.routeSummarize(ctx, client, readClient, post, isBotGroup)
	}

	ag := h.getDefaultAgent()
	if ag == nil {
		return false
	}

	intent := classifyIntent(ctx, ag, text)
	switch intent {
	case IntentSummarize:
		return h.routeSummarize(ctx, client, readClient, post, isBotGroup)
	case IntentTask, IntentNote, IntentEvent:
		h.sendToDefaultAgent(ctx, client, readClient, post, text)
		return true
	default:
		return false
	}
}

// routeSummarize handles the summarize flow with permission checks.
// Returns true (always handles the message).
func (h *Handler) routeSummarize(ctx context.Context, client *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post, isBotGroup bool) bool {
	chatID := post.GroupID
	if readClient == client {
		logSendError(SendTextReply(ctx, client, chatID, "Summarize requires a Private App to be configured. Run 'ringclaw setup' to add one."))
		return true
	}
	if isBotGroup {
		logSendError(SendTextReply(ctx, client, chatID, "This command can only be used in a direct message with the bot."))
		return true
	}
	h.handleSummarize(ctx, client, readClient, post)
	return true
}

func (h *Handler) handleSummarize(ctx context.Context, replyClient *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post) {
	chatID := post.GroupID
	text := strings.TrimSpace(post.Text)

	placeholderID, placeholderErr := SendTypingPlaceholder(ctx, replyClient, chatID)
	if placeholderErr != nil {
		slog.Error("failed to send typing placeholder", "component", "handler", "error", placeholderErr)
	}

	sendReply := func(reply string) {
		if placeholderID != "" {
			if err := UpdatePostText(ctx, replyClient, chatID, placeholderID, reply); err != nil {
				slog.Error("failed to update placeholder", "component", "handler", "error", err)
				logSendError(SendTextReply(ctx, replyClient, chatID, reply))
			}
		} else {
			logSendError(SendTextReply(ctx, replyClient, chatID, reply))
		}
	}

	// Resolve target chat using readClient (private app has access to all chats)
	req, err := ResolveChatTarget(ctx, readClient, text, post.Mentions)
	if err != nil {
		sendReply(fmt.Sprintf("Error: %v", err))
		return
	}

	slog.Info("summarize target chat", "component", "summarize", "chatName", req.ChatName, "chatID", req.ChatID, "from", req.TimeFrom.Format(time.RFC3339))

	// Build prompt using readClient (private app can read any chat's messages)
	prompt, err := BuildSummaryPrompt(ctx, readClient, req)
	if err != nil {
		sendReply(fmt.Sprintf("Error: %v", err))
		return
	}

	// Send to default agent
	ag := h.getDefaultAgent()
	if ag == nil {
		sendReply("Error: no agent available for summarization")
		return
	}

	reply, err := h.chatWithAgent(ctx, ag, post.CreatorID, prompt)
	if err != nil {
		sendReply(fmt.Sprintf("Error: %v", err))
		return
	}

	// Parse and execute any ACTION blocks from the agent's response
	cleanReply, actions := ParseAgentActions(reply)
	if replyClient.IsBot() {
		sendReply(cleanReply)
	} else {
		sendReply(wrapAnswer(cleanReply))
	}

	if len(actions) > 0 {
		// Execute actions in the current chat (not the summarized chat) using readClient
		results := ExecuteAgentActions(ctx, replyClient, readClient, chatID, actions)
		if len(results) > 0 {
			logSendError(SendTextReply(ctx, replyClient, chatID, strings.Join(results, "\n")))
		}
	}
}
