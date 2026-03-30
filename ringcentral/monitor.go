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

// Monitor manages the WebSocket connection for receiving messages.
// Monitor manages the WebSocket connection for receiving messages.
type Monitor struct {
	client         *Client
	botClient      *Client
	botDMChatID    string
	botMentionOnly bool
	allowedChatIDs map[string]bool // combined chat filter + bot routing
	handler        MessageHandler
	failures       int
	sentPosts      map[string]time.Time // post ID -> timestamp
	lastEvict      time.Time
	mu             sync.Mutex
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
// chatIDs limits which chats are monitored; empty means no chats.
func NewMonitor(client *Client, handler MessageHandler, chatIDs []string) *Monitor {
	allowed := make(map[string]bool, len(chatIDs))
	for _, id := range chatIDs {
		allowed[id] = true
	}
	return &Monitor{
		client:         client,
		handler:        handler,
		allowedChatIDs: allowed,
		sentPosts:      make(map[string]time.Time),
	}
}

// SetBotClient configures a bot client for routing replies.
// dmChatID is the default DM chat between the bot and the installer.
// mentionOnly controls whether group chats require @mention (default true).
// The bot's DM chat is automatically added to the allowed chat list.
func (m *Monitor) SetBotClient(bot *Client, dmChatID string, mentionOnly bool) {
	m.botClient = bot
	m.botDMChatID = dmChatID
	m.botMentionOnly = mentionOnly
	// Ensure bot DM is always in the allowed list
	if dmChatID != "" {
		m.allowedChatIDs[dmChatID] = true
	}
}

// chooseClient returns the bot client if the chat is in the bot's
// allowed list, otherwise returns the private app client.
func (m *Monitor) chooseClient(chatID string) *Client {
	if m.botClient == nil {
		return m.client
	}
	if chatID == m.botDMChatID {
		return m.botClient
	}
	if m.allowedChatIDs[chatID] {
		return m.botClient
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
	slog.Info("connected", "component", "monitor", "details", string(msg))

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
	if m.botClient != nil && event.Body.CreatorID == m.botClient.OwnerID() {
		slog.Debug("ignoring bot client's own post", "component", "monitor", "postID", event.Body.ID)
		return
	}

	replyClient := m.chooseClient(event.Body.GroupID)

	// In group chats routed to the bot, only respond when the bot is @mentioned (if enabled)
	if m.botMentionOnly && replyClient == m.botClient && event.Body.GroupID != m.botDMChatID {
		if !m.isBotMentioned(event.Body.Mentions) {
			slog.Debug("ignoring group message without bot mention", "component", "monitor", "chatID", event.Body.GroupID)
			return
		}
	}

	slog.Info("received post", "component", "monitor", "creatorID", event.Body.CreatorID, "chatID", event.Body.GroupID, "text", truncate(event.Body.Text, 50))

	go m.handler(ctx, replyClient, m.client, event.Body)
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// isBotMentioned checks if the bot's extension ID appears in the post mentions.
func (m *Monitor) isBotMentioned(mentions []Mention) bool {
	if m.botClient == nil {
		return false
	}
	botID := m.botClient.OwnerID()
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
