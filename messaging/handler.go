package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"
)

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

// SetCustomAliases sets custom alias mappings from config.
func (h *Handler) SetCustomAliases(aliases map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.customAliases = aliases
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
func (h *Handler) HandleMessage(ctx context.Context, client *ringcentral.Client, post ringcentral.Post) {
	text := strings.TrimSpace(post.Text)
	if text == "" {
		slog.Debug("received empty message, skipping", "component", "handler", "creatorID", post.CreatorID)
		return
	}

	chatID := post.GroupID
	slog.Info("received message", "component", "handler", "creatorID", post.CreatorID, "chatID", chatID, "text", truncate(text, 80))

	// Built-in commands (no typing needed)
	if text == "/info" || text == "/status" {
		reply := h.buildStatus()
		if err := SendTextReply(ctx, client, chatID, reply); err != nil {
			slog.Error("failed to send reply", "component", "handler", "error", err)
		}
		return
	} else if text == "/new" || text == "/clear" {
		reply := h.resetDefaultSession(ctx, post.CreatorID)
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
	}

	// Summarize command
	if IsSummarizeCommand(text) {
		h.handleSummarize(ctx, client, post)
		return
	}

	// Action commands: /task, /note, /event
	if IsActionCommand(text) {
		reply := HandleActionCommand(ctx, client, chatID, text)
		if err := SendTextReply(ctx, client, chatID, reply); err != nil {
			slog.Error("failed to send action reply", "component", "handler", "error", err)
		}
		return
	}

	// Route: "/agent msg" or "/a /b msg" -> agent(s)
	agentNames, message := h.parseCommand(text)

	// No command prefix -> send to default agent
	if len(agentNames) == 0 {
		h.sendToDefaultAgent(ctx, client, post, text)
		return
	}

	// No message -> switch default agent (only first name)
	if message == "" {
		if len(agentNames) == 1 && h.isKnownAgent(agentNames[0]) {
			reply := h.switchDefault(ctx, agentNames[0])
			if err := SendTextReply(ctx, client, chatID, reply); err != nil {
				slog.Error("failed to send reply", "component", "handler", "error", err)
			}
		} else if len(agentNames) == 1 && !h.isKnownAgent(agentNames[0]) {
			h.sendToDefaultAgent(ctx, client, post, text)
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
		h.sendToDefaultAgent(ctx, client, post, text)
		return
	}

	if len(knownNames) == 1 {
		h.sendToNamedAgent(ctx, client, post, knownNames[0], message)
	} else {
		// Multi-agent broadcast: parallel dispatch
		h.broadcastToAgents(ctx, client, post, knownNames, message)
	}
}

// sendToDefaultAgent sends the message to the default agent and replies.
func (h *Handler) sendToDefaultAgent(ctx context.Context, client *ringcentral.Client, post ringcentral.Post, text string) {
	chatID := post.GroupID

	placeholderID, placeholderErr := SendTypingPlaceholder(ctx, client, chatID)
	if placeholderErr != nil {
		slog.Error("failed to send typing placeholder", "component", "handler", "error", placeholderErr)
	}

	ag := h.getDefaultAgent()
	var reply string
	if ag != nil {
		var err error
		reply, err = h.chatWithAgent(ctx, ag, post.CreatorID, text+ActionPrompt)
		if err != nil {
			reply = fmt.Sprintf("Error: %v", err)
		}
	} else {
		slog.Warn("agent not ready, using echo mode", "component", "handler", "creatorID", post.CreatorID)
		reply = "[echo] " + text
	}

	h.sendReplyWithActions(ctx, client, post, reply, placeholderID)
}

// sendToNamedAgent sends the message to a specific agent and replies.
func (h *Handler) sendToNamedAgent(ctx context.Context, client *ringcentral.Client, post ringcentral.Post, name, message string) {
	chatID := post.GroupID

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

	reply, err := h.chatWithAgent(ctx, ag, post.CreatorID, message+ActionPrompt)
	if err != nil {
		reply = fmt.Sprintf("Error: %v", err)
	}

	h.sendReplyWithActions(ctx, client, post, reply, placeholderID)
}

// broadcastToAgents sends the message to multiple agents in parallel.
// Each reply is sent as a separate message with the agent name prefix.
func (h *Handler) broadcastToAgents(ctx context.Context, client *ringcentral.Client, post ringcentral.Post, names []string, message string) {
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
			reply, err := h.chatWithAgent(ctx, ag, post.CreatorID, message+ActionPrompt)
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
		h.sendReplyWithActions(ctx, client, post, reply, "")
	}
}

// sendReplyWithActions processes action blocks and sends the final reply.
func (h *Handler) sendReplyWithActions(ctx context.Context, client *ringcentral.Client, post ringcentral.Post, reply, placeholderID string) {
	chatID := post.GroupID

	// Parse and execute any ACTION blocks from the agent's response
	cleanReply, actions := ParseAgentActions(reply)
	if len(actions) > 0 {
		reply = cleanReply
		results := ExecuteAgentActions(ctx, client, chatID, actions)
		if len(results) > 0 {
			defer func() {
				_ = SendTextReply(ctx, client, chatID, strings.Join(results, "\n"))
			}()
		}
	}

	// Extract image URLs from markdown
	imageURLs := ExtractImageURLs(reply)

	// Wrap reply with answer markers
	reply = wrapAnswer(reply)

	// Update the placeholder with the real reply, or send a new post
	if placeholderID != "" {
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

	slog.Info("agent replied", "component", "handler", "info", info, "elapsed", elapsed, "reply", truncate(reply, 100))
	return reply, nil
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

// resetDefaultSession resets the session for the given userID on the default agent.
func (h *Handler) resetDefaultSession(ctx context.Context, userID string) string {
	ag := h.getDefaultAgent()
	if ag == nil {
		return "No agent running."
	}
	name := ag.Info().Name
	sessionID, err := ag.ResetSession(ctx, userID)
	if err != nil {
		slog.Error("reset session failed", "component", "handler", "userID", userID, "error", err)
		return fmt.Sprintf("Failed to reset session: %v", err)
	}
	if sessionID != "" {
		return fmt.Sprintf("New %s session created\n%s", name, sessionID)
	}
	return fmt.Sprintf("New %s session created", name)
}

// handleCwd handles the /cwd command. Updates the working directory for all running agents.
//
// Ported from github.com/fastclaw-ai/weclaw commits 2b24d5d + 6df63a9.
func (h *Handler) handleCwd(text string) string {
	arg := strings.TrimSpace(strings.TrimPrefix(text, "/cwd"))
	if arg == "" {
		ag := h.getDefaultAgent()
		if ag == nil {
			return "No agent running."
		}
		info := ag.Info()
		return fmt.Sprintf("cwd: (check agent config)\nagent: %s", info.Name)
	}

	// Expand ~ to home directory (fix: use arg[2:] not arg[1:] for ~/path)
	if arg == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			arg = home
		}
	} else if strings.HasPrefix(arg, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			arg = filepath.Join(home, arg[2:])
		}
	}

	absPath, err := filepath.Abs(arg)
	if err != nil {
		return fmt.Sprintf("Invalid path: %v", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("Path not found: %s", absPath)
	}
	if !info.IsDir() {
		return fmt.Sprintf("Not a directory: %s", absPath)
	}

	h.mu.RLock()
	agents := make(map[string]agent.Agent, len(h.agents))
	for name, ag := range h.agents {
		agents[name] = ag
	}
	h.mu.RUnlock()

	for name, ag := range agents {
		ag.SetCwd(absPath)
		slog.Info("updated cwd for agent", "component", "handler", "agent", name, "cwd", absPath)
	}

	return fmt.Sprintf("cwd: %s", absPath)
}

func (h *Handler) buildStatus() string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var b strings.Builder

	// System info
	b.WriteString(fmt.Sprintf("ringclaw %s (%s/%s)\n", h.version, runtime.GOOS, runtime.GOARCH))
	b.WriteString(fmt.Sprintf("uptime: %s\n", formatDuration(time.Since(h.startTime))))
	b.WriteString(fmt.Sprintf("go: %s\n", runtime.Version()))
	b.WriteString("\n")

	// Default agent
	if h.defaultName == "" {
		b.WriteString("default agent: none (echo mode)\n")
	} else if ag, ok := h.agents[h.defaultName]; !ok {
		b.WriteString(fmt.Sprintf("default agent: %s (not started)\n", h.defaultName))
	} else {
		info := ag.Info()
		b.WriteString(fmt.Sprintf("default agent: %s\n", h.defaultName))
		b.WriteString(fmt.Sprintf("  type: %s\n", info.Type))
		if info.Model != "" {
			b.WriteString(fmt.Sprintf("  model: %s\n", info.Model))
		}
		if info.PID > 0 {
			b.WriteString(fmt.Sprintf("  pid: %d\n", info.PID))
		}
	}

	// Active sessions
	activeSessions := 0
	for range h.agents {
		activeSessions++
	}
	b.WriteString(fmt.Sprintf("\nactive sessions: %d\n", activeSessions))

	// All available agents
	if len(h.agentMetas) > 0 {
		b.WriteString("\navailable agents:\n")
		for _, m := range h.agentMetas {
			marker := " "
			if m.Name == h.defaultName {
				marker = "*"
			}
			model := m.Model
			if model == "" {
				model = "-"
			}
			b.WriteString(fmt.Sprintf("  %s %-12s  type=%-4s  model=%s\n", marker, m.Name, m.Type, model))
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, mins, secs)
	}
	if mins > 0 {
		return fmt.Sprintf("%dm %ds", mins, secs)
	}
	return fmt.Sprintf("%ds", secs)
}

func buildHelpText() string {
	return `Available commands:
/agent - Switch default agent
/agent msg - Send to a specific agent
/a /b msg - Broadcast to multiple agents
/new or /clear - Start a new session
/cwd /path - Switch workspace directory
/info - Show current agent info
/help - Show this help message

/task list|create|get|update|delete|complete
/note list|create|get|update|delete
/event list|create|get|update|delete
/card get|delete

Aliases: /cc(claude) /cx(codex) /cs(cursor) /km(kimi) /gm(gemini) /oc(openclaw) /ocd(opencode) /pi(pi) /cp(copilot) /dr(droid) /if(iflow) /kr(kiro) /qw(qwen)`
}

func wrapAnswer(text string) string {
	return "--------answer--------\n" + text + "\n---------end----------"
}

func (h *Handler) handleSummarize(ctx context.Context, client *ringcentral.Client, post ringcentral.Post) {
	chatID := post.GroupID
	text := strings.TrimSpace(post.Text)

	placeholderID, placeholderErr := SendTypingPlaceholder(ctx, client, chatID)
	if placeholderErr != nil {
		slog.Error("failed to send typing placeholder", "component", "handler", "error", placeholderErr)
	}

	sendReply := func(reply string) {
		if placeholderID != "" {
			if err := UpdatePostText(ctx, client, chatID, placeholderID, reply); err != nil {
				slog.Error("failed to update placeholder", "component", "handler", "error", err)
				_ = SendTextReply(ctx, client, chatID, reply)
			}
		} else {
			_ = SendTextReply(ctx, client, chatID, reply)
		}
	}

	// Resolve target chat
	req, err := ResolveChatTarget(ctx, client, text, post.Mentions)
	if err != nil {
		sendReply(fmt.Sprintf("Error: %v", err))
		return
	}

	slog.Info("summarize target chat", "component", "summarize", "chatName", req.ChatName, "chatID", req.ChatID, "from", req.TimeFrom.Format(time.RFC3339))

	// Build prompt from chat messages
	prompt, err := BuildSummaryPrompt(ctx, client, req)
	if err != nil {
		sendReply(fmt.Sprintf("Error: %v", err))
		return
	}

	// Send to default agent
	ag := h.getDefaultAgent()
	if ag == nil {
		sendReply("Error: no agent available for summarization")
		return
	}

	reply, err := h.chatWithAgent(ctx, ag, post.CreatorID, prompt)
	if err != nil {
		sendReply(fmt.Sprintf("Error: %v", err))
		return
	}

	// Parse and execute any ACTION blocks from the agent's response
	cleanReply, actions := ParseAgentActions(reply)
	sendReply(wrapAnswer(cleanReply))

	if len(actions) > 0 {
		targetChatID := req.ChatID
		results := ExecuteAgentActions(ctx, client, targetChatID, actions)
		if len(results) > 0 {
			_ = SendTextReply(ctx, client, chatID, strings.Join(results, "\n"))
		}
	}
}
