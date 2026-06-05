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

func resolveNameToPhoneNumber(ctx context.Context, client *ringcentral.Client, name string) (string, string, error) {
	result, err := client.SearchDirectory(ctx, name)
	if err != nil {
		return "", "", fmt.Errorf("directory search: %w", err)
	}
	if best := bestDirectoryMatch(result.Records, name); best != nil {
		if number := bestContactPhoneNumber(best.PhoneNumbers, best.ExtensionNumber); number != "" {
			fullName := strings.TrimSpace(best.FirstName + " " + best.LastName)
			slog.Info("action: resolved phone contact from directory", "name", name, "match", fullName, "id", best.ID)
			return number, fullName, nil
		}
	}

	contacts, err := client.SearchContacts(ctx, name)
	if err != nil {
		return "", "", fmt.Errorf("contact search: %w", err)
	}
	best := bestContactMatch(contacts.Records, name)
	if best == nil {
		return "", "", fmt.Errorf("no phone contact found matching '%s'", name)
	}
	if number := contactPhoneNumber(*best); number != "" {
		fullName := strings.TrimSpace(best.FirstName + " " + best.LastName)
		if fullName == "" {
			fullName = best.Company
		}
		slog.Info("action: resolved phone contact from address book", "name", name, "match", fullName, "id", best.ID)
		return number, fullName, nil
	}
	return "", "", fmt.Errorf("contact '%s' matched but has no callable phone number", name)
}

func bestContactMatch(records []ringcentral.Contact, name string) *ringcentral.Contact {
	for i := range records {
		contact := &records[i]
		fullName := strings.TrimSpace(contact.FirstName + " " + contact.LastName)
		if exactMatch(fullName, name) || exactMatch(contact.Email, name) || exactMatch(contact.Company, name) {
			return contact
		}
	}
	var best *ringcentral.Contact
	bestLen := int(^uint(0) >> 1)
	for i := range records {
		contact := &records[i]
		fullName := strings.TrimSpace(contact.FirstName + " " + contact.LastName)
		if fuzzyMatch(fullName, name) || fuzzyMatch(contact.Email, name) || fuzzyMatch(contact.Company, name) {
			label := fullName
			if label == "" {
				label = contact.Company
			}
			if len(label) < bestLen {
				best = contact
				bestLen = len(label)
			}
		}
	}
	return best
}

func contactPhoneNumber(contact ringcentral.Contact) string {
	return bestContactPhoneNumber(contact.PhoneNumbers,
		contact.MobilePhone,
		contact.BusinessPhone,
		contact.BusinessPhone2,
		contact.HomePhone,
		contact.HomePhone2,
		contact.OtherPhone,
		contact.AssistantPhone,
		contact.CallbackPhone,
		contact.CarPhone,
		contact.CompanyPhone,
	)
}

func bestContactPhoneNumber(phoneNumbers []ringcentral.ContactPhoneNumber, fallbacks ...string) string {
	preferred := []string{"direct", "mobile", "business", "work", "company", "phone"}
	for _, want := range preferred {
		for _, phone := range phoneNumbers {
			if phone.PhoneNumber == "" {
				continue
			}
			label := strings.ToLower(phone.Type + " " + phone.UsageType)
			if strings.Contains(label, want) {
				return strings.TrimSpace(phone.PhoneNumber)
			}
		}
	}
	for _, phone := range phoneNumbers {
		if strings.TrimSpace(phone.PhoneNumber) != "" {
			return strings.TrimSpace(phone.PhoneNumber)
		}
	}
	for _, fallback := range fallbacks {
		if strings.TrimSpace(fallback) != "" {
			return strings.TrimSpace(fallback)
		}
	}
	return ""
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
	"yo":  true, // Spanish
	"ich": true, // German
	"я":   true, // Russian
}

func isSelfPronoun(s string) bool {
	return selfPronouns[strings.ToLower(strings.TrimSpace(s))]
}

func resolveCurrentChatMention(raw string, mentions []ringcentral.Mention) *ringcentral.Mention {
	want := strings.TrimSpace(raw)
	if want == "" {
		return nil
	}
	wantID := extractChatID(want)
	wantNorm := normalizeMentionLabel(wantID)
	if wantNorm == "" {
		return nil
	}
	var matched *ringcentral.Mention
	for i := range mentions {
		m := &mentions[i]
		if !strings.EqualFold(strings.TrimSpace(m.Type), "Person") {
			continue
		}
		if strings.TrimSpace(m.ID) == wantID ||
			normalizeMentionLabel(m.Name) == wantNorm {
			if matched != nil {
				return nil
			}
			matched = m
		}
	}
	return matched
}

func normalizeMentionLabel(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

func ensurePersonMentionPrefix(body, personID string) string {
	body = strings.TrimSpace(body)
	personID = strings.TrimSpace(personID)
	if body == "" || personID == "" {
		return body
	}
	prefix := "![:Person](" + personID + ")"
	if strings.HasPrefix(body, prefix) {
		return body
	}
	return prefix + " " + body
}

func ensureRelayMentionPrefixes(body, targetPersonID, relayPersonID string) string {
	body = strings.TrimSpace(body)
	targetPersonID = strings.TrimSpace(targetPersonID)
	relayPersonID = strings.TrimSpace(relayPersonID)
	if body == "" || targetPersonID == "" {
		return body
	}
	if relayPersonID != "" && relayPersonID != targetPersonID {
		body = ensurePersonMentionPrefix(body, relayPersonID)
	}
	return ensurePersonMentionPrefix(body, targetPersonID)
}

func ensureCurrentChatRelay(body, collaboratorID, currentBotID string) string {
	body = strings.TrimSpace(body)
	collaboratorID = strings.TrimSpace(collaboratorID)
	currentBotID = strings.TrimSpace(currentBotID)
	if body == "" || collaboratorID == "" || currentBotID == "" {
		return body
	}

	selfPrefix := "![:Person](" + currentBotID + ")"
	if strings.HasPrefix(body, selfPrefix) {
		body = strings.TrimSpace(strings.TrimPrefix(body, selfPrefix))
	}

	body = ensurePersonMentionPrefix(body, currentBotID)
	return ensurePersonMentionPrefix(body, collaboratorID)
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

// selectCurrentChatPostClient picks the identity used for posts/cards that
// land back in the chat that triggered the bot. In that path we prefer the
// bot client so group replies are authored by the bot and do not require the
// Private App owner to be a member of the team.
func selectCurrentChatPostClient(replyClient, actionClient *ringcentral.Client, targetChat, originChat string) *ringcentral.Client {
	if targetChat == originChat && replyClient != nil {
		return replyClient
	}
	if actionClient != nil {
		return actionClient
	}
	return replyClient
}

// selectCardClient picks the right client for adaptive card creation.
func selectCardClient(replyClient, actionClient *ringcentral.Client, targetChat, originChat string) *ringcentral.Client {
	return selectCurrentChatPostClient(replyClient, actionClient, targetChat, originChat)
}
