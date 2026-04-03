package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/internal/util"
	"github.com/ringclaw/ringclaw/ringcentral"
)

const maxSeenMsgs = 10000

// AgentFactory creates an agent by config name. Returns nil if the name is unknown.
type AgentFactory func(ctx context.Context, name string) agent.Agent

// SaveDefaultFunc persists the default agent name to config file.
type SaveDefaultFunc func(name string) error

// AgentMeta holds static config info about an agent (for /status display).
type AgentMeta struct {
	Name    string
	Type    string // "acp", "cli", "http"
	Command string // binary path or endpoint
	Model   string
}

// Handler processes incoming RingCentral messages and dispatches replies.
type Handler struct {
	mu            sync.RWMutex
	defaultName   string
	agents        map[string]agent.Agent // name -> running agent
	agentMetas    []AgentMeta            // all configured agents (for /status)
	customAliases map[string]string      // custom alias -> agent name (from config)
	factory       AgentFactory
	saveDefault   SaveDefaultFunc
	version       string
	startTime     time.Time
	seenMsgs      sync.Map // map[string]time.Time — dedup by post ID
	seenMsgCount  int64    // approximate count for capacity limiting
	cronStore     *CronStore

	groupSummaryGroupID      string
	groupSummaryMessageLimit int
}

// NewHandler creates a new message handler.
func NewHandler(factory AgentFactory, saveDefault SaveDefaultFunc, version string) *Handler {
	return &Handler{
		agents:      make(map[string]agent.Agent),
		factory:     factory,
		saveDefault: saveDefault,
		version:     version,
		startTime:   time.Now(),
	}
}

// cleanSeenMsgs removes entries older than 5 minutes from the dedup cache.
// Also enforces maxSeenMsgs capacity by removing oldest entries.
func (h *Handler) cleanSeenMsgs() {
	cutoff := time.Now().Add(-5 * time.Minute)
	var removed int64
	h.seenMsgs.Range(func(key, value any) bool {
		if t, ok := value.(time.Time); ok && t.Before(cutoff) {
			h.seenMsgs.Delete(key)
			removed++
		}
		return true
	})
	if removed > 0 {
		atomic.AddInt64(&h.seenMsgCount, -removed)
	}
}

// SetCustomAliases sets custom alias mappings from config.
func (h *Handler) SetCustomAliases(aliases map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.customAliases = aliases
}

// SetCronStore sets the cron job store for /cron commands.
func (h *Handler) SetCronStore(store *CronStore) {
	h.cronStore = store
}

// SetGroupSummaryConfig configures optional summarize behavior for the current
// bot group.
func (h *Handler) SetGroupSummaryConfig(groupID string, limit int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.groupSummaryGroupID = strings.TrimSpace(groupID)
	if limit <= 0 {
		limit = defaultSummaryMessageLimit
	}
	h.groupSummaryMessageLimit = limit
}

// SetAgentMetas sets the list of all configured agents (for /status).
func (h *Handler) SetAgentMetas(metas []AgentMeta) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.agentMetas = metas
}

// SetDefaultAgent sets the default agent (already started).
func (h *Handler) SetDefaultAgent(name string, ag agent.Agent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.defaultName = name
	h.agents[name] = ag
	slog.Info("default agent ready", "component", "handler", "name", name, "info", ag.Info())
}

// getAgent returns a running agent by name, or starts it on demand via factory.
func (h *Handler) getAgent(ctx context.Context, name string) (agent.Agent, error) {
	h.mu.RLock()
	ag, ok := h.agents[name]
	h.mu.RUnlock()
	if ok {
		return ag, nil
	}

	if h.factory == nil {
		return nil, fmt.Errorf("agent %q not found and no factory configured", name)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if ag, ok := h.agents[name]; ok {
		return ag, nil
	}

	slog.Debug("starting agent on demand", "component", "handler", "name", name)
	ag = h.factory(ctx, name)
	if ag == nil {
		return nil, fmt.Errorf("agent %q not available", name)
	}

	h.agents[name] = ag
	slog.Info("agent started on demand", "component", "handler", "name", name, "info", ag.Info())
	return ag, nil
}

// isKnownAgent checks if a name matches a configured agent (started or available via factory).
func (h *Handler) isKnownAgent(name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if _, ok := h.agents[name]; ok {
		return true
	}
	for _, m := range h.agentMetas {
		if m.Name == name {
			return true
		}
	}
	return false
}

// getDefaultAgent returns the default agent (may be nil if not ready yet).
func (h *Handler) getDefaultAgent() agent.Agent {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.defaultName == "" {
		return nil
	}
	return h.agents[h.defaultName]
}

// GetDefaultAgent returns the default agent (exported for cron/heartbeat).
func (h *Handler) GetDefaultAgent() agent.Agent {
	return h.getDefaultAgent()
}

// GetAgent returns a running agent by name (exported for cron).
func (h *Handler) GetAgent(ctx context.Context, name string) (agent.Agent, error) {
	return h.getAgent(ctx, name)
}

// agentAliases maps short aliases to agent config names.
var agentAliases = map[string]string{
	"cc":  "claude",
	"cx":  "codex",
	"oc":  "openclaw",
	"cs":  "cursor",
	"km":  "kimi",
	"gm":  "gemini",
	"ocd": "opencode",
	"pi":  "pi",
	"cp":  "copilot",
	"dr":  "droid",
	"if":  "iflow",
	"kr":  "kiro",
	"qw":  "qwen",
}

// resolveAlias returns the full agent name for an alias, or the original name if no alias matches.
// Checks custom aliases (from config) first, then built-in aliases.
func (h *Handler) resolveAlias(name string) string {
	h.mu.RLock()
	custom := h.customAliases
	h.mu.RUnlock()
	if custom != nil {
		if full, ok := custom[name]; ok {
			return full
		}
	}
	if full, ok := agentAliases[name]; ok {
		return full
	}
	return name
}

// parseCommand checks if text starts with "/" followed by agent name(s).
// Supports multiple agents: "/cc /cx hello" returns (["claude","codex"], "hello").
// Returns (agentNames, actualMessage). Aliases are resolved automatically.
// If no command prefix, returns (nil, originalText).
//
// Ported from github.com/fastclaw-ai/weclaw commits 9ea72a1 + 981d58c.
func (h *Handler) parseCommand(text string) ([]string, string) {
	if !strings.HasPrefix(text, "/") {
		return nil, text
	}

	var names []string
	rest := text
	for {
		rest = strings.TrimSpace(rest)
		if !strings.HasPrefix(rest, "/") {
			break
		}

		after := rest[1:]
		idx := strings.IndexAny(after, " /")
		var token string
		if idx < 0 {
			token = after
			rest = ""
		} else if after[idx] == '/' {
			token = after[:idx]
			rest = after[idx:]
		} else {
			token = after[:idx]
			rest = strings.TrimSpace(after[idx+1:])
		}

		if token != "" {
			names = append(names, h.resolveAlias(token))
		}

		if rest == "" {
			break
		}
	}

	// Deduplicate names preserving order
	seen := make(map[string]bool)
	unique := names[:0]
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			unique = append(unique, n)
		}
	}

	return unique, rest
}

// HandleMessage processes a single incoming RingCentral post.
func (h *Handler) HandleMessage(ctx context.Context, client *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post) {
	text := strings.TrimSpace(post.Text)
	if text == "" {
		slog.Debug("received empty message, skipping", "component", "handler", "creatorID", post.CreatorID)
		return
	}

	// Deduplicate by post ID to avoid processing the same message multiple times
	if post.ID != "" {
		if _, loaded := h.seenMsgs.LoadOrStore(post.ID, time.Now()); loaded {
			slog.Debug("duplicate message skipped", "component", "handler", "postID", post.ID)
			return
		}
		atomic.AddInt64(&h.seenMsgCount, 1)
		if atomic.LoadInt64(&h.seenMsgCount) > maxSeenMsgs/2 {
			go h.cleanSeenMsgs()
		}
	}

	chatID := post.GroupID
	slog.Info("received message", "component", "handler", "creatorID", post.CreatorID, "chatID", chatID, "text", util.Truncate(text, 80))

	// In bot group chats (not bot DM), restrict privileged commands to the bot owner
	isBotGroup := client.IsBot() && !client.IsBotDM(chatID)
	if isBotGroup && isPrivilegedCommand(text) {
		if post.CreatorID != readClient.OwnerID() {
			slog.Info("blocked privileged command from non-owner", "component", "handler", "creatorID", post.CreatorID, "command", util.Truncate(text, 30))
			logSendError(SendTextReply(ctx, client, chatID, "Only the bot owner can use this command in group chats."))
			return
		}
	}

	// Built-in commands (no typing needed)
	if text == "/info" || text == "/status" {
		cardJSON := h.buildStatusCard()
		if _, err := client.CreateAdaptiveCard(ctx, chatID, cardJSON); err != nil {
			slog.Error("failed to send status card, falling back to text", "component", "handler", "error", err)
			reply := h.buildStatus()
			if err := SendTextReply(ctx, client, chatID, reply); err != nil {
				slog.Error("failed to send reply", "component", "handler", "error", err)
			}
		}
		return
	} else if text == "/new" || text == "/clear" {
		reply := h.resetDefaultSession(ctx, conversationIDForPost(client, post))
		if err := SendTextReply(ctx, client, chatID, reply); err != nil {
			slog.Error("failed to send reply", "component", "handler", "error", err)
		}
		return
	} else if strings.HasPrefix(text, "/cwd") {
		reply := h.handleCwd(text)
		if err := SendTextReply(ctx, client, chatID, reply); err != nil {
			slog.Error("failed to send reply", "component", "handler", "error", err)
		}
		return
	} else if text == "/help" {
		reply := buildHelpText()
		if err := SendTextReply(ctx, client, chatID, reply); err != nil {
			slog.Error("failed to send reply", "component", "handler", "error", err)
		}
		return
	} else if strings.HasPrefix(text, "/cron") {
		if h.cronStore == nil {
			logSendError(SendTextReply(ctx, client, chatID, "Cron is not configured."))
			return
		}
		reply := HandleCronCommand(h.cronStore, text, chatID)
		if err := SendTextReply(ctx, client, chatID, reply); err != nil {
			slog.Error("failed to send cron reply", "component", "handler", "error", err)
		}
		return
	} else if strings.HasPrefix(text, "/chatinfo") {
		reply := handleChatInfo(ctx, readClient, chatID, text)
		if err := SendTextReply(ctx, client, chatID, reply); err != nil {
			slog.Error("failed to send chatinfo reply", "component", "handler", "error", err)
		}
		return
	}

	// Explicit action commands: /task, /note, /event (use readClient for API access)
	if IsActionCommand(text) {
		reply := HandleActionCommand(ctx, readClient, chatID, text)
		if err := SendTextReply(ctx, client, chatID, reply); err != nil {
			slog.Error("failed to send action reply", "component", "handler", "error", err)
		}
		return
	}

	// AI intent classification: if the message matches loose multilingual keywords,
	// ask the default agent to classify the intent before routing.
	if matchesIntentTrigger(text) {
		if intent := h.classifyAndRoute(ctx, client, readClient, post, text, isBotGroup); intent {
			return
		}
	}

	// Route: "/agent msg" or "/a /b msg" -> agent(s)
	agentNames, message := h.parseCommand(text)

	// No command prefix -> send to default agent
	if len(agentNames) == 0 {
		h.sendToDefaultAgent(ctx, client, readClient, post, text)
		return
	}

	// No message -> switch default agent (only first name)
	if message == "" {
		if len(agentNames) == 1 && h.isKnownAgent(agentNames[0]) {
			// Block agent switch from non-owner in bot group chats
			if isBotGroup && post.CreatorID != readClient.OwnerID() {
				logSendError(SendTextReply(ctx, client, chatID, "Only the bot owner can switch agents in group chats."))
				return
			}
			reply := h.switchDefault(ctx, agentNames[0])
			if err := SendTextReply(ctx, client, chatID, reply); err != nil {
				slog.Error("failed to send reply", "component", "handler", "error", err)
			}
		} else if len(agentNames) == 1 && !h.isKnownAgent(agentNames[0]) {
			h.sendToDefaultAgent(ctx, client, readClient, post, text)
		} else {
			reply := "Usage: specify one agent to switch, or add a message to broadcast"
			if err := SendTextReply(ctx, client, chatID, reply); err != nil {
				slog.Error("failed to send reply", "component", "handler", "error", err)
			}
		}
		return
	}

	// Filter to known agents; if no known agents -> forward to default
	var knownNames []string
	for _, name := range agentNames {
		if h.isKnownAgent(name) {
			knownNames = append(knownNames, name)
		}
	}
	if len(knownNames) == 0 {
		h.sendToDefaultAgent(ctx, client, readClient, post, text)
		return
	}

	if len(knownNames) == 1 {
		h.sendToNamedAgent(ctx, client, readClient, post, knownNames[0], message)
	} else {
		// Multi-agent broadcast: parallel dispatch
		h.broadcastToAgents(ctx, client, readClient, post, knownNames, message)
	}
}

func conversationIDForPost(client *ringcentral.Client, post ringcentral.Post) string {
	chatID := strings.TrimSpace(post.GroupID)
	creatorID := strings.TrimSpace(post.CreatorID)
	if client != nil && client.IsBotDM(chatID) {
		return fmt.Sprintf("rc:dm:%s:%s", chatID, creatorID)
	}
	return fmt.Sprintf("rc:chat:%s:user:%s", chatID, creatorID)
}

// sendToDefaultAgent sends the message to the default agent and replies.
func (h *Handler) sendToDefaultAgent(ctx context.Context, client *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post, text string) {
	chatID := post.GroupID
	conversationID := conversationIDForPost(client, post)

	placeholderID, placeholderErr := SendTypingPlaceholder(ctx, client, chatID)
	if placeholderErr != nil {
		slog.Error("failed to send typing placeholder", "component", "handler", "error", placeholderErr)
	}

	ag := h.getDefaultAgent()
	var reply string
	if ag != nil {
		var err error
		reply, err = h.chatWithAgent(ctx, ag, conversationID, text+ActionPrompt())
		if err != nil {
			reply = fmt.Sprintf("Error: %v", err)
		}
	} else {
		slog.Warn("agent not ready, using echo mode", "component", "handler", "creatorID", post.CreatorID)
		reply = "[echo] " + text
	}

	h.sendReplyWithActions(ctx, client, readClient, post, reply, placeholderID)
}

// sendToNamedAgent sends the message to a specific agent and replies.
func (h *Handler) sendToNamedAgent(ctx context.Context, client *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post, name, message string) {
	chatID := post.GroupID
	conversationID := conversationIDForPost(client, post)

	placeholderID, placeholderErr := SendTypingPlaceholder(ctx, client, chatID)
	if placeholderErr != nil {
		slog.Error("failed to send typing placeholder", "component", "handler", "error", placeholderErr)
	}

	ag, agErr := h.getAgent(ctx, name)
	if agErr != nil {
		slog.Error("agent not available", "component", "handler", "agent", name, "error", agErr)
		reply := fmt.Sprintf("Agent %q is not available: %v", name, agErr)
		if err := SendTextReply(ctx, client, chatID, reply); err != nil {
			slog.Error("failed to send reply", "component", "handler", "error", err)
		}
		return
	}

	reply, err := h.chatWithAgent(ctx, ag, conversationID, message+ActionPrompt())
	if err != nil {
		reply = fmt.Sprintf("Error: %v", err)
	}

	h.sendReplyWithActions(ctx, client, readClient, post, reply, placeholderID)
}

// broadcastToAgents sends the message to multiple agents in parallel.
// Each reply is sent as a separate message with the agent name prefix.
func (h *Handler) broadcastToAgents(ctx context.Context, client *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post, names []string, message string) {
	conversationID := conversationIDForPost(client, post)
	type result struct {
		name  string
		reply string
	}

	ch := make(chan result, len(names))
	for _, name := range names {
		go func(n string) {
			ag, err := h.getAgent(ctx, n)
			if err != nil {
				ch <- result{name: n, reply: fmt.Sprintf("Error: %v", err)}
				return
			}
			reply, err := h.chatWithAgent(ctx, ag, conversationID, message+ActionPrompt())
			if err != nil {
				ch <- result{name: n, reply: fmt.Sprintf("Error: %v", err)}
				return
			}
			ch <- result{name: n, reply: reply}
		}(name)
	}

	// Send replies as they arrive
	for range names {
		r := <-ch
		reply := fmt.Sprintf("[%s] %s", r.name, r.reply)
		h.sendReplyWithActions(ctx, client, readClient, post, reply, "")
	}
}

// sendReplyWithActions processes action blocks and sends the final reply.
// actionClient is used for executing actions (should be private app when available).
func (h *Handler) sendReplyWithActions(ctx context.Context, client *ringcentral.Client, actionClient *ringcentral.Client, post ringcentral.Post, reply, placeholderID string) {
	chatID := post.GroupID

	// Parse and execute any ACTION blocks from the agent's response
	cleanReply, actions := ParseAgentActions(reply)
	if len(actions) > 0 {
		reply = cleanReply
		results := ExecuteAgentActions(ctx, client, actionClient, chatID, actions)
		if len(results) > 0 {
			defer func() {
				logSendError(SendTextReply(ctx, client, chatID, strings.Join(results, "\n")))
			}()
		}
	}

	// Extract image URLs from markdown (before conversion strips image syntax)
	imageURLs := ExtractImageURLs(reply)

	// Convert full markdown to RingCentral Mini-Markdown
	reply = MarkdownToMiniMarkdown(reply)

	// Wrap reply with answer markers (skip for bot client)
	if !client.IsBot() {
		reply = wrapAnswer(reply)
	}

	// Update the placeholder with the real reply, or send a new post
	if strings.TrimSpace(reply) == "" {
		// No text reply -- delete the placeholder instead of leaving it empty
		if placeholderID != "" {
			if delErr := client.DeletePost(ctx, chatID, placeholderID); delErr != nil {
				slog.Error("failed to delete empty placeholder", "component", "handler", "error", delErr)
			} else {
				slog.Info("deleted empty placeholder", "component", "handler", "postID", placeholderID)
			}
		}
	} else if placeholderID != "" {
		if updateErr := UpdatePostText(ctx, client, chatID, placeholderID, reply); updateErr != nil {
			slog.Error("failed to update placeholder, sending new post", "component", "handler", "error", updateErr)
			if sendErr := SendTextReply(ctx, client, chatID, reply); sendErr != nil {
				slog.Error("failed to send reply", "component", "handler", "error", sendErr)
			}
		}
	} else {
		if sendErr := SendTextReply(ctx, client, chatID, reply); sendErr != nil {
			slog.Error("failed to send reply", "component", "handler", "error", sendErr)
		}
	}

	// Send extracted images as separate file uploads
	for _, imgURL := range imageURLs {
		if mediaErr := SendMediaFromURL(ctx, client, chatID, imgURL); mediaErr != nil {
			slog.Error("failed to send image", "component", "handler", "error", mediaErr)
		}
	}
}

// chatWithAgent sends a message to an agent and returns the reply.
func (h *Handler) chatWithAgent(ctx context.Context, ag agent.Agent, userID, message string) (string, error) {
	info := ag.Info()
	slog.Info("dispatching to agent", "component", "handler", "info", info, "userID", userID)

	start := time.Now()
	reply, err := ag.Chat(ctx, userID, message)
	elapsed := time.Since(start)

	if err != nil {
		slog.Error("agent error", "component", "handler", "info", info, "elapsed", elapsed, "error", err)
		return "", err
	}

	slog.Info("agent replied", "component", "handler", "info", info, "elapsed", elapsed, "reply", util.Truncate(reply, 100))
	return reply, nil
}

func (h *Handler) configuredGroupSummaryGroupID() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.groupSummaryGroupID
}

func (h *Handler) groupSummaryLimit() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.groupSummaryMessageLimit <= 0 {
		return defaultSummaryMessageLimit
	}
	return h.groupSummaryMessageLimit
}

// switchDefault switches the default agent.
func (h *Handler) switchDefault(ctx context.Context, name string) string {
	ag, err := h.getAgent(ctx, name)
	if err != nil {
		slog.Warn("failed to switch default agent", "component", "handler", "name", name, "error", err)
		return fmt.Sprintf("Failed to switch to %q: %v", name, err)
	}

	h.mu.Lock()
	old := h.defaultName
	h.defaultName = name
	h.agents[name] = ag
	h.mu.Unlock()

	if h.saveDefault != nil {
		if err := h.saveDefault(name); err != nil {
			slog.Error("failed to save default agent to config", "component", "handler", "error", err)
		} else {
			slog.Info("saved default agent to config", "component", "handler", "name", name)
		}
	}

	info := ag.Info()
	slog.Info("switched default agent", "component", "handler", "from", old, "to", name, "info", info)
	return fmt.Sprintf("switch to %s", name)
}

// resetDefaultSession resets the session for the given conversationID on the default agent.
func (h *Handler) resetDefaultSession(ctx context.Context, conversationID string) string {
	ag := h.getDefaultAgent()
	if ag == nil {
		return "No agent running."
	}
	name := ag.Info().Name
	sessionID, err := ag.ResetSession(ctx, conversationID)
	if err != nil {
		slog.Error("reset session failed", "component", "handler", "conversationID", conversationID, "error", err)
		return fmt.Sprintf("Failed to reset session: %v", err)
	}
	if sessionID != "" {
		return fmt.Sprintf("New %s session created\n%s", name, sessionID)
	}
	return fmt.Sprintf("New %s session created", name)
}
