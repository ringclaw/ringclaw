package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ringclaw/ringclaw/ringcentral"
)

// bestDirectoryMatch finds the best matching directory entry:
// exact match first, then shortest fuzzy match.
func bestDirectoryMatch(records []ringcentral.DirectoryEntry, name string) *ringcentral.DirectoryEntry {
	// Pass 1: exact match (case-insensitive)
	for i := range records {
		e := &records[i]
		fullName := strings.TrimSpace(e.FirstName + " " + e.LastName)
		if exactMatch(fullName, name) || exactMatch(e.Email, name) {
			return e
		}
	}
	// Pass 2: fuzzy match — prefer the shortest full name (closest to input)
	var best *ringcentral.DirectoryEntry
	bestLen := int(^uint(0) >> 1) // max int
	for i := range records {
		e := &records[i]
		fullName := strings.TrimSpace(e.FirstName + " " + e.LastName)
		if fuzzyMatch(fullName, name) || fuzzyMatch(e.Email, name) {
			if len(fullName) < bestLen {
				best = e
				bestLen = len(fullName)
			}
		}
	}
	return best
}

// resolveNameToChatID resolves a person name to a Direct chat ID via directory search.
func resolveNameToChatID(ctx context.Context, client *ringcentral.Client, name string) (string, error) {
	result, err := client.SearchDirectory(ctx, name)
	if err != nil {
		return "", fmt.Errorf("directory search: %w", err)
	}
	if len(result.Records) == 0 {
		return "", fmt.Errorf("no person found matching '%s'", name)
	}

	best := bestDirectoryMatch(result.Records, name)
	if best == nil {
		return "", fmt.Errorf("no person matched '%s' (got %d results)", name, len(result.Records))
	}

	fullName := strings.TrimSpace(best.FirstName + " " + best.LastName)
	slog.Info("action: resolved person", "name", name, "match", fullName, "id", best.ID)

	chat, err := client.CreateConversation(ctx, []string{best.ID})
	if err != nil {
		return "", fmt.Errorf("create conversation with %s: %w", fullName, err)
	}
	return chat.ID, nil
}

// resolveNameToPersonID resolves a person name to a person ID via directory search.
func resolveNameToPersonID(ctx context.Context, client *ringcentral.Client, name string) (string, error) {
	result, err := client.SearchDirectory(ctx, name)
	if err != nil {
		return "", fmt.Errorf("directory search: %w", err)
	}
	if len(result.Records) == 0 {
		return "", fmt.Errorf("no person found matching '%s'", name)
	}

	best := bestDirectoryMatch(result.Records, name)
	if best == nil {
		return "", fmt.Errorf("no person matched '%s'", name)
	}

	fullName := strings.TrimSpace(best.FirstName + " " + best.LastName)
	slog.Info("action: resolved assignee", "name", name, "match", fullName, "id", best.ID)
	return best.ID, nil
}

func isNumericID(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// selfPronouns maps first-person pronouns (multilingual) that should resolve
// to the current chat instead of triggering a directory search.
var selfPronouns = map[string]bool{
	"我": true, "me": true, "myself": true,
	"私": true, "自分": true, // Japanese
	"나": true, "저": true, // Korean
	"moi": true, // French
	"yo": true,  // Spanish
	"ich": true, // German
	"я":  true,  // Russian
}

func isSelfPronoun(s string) bool {
	return selfPronouns[strings.ToLower(strings.TrimSpace(s))]
}

// resolveChatParam resolves a chatid param: numeric IDs pass through,
// self-pronouns resolve to currentChatID, names are resolved via directory search.
func resolveChatParam(ctx context.Context, client *ringcentral.Client, raw string, currentChatID string) (string, error) {
	id := extractChatID(raw)
	if isNumericID(id) {
		return id, nil
	}
	if isSelfPronoun(id) {
		slog.Info("action: resolved self-pronoun to current chat", "pronoun", id, "chatID", currentChatID)
		return currentChatID, nil
	}
	return resolveNameToChatID(ctx, client, id)
}

// resolveAssigneeParam resolves an assignee param: numeric IDs pass through,
// names are resolved via directory search.
func resolveAssigneeParam(ctx context.Context, client *ringcentral.Client, raw string) (string, error) {
	id := extractChatID(raw)
	if isNumericID(id) {
		return id, nil
	}
	return resolveNameToPersonID(ctx, client, id)
}

// selectCardClient picks the right client for adaptive card creation.
//
// Always prefer the Private App (actionClient) when one is configured —
// this includes the bot's own DM. The Private App is empirically allowed
// to POST /chats/{botDM}/adaptive-cards (verified by spike) and posting
// it under the user's identity is consistent with how NOTE / TASK /
// EVENT actions already behave: the user owns the artefact.
//
// We only fall back to replyClient (the Bot) when no Private App is
// configured at all.
func selectCardClient(replyClient, actionClient *ringcentral.Client, _ string) *ringcentral.Client {
	if actionClient != nil {
		return actionClient
	}
	return replyClient
}
