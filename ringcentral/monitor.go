package ringcentral

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
type MessageHandler func(ctx context.Context, client *Client, post Post)

// Monitor manages the WebSocket connection for receiving messages.
type Monitor struct {
	client    *Client
	handler   MessageHandler
	failures  int
	sentPosts map[string]time.Time // post ID -> timestamp
	mu        sync.Mutex
}

const sentPostTTL = 5 * time.Minute

// MarkSentPost records a post ID as sent by the bot.
func (m *Monitor) MarkSentPost(id string) {
	m.mu.Lock()
	m.sentPosts[id] = time.Now()
	// Evict expired entries
	if len(m.sentPosts) > 100 {
		now := time.Now()
		for k, t := range m.sentPosts {
			if now.Sub(t) > sentPostTTL {
				delete(m.sentPosts, k)
			}
		}
	}
	m.mu.Unlock()
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
func NewMonitor(client *Client, handler MessageHandler) *Monitor {
	return &Monitor{
		client:    client,
		handler:   handler,
		sentPosts: make(map[string]time.Time),
	}
}

// Run starts the WebSocket event loop. Blocks until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) error {
	log.Println("[monitor] starting WebSocket event loop")

	for {
		select {
		case <-ctx.Done():
			log.Println("[monitor] shutting down")
			return ctx.Err()
		default:
		}

		err := m.connectAndListen(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		m.failures++
		backoff := m.calcBackoff()
		log.Printf("[monitor] WebSocket disconnected (%d/%d, backoff=%s): %v",
			m.failures, maxConsecutiveFailures, backoff, err)

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
	log.Printf("[monitor] connecting to WebSocket: %s", wsToken.URI)

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
	log.Printf("[monitor] connected: %s", string(msg))

	// Subscribe to team messaging post events
	if err := m.subscribe(conn); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	m.failures = 0
	log.Println("[monitor] subscribed to post events, listening...")

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
	log.Printf("[monitor] subscription response: %s", string(resp))
	return nil
}

func (m *Monitor) handleWSMessage(ctx context.Context, msg []byte) {
	log.Printf("[monitor] raw WS message: %s", string(msg))

	// RingCentral WebSocket messages are JSON arrays: [header, body]
	// Try to parse as array first, then extract the event from the second element.
	var event WSEvent

	var arr []json.RawMessage
	if err := json.Unmarshal(msg, &arr); err == nil && len(arr) >= 2 {
		// Parse the second element as the event
		if err := json.Unmarshal(arr[1], &event); err != nil {
			log.Printf("[monitor] failed to parse event from array: %v", err)
			return
		}
	} else if err := json.Unmarshal(msg, &event); err != nil {
		// Fallback: try parsing as a single object
		log.Printf("[monitor] ignoring non-event message: %v", err)
		return
	}

	if event.Body.EventType == "" {
		log.Printf("[monitor] ignoring message without eventType")
		return
	}

	// Only process PostAdded events
	if event.Body.EventType != "PostAdded" {
		log.Printf("[monitor] ignoring event type: %s", event.Body.EventType)
		return
	}

	// Skip bot messages: check answer markers and known bot texts
	if isBotMessage(event.Body.Text) {
		log.Printf("[monitor] ignoring bot message (text match): %s", event.Body.ID)
		return
	}

	// Fallback: skip posts tracked by ID (covers edge cases)
	if m.IsSentPost(event.Body.ID) {
		log.Printf("[monitor] ignoring bot's own post %s", event.Body.ID)
		return
	}

	// Only process text messages
	if event.Body.Type != "TextMessage" {
		log.Printf("[monitor] ignoring non-text message type: %s", event.Body.Type)
		return
	}

	// Filter by chat ID if configured
	chatID := m.client.ChatID()
	if chatID != "" && event.Body.GroupID != chatID {
		log.Printf("[monitor] ignoring message from chat %s (expected %s)", event.Body.GroupID, chatID)
		return
	}

	log.Printf("[monitor] received post from %s in chat %s: %q",
		event.Body.CreatorID, event.Body.GroupID, truncate(event.Body.Text, 50))

	go m.handler(ctx, m.client, event.Body)
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

func isBotMessage(text string) bool {
	return strings.HasPrefix(text, "--------answer--------") || text == "Thinking..."
}
