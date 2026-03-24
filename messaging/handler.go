package messaging

import (
	"context"
	"fmt"
	"log"
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
	mu          sync.RWMutex
	defaultName string
	agents      map[string]agent.Agent // name -> running agent
	agentMetas  []AgentMeta            // all configured agents (for /status)
	factory     AgentFactory
	saveDefault SaveDefaultFunc
	version     string
	startTime   time.Time
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
	log.Printf("[handler] default agent ready: %s (%s)", name, ag.Info())
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

	log.Printf("[handler] starting agent %q on demand...", name)
	ag = h.factory(ctx, name)
	if ag == nil {
		return nil, fmt.Errorf("agent %q not available", name)
	}

	h.agents[name] = ag
	log.Printf("[handler] agent started on demand: %s (%s)", name, ag.Info())
	return ag, nil
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
func resolveAlias(name string) string {
	if full, ok := agentAliases[name]; ok {
		return full
	}
	return name
}

// parseCommand checks if text starts with "/agentname " and returns (agentName, actualMessage).
func parseCommand(text string) (string, string) {
	if !strings.HasPrefix(text, "/") {
		return "", text
	}

	rest := text[1:]
	idx := strings.IndexByte(rest, ' ')
	if idx <= 0 {
		return resolveAlias(rest), ""
	}

	name := resolveAlias(rest[:idx])
	return name, strings.TrimSpace(rest[idx+1:])
}

// HandleMessage processes a single incoming RingCentral post.
func (h *Handler) HandleMessage(ctx context.Context, client *ringcentral.Client, post ringcentral.Post) {
	text := strings.TrimSpace(post.Text)
	if text == "" {
		log.Printf("[handler] received empty message from %s, skipping", post.CreatorID)
		return
	}

	chatID := post.GroupID
	log.Printf("[handler] received from %s in %s: %q", post.CreatorID, chatID, truncate(text, 80))

	// Built-in commands (no typing needed)
	if text == "/status" {
		reply := h.buildStatus()
		if err := SendTextReply(ctx, client, chatID, reply); err != nil {
			log.Printf("[handler] failed to send reply: %v", err)
		}
		return
	} else if text == "/help" {
		reply := buildHelpText()
		if err := SendTextReply(ctx, client, chatID, reply); err != nil {
			log.Printf("[handler] failed to send reply: %v", err)
		}
		return
	}

	// Summarize command
	if IsSummarizeCommand(text) {
		h.handleSummarize(ctx, client, post)
		return
	}

	// Route: "/agentname message" -> specific agent, otherwise -> default
	agentName, message := parseCommand(text)

	var reply string
	var err error
	needsAgent := false

	if agentName != "" {
		if message == "" {
			reply = h.switchDefault(ctx, agentName)
		} else {
			needsAgent = true
		}
	} else {
		needsAgent = true
	}

	if needsAgent {
		// Send "Thinking..." placeholder and get postID for later update
		placeholderID, placeholderErr := SendTypingPlaceholder(ctx, client, chatID)
		if placeholderErr != nil {
			log.Printf("[handler] failed to send typing placeholder: %v", placeholderErr)
		}

		if agentName != "" {
			ag, agErr := h.getAgent(ctx, agentName)
			if agErr != nil {
				log.Printf("[handler] agent %q not available: %v", agentName, agErr)
				reply = fmt.Sprintf("Agent %q is not available: %v", agentName, agErr)
			} else {
				reply, err = h.chatWithAgent(ctx, ag, post.CreatorID, message)
			}
		} else {
			ag := h.getDefaultAgent()
			if ag != nil {
				reply, err = h.chatWithAgent(ctx, ag, post.CreatorID, text)
			} else {
				log.Printf("[handler] agent not ready, using echo mode for %s", post.CreatorID)
				reply = "[echo] " + text
			}
		}

		if err != nil {
			reply = fmt.Sprintf("Error: %v", err)
		}

		// Extract image URLs from markdown
		imageURLs := ExtractImageURLs(reply)

		// Wrap reply with answer markers
		reply = wrapAnswer(reply)

		// Update the placeholder with the real reply, or send a new post if placeholder failed
		if placeholderID != "" {
			if updateErr := UpdatePostText(ctx, client, chatID, placeholderID, reply); updateErr != nil {
				log.Printf("[handler] failed to update placeholder, sending new post: %v", updateErr)
				if sendErr := SendTextReply(ctx, client, chatID, reply); sendErr != nil {
					log.Printf("[handler] failed to send reply: %v", sendErr)
				}
			}
		} else {
			if sendErr := SendTextReply(ctx, client, chatID, reply); sendErr != nil {
				log.Printf("[handler] failed to send reply: %v", sendErr)
			}
		}

		// Send extracted images as separate file uploads
		for _, imgURL := range imageURLs {
			if mediaErr := SendMediaFromURL(ctx, client, chatID, imgURL); mediaErr != nil {
				log.Printf("[handler] failed to send image: %v", mediaErr)
			}
		}
		return
	}

	if err != nil {
		reply = fmt.Sprintf("Error: %v", err)
	}

	if reply != "" {
		if sendErr := SendTextReply(ctx, client, chatID, reply); sendErr != nil {
			log.Printf("[handler] failed to send reply: %v", sendErr)
		}
	}
}

// chatWithAgent sends a message to an agent and returns the reply.
func (h *Handler) chatWithAgent(ctx context.Context, ag agent.Agent, userID, message string) (string, error) {
	info := ag.Info()
	log.Printf("[handler] dispatching to agent (%s) for %s", info, userID)

	start := time.Now()
	reply, err := ag.Chat(ctx, userID, message)
	elapsed := time.Since(start)

	if err != nil {
		log.Printf("[handler] agent error (%s, elapsed=%s): %v", info, elapsed, err)
		return "", err
	}

	log.Printf("[handler] agent replied (%s, elapsed=%s): %q", info, elapsed, truncate(reply, 100))
	return reply, nil
}

// switchDefault switches the default agent.
func (h *Handler) switchDefault(ctx context.Context, name string) string {
	ag, err := h.getAgent(ctx, name)
	if err != nil {
		log.Printf("[handler] failed to switch default to %q: %v", name, err)
		return fmt.Sprintf("Failed to switch to %q: %v", name, err)
	}

	h.mu.Lock()
	old := h.defaultName
	h.defaultName = name
	h.agents[name] = ag
	h.mu.Unlock()

	if h.saveDefault != nil {
		if err := h.saveDefault(name); err != nil {
			log.Printf("[handler] failed to save default agent to config: %v", err)
		} else {
			log.Printf("[handler] saved default agent %q to config", name)
		}
	}

	info := ag.Info()
	log.Printf("[handler] switched default agent: %s -> %s (%s)", old, name, info)
	return fmt.Sprintf("switch to %s", name)
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
/agentname - Switch default agent
/agentname message - Send message to a specific agent
/status - Show current agent info
/help - Show this help message

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
		log.Printf("[handler] failed to send typing placeholder: %v", placeholderErr)
	}

	sendReply := func(reply string) {
		if placeholderID != "" {
			if err := UpdatePostText(ctx, client, chatID, placeholderID, reply); err != nil {
				log.Printf("[handler] failed to update placeholder: %v", err)
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

	log.Printf("[summarize] target chat: %s (%s), from: %s", req.ChatName, req.ChatID, req.TimeFrom.Format(time.RFC3339))

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

	sendReply(wrapAnswer(reply))
}
