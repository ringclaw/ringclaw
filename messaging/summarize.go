package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
)

var summarizeKeywords = []string{"总结", "summarize", "summary"}

const defaultSummaryMessageLimit = 250

// IsSummarizeCommand checks if the text starts with a summarize keyword.
func IsSummarizeCommand(text string) bool {
	return isSummarizeKeyword(text)
}

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
	UserRequest  string
	MessageLimit int
}

// BuildSummaryPrompt fetches chat messages and builds a prompt for the agent.
func BuildSummaryPrompt(ctx context.Context, client *ringcentral.Client, req *SummarizeRequest) (string, error) {
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

	oldest := posts.Records[len(posts.Records)-1].CreationTime
	newest := posts.Records[0].CreationTime
	slog.Debug("fetched posts", "component", "summarize", "chatID", req.ChatID, "count", len(posts.Records), "oldest", oldest, "newest", newest, "timeFrom", req.TimeFrom.Format(time.RFC3339))

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

	prompt := fmt.Sprintf(SummaryPrompt(),
		userReq, chatLabel, timeDesc, limit, len(lines), strings.Join(lines, "\n"), ActionPrompt())

	slog.Info("built prompt", "component", "summarize", "chatLabel", chatLabel, "messages", len(lines), "chars", len(prompt))
	return prompt, nil
}

func todayStart() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}
