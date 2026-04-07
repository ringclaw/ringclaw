package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ringclaw/ringclaw/internal/util"
	"github.com/ringclaw/ringclaw/ringcentral"
)

var thinkingEmojis = []string{"1F440", "1F914", "1F525", "26A1", "1F9E0"} // 👀🤔🔥⚡🧠
const doneEmoji = "2705"                                                   // ✅
const reactionInterval = 5 * time.Second

// SendTypingPlaceholder sends a "Thinking..." placeholder message and returns its post ID.
func SendTypingPlaceholder(ctx context.Context, client *ringcentral.Client, chatID string) (string, error) {
	post, err := client.SendPost(ctx, chatID, "Thinking...")
	if err != nil {
		return "", fmt.Errorf("send typing placeholder: %w", err)
	}
	slog.Info("sent typing placeholder", "component", "sender", "chatID", chatID, "postID", post.ID)
	return post.ID, nil
}

// StartThinkingReaction starts rolling emoji reactions on a message.
// Returns a stop function: call stop(true) on success to leave ✅, stop(false) to clean up silently.
func StartThinkingReaction(ctx context.Context, client *ringcentral.Client, chatID, postID string) func(success bool) {
	var (
		mu      sync.Mutex
		current string
		stopped bool
	)

	addReaction := func(code string) {
		if err := client.AddReaction(ctx, chatID, postID, code); err != nil {
			slog.Debug("failed to add reaction", "component", "sender", "emoji", code, "error", err)
		}
	}
	removeReaction := func(code string) {
		if err := client.RemoveReaction(ctx, chatID, postID, code); err != nil {
			slog.Debug("failed to remove reaction", "component", "sender", "emoji", code, "error", err)
		}
	}

	// Add the first emoji immediately
	current = thinkingEmojis[0]
	addReaction(current)

	ticker := time.NewTicker(reactionInterval)
	idx := 0

	go func() {
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			if stopped {
				mu.Unlock()
				return
			}
			prev := current
			idx = (idx + 1) % len(thinkingEmojis)
			current = thinkingEmojis[idx]
			mu.Unlock()

			removeReaction(prev)
			addReaction(current)
		}
	}()

	return func(success bool) {
		mu.Lock()
		if stopped {
			mu.Unlock()
			return
		}
		stopped = true
		prev := current
		mu.Unlock()

		removeReaction(prev)
		if success {
			addReaction(doneEmoji)
		}
	}
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


