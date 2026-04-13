package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"
)

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

func (c *chatCache) addEntry(entry chatCacheEntry) {
	c.mu.Lock()
	c.entries = append(c.entries, entry)
	c.loaded = true
	c.mu.Unlock()
	c.saveToDisk()
}

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

func (c *chatCache) lookup(name string) *chatCacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.entries {
		if exactMatch(c.entries[i].ChatName, name) {
			return &c.entries[i]
		}
	}
	var best *chatCacheEntry
	for i := range c.entries {
		if fuzzyMatch(c.entries[i].ChatName, name) {
			if best == nil || len(c.entries[i].ChatName) < len(best.ChatName) {
				best = &c.entries[i]
			}
		}
	}
	return best
}

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

	best := bestDirectoryMatch(result.Records, name)
	if best == nil {
		slog.Warn("directory returned entries but none matched", "component", "summarize", "count", len(result.Records), "name", name)
		return nil
	}

	fullName := strings.TrimSpace(best.FirstName + " " + best.LastName)
	slog.Info("directory matched", "component", "summarize", "fullName", fullName, "id", best.ID, "email", best.Email)

	chat, err := client.CreateConversation(ctx, []string{best.ID})
	if err != nil {
		slog.Warn("create conversation failed", "component", "summarize", "error", err)
		return nil
	}

	slog.Info("resolved Direct chat", "component", "summarize", "fullName", fullName, "chatID", chat.ID)

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

// ResolveChatTarget finds the target chat ID from mentions or fuzzy name matching.
func ResolveChatTarget(ctx context.Context, client *ringcentral.Client, ag agent.Agent, text string, mentions []ringcentral.Mention) (*SummarizeRequest, error) {
	req := &SummarizeRequest{
		TimeFrom:    todayStart(),
		UserRequest: text,
	}

	if ag != nil {
		req.TimeFrom = extractDateViaAgent(ctx, ag, text)
	} else {
		req.TimeFrom = parseTimeRange(text)
	}

	botID := client.OwnerID()
	for _, m := range mentions {
		switch m.Type {
		case "Team":
			req.ChatID = m.ID
			req.ChatName = m.Name
			return req, nil
		case "Person":
			if m.ID == botID {
				continue
			}
			chatID, err := findDirectChat(ctx, client, m.ID)
			if err != nil {
				return nil, fmt.Errorf("find chat with %s: %w", m.Name, err)
			}
			req.ChatID = chatID
			req.ChatName = m.Name
			return req, nil
		}
	}

	var name string
	if ag != nil {
		name = extractNameViaAgent(ctx, ag, text)
	}
	if name == "" {
		name = extractNameFromText(text)
	}
	if name == "" {
		return nil, fmt.Errorf("cannot determine which chat to summarize. Use a mention or specify a name")
	}

	slog.Info("looking up chat", "component", "summarize", "name", name)

	globalChatCache.ensureLoaded()

	cached := globalChatCache.lookup(name)
	if cached != nil && exactMatch(cached.ChatName, name) {
		req.ChatID = cached.ChatID
		req.ChatName = cached.ChatName
		slog.Info("cache hit (exact)", "component", "summarize", "chatName", cached.ChatName, "chatID", cached.ChatID)
		return req, nil
	}

	if entry := globalChatCache.lookupViaDirectory(ctx, client, name); entry != nil {
		req.ChatID = entry.ChatID
		req.ChatName = entry.ChatName
		return req, nil
	}

	if cached != nil {
		req.ChatID = cached.ChatID
		req.ChatName = cached.ChatName
		slog.Info("cache hit (fuzzy)", "component", "summarize", "chatName", cached.ChatName, "chatID", cached.ChatID)
		return req, nil
	}

	return nil, fmt.Errorf("could not find a chat matching %q. For group chats, use mention format: ![:Team](id)", name)
}

// --- name extraction ---

var (
	reMention        = regexp.MustCompile(`!\[:\w+\]\(\d+\)`)
	reDigits         = regexp.MustCompile(`\d+`)
	rePunctSpace     = regexp.MustCompile(`[，。！？,\.!\?\s]+`)
	reInstructionSplit = regexp.MustCompile(`(?i)(?:` +
		`并用|并且|并|然后|接着|之后|同时|通过|` +
		`and then|then|and also|also|and send|and create|and post|` +
		`そして|それから|その後|` +
		`그리고|그런\s*다음|` +
		`puis|ensuite|et\s+aussi|` +
		`luego|después|y\s+también|` +
		`dann|und\s+auch|` +
		`потом|затем|и\s+также)`)
)

var cjkFillers = flattenLang(map[string][]string{
	"zh": {"一下", "下", "的", "消息", "聊天", "对话", "群聊", "群",
		"跟", "和", "与", "我", "了",
		"发给", "发送", "发到", "给", "他", "她", "它", "他们",
		"笔记", "任务", "日程"},
	"ja": {"チャット", "会話", "メッセージ", "要約して", "要約", "まとめて", "まとめ",
		"の", "と", "を", "は", "が", "で", "に", "へ"},
	"ko": {"과의", "와의", "과", "와", "의", "을", "를", "에서",
		"채팅", "대화", "메시지", "요약"},
})

var enFillers = flattenLang(map[string][]string{
	"en": {"messages", "chat", "conversation", "with", "my", "the", "of", "a",
		"send", "to", "him", "her", "them",
		"note", "task", "event"},
})

const nameExtractConversationID = "name:extractor"

func extractNameViaAgent(ctx context.Context, ag agent.Agent, text string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	start := time.Now()
	reply, err := ag.Chat(ctx, nameExtractConversationID, fmt.Sprintf(NameExtractPrompt(), text))
	elapsed := time.Since(start)
	if err != nil {
		slog.Warn("agent name extraction failed", "component", "summarize", "error", err, "elapsed", elapsed)
		return ""
	}

	name := strings.TrimSpace(reply)
	name = strings.Trim(name, `"'`)
	if strings.EqualFold(name, "none") || name == "" {
		slog.Info("agent found no target name", "component", "summarize", "elapsed", elapsed)
		return ""
	}
	slog.Info("agent extracted name", "component", "summarize", "name", name, "elapsed", elapsed)
	return strings.ToLower(name)
}

func extractNameFromText(text string) string {
	clean := text
	for _, kw := range summarizeKeywords {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	clean = reMention.ReplaceAllString(clean, "")

	if parts := reInstructionSplit.Split(clean, 2); len(parts) > 1 {
		clean = parts[0]
	}

	clean = strings.ToLower(clean)
	for _, kw := range timeWords {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	for _, kw := range cjkFillers {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	clean = removeWholeWords(clean, enFillers)
	clean = reDigits.ReplaceAllString(clean, "")
	clean = rePunctSpace.ReplaceAllString(clean, " ")
	for _, kw := range []string{"天", "小时", "个", "hours", "days"} {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	clean = strings.TrimSpace(clean)
	return clean
}

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

func exactMatch(haystack, needle string) bool {
	h := strings.ToLower(strings.TrimSpace(haystack))
	n := strings.ToLower(strings.TrimSpace(needle))
	return h == n
}

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
