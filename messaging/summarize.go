package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"
)

const dateExtractConversationID = "date:extractor"

// dateExtractTimeout caps how long we wait on the LLM to extract a date.
// The previous 10s budget regularly hit context-deadline-exceeded with
// long-running ACP sessions; the regex fast-path now handles common cases
// without paying any LLM latency, so the LLM only runs on phrases the
// regex can't classify.
const dateExtractTimeout = 20 * time.Second

func extractDateViaAgent(ctx context.Context, ag agent.Agent, text string) time.Time {
	// Fast path: try the deterministic regex parser first. It handles
	// canonical phrases like "最近一周", "过去 7 天", "last 3 days",
	// "4月10日", "2026-04-10", "昨天/last week/上个月" with zero LLM tokens
	// and zero network latency. parseTimeRange only returns todayStart()
	// when nothing matches, which is our signal to consult the LLM.
	if parsed := parseTimeRange(text); !parsed.Equal(todayStart()) {
		slog.Debug("date resolved via regex (LLM skipped)", "component", "summarize", "resolved", parsed.Format(time.RFC3339))
		return parsed
	}

	if ag == nil {
		return todayStart()
	}

	ctx, cancel := context.WithTimeout(ctx, dateExtractTimeout)
	defer cancel()

	now := time.Now()
	prompt := fmt.Sprintf(DateExtractPrompt(), now.Format("2006-01-02"), text)

	start := time.Now()
	reply, err := ag.Chat(ctx, dateExtractConversationID, prompt)
	elapsed := time.Since(start)
	if err != nil {
		slog.Warn("agent date extraction failed, defaulting to today", "component", "summarize", "error", err, "elapsed", elapsed)
		return todayStart()
	}

	cleaned := strings.TrimSpace(reply)
	cleaned = strings.Trim(cleaned, `"'`)

	if strings.EqualFold(cleaned, "none") || cleaned == "" {
		slog.Info("agent found no date, defaulting to today", "component", "summarize", "elapsed", elapsed)
		return todayStart()
	}

	if t, err := time.Parse("2006-01-02", cleaned); err == nil {
		slog.Info("agent extracted date", "component", "summarize", "date", cleaned, "elapsed", elapsed)
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
	}

	if parsed := parseTimeRange(cleaned); !parsed.Equal(todayStart()) {
		slog.Info("agent extracted relative date", "component", "summarize", "reply", cleaned, "resolved", parsed.Format("2006-01-02"), "elapsed", elapsed)
		return parsed
	}

	slog.Info("agent date reply not parseable, defaulting to today", "component", "summarize", "reply", cleaned, "elapsed", elapsed)
	return todayStart()
}

var summarizeKeywords = []string{"总结", "summarize", "summarise", "summary"}

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
