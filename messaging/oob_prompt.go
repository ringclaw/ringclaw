package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ringclaw/ringclaw/ringcentral"
)

// formatPersonLabel renders a directory person as a human-readable
// identifier suitable for owner-DM prompts. The returned string is
// one of:
//
//	"Eve Doe <eve@example.com> (id=12345)"
//	"Eve Doe (id=12345)"            // no email
//	"eve@example.com (id=12345)"    // no display name
//	"12345"                         // no name and no email; bare ID
//
// All inputs are independently trimmed; an empty userID returns "".
// Centralized so resolveRequesterLabel and the authorize-mention
// rich prompt produce identical text for the same record.
func formatPersonLabel(name, email, userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
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

// formatChatLabel renders a directory chat as a human-readable
// identifier suitable for owner-DM prompts: "<chat name> (id=<id>)"
// or just the bare chatID when name is empty. Empty chatID returns
// "". Centralized so every cross-chat / authorize prompt formats the
// chat field identically.
func formatChatLabel(name, chatID string) string {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return ""
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return chatID
	}
	return fmt.Sprintf("%s (id=%s)", name, chatID)
}

// lookupPerson is the best-effort directory wrapper used by the OOB
// prompts. Returns (display name, email) — both possibly empty. Any
// API failure is logged at debug and replaced with empty strings so
// the caller can fall back to the bare ID. A nil readClient yields
// the zero result without an extra log line.
func lookupPerson(ctx context.Context, readClient *ringcentral.Client, userID string) (name, email string) {
	if readClient == nil {
		return "", ""
	}
	p, err := readClient.GetPersonInfo(ctx, userID)
	if err != nil || p == nil {
		if err != nil {
			slog.Debug("oob-prompt: person lookup failed",
				"component", "handler", "userID", userID, "error", err)
		}
		return "", ""
	}
	name = strings.TrimSpace(strings.TrimSpace(p.FirstName) + " " + strings.TrimSpace(p.LastName))
	email = strings.TrimSpace(p.Email)
	return name, email
}

// lookupChat is the best-effort directory wrapper used by the OOB
// prompts. Returns the resolved chat name (Type as fallback) or ""
// when the lookup fails or the client is nil. Centralized so the
// authorize-mention meta collector and the cross-chat prompt
// builder make identical lookup decisions.
func lookupChat(ctx context.Context, readClient *ringcentral.Client, chatID string) string {
	if readClient == nil {
		return ""
	}
	chat, err := readClient.GetChat(ctx, chatID)
	if err != nil || chat == nil {
		if err != nil {
			slog.Debug("oob-prompt: chat lookup failed",
				"component", "handler", "chatID", chatID, "error", err)
		}
		return ""
	}
	name := strings.TrimSpace(chat.Name)
	if name == "" {
		name = strings.TrimSpace(chat.Type)
	}
	return name
}

// resolveRequesterLabel does a best-effort directory lookup and
// returns a human-readable identifier for the operator-DM prompts
// (see formatPersonLabel). Lookup failure is logged at debug level
// and never blocks the caller — the OOB prompt should still ship
// even when the directory API is flaky.
func resolveRequesterLabel(ctx context.Context, readClient *ringcentral.Client, userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	name, email := lookupPerson(ctx, readClient, userID)
	return formatPersonLabel(name, email, userID)
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
	name := lookupChat(ctx, readClient, chatID)
	return formatChatLabel(name, chatID)
}
