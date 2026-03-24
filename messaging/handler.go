package messaging

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/danbao/ringclaw/agent"
	"github.com/danbao/ringclaw/ringcentral"
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
}

// NewHandler creates a new message handler.
func NewHandler(factory AgentFactory, saveDefault SaveDefaultFunc) *Handler {
	return &Handler{
		agents:      make(map[string]agent.Agent),
		factory:     factory,
		saveDefault: saveDefault,
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

	if h.defaultName == "" {
		return "agent: none (echo mode)"
	}

	ag, ok := h.agents[h.defaultName]
	if !ok {
		return fmt.Sprintf("agent: %s (not started)", h.defaultName)
	}

	info := ag.Info()
	return fmt.Sprintf("agent: %s\ntype: %s\nmodel: %s", h.defaultName, info.Type, info.Model)
}

func buildHelpText() string {
	return `Available commands:
/agentname - Switch default agent
/agentname message - Send message to a specific agent
/status - Show current agent info
/help - Show this help message

Aliases: /cc(claude) /cx(codex) /cs(cursor) /km(kimi) /gm(gemini) /oc(openclaw) /ocd(opencode)`
}

func wrapAnswer(text string) string {
	return "--------answer--------\n" + text + "\n---------end----------"
}
