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
	"github.com/ringclaw/ringclaw/messaging/oob"
	"github.com/ringclaw/ringclaw/messaging/persona"
	"github.com/ringclaw/ringclaw/ringcentral"
)

const maxSeenMsgs = 10000

var defaultEnabledCapabilities = []string{"message", "summary", "video", "phone", "call_log", "sms"}

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
	enabledCapabilities      map[string]bool

	// trustedSenders is the set of user IDs allowed to drive the agent.
	// Mirrors the Monitor's allowlist as a defense-in-depth check so callers
	// that bypass the WebSocket path (cron, /api/send, tests) cannot inject
	// posts from arbitrary CreatorIDs. Empty + allowAllSenders=false means
	// only the bot's own posts and configured cron jobs may dispatch.
	trustedSenders  map[string]bool
	allowAllSenders bool

	// chatUserAllow mirrors the Monitor's per-chat allowlist for
	// defense-in-depth. Maps chat ID -> set of numeric user IDs that
	// may drive the bot in that specific chat. Layered ON TOP of
	// trustedSenders: a sender is considered trusted when they
	// appear in either set. Populated by the authorize-mention OOB
	// flow on approval and by config.json at startup.
	chatUserAllow map[string]map[string]bool

	// authorizeMonitor is the Monitor reference used by the
	// authorize-mention OOB flow to push approved (chat, user)
	// grants back into the WebSocket allowlist. Nil when the
	// authorize-mention feature is disabled.
	authorizeMonitor authorizeMonitorIface

	// authorizePersist is the persistence callback that writes a
	// newly approved (chat, identifier) grant to config.json.
	// identifier is the human-friendly form (email preferred,
	// numeric ID fallback) — see handler_authorize.go.
	authorizePersist PersistAuthorizeFunc

	// pendingAuthorize is the dedupe set for outstanding
	// authorize-mention challenges. Keys are "chatID|userID".
	pendingAuthorize map[string]bool

	// authorizeMeta caches per-challenge metadata (display name,
	// email, chat name) captured when the prompt is built so the
	// approval handler can persist the human-friendly identifier
	// without a second directory lookup that may transiently fail.
	authorizeMeta map[string]authorizeMeta

	// authorizeCooldown silences a (chat, user) pair for
	// authorizeCooldownTTL after a deny / expire so a hostile or
	// noisy non-trusted sender cannot keep pushing /approval prompts
	// to the owner DM by repeatedly @mentioning the bot. Keys mirror
	// pendingAuthorize ("chatID|userID"). Approve never writes here
	// (the user becomes trusted via chat_user_allow and won't reach
	// AuthorizeMention again); expire / deny do. In-memory only —
	// process restart resets the cooldown, which is acceptable
	// because restarts are rare and operator-driven.
	authorizeCooldown map[string]time.Time

	// oobManager and ownerDMChatID power the Phase 2b /approval flow.
	// When oobManager is nil, /full-access is disabled and cross-chat
	// actions skip the owner-DM notice (falling back to the Phase 1
	// warn-log for cross-chat and env-only full-access
	// acknowledgement). ownerDMChatID is the chat ID where /approval
	// prompts and cross-chat notices are delivered; empty disables OOB
	// even when the manager is configured.
	oobManager    *oob.Manager
	ownerDMChatID string

	// persona is the optional SOUL + layered MEMORY loader. When
	// non-nil and Enabled, its Build() output is prepended to every
	// prompt dispatched through the WebSocket message path so
	// switching agents or resetting sessions does not wipe the
	// operator's persona / memory context. A nil loader is safe to
	// carry (Enabled reports false).
	personaLoader *persona.Loader

	// conversationNamespace prefixes agent conversation IDs so multiple
	// long-lived bot pods can share an external AI gateway without colliding
	// on the same RingCentral chat/user IDs.
	conversationNamespace string
}

// NewHandler creates a new message handler.
func NewHandler(factory AgentFactory, saveDefault SaveDefaultFunc, version string) *Handler {
	return &Handler{
		agents:            make(map[string]agent.Agent),
		factory:           factory,
		saveDefault:       saveDefault,
		version:           version,
		startTime:         time.Now(),
		trustedSenders:    make(map[string]bool),
		allowAllSenders:   true, // legacy-compatible default; cmd/start.go flips this off
		chatUserAllow:     make(map[string]map[string]bool),
		pendingAuthorize:  make(map[string]bool),
		authorizeMeta:     make(map[string]authorizeMeta),
		authorizeCooldown: make(map[string]time.Time),
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

// SetOOBManager installs the Phase 2 out-of-band approval manager and the
// chat ID that should receive approval cards (the Private App owner's bot
// DM). Pass a nil manager to disable OOB and revert to Phase 1 semantics.
//
// ownerDMChatID is the bot's own DM chat with the trusted machine owner
// (typically *ringcentral.Client.IsBotDM(...) returns true for it). When
// empty, OOB call sites refuse to gate actions.
func (h *Handler) SetOOBManager(mgr *oob.Manager, ownerDMChatID string) {
	h.mu.Lock()
	h.oobManager = mgr
	h.ownerDMChatID = ownerDMChatID
	h.mu.Unlock()
}

// OOBManager returns the configured manager (or nil). Used by callers
// (e.g. the /full-access slash command, /info status card) that need to
// inspect or drive OOB state.
func (h *Handler) OOBManager() *oob.Manager {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.oobManager
}

// OwnerDMChatID returns the chat ID where OOB approval cards should be
// posted; empty when OOB is not configured.
func (h *Handler) OwnerDMChatID() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.ownerDMChatID
}

// SetPersonaLoader installs the persona + memory banner loader. Pass
// nil to disable persona injection. See messaging/persona for the
// loader's construction from config.
func (h *Handler) SetPersonaLoader(l *persona.Loader) {
	h.mu.Lock()
	h.personaLoader = l
	h.mu.Unlock()
}

// SetConversationNamespace installs a stable namespace for all agent
// conversation IDs generated by this handler. Empty preserves legacy IDs.
func (h *Handler) SetConversationNamespace(namespace string) {
	h.mu.Lock()
	h.conversationNamespace = strings.TrimSpace(namespace)
	h.mu.Unlock()
}

// SetCapabilities installs the AVA/RingClaw runtime capabilities selected at
// onboarding. Video, phone, and SMS are product-default capabilities; scopes
// and RingCentral permissions still decide whether the backing API call
// succeeds.
func (h *Handler) SetCapabilities(capabilities []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	enabled := make(map[string]bool, len(capabilities)+len(defaultEnabledCapabilities))
	for _, capability := range defaultEnabledCapabilities {
		enabled[capability] = true
	}
	for _, capability := range capabilities {
		capability = strings.ToLower(strings.TrimSpace(capability))
		if capability != "" {
			enabled[capability] = true
		}
	}
	h.enabledCapabilities = enabled
}

func (h *Handler) isCapabilityEnabled(capability string) bool {
	capability = strings.ToLower(strings.TrimSpace(capability))
	if capability == "" || capability == "message" || capability == "summary" {
		return true
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.enabledCapabilities == nil {
		return true
	}
	if capability == "call_log" {
		return h.enabledCapabilities["call_log"] || h.enabledCapabilities["phone"]
	}
	return h.enabledCapabilities[capability]
}

func (h *Handler) actionCapabilities() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.enabledCapabilities == nil {
		return nil
	}
	out := make([]string, 0, len(h.enabledCapabilities))
	for capability := range h.enabledCapabilities {
		out = append(out, capability)
	}
	return out
}

// PersonaLoader returns the installed loader (or nil). Used by the
// /mem add|show|del and /persona slash commands.
func (h *Handler) PersonaLoader() *persona.Loader {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.personaLoader
}

// buildPersonaBanner returns the context banner to prepend to a
// prompt, or the empty string when persona is disabled. Centralized
// here so both dispatchToAgent and broadcastToAgents share exactly
// the same injection logic.
func (h *Handler) buildPersonaBanner(ctx context.Context, client *ringcentral.Client, post ringcentral.Post) string {
	loader := h.PersonaLoader()
	if !loader.Enabled() {
		return ""
	}
	isDM := client != nil && client.IsBotDM(post.GroupID)
	return loader.Build(ctx, post.GroupID, post.CreatorID, isDM)
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

// AddChatUserAllow grants a numeric user ID per-chat trust on the
// handler's defense-in-depth allowlist. Used by the authorize-mention
// OOB flow on approval and by start.go when seeding from
// config.json's ringcentral.chat_user_allow.
func (h *Handler) AddChatUserAllow(chatID, userID string) {
	chatID = strings.TrimSpace(chatID)
	userID = strings.TrimSpace(userID)
	if chatID == "" || userID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.chatUserAllow == nil {
		h.chatUserAllow = make(map[string]map[string]bool)
	}
	set, ok := h.chatUserAllow[chatID]
	if !ok {
		set = make(map[string]bool)
		h.chatUserAllow[chatID] = set
	}
	set[userID] = true
}

// isChatUserAllowed reports whether (chatID, userID) is on the
// per-chat allowlist.
func (h *Handler) isChatUserAllowed(chatID, userID string) bool {
	if chatID == "" || userID == "" {
		return false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	set, ok := h.chatUserAllow[chatID]
	if !ok {
		return false
	}
	return set[userID]
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
		// Voice messages arrive as posts with empty text and an audio attachment.
		// Proceed with dispatch; dispatchToAgent will pass the audio to the agent.
		if !hasAudioAttachments(post) {
			slog.Debug("received empty message, skipping", "component", "handler", "creatorID", post.CreatorID)
			return
		}
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
	// (cron jobs, /api/send, tests) from spoofing CreatorID. The
	// per-chat allowlist (chat_user_allow + authorize-mention OOB
	// approvals) is layered on top of the global allowlist.
	if !h.isTrustedSender(post.CreatorID) && !h.isChatUserAllowed(chatID, post.CreatorID) {
		slog.Warn("dropping message from untrusted sender",
			"component", "handler", "creatorID", post.CreatorID, "chatID", chatID)
		return
	}

	// Phase 2b OOB approval interception. `/approval <id>` and
	// `/approval deny <id>` replies are typed back into the bot DM (the
	// same chat the prompt was posted to). When the message matches a
	// recognized approval shape we route it to the OOB manager and
	// short-circuit normal agent dispatch so the slash command is never
	// forwarded to the AI agent.
	if h.routeOOBApprovalReply(ctx, client, chatID, post.CreatorID, text) {
		return
	}

	// Privileged-command owner gate.
	//
	// Layer 1 of the permission matrix. Two cases:
	//
	//  1. Bot group chat: always enforce — non-owner cannot run
	//     privileged commands (historical behavior).
	//  2. Bot DM: enforce ONLY when a Private App is configured, so
	//     `readClient != client`. In that case `readClient.OwnerID()`
	//     names the true machine owner and non-owner trusted senders
	//     (e.g. a teammate also listed in source_user_ids) are blocked.
	//     Without a Private App, RingClaw has no reliable owner ID
	//     (readClient == bot client, whose OwnerID is the bot itself),
	//     so the DM path falls back to "every trusted sender is
	//     trusted" — see the Permission Matrix "DM is the trust
	//     boundary" warning in docs/security/index.md.
	isBotGroup := client.IsBot() && !client.IsBotDM(chatID)
	hasPrivateApp := readClient != nil && readClient != client
	if isPrivilegedCommand(text) {
		switch {
		case isBotGroup:
			if post.CreatorID != readClient.OwnerID() {
				slog.Info("blocked privileged command from non-owner (group)",
					"component", "handler", "creatorID", post.CreatorID, "command", util.Truncate(text, 30))
				logSendError(SendTextReply(ctx, client, chatID, "Only the bot owner can use this command in group chats."))
				return
			}
		case hasPrivateApp:
			if post.CreatorID != readClient.OwnerID() {
				slog.Info("blocked privileged command from non-owner (DM + PrivateApp)",
					"component", "handler", "creatorID", post.CreatorID, "command", util.Truncate(text, 30))
				logSendError(SendTextReply(ctx, client, chatID, "Only the Private App owner can use this privileged command."))
				return
			}
		}
		// No Private App + DM: cannot identify an owner; fall through.
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
		logSendError(SendTextReply(ctx, client, chatID, h.resetDefaultSession(ctx, h.conversationIDForPost(client, post))))
		return
	} else if strings.HasPrefix(text, "/cwd") {
		logSendError(SendTextReply(ctx, client, chatID, h.handleCwd(text)))
		return
	} else if IsFullAccessCommand(text) {
		h.handleFullAccess(ctx, client, readClient, chatID, post.CreatorID, text)
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
	} else if IsMemCommand(text) {
		isDM := client != nil && client.IsBotDM(chatID)
		logSendError(SendTextReply(ctx, client, chatID, h.handleMemCommand(text, chatID, post.CreatorID, isDM)))
		return
	} else if IsPersonaCommand(text) {
		logSendError(SendTextReply(ctx, client, chatID, h.handlePersonaCommand()))
		return
	}

	// Explicit action commands: /task, /note, /event (use readClient for API access)
	if IsActionCommand(text) {
		if capability := actionCommandCapability(text); capability != "" && !h.isCapabilityEnabled(capability) {
			logSendError(SendTextReply(ctx, client, chatID, capabilityDisabledMessage(capability)))
			return
		}
		logSendError(SendTextReply(ctx, client, chatID, HandleActionCommandWithRequester(ctx, readClient, chatID, text, post.CreatorID)))
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

// routeOOBApprovalReply is the Phase 2b hook that consumes `/approval`
// replies in the owner's bot DM before they reach the agent. Returns
// true when the message was handled (caller must short-circuit).
//
// In the bot DM the reply is passed to the OOB manager. Outside the
// bot DM (group chats, other DMs) a recognizable `/approval ...`
// shape is NOT dispatched to the agent as text — instead we post a
// short explanation so the operator gets a clear signal that they
// used the command in the wrong place, rather than seeing the AI
// answer a confused "what is /approval abc123?" prompt.
func (h *Handler) routeOOBApprovalReply(ctx context.Context, client *ringcentral.Client, chatID, senderID, text string) bool {
	mgr := h.OOBManager()
	if mgr == nil {
		return false
	}
	if client.IsBotDM(chatID) {
		return mgr.HandleApprovalReply(ctx, newOOBClient(client), chatID, senderID, text)
	}
	// Outside the owner DM, intercept recognizable /approval shapes
	// with an explicit refusal so they neither reach the OOB manager
	// nor leak into the default agent prompt.
	if reply := oob.ParseApprovalReply(text); reply.Kind != oob.ReplyNone {
		slog.Info("refused /approval outside bot DM", "component", "handler", "chatID", chatID, "senderID", senderID)
		logSendError(SendTextReply(ctx, client, chatID, "`/approval` is only recognized in the bot DM with the owner."))
		return true
	}
	return false
}

// conversationIDForPost returns the session key used to address a single
// agent conversation. The key MUST be unique per (chatID, creatorID) pair
// so that:
//
//  1. Different users in the same group chat get isolated agent contexts
//     (no piggybacking on another user's prior session). This is the
//     mitigation referenced in security review Finding #4.
//  2. The same user in different chats does not leak conversation history
//     across chats.
//  3. Bot DMs and group chats live in distinct namespaces so renaming a
//     chat ID can never collide with an existing DM session.
//
// Any caller building an ad-hoc conversationID must preserve these
// invariants; in particular, do not reduce the key to just the chat or
// just the user.
func conversationIDForPost(client *ringcentral.Client, post ringcentral.Post) string {
	chatID := strings.TrimSpace(post.GroupID)
	creatorID := strings.TrimSpace(post.CreatorID)
	if client != nil && client.IsBotDM(chatID) {
		return fmt.Sprintf("rc:dm:%s:%s", chatID, creatorID)
	}
	return fmt.Sprintf("rc:chat:%s:user:%s", chatID, creatorID)
}

func (h *Handler) conversationIDForPost(client *ringcentral.Client, post ringcentral.Post) string {
	base := conversationIDForPost(client, post)
	h.mu.RLock()
	namespace := h.conversationNamespace
	h.mu.RUnlock()
	if namespace == "" {
		return base
	}
	return "bot:" + sanitizeConversationNamespace(namespace) + ":" + base
}

func sanitizeConversationNamespace(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	namespace = strings.ReplaceAll(namespace, ":", "_")
	namespace = strings.ReplaceAll(namespace, " ", "_")
	return namespace
}

// dispatchToAgent handles the common pattern: placeholder → extract attachments → chat → reply with actions.
func (h *Handler) dispatchToAgent(ctx context.Context, client *ringcentral.Client, readClient *ringcentral.Client, post ringcentral.Post, ag agent.Agent, message, placeholderID string) {
	conversationID := h.conversationIDForPost(client, post)

	// v0.4.3: tag the request with the sender's Origin so the agent
	// layer can apply the non-owner restricted-mode + fail-closed
	// fs/terminal gate.
	ctx = h.withOriginForPost(ctx, client, post)

	// Prepend the persona + memory banner (empty string when persona
	// is disabled or all sources are blank). This keeps the operator's
	// SOUL and layered memory visible to every agent regardless of
	// which one is currently default.
	prompt := h.buildPersonaBanner(ctx, client, post) + message + ActionPrompt()

	audio := extractAudioAttachments(ctx, client, post)
	var images []agent.ImageAttachment
	if len(audio) == 0 {
		images = extractImageAttachments(ctx, client, post)
	}
	reply, err := h.chatWithAttachments(ctx, ag, conversationID, prompt, images, audio)
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
	conversationID := h.conversationIDForPost(client, post)

	// v0.4.3: tag the broadcast ctx with Origin so every fan-out
	// agent invocation honors the non-owner restricted-mode +
	// fail-closed fs/terminal gate.
	ctx = h.withOriginForPost(ctx, client, post)

	// Extract attachments once; audio takes priority over images (same as dispatchToAgent).
	audio := extractAudioAttachments(ctx, client, post)
	var images []agent.ImageAttachment
	if len(audio) == 0 {
		images = extractImageAttachments(ctx, client, post)
	}

	// Compute the persona banner once outside the fan-out so all
	// broadcast targets see identical context. Empty when persona is
	// disabled.
	prompt := h.buildPersonaBanner(ctx, client, post) + message + ActionPrompt()

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
			reply, err := h.chatWithAttachments(ctx, ag, conversationID, prompt, images, audio)
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

	// Parse and execute any ACTION blocks from the agent's response.
	// originIsOwner mirrors the trusted-senders allowlist so non-owner posts
	// cannot pivot the agent's reply into a different chat (Finding #5).
	cleanReply, actions := ParseAgentActions(reply)
	if len(actions) > 0 {
		reply = cleanReply
		ownerID := ""
		if actionClient != nil {
			ownerID = actionClient.OwnerID()
		}
		results := ExecuteAgentActions(ctx, client, actionClient, chatID, actions, ActionContext{
			OriginIsOwner: h.isTrustedSender(post.CreatorID),
			OOB:           h.OOBManager(),
			OwnerDMChat:   h.OwnerDMChatID(),
			RequesterID:   post.CreatorID,
			OwnerID:       ownerID,
			Capabilities:  h.actionCapabilities(),
		})
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

	// Update the placeholder with the real reply, or send a new post.
	// Empty reply (e.g. agent response was 100% ACTION blocks) deletes
	// the placeholder so no phantom empty post remains.
	FinalizeReply(ctx, client, chatID, placeholderID, reply, "handler")

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

// dispatchTypedMedia calls fn with timing/logging, returning its result.
// mediaKind is used in log messages (e.g. "images", "audio").
func dispatchTypedMedia(ag agent.Agent, conversationID, mediaKind string, count int, fn func() (string, error)) (string, error) {
	info := ag.Info()
	slog.Info("dispatching to agent with "+mediaKind, "component", "handler", "agent", info.Name, "conversationID", conversationID, mediaKind, count)
	start := time.Now()
	reply, err := fn()
	elapsed := time.Since(start)
	if err != nil {
		slog.Error("agent error", "component", "handler", "agent", info.Name, "elapsed", elapsed, "error", err)
		return "", err
	}
	slog.Info("agent replied", "component", "handler", "agent", info.Name, "elapsed", elapsed, "reply", util.Truncate(reply, 100))
	return reply, nil
}

// agentSupportsMedia consults the optional MediaCapable interface to
// decide whether the agent should receive prompts of the given media
// kind at runtime. Agents that don't implement MediaCapable fall back
// to "assume supported" so existing mocks and non-ACP agents keep
// working unchanged.
func agentSupportsMedia(ag agent.Agent, kind string) bool {
	mc, ok := ag.(agent.MediaCapable)
	if !ok {
		return true
	}
	return mc.SupportsMedia(kind)
}

// chatWithAttachments dispatches to the appropriate multimedia-capable interface
// on the agent (audio first, then images), or falls back to text-only with an
// informational note appended to the message.
//
// The dispatch is gated by both the static interface assertion
// (ImageSupporter / AudioSupporter) AND the runtime MediaCapable check.
// Some adapters (notably ACPAgent) statically expose ChatWithAudio /
// ChatWithImages but the underlying CLI may decline a given media kind
// in its initialize handshake; in that case we silently drop the media
// entries and leave a fallback note in the prompt so the user can see
// what happened.
func (h *Handler) chatWithAttachments(ctx context.Context, ag agent.Agent, conversationID, message string, images []agent.ImageAttachment, audio []agent.AudioAttachment) (string, error) {
	if len(audio) > 0 {
		if as, ok := ag.(agent.AudioSupporter); ok && agentSupportsMedia(ag, agent.MediaKindAudio) {
			return dispatchTypedMedia(ag, conversationID, "audio", len(audio), func() (string, error) {
				return as.ChatWithAudio(ctx, conversationID, message, audio)
			})
		}
		slog.Info("dropping audio attachments: agent does not support audio",
			"component", "handler", "agent", ag.Info().Name, "audio", len(audio))
		message += fmt.Sprintf("\n\n[Note: %d voice message(s) were attached but this agent does not support audio input.]", len(audio))
	}
	if len(images) > 0 {
		if is, ok := ag.(agent.ImageSupporter); ok && agentSupportsMedia(ag, agent.MediaKindImage) {
			return dispatchTypedMedia(ag, conversationID, "images", len(images), func() (string, error) {
				return is.ChatWithImages(ctx, conversationID, message, images)
			})
		}
		slog.Info("dropping image attachments: agent does not support images",
			"component", "handler", "agent", ag.Info().Name, "images", len(images))
		message += fmt.Sprintf("\n\n[Note: %d image(s) were attached but this agent does not support image input.]", len(images))
	}
	return h.chatWithAgent(ctx, ag, conversationID, message)
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
