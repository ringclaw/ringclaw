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
	"github.com/ringclaw/ringclaw/config"
	"github.com/ringclaw/ringclaw/internal/util"
	"github.com/ringclaw/ringclaw/ringcentral"
)

const maxSeenMsgs = 10000

// AgentFactory creates an agent by config name. Returns nil if the name is unknown.
type AgentFactory func(ctx context.Context, name string) agent.Agent

// SaveDefaultFunc persists the default agent name to config file.
type SaveDefaultFunc func(name string) error

// ReloadAgentsFunc re-detects agents and returns (newMetas, newAliases, added agent names).
type ReloadAgentsFunc func() ([]AgentMeta, map[string]string, []string)

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
	reloadAgents  ReloadAgentsFunc

	groupSummaryGroupID      string
	groupSummaryMessageLimit int

	// trustedSenders is the set of user IDs allowed to drive the agent.
	// Mirrors the Monitor's allowlist as a defense-in-depth check so callers
	// that bypass the WebSocket path (cron, /api/send, tests) cannot inject
	// posts from arbitrary CreatorIDs. Empty + allowAllSenders=false means
	// only the bot's own posts and configured cron jobs may dispatch.
	trustedSenders  map[string]bool
	allowAllSenders bool
}

// NewHandler creates a new message handler.
func NewHandler(factory AgentFactory, saveDefault SaveDefaultFunc, version string) *Handler {
	return &Handler{
		agents:          make(map[string]agent.Agent),
		factory:         factory,
		saveDefault:     saveDefault,
		version:         version,
		startTime:       time.Now(),
		trustedSenders:  make(map[string]bool),
		allowAllSenders: true, // legacy-compatible default; cmd/start.go flips this off
	}
}

// AddTrustedSender adds a user ID to the handler's defense-in-depth sender
// allowlist. Mirrors Monitor.AddTrustedSender; both layers must agree before
// a message is dispatched to an agent.
func (h *Handler) AddTrustedSender(userID string) {
	if userID == "" {
		return
	}
	h.mu.Lock()
	if h.trustedSenders == nil {
		h.trustedSenders = make(map[string]bool)
	}
	h.trustedSenders[userID] = true
	h.mu.Unlock()
}

// EnforceSenderAllowlist switches the handler into strict mode: only IDs on
// the trusted senders list may drive an agent. Should be called by production
// startup code after AddTrustedSender has populated the allowlist.
func (h *Handler) EnforceSenderAllowlist() {
	h.mu.Lock()
	h.allowAllSenders = false
	h.mu.Unlock()
}

// isTrustedSender reports whether a given creator ID may drive the agent.
// Returns true when allow-all mode is enabled, or when the ID is on the
// trusted senders allowlist. An empty ID is treated as untrusted.
func (h *Handler) isTrustedSender(creatorID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.allowAllSenders {
		return true
	}
	if creatorID == "" {
		return false
	}
	return h.trustedSenders[creatorID]
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

// SetReloadAgents sets the callback for /reload to re-detect agents.
func (h *Handler) SetReloadAgents(fn ReloadAgentsFunc) {
	h.reloadAgents = fn
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
	info := ag.Info()
	slog.Info("default agent ready", "component", "handler", "agent", name, "type", info.Type, "pid", info.PID)
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
	"ag":  "augment",
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

// stripForwardedPrefix removes the "XXX posted in ![:Team](ID)\n> " prefix
// from forwarded messages, returning just the quoted content.
func stripForwardedPrefix(text string) string {
	idx := strings.Index(text, " posted in ![:Team](")
	if idx < 0 {
		return text
	}
	// Find the end of the prefix line
	nl := strings.Index(text[idx:], "\n")
	if nl < 0 {
		return text
	}
	after := text[idx+nl+1:]
	// Strip leading "> " from each line (blockquote)
	var lines []string
	for _, line := range strings.Split(after, "\n") {
		line = strings.TrimPrefix(line, "> ")
		lines = append(lines, line)
	}
	result := strings.TrimSpace(strings.Join(lines, "\n"))
	if result == "" {
		return text
	}
	slog.Debug("stripped forwarded prefix", "component", "handler")
	return result
}

// HandleMessage processes a single incoming RingCentral post.
func (h *Handler) HandleMessage(ctx context.Context, client *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post) {
	text := strings.TrimSpace(post.Text)
	if text == "" {
		slog.Debug("received empty message, skipping", "component", "handler", "creatorID", post.CreatorID)
		return
	}

	// Strip bot mention prefix (e.g. "![:Person](12345) /help" -> "/help")
	if botID := client.OwnerID(); botID != "" {
		prefix := "![:Person](" + botID + ")"
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
			// Also strip leading comma/colon that users sometimes add after mention
			text = strings.TrimSpace(strings.TrimLeft(text, ",:"))
		}
	}

	// Strip forwarded message prefix: "XXX posted in ![:Team](ID)\n> content" → "content"
	text = stripForwardedPrefix(text)

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

	// Defense-in-depth sender allowlist. Monitor already filters at the
	// WebSocket layer; this re-check protects callers that bypass Monitor
	// (cron jobs, /api/send, tests) from spoofing CreatorID.
	if !h.isTrustedSender(post.CreatorID) {
		slog.Warn("dropping message from untrusted sender",
			"component", "handler", "creatorID", post.CreatorID, "chatID", chatID)
		return
	}

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
	if text == "/reload" {
		logSendError(SendTextReply(ctx, client, chatID, h.handleReload()))
		return
	}
	if text == "/info" || text == "/status" {
		cardJSON := h.buildStatusCard()
		if _, err := client.CreateAdaptiveCard(ctx, chatID, cardJSON); err != nil {
			slog.Error("failed to send status card, falling back to text", "component", "handler", "error", err)
			logSendError(SendTextReply(ctx, client, chatID, h.buildStatus()))
		}
		return
	} else if text == "/new" || text == "/clear" {
		logSendError(SendTextReply(ctx, client, chatID, h.resetDefaultSession(ctx, conversationIDForPost(client, post))))
		return
	} else if strings.HasPrefix(text, "/cwd") {
		logSendError(SendTextReply(ctx, client, chatID, h.handleCwd(text)))
		return
	} else if text == "/help" {
		cardJSON := buildHelpCard()
		if _, err := client.CreateAdaptiveCard(ctx, chatID, cardJSON); err != nil {
			slog.Error("failed to send help card, falling back to text", "component", "handler", "error", err)
			logSendError(SendTextReply(ctx, client, chatID, buildHelpText()))
		}
		return
	} else if strings.HasPrefix(text, "/cron") {
		if h.cronStore == nil {
			logSendError(SendTextReply(ctx, client, chatID, "Cron is not configured."))
			return
		}
		logSendError(SendTextReply(ctx, client, chatID, HandleCronCommand(h.cronStore, text, chatID)))
		return
	} else if strings.HasPrefix(text, "/chatinfo") {
		logSendError(SendTextReply(ctx, client, chatID, handleChatInfo(ctx, readClient, chatID, text)))
		return
	}

	// Explicit action commands: /task, /note, /event (use readClient for API access)
	if IsActionCommand(text) {
		logSendError(SendTextReply(ctx, client, chatID, HandleActionCommand(ctx, readClient, chatID, text)))
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
			logSendError(SendTextReply(ctx, client, chatID, h.switchDefault(ctx, agentNames[0])))
		} else if len(agentNames) == 1 && !h.isKnownAgent(agentNames[0]) {
			h.sendToDefaultAgent(ctx, client, readClient, post, text)
		} else {
			logSendError(SendTextReply(ctx, client, chatID, "Usage: specify one agent to switch, or add a message to broadcast"))
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

// dispatchToAgent handles the common pattern: placeholder → extract images → chat → reply with actions.
func (h *Handler) dispatchToAgent(ctx context.Context, client *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post, ag agent.Agent, message, placeholderID string) {
	conversationID := conversationIDForPost(client, post)
	images := extractImageAttachments(ctx, client, post)


	reply, err := h.chatWithAgentOrImages(ctx, ag, conversationID, message+ActionPrompt(), images)
	if err != nil {
		reply = agent.UserMessage(err)
	}

	h.sendReplyWithActions(ctx, client, readClient, post, reply, placeholderID)
}

// sendToDefaultAgent sends the message to the default agent and replies.
func (h *Handler) sendToDefaultAgent(ctx context.Context, client *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post, text string) {
	placeholderID, placeholderErr := SendTypingPlaceholder(ctx, client, post.GroupID)
	if placeholderErr != nil {
		slog.Error("failed to send typing placeholder", "component", "handler", "error", placeholderErr)
	}

	ag := h.getDefaultAgent()
	if ag == nil {
		slog.Warn("agent not ready, using echo mode", "component", "handler", "creatorID", post.CreatorID)
		reply := "[echo] " + text
		h.sendReplyWithActions(ctx, client, readClient, post, reply, placeholderID)
		return
	}

	h.dispatchToAgent(ctx, client, readClient, post, ag, text, placeholderID)
}

// sendToNamedAgent sends the message to a specific agent and replies.
func (h *Handler) sendToNamedAgent(ctx context.Context, client *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post, name, message string) {
	placeholderID, placeholderErr := SendTypingPlaceholder(ctx, client, post.GroupID)
	if placeholderErr != nil {
		slog.Error("failed to send typing placeholder", "component", "handler", "error", placeholderErr)
	}

	ag, agErr := h.getAgent(ctx, name)
	if agErr != nil {
		slog.Error("agent not available", "component", "handler", "agent", name, "error", agErr)
		logSendError(SendTextReply(ctx, client, post.GroupID, fmt.Sprintf("Agent %q is not available: %v", name, agErr)))
		return
	}

	h.dispatchToAgent(ctx, client, readClient, post, ag, message, placeholderID)
}

// broadcastToAgents sends the message to multiple agents in parallel.
func (h *Handler) broadcastToAgents(ctx context.Context, client *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post, names []string, message string) {
	conversationID := conversationIDForPost(client, post)
	images := extractImageAttachments(ctx, client, post)

	type result struct {
		name  string
		reply string
	}

	ch := make(chan result, len(names))
	for _, name := range names {
		go func(n string) {
			ag, err := h.getAgent(ctx, n)
			if err != nil {
				ch <- result{name: n, reply: agent.UserMessage(err)}
				return
			}
			reply, err := h.chatWithAgentOrImages(ctx, ag, conversationID, message+ActionPrompt(), images)
			if err != nil {
				ch <- result{name: n, reply: agent.UserMessage(err)}
				return
			}
			ch <- result{name: n, reply: reply}
		}(name)
	}

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

	// Mention the questioner in bot group chats so they get a notification
	if client.IsBot() && !client.IsBotDM(chatID) && post.CreatorID != "" {
		reply = fmt.Sprintf("![:Person](%s) %s", post.CreatorID, reply)
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
	slog.Info("dispatching to agent", "component", "handler", "agent", info.Name, "conversationID", userID)

	start := time.Now()
	reply, err := ag.Chat(ctx, userID, message)
	elapsed := time.Since(start)

	if err != nil {
		slog.Error("agent error", "component", "handler", "agent", info.Name, "elapsed", elapsed, "error", err)
		return "", err
	}

	slog.Info("agent replied", "component", "handler", "agent", info.Name, "elapsed", elapsed, "reply", util.Truncate(reply, 100))
	return reply, nil
}

// chatWithAgentOrImages dispatches to ChatWithImages if agent supports it, otherwise text-only.
func (h *Handler) chatWithAgentOrImages(ctx context.Context, ag agent.Agent, conversationID, message string, images []agent.ImageAttachment) (string, error) {
	if len(images) > 0 {
		if is, ok := ag.(agent.ImageSupporter); ok {
			info := ag.Info()
			slog.Info("dispatching to agent with images", "component", "handler", "agent", info.Name, "conversationID", conversationID, "images", len(images))
			start := time.Now()
			reply, err := is.ChatWithImages(ctx, conversationID, message, images)
			elapsed := time.Since(start)
			if err != nil {
				slog.Error("agent error", "component", "handler", "agent", info.Name, "elapsed", elapsed, "error", err)
				return "", err
			}
			slog.Info("agent replied", "component", "handler", "agent", info.Name, "elapsed", elapsed, "reply", util.Truncate(reply, 100))
			return reply, nil
		}
		message += fmt.Sprintf("\n\n[Note: %d image(s) were attached but this agent does not support image input.]", len(images))
	}
	return h.chatWithAgent(ctx, ag, conversationID, message)
}

const maxImages = 5

var imageMediaTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
	"image/jpg":  true,
}

func extractImageAttachments(ctx context.Context, client *ringcentral.Client, post ringcentral.Post) []agent.ImageAttachment {
	var images []agent.ImageAttachment
	for _, att := range post.Attachments {
		if len(images) >= maxImages {
			break
		}
		if att.ContentURI == "" {
			continue
		}
		mt := att.MediaType
		if mt == "" {
			mt = inferMediaType(att.Name)
		}
		if !imageMediaTypes[mt] {
			continue
		}
		data, detectedMT, err := client.DownloadAttachment(ctx, att.ContentURI)
		if err != nil {
			slog.Error("failed to download attachment", "component", "handler", "id", att.ID, "error", err)
			continue
		}
		if detectedMT != "" && !imageMediaTypes[detectedMT] {
			continue
		}
		if detectedMT != "" {
			mt = detectedMT
		}
		images = append(images, agent.ImageAttachment{
			Data:      data,
			MediaType: mt,
			Name:      att.Name,
		})
		slog.Info("downloaded image attachment", "component", "handler", "id", att.ID, "name", att.Name, "size", len(data))
	}
	return images
}

func inferMediaType(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	}
	return ""
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

	reply := fmt.Sprintf("switch to %s", name)

	// Warn if agent is running in CLI mode (no multi-turn context)
	if info.Type == "cli" {
		reply += " (CLI mode — no multi-turn context)"
		if hint := config.ACPInstallHint(name); hint != "" {
			reply += fmt.Sprintf("\nTip: install ACP adapter for session persistence:\n  %s\nThen use /reload to upgrade.", hint)
		}
	}

	return reply
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

// handleReload re-detects installed agents and updates the handler.
func (h *Handler) handleReload() string {
	if h.reloadAgents == nil {
		return "Reload is not available."
	}
	metas, aliases, added := h.reloadAgents()

	h.mu.Lock()
	h.agentMetas = metas
	h.customAliases = aliases
	h.mu.Unlock()

	if len(added) == 0 {
		return "Reloaded. No new agents detected."
	}
	return fmt.Sprintf("Reloaded. New agents detected: %s", strings.Join(added, ", "))
}
