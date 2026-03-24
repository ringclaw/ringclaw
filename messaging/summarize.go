package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
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
	ChatID   string `json:"chat_id"`
	ChatName string `json:"chat_name"`
	ChatType string `json:"chat_type"` // "Direct", "Team", "Group"
}

// cachedPerson is the JSON-serializable subset of PersonInfo.
type cachedPerson struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}

// persistentCacheData is the on-disk format for ~/.ringclaw/chat_cache.json.
type persistentCacheData struct {
	Entries []chatCacheEntry           `json:"entries"`
	Persons map[string]cachedPerson    `json:"persons"`
	SavedAt time.Time                  `json:"saved_at"`
}

// chatCache caches Direct chat lookups and person info.
// Direct chats have stable IDs and are cached permanently.
// Team/Group chats use mentions (have explicit IDs), no cache needed.
type chatCache struct {
	mu       sync.RWMutex
	entries  []chatCacheEntry
	persons  map[string]*ringcentral.PersonInfo
	loaded   bool
}

var globalChatCache = &chatCache{
	persons: make(map[string]*ringcentral.PersonInfo),
}

func cacheFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ringclaw", "chat_cache.json")
}

func (c *chatCache) ensureLoaded() {
	c.mu.RLock()
	loaded := c.loaded
	c.mu.RUnlock()
	if loaded {
		return
	}
	c.loadFromDisk()
}

// addEntry adds a new cache entry and persists to disk.
func (c *chatCache) addEntry(entry chatCacheEntry) {
	c.mu.Lock()
	c.entries = append(c.entries, entry)
	c.loaded = true
	c.mu.Unlock()
	c.saveToDisk()
}

// loadFromDisk reads the Direct chat cache from disk.
func (c *chatCache) loadFromDisk() {
	path := cacheFilePath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var pd persistentCacheData
	if err := json.Unmarshal(data, &pd); err != nil {
		log.Printf("[summarize] failed to parse cache file: %v", err)
		return
	}

	c.mu.Lock()
	c.entries = pd.Entries
	for id, cp := range pd.Persons {
		c.persons[id] = &ringcentral.PersonInfo{
			ID:        cp.ID,
			FirstName: cp.FirstName,
			LastName:  cp.LastName,
			Email:     cp.Email,
		}
	}
	c.loaded = len(c.entries) > 0
	c.mu.Unlock()

	log.Printf("[summarize] loaded cache from disk: %d chats, %d persons", len(pd.Entries), len(pd.Persons))
}

// saveToDisk writes the cache to disk.
func (c *chatCache) saveToDisk() {
	path := cacheFilePath()
	if path == "" {
		return
	}
	c.mu.RLock()
	pd := persistentCacheData{
		Entries: c.entries,
		Persons: make(map[string]cachedPerson, len(c.persons)),
		SavedAt: time.Now(),
	}
	for id, p := range c.persons {
		pd.Persons[id] = cachedPerson{
			ID:        p.ID,
			FirstName: p.FirstName,
			LastName:  p.LastName,
			Email:     p.Email,
		}
	}
	c.mu.RUnlock()

	data, err := json.Marshal(pd)
	if err != nil {
		log.Printf("[summarize] failed to marshal cache: %v", err)
		return
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("[summarize] failed to write cache file: %v", err)
		return
	}
	log.Printf("[summarize] saved cache to disk: %d chats, %d persons", len(pd.Entries), len(pd.Persons))
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

// lookupViaDirectory searches the company directory and creates/finds the Direct chat.
func (c *chatCache) lookupViaDirectory(ctx context.Context, client *ringcentral.Client, name string) *chatCacheEntry {
	log.Printf("[summarize] searching company directory for %q", name)
	result, err := client.SearchDirectory(ctx, name)
	if err != nil {
		log.Printf("[summarize] directory search failed: %v", err)
		return nil
	}
	if len(result.Records) == 0 {
		log.Printf("[summarize] no directory entries found for %q", name)
		return nil
	}

	// Pick best match
	var best *ringcentral.DirectoryEntry
	for i := range result.Records {
		e := &result.Records[i]
		fullName := strings.TrimSpace(e.FirstName + " " + e.LastName)
		if fuzzyMatch(fullName, name) || fuzzyMatch(e.Email, name) {
			best = e
			break
		}
	}
	if best == nil {
		log.Printf("[summarize] directory returned %d entries but none matched %q", len(result.Records), name)
		return nil
	}

	fullName := strings.TrimSpace(best.FirstName + " " + best.LastName)
	log.Printf("[summarize] directory matched: %q (id=%s, email=%s)", fullName, best.ID, best.Email)

	// Find Direct chat: try conversations API first, fall back to member search
	chat, err := client.CreateConversation(ctx, []string{best.ID})
	if err != nil {
		log.Printf("[summarize] conversations API failed: %v, trying member search...", err)
		chat, err = client.FindDirectChatByMember(ctx, best.ID)
		if err != nil {
			log.Printf("[summarize] member search also failed: %v", err)
			return nil
		}
	}

	log.Printf("[summarize] resolved Direct chat: %q -> %s", fullName, chat.ID)

	// Cache person info
	c.mu.Lock()
	c.persons[best.ID] = &ringcentral.PersonInfo{
		ID:        best.ID,
		FirstName: best.FirstName,
		LastName:  best.LastName,
		Email:     best.Email,
	}
	c.mu.Unlock()

	entry := chatCacheEntry{ChatID: chat.ID, ChatName: fullName, ChatType: "Direct"}
	c.addEntry(entry)
	return &entry
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

	log.Printf("[summarize] looking up %q", name)

	// Load cache from disk if not yet loaded
	globalChatCache.ensureLoaded()

	// Search local cache first
	if entry := globalChatCache.lookup(name); entry != nil {
		req.ChatID = entry.ChatID
		req.ChatName = entry.ChatName
		log.Printf("[summarize] cache hit: %q (id=%s)", entry.ChatName, entry.ChatID)
		return req, nil
	}

	// Cache miss: search company directory -> create/find conversation -> cache result
	if entry := globalChatCache.lookupViaDirectory(ctx, client, name); entry != nil {
		req.ChatID = entry.ChatID
		req.ChatName = entry.ChatName
		return req, nil
	}

	return nil, fmt.Errorf("could not find a chat matching %q. For group chats, use mention format: ![:Team](id)", name)
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
