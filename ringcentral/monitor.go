package ringcentral

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/ringclaw/ringclaw/internal/util"
)

const (
	maxConsecutiveFailures = 5
	initialBackoff         = 3 * time.Second
	maxBackoff             = 60 * time.Second
	wsPingInterval         = 30 * time.Second
	wsPongWait             = 60 * time.Second
)

// MessageHandler is called for each received post.
// replyClient is for sending replies (bot or private app depending on routing).
// readClient is always the private app for reading any chat's messages.
type MessageHandler func(ctx context.Context, replyClient *Client, readClient *Client, post Post)

// MentionAuthorizeFunc is called when a non-trusted sender @mentions
// the bot in an allowed group chat AND the authorize-mention feature
// is enabled. Implementations should issue an OOB approval challenge
// and, on approval, add the user to the per-chat allowlist via
// AddChatUserAllow. The original post is dropped — implementations
// MUST NOT dispatch it to the agent.
type MentionAuthorizeFunc func(ctx context.Context, replyClient *Client, readClient *Client, post Post)

// Monitor manages the WebSocket connection for receiving messages.
// The bot client is required and used for WS connection and replies.
// The private client is optional and used for reading other chats.
type Monitor struct {
	client          *Client // bot client (required)
	privateClient   *Client // private app client (optional)
	botMentionOnly  bool
	allowedChatIDs  map[string]bool
	allowedUserIDs  map[string]bool
	allowAllSenders bool // when true, empty allowedUserIDs means "allow all"; default false = mandatory allowlist
	handler         MessageHandler
	// chatUserAllow maps chat ID -> set of numeric user IDs that may
	// drive the bot in that specific chat. Layered ON TOP of
	// allowedUserIDs: a sender is considered trusted when they appear
	// in either set. Populated by the authorize-mention OOB flow and
	// by config.json's ringcentral.chat_user_allow at startup.
	chatUserAllow map[string]map[string]bool
	// mentionAuthorize, when non-nil, is called for non-trusted
	// senders that @mention the bot in an allowed group chat. The
	// original post is NOT dispatched to the message handler; the
	// callback owns the post.
	mentionAuthorize MentionAuthorizeFunc
	failures         int
	sentPosts        map[string]time.Time // post ID -> timestamp
	lastEvict        time.Time
	mu               sync.Mutex
}

const (
	sentPostTTL      = 5 * time.Minute
	evictInterval    = 1 * time.Minute
)

// MarkSentPost records a post ID as sent by the bot.
func (m *Monitor) MarkSentPost(id string) {
	m.mu.Lock()
	m.sentPosts[id] = time.Now()
	if time.Since(m.lastEvict) > evictInterval {
		m.evictExpiredLocked()
		m.lastEvict = time.Now()
	}
	m.mu.Unlock()
}

// evictExpiredLocked removes expired entries from sentPosts. Must hold m.mu.
func (m *Monitor) evictExpiredLocked() {
	now := time.Now()
	for k, t := range m.sentPosts {
		if now.Sub(t) > sentPostTTL {
			delete(m.sentPosts, k)
		}
	}
}

// IsSentPost checks if a post was recently sent by the bot.
func (m *Monitor) IsSentPost(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.sentPosts[id]
	if !ok {
		return false
	}
	if time.Since(t) > sentPostTTL {
		delete(m.sentPosts, id)
		return false
	}
	return true
}

// NewMonitor creates a new WebSocket monitor.
// botClient is used for WS connection and replies.
// chatIDs limits which chats are monitored; empty means no chats.
// sourceUserIDs is the sender allowlist. By default the allowlist is advisory
// (empty means allow all) for backward compatibility; production callers
// should call EnforceSenderAllowlist after populating the list to switch the
// monitor into strict mode where only trusted senders can drive the agent.
// mentionOnly controls whether group chats require @mention.
func NewMonitor(botClient *Client, handler MessageHandler, chatIDs []string, sourceUserIDs []string, mentionOnly bool) *Monitor {
	allowed := make(map[string]bool, len(chatIDs))
	for _, id := range chatIDs {
		allowed[id] = true
	}
	// Ensure bot DM is always in the allowed list
	if botClient.dmChatID != "" {
		allowed[botClient.dmChatID] = true
	}
	allowedUsers := make(map[string]bool, len(sourceUserIDs))
	for _, id := range sourceUserIDs {
		allowedUsers[id] = true
	}
	return &Monitor{
		client:          botClient,
		botMentionOnly:  mentionOnly,
		handler:         handler,
		allowedChatIDs:  allowed,
		allowedUserIDs:  allowedUsers,
		allowAllSenders: true, // legacy-compatible default; cmd/start.go flips this off
		chatUserAllow:   make(map[string]map[string]bool),
		sentPosts:       make(map[string]time.Time),
	}
}

// SetChatUserAllow installs the per-chat user allowlist resolved at
// startup. Pre-existing in-memory entries (e.g. from runtime
// AddChatUserAllow calls) are NOT preserved — callers should call this
// once during startup and then rely on AddChatUserAllow for runtime
// additions.
func (m *Monitor) SetChatUserAllow(byChat map[string][]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chatUserAllow = make(map[string]map[string]bool, len(byChat))
	for chatID, ids := range byChat {
		chatID = strings.TrimSpace(chatID)
		if chatID == "" {
			continue
		}
		set := make(map[string]bool, len(ids))
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			set[id] = true
		}
		if len(set) > 0 {
			m.chatUserAllow[chatID] = set
		}
	}
}

// AddChatUserAllow grants a numeric user ID per-chat trust. Used by
// the authorize-mention OOB flow on approval so the very next message
// from that user (in that chat) is dispatched normally.
func (m *Monitor) AddChatUserAllow(chatID, userID string) {
	chatID = strings.TrimSpace(chatID)
	userID = strings.TrimSpace(userID)
	if chatID == "" || userID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.chatUserAllow == nil {
		m.chatUserAllow = make(map[string]map[string]bool)
	}
	set, ok := m.chatUserAllow[chatID]
	if !ok {
		set = make(map[string]bool)
		m.chatUserAllow[chatID] = set
	}
	set[userID] = true
}

// SetMentionAuthorize installs the callback invoked when a
// non-trusted sender @mentions the bot in an allowed group chat.
// Pass nil to disable the authorize-mention path (the legacy
// silent-drop behavior is restored).
func (m *Monitor) SetMentionAuthorize(fn MentionAuthorizeFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mentionAuthorize = fn
}

// isChatUserAllowed reports whether the given (chat, user) pair is on
// the per-chat allowlist.
func (m *Monitor) isChatUserAllowed(chatID, userID string) bool {
	if chatID == "" || userID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	set, ok := m.chatUserAllow[chatID]
	if !ok {
		return false
	}
	return set[userID]
}

// hasAnyChatUserAllow reports whether any chat has at least one entry
// on the per-chat allowlist. Used to keep the empty-allowlist startup
// guard from blocking deployments that authorize all senders via
// chat_user_allow only.
func (m *Monitor) hasAnyChatUserAllow() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, set := range m.chatUserAllow {
		if len(set) > 0 {
			return true
		}
	}
	return false
}

// SetPrivateClient configures an optional private app client for reading
// other chats and cross-chat actions (e.g. summarize).
func (m *Monitor) SetPrivateClient(c *Client) {
	m.privateClient = c
}

// SetAllowAllSenders toggles whether an empty sender allowlist permits every
// user (true, legacy default) or denies every user (false, strict mode).
// Production callers should set this to false after populating the trusted
// sender list via AddTrustedSender.
func (m *Monitor) SetAllowAllSenders(allow bool) {
	m.allowAllSenders = allow
}

// EnforceSenderAllowlist switches the monitor into strict mode: an empty
// allowlist denies all senders, and only IDs added via the constructor or
// AddTrustedSender may drive the agent.
func (m *Monitor) EnforceSenderAllowlist() {
	m.allowAllSenders = false
}

// AddTrustedSender adds a single user ID to the sender allowlist.
// Used by start.go to inject the Private App owner so the local machine
// owner's DMs are always accepted.
func (m *Monitor) AddTrustedSender(userID string) {
	if userID == "" {
		return
	}
	m.mu.Lock()
	if m.allowedUserIDs == nil {
		m.allowedUserIDs = make(map[string]bool)
	}
	m.allowedUserIDs[userID] = true
	m.mu.Unlock()
}

// HasTrustedSenders reports whether any sender is on the allowlist.
func (m *Monitor) HasTrustedSenders() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.allowedUserIDs) > 0
}

// readClient returns the private client if available, otherwise the bot client.
func (m *Monitor) readClient() *Client {
	if m.privateClient != nil {
		return m.privateClient
	}
	return m.client
}

// Run starts the WebSocket event loop with automatic reconnection.
// Blocks until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) error {
	slog.Info("starting WebSocket event loop", "component", "monitor")

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down", "component", "monitor")
			return ctx.Err()
		default:
		}

		err := m.connectAndListen(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		m.failures++
		backoff := m.calcBackoff()
		slog.Warn("WebSocket disconnected", "component", "monitor", "failures", m.failures, "backoff", backoff, "error", err)

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (m *Monitor) connectAndListen(ctx context.Context) error {
	wsToken, err := m.client.Auth().GetWSToken()
	if err != nil {
		return fmt.Errorf("get WS token: %w", err)
	}

	wsURL := wsToken.URI + "?access_token=" + url.QueryEscape(wsToken.WSAccessToken)
	slog.Info("connecting to WebSocket", "component", "monitor", "uri", wsToken.URI)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("WebSocket dial: %w", err)
	}
	defer conn.Close()

	// Read ConnectionDetails
	_, msg, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read connection details: %w", err)
	}
	slog.Debug("connected", "component", "monitor", "details", string(msg))

	// Subscribe to team messaging post events
	if err := m.subscribe(conn); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	m.failures = 0
	slog.Info("subscribed to post events, listening...", "component", "monitor")

	// Set up pong handler to extend read deadline on each pong received
	conn.SetPongHandler(func(appData string) error {
		conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	// Set initial read deadline
	conn.SetReadDeadline(time.Now().Add(wsPongWait))

	// Use a channel to signal errors from the read goroutine
	errCh := make(chan error, 1)

	// Write goroutine: sends pings periodically
	var writeMu sync.Mutex
	go func() {
		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				writeMu.Lock()
				conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				writeMu.Unlock()
				return
			case <-ticker.C:
				writeMu.Lock()
				err := conn.WriteMessage(websocket.PingMessage, nil)
				writeMu.Unlock()
				if err != nil {
					errCh <- fmt.Errorf("ping: %w", err)
					return
				}
			}
		}
	}()

	// Read loop in main goroutine
	for {
		select {
		case err := <-errCh:
			return err
		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read message: %w", err)
		}

		// Extend deadline on any received message
		conn.SetReadDeadline(time.Now().Add(wsPongWait))

		m.handleWSMessage(ctx, msg)
	}
}

func (m *Monitor) subscribe(conn *websocket.Conn) error {
	subReq := []interface{}{
		WSClientRequest{
			Type:      "ClientRequest",
			MessageID: uuid.New().String(),
			Method:    "POST",
			Path:      "/restapi/v1.0/subscription/",
		},
		WSSubscriptionBody{
			EventFilters: []string{
				"/team-messaging/v1/posts",
			},
			DeliveryMode: WSDeliveryMode{
				TransportType: "WebSocket",
			},
		},
	}

	data, err := json.Marshal(subReq)
	if err != nil {
		return fmt.Errorf("marshal subscription: %w", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("send subscription: %w", err)
	}

	_, resp, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read subscription response: %w", err)
	}
	slog.Debug("subscription response", "component", "monitor", "response", string(resp))
	return nil
}

func (m *Monitor) handleWSMessage(ctx context.Context, msg []byte) {
	slog.Debug("raw WS message", "component", "monitor", "message", string(msg))

	// RingCentral WebSocket messages are JSON arrays: [header, body]
	// Try to parse as array first, then extract the event from the second element.
	var event WSEvent

	var arr []json.RawMessage
	if err := json.Unmarshal(msg, &arr); err == nil && len(arr) >= 2 {
		// Parse the second element as the event
		if err := json.Unmarshal(arr[1], &event); err != nil {
			slog.Error("failed to parse event from array", "component", "monitor", "error", err)
			return
		}
	} else if err := json.Unmarshal(msg, &event); err != nil {
		// Fallback: try parsing as a single object
		slog.Debug("ignoring non-event message", "component", "monitor", "error", err)
		return
	}

	if event.Body.EventType == "" {
		slog.Debug("ignoring message without eventType", "component", "monitor")
		return
	}

	// Only process PostAdded events
	if event.Body.EventType != "PostAdded" {
		slog.Debug("ignoring event type", "component", "monitor", "eventType", event.Body.EventType)
		return
	}

	// Skip bot messages: check answer markers and known bot texts
	if isBotMessage(event.Body.Text) {
		slog.Debug("ignoring bot message (text match)", "component", "monitor", "postID", event.Body.ID)
		return
	}

	// Fallback: skip posts tracked by ID (covers edge cases)
	if m.IsSentPost(event.Body.ID) {
		slog.Debug("ignoring bot's own post", "component", "monitor", "postID", event.Body.ID)
		return
	}

	// Only process text messages
	if event.Body.Type != "TextMessage" {
		slog.Debug("ignoring non-text message type", "component", "monitor", "type", event.Body.Type)
		return
	}

	// Filter by allowed chat IDs (empty = reject all)
	if !m.allowedChatIDs[event.Body.GroupID] {
		slog.Debug("ignoring message from non-allowed chat", "component", "monitor", "chatID", event.Body.GroupID)
		return
	}

	// Skip messages from the bot's own extension
	if m.client.OwnerID() != "" && event.Body.CreatorID == m.client.OwnerID() {
		slog.Debug("ignoring bot's own post", "component", "monitor", "postID", event.Body.ID)
		return
	}

	// Snapshot the racy Layer 0 fields once under m.mu so concurrent
	// SetMentionAuthorize / Set/AddChatUserAllow / AddTrustedSender /
	// SetAllowAllSenders calls cannot race with the reads below. The
	// chat_user_allow lookup (and the empty-set scan) are still routed
	// through the thread-safe helpers, which take their own lock — that
	// keeps the snapshot focused on the fields that have no helper.
	m.mu.Lock()
	allowAll := m.allowAllSenders
	nGlobal := len(m.allowedUserIDs)
	globalHit := m.allowedUserIDs[event.Body.CreatorID]
	authorizeFn := m.mentionAuthorize
	m.mu.Unlock()

	// Filter by source user IDs. The allowlist is mandatory by default: empty
	// list means deny all (effectively disabling remote control). Tests and
	// explicit opt-in deployments can call SetAllowAllSenders(true) to restore
	// the legacy "empty = allow all" behavior.
	if !allowAll && nGlobal == 0 && !m.hasAnyChatUserAllow() {
		slog.Warn("dropping message: sender allowlist is empty (set ringcentral.source_user_ids or configure a Private App owner)",
			"component", "monitor", "userID", event.Body.CreatorID, "chatID", event.Body.GroupID)
		return
	}
	// Trust sources, in priority order:
	//   1. Legacy "allow all" mode with no global allowlist configured.
	//   2. Sender appears on the global source_user_ids allowlist.
	//   3. Sender appears on the per-chat chat_user_allow set for
	//      this destination chat (seeded from config.json or pushed
	//      by the authorize-mention OOB approval flow).
	// In strict mode (allowAllSenders=false, set by EnforceSenderAllowlist),
	// an empty global allowlist does NOT short-circuit to trusted —
	// the per-chat path remains the only way in.
	senderTrusted := false
	switch {
	case allowAll && nGlobal == 0:
		senderTrusted = true
	case globalHit:
		senderTrusted = true
	case m.isChatUserAllowed(event.Body.GroupID, event.Body.CreatorID):
		senderTrusted = true
	}
	if !senderTrusted {
		// Authorize-mention path: when the operator opted into
		// allow_group_mention_authorize, a non-trusted user that
		// @mentions the bot in an allowed group chat triggers the OOB
		// approval flow. The original post is consumed (dropped) by
		// the callback, NOT dispatched to the message handler.
		if authorizeFn != nil &&
			!m.client.IsBotDM(event.Body.GroupID) &&
			m.isBotMentioned(event.Body.Mentions) {
			slog.Info("authorize-mention: routing non-trusted group mention",
				"component", "monitor", "userID", event.Body.CreatorID, "chatID", event.Body.GroupID, "postID", event.Body.ID)
			go authorizeFn(ctx, m.client, m.readClient(), event.Body)
			return
		}
		slog.Debug("ignoring message from non-allowed user", "component", "monitor", "userID", event.Body.CreatorID)
		return
	}

	// In group chats, only respond when the bot is @mentioned (if enabled)
	if m.botMentionOnly && !m.client.IsBotDM(event.Body.GroupID) {
		if !m.isBotMentioned(event.Body.Mentions) {
			slog.Debug("ignoring group message without bot mention", "component", "monitor", "chatID", event.Body.GroupID)
			return
		}
	}

	slog.Info("received post", "component", "monitor", "creatorID", event.Body.CreatorID, "chatID", event.Body.GroupID, "text", util.Truncate(event.Body.Text, 50))

	go m.handler(ctx, m.client, m.readClient(), event.Body)
}

func (m *Monitor) calcBackoff() time.Duration {
	d := initialBackoff
	for i := 1; i < m.failures; i++ {
		d *= 2
		if d > maxBackoff {
			return maxBackoff
		}
	}
	return d
}



// isBotMentioned checks if the bot's extension ID appears in the post mentions.
func (m *Monitor) isBotMentioned(mentions []Mention) bool {
	botID := m.client.OwnerID()
	if botID == "" {
		return false
	}
	for _, mention := range mentions {
		if mention.ID == botID {
			return true
		}
	}
	return false
}

func isBotMessage(text string) bool {
	return strings.HasPrefix(text, "--------answer--------") || text == "Thinking..."
}
