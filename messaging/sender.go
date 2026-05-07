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

// threadSend posts text into a thread (existing or new). When threadID
// is non-empty it replies inside that thread; otherwise it creates a
// new thread off parentPostID.
func threadSend(ctx context.Context, client *ringcentral.Client, chatID string, ti ThreadInfo, text string) (*ringcentral.Post, error) {
	if ti.ThreadID != "" {
		return client.SendThreadReply(ctx, chatID, ti.ThreadID, text)
	}
	if ti.ParentPostID != "" {
		return client.SendPostAsThread(ctx, chatID, ti.ParentPostID, text)
	}
	return client.SendPost(ctx, chatID, text)
}

// ThreadInfo carries thread targeting information for replies.
type ThreadInfo struct {
	ThreadID     string // reply inside this existing thread
	ParentPostID string // create a new thread off this post
}

// ThreadInfoFromPost derives the ThreadInfo for replying to a post.
// When the post already belongs to a thread, replies go to that
// thread; otherwise a new thread is created off the post.
func ThreadInfoFromPost(post ringcentral.Post) ThreadInfo {
	if post.ThreadID != "" {
		return ThreadInfo{ThreadID: post.ThreadID}
	}
	return ThreadInfo{ParentPostID: post.ID}
}

// SendTypingPlaceholderInThread sends a "Thinking..." placeholder inside a thread.
func SendTypingPlaceholderInThread(ctx context.Context, client *ringcentral.Client, chatID string, ti ThreadInfo) (string, error) {
	post, err := threadSend(ctx, client, chatID, ti, "Thinking...")
	if err != nil {
		return "", fmt.Errorf("send typing placeholder in thread: %w", err)
	}
	slog.Info("sent typing placeholder in thread", "component", "sender", "chatID", chatID,
		"postID", post.ID, "threadID", ti.ThreadID, "parentPostID", ti.ParentPostID)
	return post.ID, nil
}

// FinalizeThreadReply finalizes a reply inside a thread, mirroring FinalizeReply.
func FinalizeThreadReply(ctx context.Context, client *ringcentral.Client, chatID string, ti ThreadInfo, placeholderID, reply, component string) {
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
			slog.Error("failed to update placeholder, sending new thread post", "component", component, "error", err)
			if _, sendErr := threadSend(ctx, client, chatID, ti, reply); sendErr != nil {
				slog.Error("failed to send thread reply", "component", component, "error", sendErr)
			}
		}
		return
	}
	if _, err := threadSend(ctx, client, chatID, ti, reply); err != nil {
		slog.Error("failed to send thread reply", "component", component, "error", err)
	}
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


