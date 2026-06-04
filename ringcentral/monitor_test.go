package ringcentral

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/internal/util"
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
	bot := NewBotClient("", "fake-bot-token")
	var ids []string
	if chatIDs != "" {
		ids = []string{chatIDs}
	}
	return NewMonitor(bot, handler, ids, nil, false)
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

func TestMonitor_HandleWSMessage_EmptyChatIDsDenyUnlistedChats(t *testing.T) {
	var called bool
	m := newTestMonitor("", func(ctx context.Context, client *Client, _ *Client, post Post) {
		called = true
	})

	msg := makeWSMessage(Post{
		ID:        "p3-all",
		GroupID:   "chat-OTHER",
		Type:      "TextMessage",
		Text:      "hello",
		CreatorID: "user-1",
		EventType: "PostAdded",
	})

	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	if called {
		t.Fatal("handler should not be called for unlisted chats when chat_ids is empty")
	}
}

func TestMonitor_HandleWSMessage_AllowUnlistedGroupChats(t *testing.T) {
	var mu sync.Mutex
	var received []Post

	m := newTestMonitor("chat-1", func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		received = append(received, post)
		mu.Unlock()
	})
	m.SetAllowUnlistedGroupChats(true)

	msg := makeWSMessage(Post{
		ID:        "p3-group",
		GroupID:   "chat-OTHER",
		Type:      "TextMessage",
		Text:      "hello from another group",
		CreatorID: "user-1",
		EventType: "PostAdded",
	})

	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 post dispatched with allow_unlisted_group_chats, got %d", len(received))
	}
	if received[0].GroupID != "chat-OTHER" {
		t.Fatalf("expected message from chat-OTHER, got %q", received[0].GroupID)
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

func TestMonitor_ReadClient_NoPrivateClient(t *testing.T) {
	m := newTestMonitor("", func(ctx context.Context, client *Client, _ *Client, post Post) {})
	got := m.readClient()
	if got != m.client {
		t.Error("without private client, readClient should return bot client")
	}
}

func TestMonitor_ReadClient_WithPrivateClient(t *testing.T) {
	m := newTestMonitor("", func(ctx context.Context, client *Client, _ *Client, post Post) {})
	creds := &Credentials{ClientID: "id", ClientSecret: "secret", JWTToken: "jwt"}
	private := NewClient(creds)
	m.SetPrivateClient(private)

	got := m.readClient()
	if got != private {
		t.Error("with private client, readClient should return private client")
	}
}

func TestMonitor_HandleWSMessage_IgnoreBotClientPost(t *testing.T) {
	var mu sync.Mutex
	var called bool

	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-ext-123")
	bot.SetDMChatID("dm-chat")
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		called = true
		mu.Unlock()
	}, []string{"dm-chat"}, nil, true)

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
	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-ext-123")
	bot.SetDMChatID("dm-chat")
	handler := func(ctx context.Context, c *Client, _ *Client, p Post) {
		mu.Lock()
		receivedClient = c
		mu.Unlock()
	}
	m := NewMonitor(bot, handler, []string{"dm-chat", "group-1"}, nil, true)

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

func TestMonitor_HandleWSMessage_BotOwnerFiltered(t *testing.T) {
	var mu sync.Mutex
	var called bool
	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-ext-456")
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		called = true
		mu.Unlock()
	}, []string{"any-chat"}, nil, false)

	msg := makeWSMessage(Post{
		ID:        "p200",
		GroupID:   "any-chat",
		Type:      "TextMessage",
		Text:      "bot reply",
		CreatorID: "bot-ext-456",
		EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Error("handler should not be called for bot's own messages")
	}
}

func TestMonitor_SetPrivateClient(t *testing.T) {
	m := newTestMonitor("", func(ctx context.Context, client *Client, _ *Client, post Post) {})
	if m.readClient() != m.client {
		t.Error("without private client, readClient should be bot client")
	}

	creds := &Credentials{ClientID: "id", ClientSecret: "secret", JWTToken: "jwt"}
	private := NewClient(creds)
	m.SetPrivateClient(private)

	if m.readClient() != private {
		t.Error("with private client, readClient should return private client")
	}
}

func TestMonitor_SourceUserIDs_AllowsListedUser(t *testing.T) {
	var mu sync.Mutex
	var called bool

	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-ext-123")
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		called = true
		mu.Unlock()
	}, []string{"chat-1"}, []string{"user-A"}, false)

	// Message from allowed user → should be processed
	msg := makeWSMessage(Post{
		ID:        "p300",
		GroupID:   "chat-1",
		Type:      "TextMessage",
		Text:      "hello",
		CreatorID: "user-A",
		EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !called {
		t.Error("handler should be called for allowed user")
	}
	mu.Unlock()
}

func TestMonitor_SourceUserIDs_BlocksUnlistedUser(t *testing.T) {
	var mu sync.Mutex
	var called bool

	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-ext-123")
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		called = true
		mu.Unlock()
	}, []string{"chat-1"}, []string{"user-A"}, false)

	// Message from non-allowed user → should be ignored
	msg := makeWSMessage(Post{
		ID:        "p301",
		GroupID:   "chat-1",
		Type:      "TextMessage",
		Text:      "hello",
		CreatorID: "user-B",
		EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Error("handler should not be called for non-allowed user")
	}
}

func TestMonitor_SourceUserIDs_EmptyAllowsAll(t *testing.T) {
	var mu sync.Mutex
	var called bool

	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-ext-123")
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		called = true
		mu.Unlock()
	}, []string{"chat-1"}, nil, false)

	// Default semantics: empty allowlist means "allow all" (legacy compat).
	msg := makeWSMessage(Post{
		ID:        "p302",
		GroupID:   "chat-1",
		Type:      "TextMessage",
		Text:      "hello",
		CreatorID: "random-user",
		EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Error("default monitor should accept any sender when allowlist is empty")
	}
}

func TestMonitor_EnforceSenderAllowlist_EmptyDeniesAll(t *testing.T) {
	var mu sync.Mutex
	var called bool

	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-ext-123")
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		called = true
		mu.Unlock()
	}, []string{"chat-1"}, nil, false)
	m.EnforceSenderAllowlist()

	msg := makeWSMessage(Post{
		ID:        "p303",
		GroupID:   "chat-1",
		Type:      "TextMessage",
		Text:      "hello",
		CreatorID: "random-user",
		EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Error("strict mode with empty allowlist must drop all senders")
	}
}

func TestMonitor_AddTrustedSender_EnablesUser(t *testing.T) {
	var mu sync.Mutex
	var called bool

	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-ext-123")
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		called = true
		mu.Unlock()
	}, []string{"chat-1"}, nil, false)
	m.EnforceSenderAllowlist()

	if m.HasTrustedSenders() {
		t.Fatal("expected no trusted senders before AddTrustedSender")
	}
	m.AddTrustedSender("trusted-owner")
	if !m.HasTrustedSenders() {
		t.Fatal("expected HasTrustedSenders to be true after AddTrustedSender")
	}

	msg := makeWSMessage(Post{
		ID:        "p304",
		GroupID:   "chat-1",
		Type:      "TextMessage",
		Text:      "hello",
		CreatorID: "trusted-owner",
		EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Error("handler should be called for an explicitly trusted sender")
	}
}

func TestMonitor_EnforceSenderAllowlist_BlocksUntrusted(t *testing.T) {
	var mu sync.Mutex
	var called bool

	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-ext-123")
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		called = true
		mu.Unlock()
	}, []string{"chat-1"}, nil, false)
	m.EnforceSenderAllowlist()
	m.AddTrustedSender("trusted-owner")

	msg := makeWSMessage(Post{
		ID:        "p305",
		GroupID:   "chat-1",
		Type:      "TextMessage",
		Text:      "hello",
		CreatorID: "stranger",
		EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Error("strict mode must drop messages from untrusted senders")
	}
}

func newBotMonitorWithGroups(groups []string, mentionOnly bool, handler MessageHandler) (*Monitor, *Client) {
	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-ext-123")
	bot.SetDMChatID("dm-chat")
	m := NewMonitor(bot, handler, groups, nil, mentionOnly)
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
		t.Error("with group_mention_only=false, group message without mention should be processed by bot")
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
		got := util.Truncate(tt.s, tt.n)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
}

// --- handleWSMessage edge cases ---

func TestHandleWSMessage_MalformedJSON(t *testing.T) {
	var called bool
	m := newTestMonitor("chat-1", func(ctx context.Context, client *Client, _ *Client, post Post) {
		called = true
	})

	m.handleWSMessage(context.Background(), []byte(`not valid json`))
	time.Sleep(50 * time.Millisecond)

	if called {
		t.Error("handler should not be called for malformed JSON")
	}
}

func TestHandleWSMessage_NoEventType(t *testing.T) {
	var called bool
	m := newTestMonitor("chat-1", func(ctx context.Context, client *Client, _ *Client, post Post) {
		called = true
	})

	// Post with no eventType
	msg := makeWSMessage(Post{
		ID:      "p10",
		GroupID: "chat-1",
		Type:    "TextMessage",
		Text:    "hello",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	if called {
		t.Error("handler should not be called when eventType is empty")
	}
}

func TestHandleWSMessage_NonPostAddedEvent(t *testing.T) {
	var called bool
	m := newTestMonitor("chat-1", func(ctx context.Context, client *Client, _ *Client, post Post) {
		called = true
	})

	msg := makeWSMessage(Post{
		ID:        "p11",
		GroupID:   "chat-1",
		Type:      "TextMessage",
		Text:      "hello",
		CreatorID: "user-1",
		EventType: "PostRemoved",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	if called {
		t.Error("handler should not be called for PostRemoved event type")
	}
}

func TestHandleWSMessage_SingleObjectFormat(t *testing.T) {
	var mu sync.Mutex
	var received []Post
	m := newTestMonitor("chat-1", func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		received = append(received, post)
		mu.Unlock()
	})

	// Send as a single WSEvent object (not an array)
	event := WSEvent{
		UUID:  "test-uuid",
		Event: "/team-messaging/v1/posts",
		Body: Post{
			ID:        "p12",
			GroupID:   "chat-1",
			Type:      "TextMessage",
			Text:      "hello single",
			CreatorID: "user-1",
			EventType: "PostAdded",
		},
	}
	data, _ := json.Marshal(event)
	m.handleWSMessage(context.Background(), data)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 post, got %d", len(received))
	}
	if received[0].ID != "p12" {
		t.Errorf("expected p12, got %s", received[0].ID)
	}
}

func TestMonitor_EvictExpired(t *testing.T) {
	m := &Monitor{sentPosts: make(map[string]time.Time)}

	// Add a mix of expired and valid entries
	m.mu.Lock()
	m.sentPosts["expired-1"] = time.Now().Add(-10 * time.Minute)
	m.sentPosts["expired-2"] = time.Now().Add(-6 * time.Minute)
	m.sentPosts["valid-1"] = time.Now().Add(-1 * time.Minute)
	m.sentPosts["valid-2"] = time.Now()
	m.mu.Unlock()

	// Trigger eviction via MarkSentPost with stale lastEvict
	m.mu.Lock()
	m.lastEvict = time.Now().Add(-2 * time.Minute)
	m.mu.Unlock()

	m.MarkSentPost("new-post")

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.sentPosts["expired-1"]; ok {
		t.Error("expected expired-1 to be evicted")
	}
	if _, ok := m.sentPosts["expired-2"]; ok {
		t.Error("expected expired-2 to be evicted")
	}
	if _, ok := m.sentPosts["valid-1"]; !ok {
		t.Error("expected valid-1 to still exist")
	}
	if _, ok := m.sentPosts["valid-2"]; !ok {
		t.Error("expected valid-2 to still exist")
	}
	if _, ok := m.sentPosts["new-post"]; !ok {
		t.Error("expected new-post to exist")
	}
}

func TestNewMonitor_DMChatInAllowed(t *testing.T) {
	bot := NewBotClient("", "fake-bot-token")
	bot.SetDMChatID("dm-special")
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {}, []string{"chat-1"}, nil, false)

	if !m.allowedChatIDs["chat-1"] {
		t.Error("expected chat-1 in allowed list")
	}
	if !m.allowedChatIDs["dm-special"] {
		t.Error("expected dm-special (bot DM) in allowed list")
	}
}

func TestMonitor_ConcurrentMarkAndCheck(t *testing.T) {
	m := &Monitor{sentPosts: make(map[string]time.Time)}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "post-" + string(rune('A'+i%26))
			m.MarkSentPost(id)
			m.IsSentPost(id)
		}(i)
	}
	wg.Wait()

	// Just verify no panics and some posts exist
	m.mu.Lock()
	count := len(m.sentPosts)
	m.mu.Unlock()
	if count == 0 {
		t.Error("expected some sent posts after concurrent access")
	}
}

func TestMonitor_HandleWSMessage_AnswerPrefix(t *testing.T) {
	var called bool
	m := newTestMonitor("chat-1", func(ctx context.Context, client *Client, _ *Client, post Post) {
		called = true
	})

	msg := makeWSMessage(Post{
		ID:        "p20",
		GroupID:   "chat-1",
		Type:      "TextMessage",
		Text:      "--------answer--------\nSome bot reply\n---------end----------",
		CreatorID: "bot-1",
		EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	if called {
		t.Error("handler should not be called for answer-prefixed bot messages")
	}
}

func TestMonitor_CalcBackoff_FirstFailure(t *testing.T) {
	m := &Monitor{sentPosts: make(map[string]time.Time)}

	m.failures = 0
	d := m.calcBackoff()
	if d != initialBackoff {
		t.Errorf("failures=0: got %v, want %v", d, initialBackoff)
	}
}

func TestMonitor_IsBotMentioned_EmptyOwnerID(t *testing.T) {
	bot := NewBotClient("", "fake-bot-token")
	// Don't set OwnerID
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {}, nil, nil, true)

	got := m.isBotMentioned([]Mention{{ID: "someone"}})
	if got {
		t.Error("isBotMentioned should return false when OwnerID is empty")
	}
}

func TestMonitor_Run_CancelledContext(t *testing.T) {
	bot := NewBotClient("", "fake-bot-token")
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {}, nil, nil, false)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := m.Run(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestMonitor_Run_ConnectFailThenCancel(t *testing.T) {
	// Create a bot with auth that will fail on GetWSToken (needs to reach /restapi/oauth/wstoken)
	// Use a server that returns 500 for wstoken, then cancel after first failure
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/restapi/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(TokenResponse{AccessToken: "tok", ExpiresIn: 3600})
		case "/restapi/oauth/wstoken":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("ws error"))
		}
	}))
	defer srv.Close()

	auth := NewAuth("id", "secret", "jwt", srv.URL)
	auth.httpClient = srv.Client()
	auth.SetTokenForTest("tok", time.Now().Add(time.Hour))
	bot := &Client{
		serverURL:  srv.URL,
		auth:       auth,
		isBot:      true,
		httpClient: srv.Client(),
	}
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {}, nil, nil, false)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	err := m.Run(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if m.failures < 1 {
		t.Error("expected at least 1 failure recorded")
	}
}

func TestMonitor_CalcBackoff_Progression(t *testing.T) {
	m := &Monitor{sentPosts: make(map[string]time.Time)}

	tests := []struct {
		failures int
		want     time.Duration
	}{
		{1, initialBackoff},
		{2, initialBackoff * 2},
		{3, initialBackoff * 4},
		{4, initialBackoff * 8},
		{5, initialBackoff * 16},
	}
	for _, tt := range tests {
		m.failures = tt.failures
		got := m.calcBackoff()
		expected := tt.want
		if expected > maxBackoff {
			expected = maxBackoff
		}
		if got != expected {
			t.Errorf("failures=%d: got %v, want %v", tt.failures, got, expected)
		}
	}
}

// TestMonitor_ChatUserAllow_AdmitsNonSourceUser confirms that a user
// not on source_user_ids but listed in chat_user_allow for the
// destination chat is dispatched normally.
func TestMonitor_ChatUserAllow_AdmitsNonSourceUser(t *testing.T) {
	var mu sync.Mutex
	var called bool
	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-1")
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		called = true
		mu.Unlock()
	}, []string{"chat-A"}, []string{"trusted-1"}, false)
	m.SetAllowAllSenders(false)
	m.SetChatUserAllow(map[string][]string{"chat-A": {"guest-1"}})

	msg := makeWSMessage(Post{
		ID: "p1", GroupID: "chat-A", Type: "TextMessage",
		Text: "hi", CreatorID: "guest-1", EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Errorf("chat_user_allow entry should admit a non-source_user_ids sender")
	}
}

// TestMonitor_MentionAuthorize_RoutedAndNotDispatched verifies that a
// non-trusted user's @mention in a group chat triggers the
// authorize-mention callback INSTEAD of the regular handler.
func TestMonitor_MentionAuthorize_RoutedAndNotDispatched(t *testing.T) {
	var mu sync.Mutex
	var dispatched bool
	var authorized bool
	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-1")
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		dispatched = true
		mu.Unlock()
	}, []string{"chat-A"}, []string{"trusted-1"}, true)
	m.SetAllowAllSenders(false)
	m.SetMentionAuthorize(func(ctx context.Context, replyClient *Client, readClient *Client, post Post) {
		mu.Lock()
		authorized = true
		mu.Unlock()
	})

	msg := makeWSMessage(Post{
		ID: "p1", GroupID: "chat-A", Type: "TextMessage",
		Text: "@bot hi", CreatorID: "guest-1", EventType: "PostAdded",
		Mentions: []Mention{{ID: "bot-1", Type: "Person"}},
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(80 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if dispatched {
		t.Errorf("normal handler must NOT be called for a non-trusted mention while authorize-mention is enabled")
	}
	if !authorized {
		t.Errorf("authorize-mention callback should have been invoked")
	}
}

// TestMonitor_MentionAuthorize_DisabledFallsBackToDeny verifies that
// without an authorize hook a non-trusted mention is silently dropped
// (current strict-allowlist behavior).
func TestMonitor_MentionAuthorize_DisabledFallsBackToDeny(t *testing.T) {
	var mu sync.Mutex
	var dispatched bool
	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-1")
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		dispatched = true
		mu.Unlock()
	}, []string{"chat-A"}, []string{"trusted-1"}, true)
	m.SetAllowAllSenders(false)

	msg := makeWSMessage(Post{
		ID: "p1", GroupID: "chat-A", Type: "TextMessage",
		Text: "@bot hi", CreatorID: "guest-1", EventType: "PostAdded",
		Mentions: []Mention{{ID: "bot-1", Type: "Person"}},
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if dispatched {
		t.Errorf("non-trusted mention must be dropped when authorize hook is not installed")
	}
}

// TestMonitor_StrictMode_EmptySourceUsers_OnlyChatAllow_ScopesPerChat
// is a regression test for the empty-allowlist Layer 0 bypass. With
// strict mode enabled (allowAllSenders=false), no entries on
// source_user_ids, and a single (chat-A, guest-1) pair on
// chat_user_allow:
//
//   - guest-1 in chat-A      → dispatch (per-chat allowed)
//   - guest-1 in chat-B      → drop (per-chat allowlist is per-chat)
//   - other-user in chat-A   → drop (not on any allowlist)
//
// Before the fix, `senderTrusted := len(allowedUserIDs) == 0 || …`
// short-circuited to true whenever the global allowlist was empty,
// silently widening Layer 0 even in strict mode.
func TestMonitor_StrictMode_EmptySourceUsers_OnlyChatAllow_ScopesPerChat(t *testing.T) {
	dispatchCases := []struct {
		name           string
		chat           string
		creator        string
		wantDispatched bool
	}{
		{name: "trusted user in trusted chat", chat: "chat-A", creator: "guest-1", wantDispatched: true},
		{name: "trusted user in different chat", chat: "chat-B", creator: "guest-1", wantDispatched: false},
		{name: "untrusted user in trusted chat", chat: "chat-A", creator: "rando", wantDispatched: false},
	}
	for _, tc := range dispatchCases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var dispatched bool
			bot := NewBotClient("", "fake-bot-token")
			bot.SetOwnerID("bot-1")
			m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {
				mu.Lock()
				dispatched = true
				mu.Unlock()
			}, []string{"chat-A", "chat-B"}, nil, false)
			m.EnforceSenderAllowlist()
			m.SetChatUserAllow(map[string][]string{"chat-A": {"guest-1"}})

			msg := makeWSMessage(Post{
				ID: "p-" + tc.creator + "-" + tc.chat, GroupID: tc.chat, Type: "TextMessage",
				Text: "hi", CreatorID: tc.creator, EventType: "PostAdded",
			})
			m.handleWSMessage(context.Background(), msg)
			time.Sleep(50 * time.Millisecond)

			mu.Lock()
			defer mu.Unlock()
			if dispatched != tc.wantDispatched {
				t.Errorf("dispatched=%v, want %v (chat=%s creator=%s)", dispatched, tc.wantDispatched, tc.chat, tc.creator)
			}
		})
	}
}

// TestMonitor_StrictMode_EmptySourceUsers_AuthorizeMentionStillFires
// is a companion regression test: when source_user_ids is empty AND
// chat_user_allow is non-empty (so the empty-allowlist startup guard
// does not drop), a non-trusted user's @mention in a group chat that
// is in chat_ids must still hit the authorize-mention OOB callback —
// not be silently dispatched as "trusted".
//
// Before the fix, `senderTrusted` evaluated to true for every sender
// in this configuration, so the authorize-mention branch (gated on
// !senderTrusted) was unreachable.
func TestMonitor_StrictMode_EmptySourceUsers_AuthorizeMentionStillFires(t *testing.T) {
	var mu sync.Mutex
	var dispatched bool
	var authorized bool
	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-1")
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		dispatched = true
		mu.Unlock()
	}, []string{"chat-A"}, nil, true)
	m.EnforceSenderAllowlist()
	m.SetChatUserAllow(map[string][]string{"chat-A": {"guest-1"}})
	m.SetMentionAuthorize(func(ctx context.Context, replyClient *Client, readClient *Client, post Post) {
		mu.Lock()
		authorized = true
		mu.Unlock()
	})

	// rando is NOT on chat_user_allow; @mention should route to OOB.
	msg := makeWSMessage(Post{
		ID: "p1", GroupID: "chat-A", Type: "TextMessage",
		Text: "@bot hi", CreatorID: "rando", EventType: "PostAdded",
		Mentions: []Mention{{ID: "bot-1", Type: "Person"}},
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(80 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if dispatched {
		t.Errorf("non-trusted sender must NOT bypass to dispatch when only chat_user_allow is configured")
	}
	if !authorized {
		t.Errorf("authorize-mention callback should have fired for the non-trusted mention")
	}
}

// TestMonitor_LegacyAllowAll_EmptyAllowlistsTrustEveryone confirms the
// legacy semantic is preserved: when allowAllSenders=true (the default
// before EnforceSenderAllowlist), an empty source_user_ids and empty
// chat_user_allow trust every sender. This is the path used by tests
// and by deployments that explicitly opt out of strict mode.
func TestMonitor_LegacyAllowAll_EmptyAllowlistsTrustEveryone(t *testing.T) {
	var mu sync.Mutex
	var dispatched bool
	bot := NewBotClient("", "fake-bot-token")
	bot.SetOwnerID("bot-1")
	m := NewMonitor(bot, func(ctx context.Context, client *Client, _ *Client, post Post) {
		mu.Lock()
		dispatched = true
		mu.Unlock()
	}, []string{"chat-A"}, nil, false)
	// allowAllSenders=true is the constructor default.

	msg := makeWSMessage(Post{
		ID: "p1", GroupID: "chat-A", Type: "TextMessage",
		Text: "hi", CreatorID: "anyone", EventType: "PostAdded",
	})
	m.handleWSMessage(context.Background(), msg)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !dispatched {
		t.Errorf("legacy allowAllSenders=true with empty allowlists must trust every sender")
	}
}
