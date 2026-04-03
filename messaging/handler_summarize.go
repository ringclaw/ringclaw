package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
)

var groupCrossTargetDenyPhrases = []string{
	"其他群", "别的群", "另一个群", "其它群", "私聊", "别人", "其他人", "别人的",
	"other group", "another group", "other chat", "another chat", "private chat", "direct chat", "dm ",
	"chat with", "conversation with",
}

// genericCurrentGroupSummaryTokens are words that indicate the user is referring
// to the current group itself, not targeting a specific person or chat.
// Keep this list tight — only structural/deictic words, not content words like
// "讨论" or "discussion" which could appear inside a person-targeting phrase.
var genericCurrentGroupSummaryTokens = []string{
	"这个", "当前", "本", "这里", "这边", "this", "current", "here",
}

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
	if isBotGroup {
		allowedGroupID := h.configuredGroupSummaryGroupID()
		if allowedGroupID == "" {
			logSendError(SendTextReply(ctx, client, chatID, "Summarize in group chats requires ringcentral.group_summary_group_id to be configured."))
			return true
		}
		if chatID != allowedGroupID {
			logSendError(SendTextReply(ctx, client, chatID, fmt.Sprintf("Summarize is only allowed in the configured group (%s).", allowedGroupID)))
			return true
		}
		if denyReason := denyGroupCrossTargetSummary(post.Text, post.Mentions, client.OwnerID()); denyReason != "" {
			logSendError(SendTextReply(ctx, client, chatID, denyReason))
			return true
		}
		h.handleCurrentGroupSummarize(ctx, client, readClient, post)
		return true
	}
	if readClient == client {
		logSendError(SendTextReply(ctx, client, chatID, "Summarize requires a Private App to be configured. Run 'ringclaw setup' to add one."))
		return true
	}
	h.handleSummarize(ctx, client, readClient, post)
	return true
}

func denyGroupCrossTargetSummary(text string, mentions []ringcentral.Mention, botID string) string {
	trimmedBotID := strings.TrimSpace(botID)
	for _, m := range mentions {
		switch m.Type {
		case "Team":
			return "I don't have permission to summarize other groups from within this group."
		case "Person":
			if strings.TrimSpace(m.ID) != "" && strings.TrimSpace(m.ID) != trimmedBotID {
				return "I don't have permission to summarize another user's information or private conversations from within this group."
			}
		}
	}

	lower := strings.ToLower(strings.TrimSpace(text))
	for _, phrase := range groupCrossTargetDenyPhrases {
		if strings.Contains(lower, phrase) {
			if strings.Contains(phrase, "group") || strings.Contains(phrase, "群") || strings.Contains(phrase, "chat") || strings.Contains(phrase, "dm") {
				return "I don't have permission to summarize other groups or chats from within this group."
			}
			return "I don't have permission to summarize another user's information or private conversations from within this group."
		}
	}

	target := strings.TrimSpace(extractNameFromText(text))
	if target == "" {
		return ""
	}
	if isGenericCurrentGroupSummaryTarget(target) {
		return ""
	}
	return "I don't have permission to summarize another user's information or another chat from within this group."
}

func isGenericCurrentGroupSummaryTarget(target string) bool {
	lower := strings.ToLower(strings.TrimSpace(target))
	if lower == "" {
		return true
	}
	for _, token := range genericCurrentGroupSummaryTokens {
		if lower == token || strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func (h *Handler) handleSummarize(ctx context.Context, replyClient *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post) {
	text := strings.TrimSpace(post.Text)

	// Resolve target chat using readClient (private app has access to all chats)
	req, err := ResolveChatTarget(ctx, readClient, text, post.Mentions)
	if err != nil {
		logSendError(SendTextReply(ctx, replyClient, post.GroupID, fmt.Sprintf("Error: %v", err)))
		return
	}

	slog.Info("summarize target chat", "component", "summarize", "chatName", req.ChatName, "chatID", req.ChatID, "from", req.TimeFrom.Format(time.RFC3339))
	h.executeSummarize(ctx, replyClient, readClient, post, req)
}

func (h *Handler) handleCurrentGroupSummarize(ctx context.Context, replyClient *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post) {
	chatID := post.GroupID
	text := strings.TrimSpace(post.Text)

	req := &SummarizeRequest{
		ChatID:       chatID,
		ChatName:     chatID,
		TimeFrom:     parseTimeRange(text),
		UserRequest:  text,
		MessageLimit: h.groupSummaryLimit(),
	}

	slog.Info("summarize current group", "component", "summarize", "chatName", req.ChatName, "chatID", req.ChatID, "from", req.TimeFrom.Format(time.RFC3339), "limit", req.MessageLimit)
	h.executeSummarize(ctx, replyClient, readClient, post, req)
}

// executeSummarize is the shared summarize execution path for both DM and group summarize.
func (h *Handler) executeSummarize(ctx context.Context, replyClient *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post, req *SummarizeRequest) {
	chatID := post.GroupID

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

	prompt, err := BuildSummaryPrompt(ctx, readClient, req)
	if err != nil {
		sendReply(fmt.Sprintf("Error: %v", err))
		return
	}

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

	cleanReply, actions := ParseAgentActions(reply)
	if replyClient.IsBot() {
		sendReply(cleanReply)
	} else {
		sendReply(wrapAnswer(cleanReply))
	}

	if len(actions) > 0 {
		results := ExecuteAgentActions(ctx, replyClient, readClient, chatID, actions)
		if len(results) > 0 {
			logSendError(SendTextReply(ctx, replyClient, chatID, strings.Join(results, "\n")))
		}
	}
}
