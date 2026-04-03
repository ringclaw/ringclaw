package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

const defaultSummaryMessageLimit = 250

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
	Entries []chatCacheEntry        `json:"entries"`
	Persons map[string]cachedPerson `json:"persons"`
	SavedAt time.Time               `json:"saved_at"`
}

// chatCache caches Direct chat lookups and person info.
// Direct chats have stable IDs and are cached permanently.
// Team/Group chats use mentions (have explicit IDs), no cache needed.
type chatCache struct {
	mu      sync.RWMutex
	entries []chatCacheEntry
	persons map[string]*ringcentral.PersonInfo
	loaded  bool
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
		slog.Warn("failed to parse cache file", "component", "summarize", "error", err)
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

	slog.Info("loaded cache from disk", "component", "summarize", "chats", len(pd.Entries), "persons", len(pd.Persons))
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
		slog.Error("failed to marshal cache", "component", "summarize", "error", err)
		return
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		slog.Error("failed to write cache file", "component", "summarize", "error", err)
		return
	}
	slog.Info("saved cache to disk", "component", "summarize", "chats", len(pd.Entries), "persons", len(pd.Persons))
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
	slog.Info("searching company directory", "component", "summarize", "name", name)
	result, err := client.SearchDirectory(ctx, name)
	if err != nil {
		slog.Warn("directory search failed", "component", "summarize", "error", err)
		return nil
	}
	if len(result.Records) == 0 {
		slog.Warn("no directory entries found", "component", "summarize", "name", name)
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
		slog.Warn("directory returned entries but none matched", "component", "summarize", "count", len(result.Records), "name", name)
		return nil
	}

	fullName := strings.TrimSpace(best.FirstName + " " + best.LastName)
	slog.Info("directory matched", "component", "summarize", "fullName", fullName, "id", best.ID, "email", best.Email)

	// Find or create Direct chat via conversations API (idempotent)
	chat, err := client.CreateConversation(ctx, []string{best.ID})
	if err != nil {
		slog.Warn("create conversation failed", "component", "summarize", "error", err)
		return nil
	}

	slog.Info("resolved Direct chat", "component", "summarize", "fullName", fullName, "chatID", chat.ID)

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
// Used by isPrivilegedCommand for access control.
func IsSummarizeCommand(text string) bool {
	return isSummarizeKeyword(text)
}

// isSummarizeKeyword checks if the text starts with a summarize keyword (prefix match).
// Used as fallback when the AI intent classifier is unavailable.
func isSummarizeKeyword(text string) bool {
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
	ChatID       string
	ChatName     string
	TimeFrom     time.Time
	UserRequest  string // original user message
	MessageLimit int
}

// ResolveChatTarget finds the target chat ID from mentions or fuzzy name matching.
func ResolveChatTarget(ctx context.Context, client *ringcentral.Client, text string, mentions []ringcentral.Mention) (*SummarizeRequest, error) {
	req := &SummarizeRequest{
		TimeFrom:    todayStart(),
		UserRequest: text,
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

	slog.Info("looking up chat", "component", "summarize", "name", name)

	// Load cache from disk if not yet loaded
	globalChatCache.ensureLoaded()

	// Search local cache first
	if entry := globalChatCache.lookup(name); entry != nil {
		req.ChatID = entry.ChatID
		req.ChatName = entry.ChatName
		slog.Info("cache hit", "component", "summarize", "chatName", entry.ChatName, "chatID", entry.ChatID)
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
	limit := req.MessageLimit
	if limit <= 0 {
		limit = defaultSummaryMessageLimit
	}
	opts := ringcentral.ListPostsOpts{
		RecordCount: limit,
	}

	posts, err := client.ListPosts(ctx, req.ChatID, opts)
	if err != nil {
		return "", fmt.Errorf("fetch posts: %w", err)
	}

	if len(posts.Records) == 0 {
		return "", fmt.Errorf("no messages found in chat %s", req.ChatID)
	}

	// Log fetched range for debugging
	oldest := posts.Records[len(posts.Records)-1].CreationTime
	newest := posts.Records[0].CreationTime
	slog.Debug("fetched posts", "component", "summarize", "chatID", req.ChatID, "count", len(posts.Records), "oldest", oldest, "newest", newest, "timeFrom", req.TimeFrom.Format(time.RFC3339))

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
	lines := make([]string, 0, len(posts.Records))
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

	userReq := req.UserRequest
	if userReq == "" {
		userReq = "summarize the chat"
	}

	prompt := fmt.Sprintf(`User request: %s

Please summarize the following chat messages from "%s" (%s).
These are the most recent %d messages fetched from the chat before time filtering.
Provide a concise summary in the same language as the messages. 
Highlight key topics, decisions, and action items if any.
%s
--- Messages (%d total) ---
%s
--- End of Messages ---`,
		userReq, chatLabel, timeDesc, limit, ActionPrompt(), len(lines), strings.Join(lines, "\n"))

	slog.Info("built prompt", "component", "summarize", "chatLabel", chatLabel, "messages", len(lines), "chars", len(prompt))
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
	reDigits     = regexp.MustCompile(`\d+`)
	rePunctSpace = regexp.MustCompile(`[，。！？,\.!\?\s]+`)
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

// reInstructionSplit matches multilingual connectors that signal trailing instructions
// (e.g. "并用 note 发给他", "and then create a note", "そして送る").
// We split on these and keep only the first segment (the target name).
var reInstructionSplit = regexp.MustCompile(`(?i)(?:` +
	`并用|并且|并|然后|接着|之后|同时|通过|` + // Chinese (no standalone 用/再 — too common)
	`and then|then|and also|also|and send|and create|and post|` + // English
	`そして|それから|その後|` + // Japanese
	`그리고|그런\s*다음|` + // Korean
	`puis|ensuite|et\s+aussi|` + // French
	`luego|después|y\s+también|` + // Spanish
	`dann|und\s+auch|` + // German
	`потом|затем|и\s+также)`) // Russian

func extractNameFromText(text string) string {
	clean := text
	// Remove summarize keywords
	for _, kw := range summarizeKeywords {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	// Remove mentions
	clean = reMention.ReplaceAllString(clean, "")

	// Split on instruction connectors — keep only the first segment (the target name)
	if parts := reInstructionSplit.Split(clean, 2); len(parts) > 1 {
		clean = parts[0]
	}

	// Lowercase for filler removal
	clean = strings.ToLower(clean)
	// Remove time keywords
	for _, kw := range []string{"今天", "昨天", "本周", "最近", "过去", "today", "yesterday", "this week", "last"} {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	// Remove CJK filler words (safe for substring removal — no Latin overlap)
	for _, kw := range []string{
		"一下", "下", "的", "消息", "聊天", "对话", "群聊", "群",
		"跟", "和", "与", "我", "了",
		"发给", "发送", "发到", "给", "他", "她", "它", "他们",
		"笔记", "任务", "日程",
	} {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	// Remove English filler words (whole-word only to avoid stripping letters from names)
	clean = removeWholeWords(clean, []string{
		"messages", "chat", "conversation", "with", "my", "the", "of", "a",
		"send", "to", "him", "her", "them",
		"note", "task", "event",
	})
	// Remove digits
	clean = reDigits.ReplaceAllString(clean, "")
	// Remove punctuation and collapse whitespace
	clean = rePunctSpace.ReplaceAllString(clean, " ")
	// Remove remaining time units
	for _, kw := range []string{"天", "小时", "个", "hours", "days"} {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	clean = strings.TrimSpace(clean)
	return clean
}

// removeWholeWords removes English words by splitting on spaces (avoids stripping letters from names).
func removeWholeWords(text string, words []string) string {
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[strings.ToLower(w)] = true
	}
	parts := strings.Fields(text)
	var kept []string
	for _, p := range parts {
		if !set[strings.ToLower(p)] {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " ")
}

// exactMatch checks case-insensitive equality (ignoring extra whitespace).
func exactMatch(haystack, needle string) bool {
	h := strings.ToLower(strings.TrimSpace(haystack))
	n := strings.ToLower(strings.TrimSpace(needle))
	return h == n
}

// fuzzyMatch checks if haystack contains needle or vice versa (case-insensitive, spaces removed).
func fuzzyMatch(haystack, needle string) bool {
	if needle == "" || haystack == "" {
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
