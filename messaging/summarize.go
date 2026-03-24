package messaging

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
)

var summarizeKeywords = []string{"总结", "summarize", "summary"}

// IsSummarizeCommand checks if the text is a summarize request.
func IsSummarizeCommand(text string) bool {
	lower := strings.ToLower(text)
	for _, kw := range summarizeKeywords {
		if strings.Contains(lower, kw) {
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

	// Search Teams by name
	teamChats, err := client.ListChats(ctx, "Team")
	if err == nil {
		for _, chat := range teamChats.Records {
			if fuzzyMatch(chat.Name, name) {
				req.ChatID = chat.ID
				req.ChatName = chat.Name
				log.Printf("[summarize] matched team %q (id=%s)", chat.Name, chat.ID)
				return req, nil
			}
		}
	}

	// Search Group chats by name
	groupChats, err := client.ListChats(ctx, "Group")
	if err == nil {
		for _, chat := range groupChats.Records {
			if fuzzyMatch(chat.Name, name) {
				req.ChatID = chat.ID
				req.ChatName = chat.Name
				log.Printf("[summarize] matched group %q (id=%s)", chat.Name, chat.ID)
				return req, nil
			}
		}
	}

	// Search Direct chats by member name
	directChats, err := client.ListChats(ctx, "Direct")
	if err == nil {
		for _, chat := range directChats.Records {
			for _, memberID := range chat.Members {
				person, perr := client.GetPersonInfo(ctx, memberID)
				if perr != nil {
					continue
				}
				fullName := strings.TrimSpace(person.FirstName + " " + person.LastName)
				if fuzzyMatch(fullName, name) || fuzzyMatch(person.Email, name) {
					req.ChatID = chat.ID
					req.ChatName = fullName
					log.Printf("[summarize] matched person %q in direct chat (id=%s)", fullName, chat.ID)
					return req, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("could not find a chat matching %q", name)
}

// BuildSummaryPrompt fetches chat messages and builds a prompt for the agent.
func BuildSummaryPrompt(ctx context.Context, client *ringcentral.Client, req *SummarizeRequest) (string, error) {
	opts := ringcentral.ListPostsOpts{
		RecordCount:      250,
		CreationTimeFrom: req.TimeFrom.UTC().Format(time.RFC3339),
	}

	posts, err := client.ListPosts(ctx, req.ChatID, opts)
	if err != nil {
		return "", fmt.Errorf("fetch posts: %w", err)
	}

	if len(posts.Records) == 0 {
		return "", fmt.Errorf("no messages found in the specified time range")
	}

	// Resolve person names (with cache)
	nameCache := make(map[string]string)
	resolveName := func(creatorID string) string {
		if n, ok := nameCache[creatorID]; ok {
			return n
		}
		person, err := client.GetPersonInfo(ctx, creatorID)
		if err != nil {
			nameCache[creatorID] = creatorID
			return creatorID
		}
		name := strings.TrimSpace(person.FirstName + " " + person.LastName)
		if name == "" {
			name = creatorID
		}
		nameCache[creatorID] = name
		return name
	}

	// Posts are returned newest-first, reverse for chronological order
	var lines []string
	for i := len(posts.Records) - 1; i >= 0; i-- {
		p := posts.Records[i]
		if p.Text == "" {
			continue
		}
		t, _ := time.Parse(time.RFC3339, p.CreationTime)
		name := resolveName(p.CreatorID)
		lines = append(lines, fmt.Sprintf("[%s] %s: %s", t.Format("15:04"), name, p.Text))
	}

	if len(lines) == 0 {
		return "", fmt.Errorf("no text messages found in the specified time range")
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
	// Remove summarize keywords
	clean := text
	for _, kw := range summarizeKeywords {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	// Remove mentions
	clean = reMention.ReplaceAllString(clean, "")
	// Remove time keywords
	for _, kw := range []string{"今天", "昨天", "本周", "最近", "过去", "today", "yesterday", "this week", "last"} {
		clean = strings.ReplaceAll(strings.ToLower(clean), kw, "")
	}
	// Remove common filler words
	for _, kw := range []string{"一下", "的", "消息", "聊天", "对话", "跟", "和", "与", "我", "messages", "chat", "conversation", "with", "my"} {
		clean = strings.ReplaceAll(clean, kw, "")
	}
	// Remove digits (days count etc)
	clean = regexp.MustCompile(`\d+`).ReplaceAllString(clean, "")
	// Remove punctuation and trim
	clean = regexp.MustCompile(`[，。！？,\.!\?\s]+`).ReplaceAllString(clean, " ")
	clean = strings.TrimSpace(clean)

	// Remove remaining filler
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
		for _, memberID := range chat.Members {
			if memberID == personID {
				return chat.ID, nil
			}
		}
	}
	return "", fmt.Errorf("no direct chat found with person %s", personID)
}
