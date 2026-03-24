package messaging

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
)

var summarizeKeywords = []string{"总结", "summarize", "summary"}

// chatCacheEntry stores a resolved chat name -> ID mapping.
type chatCacheEntry struct {
	ChatID   string
	ChatName string
	ChatType string // "Direct", "Team", "Group"
}

// chatCache caches chat lookups to avoid repeated API calls.
type chatCache struct {
	mu        sync.RWMutex
	entries   []chatCacheEntry
	persons   map[string]*ringcentral.PersonInfo // personID -> info
	loadedAt  time.Time
	ttl       time.Duration
}

var globalChatCache = &chatCache{
	persons: make(map[string]*ringcentral.PersonInfo),
	ttl:     10 * time.Minute,
}

func (c *chatCache) isStale() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entries == nil || time.Since(c.loadedAt) > c.ttl
}

// lookup searches cached entries by name. Returns nil if not found.
func (c *chatCache) lookup(name string) *chatCacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.entries {
		if fuzzyMatch(c.entries[i].ChatName, name) {
			return &c.entries[i]
		}
	}
	return nil
}

// getPerson returns cached person info or fetches it.
func (c *chatCache) getPerson(ctx context.Context, client *ringcentral.Client, personID string) *ringcentral.PersonInfo {
	c.mu.RLock()
	if p, ok := c.persons[personID]; ok {
		c.mu.RUnlock()
		return p
	}
	c.mu.RUnlock()

	if strings.HasPrefix(personID, "glip-") {
		return nil
	}
	person, err := client.GetPersonInfo(ctx, personID)
	if err != nil {
		return nil
	}
	c.mu.Lock()
	c.persons[personID] = person
	c.mu.Unlock()
	return person
}

// load fetches all chats and builds the cache.
func (c *chatCache) load(ctx context.Context, client *ringcentral.Client) {
	log.Println("[summarize] loading chat cache...")
	var entries []chatCacheEntry
	ownerID := client.OwnerID()

	for _, chatType := range []string{"Direct", "Team", "Group"} {
		chats, err := client.ListChats(ctx, chatType)
		if err != nil {
			log.Printf("[summarize] failed to list %s chats: %v", chatType, err)
			continue
		}

		for _, chat := range chats.Records {
			name := chat.Name
			// Direct chats may have empty Name; resolve from member
			if name == "" && chatType == "Direct" {
				for _, m := range chat.Members {
					if m.ID == ownerID || strings.HasPrefix(m.ID, "glip-") {
						continue
					}
					person := c.getPerson(ctx, client, m.ID)
					if person != nil {
						name = strings.TrimSpace(person.FirstName + " " + person.LastName)
					}
					break
				}
			}
			if name == "" {
				continue
			}
			entries = append(entries, chatCacheEntry{
				ChatID:   chat.ID,
				ChatName: name,
				ChatType: chatType,
			})
		}
	}

	c.mu.Lock()
	c.entries = entries
	c.loadedAt = time.Now()
	c.mu.Unlock()

	log.Printf("[summarize] chat cache loaded: %d entries", len(entries))
}

// IsSummarizeCommand checks if the text starts with a summarize keyword.
func IsSummarizeCommand(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, kw := range summarizeKeywords {
		if strings.HasPrefix(lower, kw) {
			return true
		}
	}
	return false
}

// SummarizeRequest holds parsed summarize parameters.
type SummarizeRequest struct {
	ChatID   string
	ChatName string
	TimeFrom time.Time
}

// ResolveChatTarget finds the target chat ID from mentions or fuzzy name matching.
func ResolveChatTarget(ctx context.Context, client *ringcentral.Client, text string, mentions []ringcentral.Mention) (*SummarizeRequest, error) {
	req := &SummarizeRequest{
		TimeFrom: todayStart(),
	}

	// Parse time range from text
	req.TimeFrom = parseTimeRange(text)

	// 1. Try to extract from mentions
	for _, m := range mentions {
		switch m.Type {
		case "Team":
			req.ChatID = m.ID
			req.ChatName = m.Name
			return req, nil
		case "Person":
			// Find Direct chat with this person
			chatID, err := findDirectChat(ctx, client, m.ID)
			if err != nil {
				return nil, fmt.Errorf("find chat with %s: %w", m.Name, err)
			}
			req.ChatID = chatID
			req.ChatName = m.Name
			return req, nil
		}
	}

	// 2. Extract name from text and fuzzy match
	name := extractNameFromText(text)
	if name == "" {
		return nil, fmt.Errorf("cannot determine which chat to summarize. Use a mention or specify a name")
	}

	log.Printf("[summarize] fuzzy searching for %q", name)

	// Load cache if stale or empty
	if globalChatCache.isStale() {
		globalChatCache.load(ctx, client)
	}

	// Search cache
	if entry := globalChatCache.lookup(name); entry != nil {
		req.ChatID = entry.ChatID
		req.ChatName = entry.ChatName
		log.Printf("[summarize] cache hit: %s chat %q (id=%s)", entry.ChatType, entry.ChatName, entry.ChatID)
		return req, nil
	}

	// Cache miss: force refresh and retry once
	log.Printf("[summarize] cache miss for %q, refreshing...", name)
	globalChatCache.load(ctx, client)

	if entry := globalChatCache.lookup(name); entry != nil {
		req.ChatID = entry.ChatID
		req.ChatName = entry.ChatName
		log.Printf("[summarize] cache hit after refresh: %s chat %q (id=%s)", entry.ChatType, entry.ChatName, entry.ChatID)
		return req, nil
	}

	return nil, fmt.Errorf("could not find a chat matching %q", name)
}

// BuildSummaryPrompt fetches chat messages and builds a prompt for the agent.
func BuildSummaryPrompt(ctx context.Context, client *ringcentral.Client, req *SummarizeRequest) (string, error) {
	// RingCentral List Posts API does not support time filters,
	// so we fetch max records and filter by time client-side.
	opts := ringcentral.ListPostsOpts{
		RecordCount: 250,
	}

	posts, err := client.ListPosts(ctx, req.ChatID, opts)
	if err != nil {
		return "", fmt.Errorf("fetch posts: %w", err)
	}

	if len(posts.Records) == 0 {
		return "", fmt.Errorf("no messages found")
	}

	// Resolve person names using global cache
	resolveName := func(creatorID string) string {
		person := globalChatCache.getPerson(ctx, client, creatorID)
		if person == nil {
			return creatorID
		}
		name := strings.TrimSpace(person.FirstName + " " + person.LastName)
		if name == "" {
			return creatorID
		}
		return name
	}

	// Posts are returned newest-first, reverse for chronological order
	// Filter by time range client-side
	timeFrom := req.TimeFrom.UTC()
	var lines []string
	for i := len(posts.Records) - 1; i >= 0; i-- {
		p := posts.Records[i]
		if p.Text == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, p.CreationTime)
		if err != nil {
			continue
		}
		if t.Before(timeFrom) {
			continue
		}
		name := resolveName(p.CreatorID)
		lines = append(lines, fmt.Sprintf("[%s] %s: %s", t.Format("15:04"), name, p.Text))
	}

	if len(lines) == 0 {
		return "", fmt.Errorf("no messages found in the specified time range (since %s)", req.TimeFrom.Format("2006-01-02 15:04"))
	}

	chatLabel := req.ChatName
	if chatLabel == "" {
		chatLabel = req.ChatID
	}

	timeDesc := formatTimeDesc(req.TimeFrom)

	prompt := fmt.Sprintf(`Please summarize the following chat messages from "%s" (%s). 
Provide a concise summary in the same language as the messages. 
Highlight key topics, decisions, and action items if any.

--- Messages (%d total) ---
%s
--- End of Messages ---`,
		chatLabel, timeDesc, len(lines), strings.Join(lines, "\n"))

	log.Printf("[summarize] built prompt for %q: %d messages, %d chars", chatLabel, len(lines), len(prompt))
	return prompt, nil
}

// --- helpers ---

func todayStart() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

var (
	reLastNDays  = regexp.MustCompile(`(?:最近|过去|last)\s*(\d+)\s*(?:天|days?)`)
	reLastNHours = regexp.MustCompile(`(?:最近|过去|last)\s*(\d+)\s*(?:小时|个小时|hours?)`)
)

func parseTimeRange(text string) time.Time {
	lower := strings.ToLower(text)
	now := time.Now()

	if m := reLastNDays.FindStringSubmatch(lower); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		if n > 0 {
			return now.AddDate(0, 0, -n)
		}
	}
	if m := reLastNHours.FindStringSubmatch(lower); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		if n > 0 {
			return now.Add(-time.Duration(n) * time.Hour)
		}
	}

	if strings.Contains(lower, "本周") || strings.Contains(lower, "this week") {
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		monday := now.AddDate(0, 0, -(weekday - 1))
		return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, now.Location())
	}

	if strings.Contains(lower, "昨天") || strings.Contains(lower, "yesterday") {
		y := now.AddDate(0, 0, -1)
		return time.Date(y.Year(), y.Month(), y.Day(), 0, 0, 0, 0, now.Location())
	}

	// Default: today
	return todayStart()
}

func formatTimeDesc(from time.Time) string {
	now := time.Now()
	today := todayStart()
	if from.Equal(today) {
		return "today"
	}
	diff := now.Sub(from)
	if diff < 48*time.Hour {
		return "since yesterday"
	}
	days := int(diff.Hours() / 24)
	return fmt.Sprintf("last %d days", days)
}

var reMention = regexp.MustCompile(`!\[:\w+\]\(\d+\)`)

func extractNameFromText(text string) string {
	clean := text
	// Remove summarize keywords
	for _, kw := range summarizeKeywords {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	// Remove mentions
	clean = reMention.ReplaceAllString(clean, "")
	// Lowercase for filler removal
	clean = strings.ToLower(clean)
	// Remove time keywords
	for _, kw := range []string{"今天", "昨天", "本周", "最近", "过去", "today", "yesterday", "this week", "last"} {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	// Remove common filler words (Chinese single chars and phrases)
	for _, kw := range []string{
		"一下", "下", "的", "消息", "聊天", "对话", "群聊", "群",
		"跟", "和", "与", "我", "了",
		"messages", "chat", "conversation", "with", "my", "the",
	} {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	// Remove digits
	clean = regexp.MustCompile(`\d+`).ReplaceAllString(clean, "")
	// Remove punctuation and collapse whitespace
	clean = regexp.MustCompile(`[，。！？,\.!\?\s]+`).ReplaceAllString(clean, " ")
	// Remove remaining time units
	for _, kw := range []string{"天", "小时", "个", "hours", "days"} {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	clean = strings.TrimSpace(clean)
	return clean
}

func fuzzyMatch(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	h := strings.ToLower(strings.ReplaceAll(haystack, " ", ""))
	n := strings.ToLower(strings.ReplaceAll(needle, " ", ""))
	return strings.Contains(h, n) || strings.Contains(n, h)
}

func findDirectChat(ctx context.Context, client *ringcentral.Client, personID string) (string, error) {
	chats, err := client.ListChats(ctx, "Direct")
	if err != nil {
		return "", err
	}
	for _, chat := range chats.Records {
		for _, m := range chat.Members {
			if m.ID == personID {
				return chat.ID, nil
			}
		}
	}
	return "", fmt.Errorf("no direct chat found with person %s", personID)
}
