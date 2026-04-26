package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ringclaw/ringclaw/internal/util"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// SendTypingPlaceholder sends a "Thinking..." placeholder message and returns its post ID.
func SendTypingPlaceholder(ctx context.Context, client *ringcentral.Client, chatID string) (string, error) {
	post, err := client.SendPost(ctx, chatID, "Thinking...")
	if err != nil {
		return "", fmt.Errorf("send typing placeholder: %w", err)
	}
	slog.Info("sent typing placeholder", "component", "sender", "chatID", chatID, "postID", post.ID)
	return post.ID, nil
}

// UpdatePostText updates an existing post's text content.
func UpdatePostText(ctx context.Context, client *ringcentral.Client, chatID, postID, text string) error {
	_, err := client.UpdatePost(ctx, chatID, postID, text)
	if err != nil {
		return fmt.Errorf("update post: %w", err)
	}
	slog.Info("updated post", "component", "sender", "postID", postID, "chatID", chatID, "text", util.Truncate(text, 50))
	return nil
}

// SendTextReply sends a text reply to a chat.
func SendTextReply(ctx context.Context, client *ringcentral.Client, chatID, text string) error {
	_, err := client.SendPost(ctx, chatID, text)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	slog.Info("sent reply", "component", "sender", "chatID", chatID, "text", util.Truncate(text, 50))
	return nil
}

// logSendError logs a send error if non-nil. Use instead of _ = SendTextReply(...).
func logSendError(err error) {
	if err != nil {
		slog.Error("failed to send reply", "component", "sender", "error", err)
	}
}

// FinalizeReply finalizes a chat reply against an optional "Thinking..."
// placeholder. It centralizes three branches that used to be duplicated
// in handler.go and handler_summarize.go:
//
//  1. reply is blank (e.g. agent response was 100% ACTION blocks)
//     → DELETE the placeholder so no phantom empty post remains;
//  2. reply has content AND a placeholder exists
//     → PATCH the placeholder, falling back to a fresh post on PATCH error;
//  3. reply has content but no placeholder
//     → just send a fresh post.
//
// component is the slog "component" label (e.g. "handler", "summarize")
// so audit logs preserve the call site.
func FinalizeReply(ctx context.Context, client *ringcentral.Client, chatID, placeholderID, reply, component string) {
	if strings.TrimSpace(reply) == "" {
		if placeholderID == "" {
			return
		}
		if err := client.DeletePost(ctx, chatID, placeholderID); err != nil {
			slog.Error("failed to delete empty placeholder", "component", component, "error", err)
			return
		}
		slog.Info("deleted empty placeholder", "component", component, "postID", placeholderID)
		return
	}
	if placeholderID != "" {
		if err := UpdatePostText(ctx, client, chatID, placeholderID, reply); err != nil {
			slog.Error("failed to update placeholder, sending new post", "component", component, "error", err)
			if sendErr := SendTextReply(ctx, client, chatID, reply); sendErr != nil {
				slog.Error("failed to send reply", "component", component, "error", sendErr)
			}
		}
		return
	}
	if err := SendTextReply(ctx, client, chatID, reply); err != nil {
		slog.Error("failed to send reply", "component", component, "error", err)
	}
}


