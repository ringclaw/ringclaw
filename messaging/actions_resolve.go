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
		if exactMatch(fullName, name) || exactMatch(e.Email, name) || exactMatch(e.ID, name) || exactMatch(e.ExtensionNumber, name) {
			return e
		}
	}
	// Pass 2: fuzzy match — prefer the shortest full name (closest to input)
	var best *ringcentral.DirectoryEntry
	bestLen := int(^uint(0) >> 1) // max int
	for i := range records {
		e := &records[i]
		fullName := strings.TrimSpace(e.FirstName + " " + e.LastName)
		if fuzzyMatch(fullName, name) || fuzzyMatch(e.Email, name) || fuzzyMatch(e.ID, name) || fuzzyMatch(e.ExtensionNumber, name) {
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

	if chatID, err := findExistingDirectChat(ctx, client, best.ID); err != nil {
		slog.Warn("action: failed to inspect existing direct chats", "name", name, "personID", best.ID, "error", err)
	} else if chatID != "" {
		slog.Info("action: reused existing direct chat", "name", name, "match", fullName, "chatID", chatID)
		return chatID, nil
	}

	chat, err := client.CreateConversation(ctx, []string{best.ID})
	if err != nil {
		return "", fmt.Errorf("create conversation with %s: %w", fullName, err)
	}
	return chat.ID, nil
}

func findExistingDirectChat(ctx context.Context, client *ringcentral.Client, personID string) (string, error) {
	if client == nil {
		return "", nil
	}
	ownerID := strings.TrimSpace(client.OwnerID())
	if ownerID == "" || strings.TrimSpace(personID) == "" {
		return "", nil
	}
	chats, err := client.ListChats(ctx, "Direct")
	if err != nil {
		return "", err
	}
	for _, chat := range chats.Records {
		if directChatMatches(chat, ownerID, personID) {
			return strings.TrimSpace(chat.ID), nil
		}
	}
	return "", nil
}

func directChatMatches(chat ringcentral.Chat, ownerID, personID string) bool {
	if !strings.EqualFold(strings.TrimSpace(chat.Type), "Direct") {
		return false
	}
	ownerID = strings.TrimSpace(ownerID)
	personID = strings.TrimSpace(personID)
	if ownerID == "" || personID == "" {
		return false
	}
	matchedOwner := false
	matchedPerson := false
	for _, member := range chat.Members {
		if directChatMemberMatches(member, ownerID) {
			matchedOwner = true
		}
		if directChatMemberMatches(member, personID) {
			matchedPerson = true
		}
	}
	return matchedOwner && matchedPerson
}

func directChatMemberMatches(member ringcentral.ChatMember, targetID string) bool {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return false
	}
	for _, candidate := range []string{member.ID, member.ExtensionID} {
		if strings.TrimSpace(candidate) == targetID {
			return true
		}
	}
	return false
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

type rolePeerDelivery struct {
	TargetChat       string
	TargetChatSource string
	MentionID        string
	MentionSource    string
}

func resolveRolePeerDelivery(ctx context.Context, client *ringcentral.Client, peer RolePeer) (rolePeerDelivery, error) {
	if client == nil {
		return rolePeerDelivery{}, fmt.Errorf("reply client is not configured")
	}
	if chatID := firstNonEmptyString(peer.SharedChatIDs...); chatID != "" {
		mentionID, mentionSource := resolveRolePeerConfiguredPersonID(peer)
		if mentionID == "" {
			mentionID, mentionSource = resolveRolePeerMentionIDFromChat(ctx, client, chatID, peer)
		}
		if mentionID == "" {
			mentionID, mentionSource = resolveRolePeerMentionID(ctx, client, peer)
		}
		if mentionID == "" {
			return rolePeerDelivery{}, fmt.Errorf("target bot mention ID could not be resolved for shared chat %s", chatID)
		}
		return rolePeerDelivery{
			TargetChat:       chatID,
			TargetChatSource: "shared_chat",
			MentionID:        mentionID,
			MentionSource:    mentionSource,
		}, nil
	}

	mentionID, mentionSource := resolveRolePeerConfiguredPersonID(peer)
	if mentionID == "" {
		mentionID, mentionSource = resolveRolePeerMentionID(ctx, client, peer)
	}
	if mentionID == "" {
		return rolePeerDelivery{}, fmt.Errorf("target bot mention ID could not be resolved")
	}
	targetChat, targetChatSource, err := resolveRolePeerTargetChat(ctx, client, mentionID)
	if err != nil {
		return rolePeerDelivery{}, err
	}
	return rolePeerDelivery{
		TargetChat:       targetChat,
		TargetChatSource: targetChatSource,
		MentionID:        mentionID,
		MentionSource:    mentionSource,
	}, nil
}

func resolveRolePeerConfiguredPersonID(peer RolePeer) (string, string) {
	if personID := strings.TrimSpace(peer.PersonID); personID != "" {
		return personID, "person_id"
	}
	return "", ""
}

func resolveRolePeerMentionIDFromChat(ctx context.Context, client *ringcentral.Client, chatID string, peer RolePeer) (string, string) {
	if client == nil || strings.TrimSpace(chatID) == "" {
		return "", ""
	}
	chat, err := client.GetChat(ctx, chatID)
	if err != nil {
		slog.Warn("action: failed to inspect shared chat members for role peer mention",
			"roleID", peer.RoleID, "chatID", chatID, "error", err)
		return "", ""
	}
	if best := bestChatMemberMatch(chat.Members, peer); best != nil && strings.TrimSpace(best.ID) != "" {
		slog.Info("action: resolved role peer mention ID from shared chat",
			"roleID", peer.RoleID, "chatID", chatID, "memberID", best.ID, "memberName", chatMemberDisplayName(*best))
		return strings.TrimSpace(best.ID), "chat_member:" + chatID
	}
	return "", ""
}

func bestChatMemberMatch(members []ringcentral.ChatMember, peer RolePeer) *ringcentral.ChatMember {
	queries := rolePeerMatchQueries(peer)
	for i := range members {
		member := &members[i]
		for _, query := range queries {
			if chatMemberExactMatch(*member, query) {
				return member
			}
		}
	}
	var best *ringcentral.ChatMember
	bestLen := int(^uint(0) >> 1)
	for i := range members {
		member := &members[i]
		for _, query := range queries {
			if chatMemberFuzzyMatch(*member, query) {
				display := chatMemberDisplayName(*member)
				if display == "" {
					display = member.ID
				}
				if len(display) < bestLen {
					best = member
					bestLen = len(display)
				}
			}
		}
	}
	return best
}

func rolePeerMatchQueries(peer RolePeer) []string {
	values := []string{peer.PersonID, peer.DisplayName, peer.RoleName, peer.BotID, peer.ExtensionID}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		out = append(out, value)
		seen[key] = true
	}
	return out
}

func chatMemberExactMatch(member ringcentral.ChatMember, query string) bool {
	for _, value := range chatMemberMatchValues(member) {
		if exactMatch(value, query) {
			return true
		}
	}
	return false
}

func chatMemberFuzzyMatch(member ringcentral.ChatMember, query string) bool {
	for _, value := range chatMemberMatchValues(member) {
		if fuzzyMatch(value, query) {
			return true
		}
	}
	return false
}

func chatMemberMatchValues(member ringcentral.ChatMember) []string {
	fullName := strings.TrimSpace(member.FirstName + " " + member.LastName)
	return []string{
		member.ID,
		member.Name,
		fullName,
		member.Email,
		member.ExtensionID,
		member.ExtensionNumber,
	}
}

func chatMemberDisplayName(member ringcentral.ChatMember) string {
	if name := strings.TrimSpace(member.Name); name != "" {
		return name
	}
	return strings.TrimSpace(member.FirstName + " " + member.LastName)
}

func resolveRolePeerMentionID(ctx context.Context, client *ringcentral.Client, peer RolePeer) (string, string) {
	for _, query := range []string{peer.DisplayName, peer.RoleName, peer.BotID, peer.ExtensionID} {
		query = strings.TrimSpace(query)
		if query == "" || client == nil {
			continue
		}
		result, err := client.SearchDirectory(ctx, query)
		if err != nil {
			slog.Warn("action: failed to resolve role peer mention ID",
				"roleID", peer.RoleID, "query", query, "error", err)
			continue
		}
		if best := bestDirectoryMatch(result.Records, query); best != nil && strings.TrimSpace(best.ID) != "" {
			fullName := strings.TrimSpace(best.FirstName + " " + best.LastName)
			slog.Info("action: resolved role peer mention ID",
				"roleID", peer.RoleID, "query", query, "match", fullName, "mentionID", best.ID)
			return strings.TrimSpace(best.ID), "directory:" + query
		}
	}
	return "", ""
}

func resolveRolePeerTargetChat(ctx context.Context, client *ringcentral.Client, mentionID string) (string, string, error) {
	if client == nil {
		return "", "", fmt.Errorf("reply client is not configured")
	}
	mentionID = strings.TrimSpace(mentionID)
	if mentionID == "" {
		return "", "", fmt.Errorf("target bot mention ID could not be resolved")
	}
	chat, err := client.CreateConversation(ctx, []string{mentionID})
	if err != nil {
		return "", "", fmt.Errorf("create direct bot chat: %w", err)
	}
	if strings.TrimSpace(chat.ID) == "" {
		return "", "", fmt.Errorf("direct bot chat ID is empty")
	}
	return strings.TrimSpace(chat.ID), "direct_chat", nil
}

func resolveNameToPhoneNumber(ctx context.Context, client *ringcentral.Client, name string) (string, string, error) {
	result, err := client.SearchDirectory(ctx, name)
	if err != nil {
		return "", "", fmt.Errorf("directory search: %w", err)
	}
	if best := bestDirectoryMatch(result.Records, name); best != nil {
		if number := bestContactPhoneNumber(best.PhoneNumbers); number != "" {
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
			number := strings.TrimSpace(phone.PhoneNumber)
			if number == "" || !looksLikeReachablePhoneNumber(number) {
				continue
			}
			label := strings.ToLower(phone.Type + " " + phone.UsageType)
			if strings.Contains(label, want) {
				return number
			}
		}
	}
	for _, phone := range phoneNumbers {
		number := strings.TrimSpace(phone.PhoneNumber)
		if number != "" && looksLikeReachablePhoneNumber(number) {
			return number
		}
	}
	for _, fallback := range fallbacks {
		number := strings.TrimSpace(fallback)
		if number != "" && looksLikeReachablePhoneNumber(number) {
			return number
		}
	}
	return ""
}

func looksLikeReachablePhoneNumber(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || !looksLikePhoneNumber(value) {
		return false
	}
	digits := 0
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	// Directory extension numbers such as "704" are not valid SMS/call
	// destinations. Keep local-length and E.164-style numbers.
	return digits >= 7
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
