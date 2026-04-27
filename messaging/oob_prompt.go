package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ringclaw/ringclaw/ringcentral"
)

// resolveRequesterLabel does a best-effort directory lookup and
// returns a human-readable identifier for the operator-DM prompts:
//
//	"Eve Doe <eve@example.com> (id=12345)"
//	"Eve Doe (id=12345)"            // no email
//	"eve@example.com (id=12345)"    // no display name
//	"12345"                         // lookup failed; bare ID
//
// Lookup failure is logged at debug level and never blocks the
// caller — the OOB prompt should still ship even when the directory
// API is flaky.
func resolveRequesterLabel(ctx context.Context, readClient *ringcentral.Client, userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	if readClient == nil {
		return userID
	}
	p, err := readClient.GetPersonInfo(ctx, userID)
	if err != nil || p == nil {
		if err != nil {
			slog.Debug("oob-prompt: person lookup failed",
				"component", "handler", "userID", userID, "error", err)
		}
		return userID
	}
	name := strings.TrimSpace(strings.TrimSpace(p.FirstName) + " " + strings.TrimSpace(p.LastName))
	email := strings.TrimSpace(p.Email)
	switch {
	case name != "" && email != "":
		return fmt.Sprintf("%s <%s> (id=%s)", name, email, userID)
	case name != "":
		return fmt.Sprintf("%s (id=%s)", name, userID)
	case email != "":
		return fmt.Sprintf("%s (id=%s)", email, userID)
	default:
		return userID
	}
}

// resolveChatLabel does a best-effort directory lookup and returns
// "<chat name> (id=<chatID>)" or just the bare ID when the lookup
// fails. Used by every operator-DM prompt that wants to surface the
// human-readable chat name alongside the ID.
func resolveChatLabel(ctx context.Context, readClient *ringcentral.Client, chatID string) string {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return ""
	}
	if readClient == nil {
		return chatID
	}
	chat, err := readClient.GetChat(ctx, chatID)
	if err != nil || chat == nil {
		if err != nil {
			slog.Debug("oob-prompt: chat lookup failed",
				"component", "handler", "chatID", chatID, "error", err)
		}
		return chatID
	}
	name := strings.TrimSpace(chat.Name)
	if name == "" {
		name = strings.TrimSpace(chat.Type)
	}
	if name == "" {
		return chatID
	}
	return fmt.Sprintf("%s (id=%s)", name, chatID)
}
