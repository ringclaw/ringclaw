package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
)

var summarizeKeywords = []string{"总结", "summarize", "summary"}

// ErrSummarizeNoChatMatch is returned when the extracted target name could not be resolved to a chat.
// It is wrapped with context; use errors.Is(err, ErrSummarizeNoChatMatch).
var ErrSummarizeNoChatMatch = errors.New("summarize: no chat matched target")

// Disambiguation failure markers (wrapped by resolveSummarizeTargetByAgent). Use errors.Is and DisambiguationUserMessage.
var (
	errDisambiguationNoCandidates = errors.New("summarize disambiguation: no direct chat candidates")
	errDisambiguationParseFailed  = errors.New("summarize disambiguation: invalid agent reply")
	errDisambiguationBadIndex     = errors.New("summarize disambiguation: choice index out of range")
)

// DisambiguationUserMessage maps a disambiguation error to a concise user-facing explanation (English).
// For non-disambiguation errors, it returns a generic fallback (caller should log the full error).
func DisambiguationUserMessage(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errDisambiguationNoCandidates):
		return "No Direct chats were available to choose from. For a group conversation, use a Team mention (![:Team](id))."
	case errors.Is(err, errDisambiguationParseFailed):
		return "Could not read the assistant's selection. Please try again, or specify the target with a @mention."
	case errors.Is(err, errDisambiguationBadIndex):
		return "The assistant picked an invalid option. Please try again or use a @mention for the target."
	default:
		return "Could not resolve which chat to summarize (assistant step). Check connectivity and try again, or use a Team/User mention."
	}
}

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
	if strings.TrimSpace(entry.ChatName) == "" {
		slog.Warn("skipping cache add: empty chat_name", "component", "summarize", "chatID", entry.ChatID)
		return
	}
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
	c.entries = nil
	for _, e := range pd.Entries {
		if strings.TrimSpace(e.ChatName) != "" {
			c.entries = append(c.entries, e)
		}
	}
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

	if len(c.entries) != len(pd.Entries) {
		slog.Warn("dropped summarize cache rows with empty chat_name", "component", "summarize", "removed", len(pd.Entries)-len(c.entries))
		c.saveToDisk()
	}

	slog.Info("loaded cache from disk", "component", "summarize", "chats", len(c.entries), "persons", len(pd.Persons))
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
		if strings.TrimSpace(c.entries[i].ChatName) == "" {
			continue
		}
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

// directorySearchQueries returns search strings: full name first, then tokens (longer first).
// Directory APIs often miss full "First Last" but match a single token.
func directorySearchQueries(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			return
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	add(name)
	parts := strings.Fields(name)
	sort.Slice(parts, func(i, j int) bool { return len(parts[i]) > len(parts[j]) })
	for _, p := range parts {
		if len(p) >= 2 {
			add(p)
		}
	}
	return out
}

// nameTokensAllPresent is true when every token in target (length >= 2) appears as a whole word in fullName.
// Substring matching would false-positive (e.g. "yuki" inside "yukio").
func nameTokensAllPresent(fullName, target string) bool {
	fullName = strings.ToLower(strings.TrimSpace(fullName))
	target = strings.ToLower(strings.TrimSpace(target))
	words := strings.Fields(fullName)
	wordSet := make(map[string]bool)
	for _, w := range words {
		w = strings.Trim(w, "., ")
		if w != "" {
			wordSet[w] = true
		}
	}
	hasToken := false
	for _, t := range strings.Fields(target) {
		if len(t) < 2 {
			continue
		}
		hasToken = true
		if !wordSet[t] {
			return false
		}
	}
	return hasToken
}

// pickBestDirectoryEntry scores merged directory rows against the user's target name.
func pickBestDirectoryEntry(entries []ringcentral.DirectoryEntry, target string) *ringcentral.DirectoryEntry {
	target = strings.TrimSpace(strings.ToLower(target))
	if target == "" || len(entries) == 0 {
		return nil
	}
	var best *ringcentral.DirectoryEntry
	bestScore := -1
	for i := range entries {
		e := &entries[i]
		full := strings.TrimSpace(strings.ToLower(e.FirstName + " " + e.LastName))
		email := strings.ToLower(e.Email)
		score := 0
		switch {
		case full == target:
			score = 1000
		case fuzzyMatch(strings.TrimSpace(e.FirstName+" "+e.LastName), target):
			score = 200
		case nameTokensAllPresent(strings.TrimSpace(e.FirstName+" "+e.LastName), target):
			score = 150
		case strings.Contains(email, target):
			score = 90
		}
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	if best == nil || bestScore < 90 {
		return nil
	}
	// Broad token search can return many rows; require a strong match when the result set is large.
	if len(entries) > 8 && bestScore < 150 {
		return nil
	}
	return best
}

func (c *chatCache) collectDirectoryEntries(ctx context.Context, client *ringcentral.Client, name string) []ringcentral.DirectoryEntry {
	byID := make(map[string]ringcentral.DirectoryEntry)
	for _, q := range directorySearchQueries(name) {
		slog.Info("searching company directory", "component", "summarize", "target", name, "query", q)
		result, err := client.SearchDirectory(ctx, q)
		if err != nil {
			slog.Warn("directory search failed", "component", "summarize", "query", q, "error", err)
			continue
		}
		for _, rec := range result.Records {
			byID[rec.ID] = rec
		}
	}
	out := make([]ringcentral.DirectoryEntry, 0, len(byID))
	for _, e := range byID {
		out = append(out, e)
	}
	return out
}

// lookupViaDirectChatList matches the target name against Direct chat display names (no directory required).
func (c *chatCache) lookupViaDirectChatList(ctx context.Context, client *ringcentral.Client, name string) *chatCacheEntry {
	chats, err := client.ListChats(ctx, "Direct")
	if err != nil {
		slog.Warn("list direct chats for summarize failed", "component", "summarize", "error", err)
		return nil
	}
	target := strings.TrimSpace(name)
	if target == "" {
		return nil
	}
	for _, chat := range chats.Records {
		if len(chat.Members) < 2 {
			continue
		}
		cn := strings.TrimSpace(chat.Name)
		if cn == "" {
			continue
		}
		if !fuzzyMatch(cn, target) && !nameTokensAllPresent(cn, target) {
			continue
		}
		slog.Info("resolved Direct chat via chat list display name", "component", "summarize", "chatName", cn, "chatID", chat.ID)
		entry := chatCacheEntry{ChatID: chat.ID, ChatName: cn, ChatType: "Direct"}
		c.addEntry(entry)
		return &entry
	}
	return nil
}

// lookupViaDirectory searches the company directory and creates/finds the Direct chat.
func (c *chatCache) lookupViaDirectory(ctx context.Context, client *ringcentral.Client, name string) *chatCacheEntry {
	entries := c.collectDirectoryEntries(ctx, client, name)
	if len(entries) == 0 {
		slog.Warn("no directory entries found", "component", "summarize", "name", name)
		return nil
	}
	best := pickBestDirectoryEntry(entries, name)
	if best == nil {
		slog.Warn("directory returned entries but none matched confidently", "component", "summarize", "count", len(entries), "name", name)
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
func IsSummarizeCommand(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, kw := range summarizeKeywords {
		if strings.HasPrefix(lower, kw) {
			return true
		}
	}
	return false
}

var reHTTPURL = regexp.MustCompile(`(?i)https?://`)

// IsChatSummarizeIntent is true when the user likely wants RingCentral chat history summarized
// (starts with a summarize keyword and does not look like a web/doc question).
// Messages with http(s) URLs are treated as general summarization for the agent (e.g. README links).
func IsChatSummarizeIntent(text string) bool {
	if !IsSummarizeCommand(text) {
		return false
	}
	if reHTTPURL.MatchString(text) {
		return false
	}
	return true
}

// SummarizeRequest holds parsed summarize parameters.
type SummarizeRequest struct {
	ChatID      string
	ChatName    string
	TimeFrom    time.Time
	UserRequest string // original user message
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

	// Fallback: match Direct chat display names (works when directory search misses e.g. display-name vs legal name)
	if entry := globalChatCache.lookupViaDirectChatList(ctx, client, name); entry != nil {
		req.ChatID = entry.ChatID
		req.ChatName = entry.ChatName
		return req, nil
	}

	return nil, fmt.Errorf("%w: could not find a chat matching %q. For group chats, use mention format: ![:Team](id)", ErrSummarizeNoChatMatch, name)
}

// SummarizeTargetCandidate is one Direct chat option for local agent disambiguation.
type SummarizeTargetCandidate struct {
	Index       int
	ChatID      string
	DisplayName string
}

// BuildSummarizeTargetCandidatesFromChats filters Direct chats that have a display name and at least two members
// (same eligibility rules as lookupViaDirectChatList).
func BuildSummarizeTargetCandidatesFromChats(list *ringcentral.ChatList) []SummarizeTargetCandidate {
	if list == nil {
		return nil
	}
	var out []SummarizeTargetCandidate
	idx := 0
	for _, chat := range list.Records {
		if len(chat.Members) < 2 {
			continue
		}
		cn := strings.TrimSpace(chat.Name)
		if cn == "" {
			continue
		}
		out = append(out, SummarizeTargetCandidate{
			Index:       idx,
			ChatID:      chat.ID,
			DisplayName: cn,
		})
		idx++
	}
	return out
}

// BuildSummarizeTargetDisambiguationPrompt asks the default agent to pick exactly one candidate by index.
func BuildSummarizeTargetDisambiguationPrompt(userText string, candidates []SummarizeTargetCandidate) string {
	var b strings.Builder
	b.WriteString(`You resolve which RingCentral Direct chat the user wants summarized.
Read the user's message and pick the single best matching candidate from the list below.
Respond with ONLY one JSON object and no other text. Use this shape:
{"choice_index":<0-based integer index of the best match, or null if none match>}

User message:
---
`)
	b.WriteString(strings.TrimSpace(userText))
	b.WriteString(`
---

Candidates (Direct chats):
`)
	for _, c := range candidates {
		fmt.Fprintf(&b, "%d. chat_id=%s display_name=%q\n", c.Index, c.ChatID, c.DisplayName)
	}
	return b.String()
}

// ParseSummarizeTargetDisambiguationReply extracts choice_index from the agent reply (JSON, optionally in a fenced block).
func ParseSummarizeTargetDisambiguationReply(reply string) (index int, ok bool) {
	raw := extractJSONObjectFromAgentReply(reply)
	if raw == "" {
		return 0, false
	}
	var pick struct {
		ChoiceIndex *int `json:"choice_index"`
	}
	if err := json.Unmarshal([]byte(raw), &pick); err != nil {
		return 0, false
	}
	if pick.ChoiceIndex == nil {
		return 0, false
	}
	return *pick.ChoiceIndex, true
}

func extractJSONObjectFromAgentReply(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(s), "```") {
		rest := s[3:]
		rest = strings.TrimLeft(rest, "\r\n")
		if strings.HasPrefix(strings.ToLower(rest), "json") {
			rest = rest[4:]
			rest = strings.TrimLeft(rest, "\r\n")
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			s = strings.TrimSpace(rest[:end])
		} else {
			s = strings.TrimSpace(rest)
		}
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
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

	userReq := req.UserRequest
	if userReq == "" {
		userReq = "summarize the chat"
	}

	prompt := fmt.Sprintf(`User request: %s

Please summarize the following chat messages from "%s" (%s). 
Provide a concise summary in the same language as the messages. 
Highlight key topics, decisions, and action items if any.
%s
--- Messages (%d total) ---
%s
--- End of Messages ---`,
		userReq, chatLabel, timeDesc, ActionPrompt, len(lines), strings.Join(lines, "\n"))

	slog.Info("built prompt", "component", "summarize", "chatLabel", chatLabel, "messages", len(lines), "chars", len(prompt))
	return prompt, nil
}

// --- helpers ---

func todayStart() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

var (
	reLastNDays    = regexp.MustCompile(`(?:最近|过去|last)\s*(\d+)\s*(?:天|days?)`)
	reLastNHours   = regexp.MustCompile(`(?:最近|过去|last)\s*(\d+)\s*(?:小时|个小时|hours?)`)
	reLastNCNDays  = regexp.MustCompile(`(?:最近|过去|last)\s*([一二三四五六七八九十百千万两]+)\s*(?:天|days?)`)
	reLastNCNHours = regexp.MustCompile(`(?:最近|过去|last)\s*([一二三四五六七八九十百千万两]+)\s*(?:小时|个小时|hours?)`)
	reDigits       = regexp.MustCompile(`\d+`)
	rePunctSpace   = regexp.MustCompile(`[，。！？,\.!\?\s]+`)
)

// chineseNumeralToInt parses simplified Chinese numerals for time ranges (1..999).
// Examples: 一->1, 两/二->2, 十->10, 十一->11, 二十三->23, 一百->100.
func chineseNumeralToInt(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	r := []rune(s)
	digit := func(x rune) (int, bool) {
		switch x {
		case '零':
			return 0, true
		case '一':
			return 1, true
		case '二', '两':
			return 2, true
		case '三':
			return 3, true
		case '四':
			return 4, true
		case '五':
			return 5, true
		case '六':
			return 6, true
		case '七':
			return 7, true
		case '八':
			return 8, true
		case '九':
			return 9, true
		}
		return 0, false
	}

	if len(r) == 2 && r[1] == '百' {
		if r[0] == '一' {
			return 100, true
		}
		if n, ok := digit(r[0]); ok {
			return n * 100, true
		}
	}

	switch len(r) {
	case 1:
		if r[0] == '十' {
			return 10, true
		}
		if n, ok := digit(r[0]); ok && r[0] != '零' {
			return n, true
		}
	case 2:
		if r[0] == '十' {
			if n, ok := digit(r[1]); ok {
				return 10 + n, true
			}
			return 10, true
		}
		if r[1] == '十' {
			if n, ok := digit(r[0]); ok {
				return n * 10, true
			}
		}
	case 3:
		if r[1] == '十' {
			a, okA := digit(r[0])
			b, okB := digit(r[2])
			if okA && okB {
				return a*10 + b, true
			}
		}
	}
	return 0, false
}

func parseTimeRange(text string) time.Time {
	lower := strings.ToLower(text)
	now := time.Now()

	if m := reLastNDays.FindStringSubmatch(lower); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		if n > 0 {
			return now.AddDate(0, 0, -n)
		}
	}
	if m := reLastNCNDays.FindStringSubmatch(lower); len(m) == 2 {
		if n, ok := chineseNumeralToInt(m[1]); ok && n > 0 {
			return now.AddDate(0, 0, -n)
		}
	}
	if m := reLastNHours.FindStringSubmatch(lower); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		if n > 0 {
			return now.Add(-time.Duration(n) * time.Hour)
		}
	}
	if m := reLastNCNHours.FindStringSubmatch(lower); len(m) == 2 {
		if n, ok := chineseNumeralToInt(m[1]); ok && n > 0 {
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

var (
	reMention = regexp.MustCompile(`!\[:\w+\]\(\d+\)`)
	// reEmail matches a typical corporate email before punctuation splitting breaks it.
	reEmail = regexp.MustCompile(`[\w.%+\-]+@[\w.\-]+\.[a-zA-Z]{2,}`)
	// reChineseTimeSpan removes Chinese (non-Arabic) durations, e.g. "两天" after stripping "最近"
	// leaves "两" if only "天" is removed — breaks directory search ("strong luo 两").
	reChineseTimeSpan = regexp.MustCompile(
		`[一二三四五六七八九十百千万两几]+\s*(?:个\s*)?(?:天|日)` +
			`|[一二三四五六七八九十百千万两几]+\s*(?:个\s*)?(?:小时|钟头)` +
			`|[一二三四五六七八九十百千万两几]+\s*(?:个\s*)?(?:礼拜|星期|周)` +
			`|[一二三四五六七八九十百千万两几]+\s*(?:个\s*)?月` +
			`|[半几]\s*(?:天|日|小时|钟头|周|星期|月)`)
	// Trailing Chinese numerals left after unit stripping (e.g. lone "两").
	reTrailingChineseNumeral = regexp.MustCompile(`\s+[一二三四五六七八九十百千万两几半]+$`)
)

func extractNameFromText(text string) string {
	text = strings.TrimSpace(text)
	if m := reEmail.FindString(text); m != "" {
		return strings.ToLower(m)
	}

	clean := text
	// Remove mentions first (they carry explicit IDs; no name to extract here)
	clean = reMention.ReplaceAllString(clean, "")
	// Remove multi-character phrases before stripping "聊天" (else "聊天记录" -> stray "记录")
	for _, phrase := range []string{
		"聊天记录", "聊天内容", "这个群", "那个群", "群里", "群里的",
	} {
		clean = strings.ReplaceAll(clean, phrase, "")
	}
	// Remove summarize keywords
	for _, kw := range summarizeKeywords {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	// Lowercase for filler removal
	clean = strings.ToLower(clean)
	// Remove time keywords
	for _, kw := range []string{"今天", "昨天", "本周", "最近", "过去", "today", "yesterday", "this week", "last"} {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	clean = reChineseTimeSpan.ReplaceAllString(clean, "")
	// Remove common filler words (Chinese single chars and phrases)
	for _, kw := range []string{
		"一下", "下", "的", "消息", "聊天", "对话", "群聊", "群", "记录",
		"跟", "和", "与", "我", "了", "这", "那",
		"messages", "chat", "conversation", "with", "my", "the",
	} {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	// Remove digits
	clean = reDigits.ReplaceAllString(clean, "")
	// Remove punctuation and collapse whitespace
	clean = rePunctSpace.ReplaceAllString(clean, " ")
	// Remove remaining time units
	for _, kw := range []string{"天", "小时", "个", "hours", "days"} {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	clean = strings.TrimSpace(clean)
	clean = reTrailingChineseNumeral.ReplaceAllString(clean, "")
	clean = strings.TrimSpace(clean)
	return clean
}

func fuzzyMatch(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	h := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(haystack), " ", ""))
	n := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(needle), " ", ""))
	if h == "" {
		// strings.Contains(n, "") is always true in Go; empty haystack must not match every lookup.
		return false
	}
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
