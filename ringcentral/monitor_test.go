package ringcentral

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestMonitor_MarkAndCheckSentPost(t *testing.T) {
	m := &Monitor{sentPosts: make(map[string]time.Time)}
	m.MarkSentPost("post-1")

	if !m.IsSentPost("post-1") {
		t.Error("expected post-1 to be marked as sent")
	}
	if m.IsSentPost("post-2") {
		t.Error("expected post-2 to NOT be marked as sent")
	}
}

func TestMonitor_SentPostExpiry(t *testing.T) {
	m := &Monitor{sentPosts: make(map[string]time.Time)}

	// Manually insert an expired entry
	m.mu.Lock()
	m.sentPosts["old-post"] = time.Now().Add(-10 * time.Minute)
	m.mu.Unlock()

	if m.IsSentPost("old-post") {
		t.Error("expected expired post to return false")
	}

	// Verify it was cleaned up
	m.mu.Lock()
	_, exists := m.sentPosts["old-post"]
	m.mu.Unlock()
	if exists {
		t.Error("expected expired post to be deleted from map")
	}
}

func TestMonitor_CalcBackoff(t *testing.T) {
	m := &Monitor{sentPosts: make(map[string]time.Time)}

	m.failures = 1
	d := m.calcBackoff()
	if d != initialBackoff {
		t.Errorf("failures=1: got %v, want %v", d, initialBackoff)
	}

	m.failures = 2
	d = m.calcBackoff()
	if d != initialBackoff*2 {
		t.Errorf("failures=2: got %v, want %v", d, initialBackoff*2)
	}

	m.failures = 100
	d = m.calcBackoff()
	if d != maxBackoff {
		t.Errorf("failures=100: got %v, want %v (maxBackoff)", d, maxBackoff)
	}
}

func TestIsBotMessage(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"--------answer--------\nhello\n---------end----------", true},
		{"Thinking...", true},
		{"hello world", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isBotMessage(tt.text)
		if got != tt.want {
			t.Errorf("isBotMessage(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func newTestMonitor(chatIDs string, handler MessageHandler) *Monitor {
	creds := &Credentials{
		ClientID:     "id",
		ClientSecret: "secret",
		JWTToken:     "jwt",
	}
	client := NewClient(creds)
	var ids []string
	if chatIDs != "" {
		ids = []string{chatIDs}
	}
	return NewMonitor(client, handler, ids)
}

func makeWSMessage(post Post) []byte {
	header := map[string]string{"type": "ServerNotification"}
	event := WSEvent{
		UUID:  "test-uuid",
		Event: "/team-messaging/v1/posts",
		Body:  post,
	}
	arr := []interface{}{header, event}
	data, _ := json.Marshal(arr)
	return data
}

func TestMonitor_HandleWSMessage_PostAdded(t *testing.T) {
	var mu sync.Mutex
	var received []Post

	m := newTestMonitor("chat-1", func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		received = append(received, post)
		mu.Unlock()
	})

	msg := makeWSMessage(Post{
		ID:        "p1",
		GroupID:   "chat-1",
		Type:      "TextMessage",
		Text:      "hello from user",
		CreatorID: "user-1",
		EventType: "PostAdded",
	})

	m.handleWSMessage(context.Background(), msg)

	// handler is called in a goroutine, wait briefly
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 post dispatched, got %d", len(received))
	}
	if received[0].ID != "p1" {
		t.Errorf("expected post ID p1, got %s", received[0].ID)
	}
}

func TestMonitor_HandleWSMessage_IgnoreBotMessage(t *testing.T) {
	var called bool
	m := newTestMonitor("chat-1", func(ctx context.Context, client *Client, _ *Client, post Post) {
		called = true
	})

	// "Thinking..." is a bot marker
	msg := makeWSMessage(Post{
		ID:        "p2",
		GroupID:   "chat-1",
		Type:      "TextMessage",
		Text:      "Thinking...",
		CreatorID: "bot-1",
		EventType: "PostAdded",
	})

	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	if called {
		t.Error("handler should not be called for bot messages")
	}
}

func TestMonitor_HandleWSMessage_FilterByChatID(t *testing.T) {
	var called bool
	m := newTestMonitor("chat-1", func(ctx context.Context, client *Client, _ *Client, post Post) {
		called = true
	})

	// Message from a different chat
	msg := makeWSMessage(Post{
		ID:        "p3",
		GroupID:   "chat-OTHER",
		Type:      "TextMessage",
		Text:      "hello",
		CreatorID: "user-1",
		EventType: "PostAdded",
	})

	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	if called {
		t.Error("handler should not be called for messages from other chats")
	}
}

func TestMonitor_HandleWSMessage_IgnoreNonText(t *testing.T) {
	var called bool
	m := newTestMonitor("chat-1", func(ctx context.Context, client *Client, _ *Client, post Post) {
		called = true
	})

	msg := makeWSMessage(Post{
		ID:        "p4",
		GroupID:   "chat-1",
		Type:      "PersonJoined",
		Text:      "",
		CreatorID: "user-1",
		EventType: "PostAdded",
	})

	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	if called {
		t.Error("handler should not be called for non-text messages")
	}
}

func TestMonitor_HandleWSMessage_IgnoreSentPost(t *testing.T) {
	var called bool
	m := newTestMonitor("chat-1", func(ctx context.Context, client *Client, _ *Client, post Post) {
		called = true
	})

	// Mark post as sent by bot
	m.MarkSentPost("p5")

	msg := makeWSMessage(Post{
		ID:        "p5",
		GroupID:   "chat-1",
		Type:      "TextMessage",
		Text:      "bot reply",
		CreatorID: "bot-1",
		EventType: "PostAdded",
	})

	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	if called {
		t.Error("handler should not be called for bot's own sent posts")
	}
}

func TestMonitor_ChooseClient_NoBotClient(t *testing.T) {
	m := newTestMonitor("", func(ctx context.Context, client *Client, _ *Client, post Post) {})
	got := m.chooseClient("any-chat")
	if got != m.client {
		t.Error("without bot client, should always return private client")
	}
}

func TestMonitor_ChooseClient_BotDM(t *testing.T) {
	m := newTestMonitor("", func(ctx context.Context, client *Client, _ *Client, post Post) {})
	bot := NewBotClient("", "fake-bot-token")
	m.SetBotClient(bot, "dm-chat-123", true)

	got := m.chooseClient("dm-chat-123")
	if got != bot {
		t.Error("bot DM chat should use bot client")
	}

	got = m.chooseClient("other-chat")
	if got != m.client {
		t.Error("non-DM chat should use private client")
	}
}

func TestMonitor_ChooseClient_AllowedChats(t *testing.T) {
	creds := &Credentials{ClientID: "id", ClientSecret: "secret", JWTToken: "jwt"}
	client := NewClient(creds)
	handler := func(ctx context.Context, c *Client, _ *Client, p Post) {}
	m := NewMonitor(client, handler, []string{"group-1", "group-2"})

	bot := NewBotClient("", "fake-bot-token")
	m.SetBotClient(bot, "dm-chat-123", true)

	if m.chooseClient("group-1") != bot {
		t.Error("group-1 should use bot client")
	}
	if m.chooseClient("group-2") != bot {
		t.Error("group-2 should use bot client")
	}
	if m.chooseClient("group-3") != m.client {
		t.Error("group-3 should use private client")
	}
	if m.chooseClient("dm-chat-123") != bot {
		t.Error("bot DM should use bot client")
	}
}

func TestMonitor_HandleWSMessage_IgnoreBotClientPost(t *testing.T) {
	var mu sync.Mutex
	var called bool
	m := newTestMonitor("", func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		called = true
		mu.Unlock()
	})
	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-ext-123")
	m.SetBotClient(bot, "dm-chat", true)

	msg := makeWSMessage(Post{
		ID:        "p99",
		GroupID:   "dm-chat",
		Type:      "TextMessage",
		Text:      "bot reply",
		CreatorID: "bot-ext-123",
		EventType: "PostAdded",
	})

	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Error("handler should not be called for bot client's own messages")
	}
}

func TestMonitor_HandleWSMessage_BotRouting(t *testing.T) {
	var mu sync.Mutex
	var receivedClient *Client
	creds := &Credentials{ClientID: "id", ClientSecret: "secret", JWTToken: "jwt"}
	client := NewClient(creds)
	handler := func(ctx context.Context, c *Client, _ *Client, p Post) {
		mu.Lock()
		receivedClient = c
		mu.Unlock()
	}
	m := NewMonitor(client, handler, []string{"dm-chat", "group-1"})
	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-ext-123")
	m.SetBotClient(bot, "dm-chat", true)

	// Message in bot DM -> should route to bot client
	msg := makeWSMessage(Post{
		ID:        "p100",
		GroupID:   "dm-chat",
		Type:      "TextMessage",
		Text:      "hello",
		CreatorID: "user-1",
		EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if receivedClient != bot {
		t.Error("DM chat should route to bot client")
	}
	receivedClient = nil
	mu.Unlock()

	// Message in group-1 with bot mention -> should route to bot client
	msg = makeWSMessage(Post{
		ID:        "p101",
		GroupID:   "group-1",
		Type:      "TextMessage",
		Text:      "@RingClaw hello",
		CreatorID: "user-1",
		EventType: "PostAdded",
		Mentions:  []Mention{{ID: "bot-ext-123", Type: "Person", Name: "RingClaw"}},
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if receivedClient != bot {
		t.Error("group-1 with bot mention should route to bot client")
	}
	receivedClient = nil
	mu.Unlock()

	// Message in random-chat (not in allowed list) -> should be ignored
	msg = makeWSMessage(Post{
		ID:        "p102",
		GroupID:   "random-chat",
		Type:      "TextMessage",
		Text:      "hello",
		CreatorID: "user-1",
		EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if receivedClient != nil {
		t.Error("random-chat not in allowed list should be ignored")
	}
	mu.Unlock()
}

func TestMonitor_HandleWSMessage_PrivateOwnerFiltered(t *testing.T) {
	var mu sync.Mutex
	var called bool
	m := newTestMonitor("", func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		called = true
		mu.Unlock()
	})
	m.client.SetOwnerID("private-ext-456")

	msg := makeWSMessage(Post{
		ID:        "p200",
		GroupID:   "any-chat",
		Type:      "TextMessage",
		Text:      "--------answer--------\nhello\n---------end----------",
		CreatorID: "private-ext-456",
		EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Error("handler should not be called for private app's own bot messages")
	}
}

func TestMonitor_SetBotClient_NoChatIDs(t *testing.T) {
	m := newTestMonitor("", func(ctx context.Context, client *Client, _ *Client, post Post) {})
	bot := NewBotClient("", "fake-token")
	m.SetBotClient(bot, "dm-chat", true)

	if m.chooseClient("dm-chat") != bot {
		t.Error("DM should route to bot")
	}
	if m.chooseClient("other") != m.client {
		t.Error("other should route to private")
	}
}

func newBotMonitorWithGroups(groups []string, mentionOnly bool, handler MessageHandler) (*Monitor, *Client) {
	creds := &Credentials{ClientID: "id", ClientSecret: "secret", JWTToken: "jwt"}
	client := NewClient(creds)
	m := NewMonitor(client, handler, groups)
	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-ext-123")
	m.SetBotClient(bot, "dm-chat", mentionOnly)
	return m, bot
}

func TestMonitor_GroupChat_RequiresMention(t *testing.T) {
	var mu sync.Mutex
	var called bool
	m, _ := newBotMonitorWithGroups([]string{"group-1"}, true, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		called = true
		mu.Unlock()
	})

	// Message in group-1 WITHOUT mention -> should be ignored
	msg := makeWSMessage(Post{
		ID:        "p300",
		GroupID:   "group-1",
		Type:      "TextMessage",
		Text:      "hello everyone",
		CreatorID: "user-1",
		EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if called {
		t.Error("group message without bot mention should be ignored")
	}
	mu.Unlock()
}

func TestMonitor_GroupChat_WithMention(t *testing.T) {
	var mu sync.Mutex
	var receivedClient *Client
	m, bot := newBotMonitorWithGroups([]string{"group-1"}, true, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		receivedClient = client
		mu.Unlock()
	})

	// Message in group-1 WITH bot mention -> should be processed
	msg := makeWSMessage(Post{
		ID:        "p301",
		GroupID:   "group-1",
		Type:      "TextMessage",
		Text:      "@RingClaw hello",
		CreatorID: "user-1",
		EventType: "PostAdded",
		Mentions:  []Mention{{ID: "bot-ext-123", Type: "Person", Name: "RingClaw"}},
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if receivedClient != bot {
		t.Error("group message with bot mention should route to bot client")
	}
}

func TestMonitor_GroupChat_WrongMention(t *testing.T) {
	var mu sync.Mutex
	var called bool
	m, _ := newBotMonitorWithGroups([]string{"group-1"}, true, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		called = true
		mu.Unlock()
	})

	// Message in group-1 mentioning someone else -> should be ignored
	msg := makeWSMessage(Post{
		ID:        "p302",
		GroupID:   "group-1",
		Type:      "TextMessage",
		Text:      "@OtherUser hello",
		CreatorID: "user-1",
		EventType: "PostAdded",
		Mentions:  []Mention{{ID: "other-user-456", Type: "Person", Name: "OtherUser"}},
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Error("group message mentioning someone else should be ignored")
	}
}

func TestMonitor_DM_NoMentionRequired(t *testing.T) {
	var mu sync.Mutex
	var receivedClient *Client
	m, bot := newBotMonitorWithGroups([]string{"group-1"}, true, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		receivedClient = client
		mu.Unlock()
	})

	// Message in DM without mention -> should still be processed
	msg := makeWSMessage(Post{
		ID:        "p303",
		GroupID:   "dm-chat",
		Type:      "TextMessage",
		Text:      "hello bot",
		CreatorID: "user-1",
		EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if receivedClient != bot {
		t.Error("DM message should be processed without mention")
	}
}

func TestMonitor_IsBotMentioned(t *testing.T) {
	m, _ := newBotMonitorWithGroups(nil, true, func(ctx context.Context, client *Client, _ *Client, post Post) {})

	tests := []struct {
		name     string
		mentions []Mention
		want     bool
	}{
		{"no mentions", nil, false},
		{"empty mentions", []Mention{}, false},
		{"other person", []Mention{{ID: "other-456"}}, false},
		{"bot mentioned", []Mention{{ID: "bot-ext-123"}}, true},
		{"bot among others", []Mention{{ID: "other-456"}, {ID: "bot-ext-123"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.isBotMentioned(tt.mentions)
			if got != tt.want {
				t.Errorf("isBotMentioned() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMonitor_GroupChat_MentionOnlyDisabled(t *testing.T) {
	var mu sync.Mutex
	var receivedClient *Client
	m, bot := newBotMonitorWithGroups([]string{"group-1"}, false, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		receivedClient = client
		mu.Unlock()
	})

	// Message in group-1 WITHOUT mention -> should still be processed
	msg := makeWSMessage(Post{
		ID:        "p400",
		GroupID:   "group-1",
		Type:      "TextMessage",
		Text:      "hello everyone",
		CreatorID: "user-1",
		EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if receivedClient != bot {
		t.Error("with bot_mention_only=false, group message without mention should be processed by bot")
	}
}

func TestNewBotClient(t *testing.T) {
	bot := NewBotClient("https://example.com", "test-bot-token")
	token, err := bot.Auth().AccessToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "test-bot-token" {
		t.Errorf("expected test-bot-token, got %q", token)
	}
	if bot.ServerURL() != "https://example.com" {
		t.Errorf("expected https://example.com, got %q", bot.ServerURL())
	}
}

func TestNewBotClient_DefaultServerURL(t *testing.T) {
	bot := NewBotClient("", "test-bot-token")
	if bot.ServerURL() != defaultServerURL {
		t.Errorf("expected %q, got %q", defaultServerURL, bot.ServerURL())
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncate(tt.s, tt.n)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
}
