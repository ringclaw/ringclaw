package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"
)

type testAgent struct {
	reply string
}

func (a *testAgent) Chat(context.Context, string, string) (string, error) {
	return a.reply, nil
}

func (a *testAgent) ResetSession(context.Context, string) (string, error) {
	return "", nil
}

func (a *testAgent) SetCwd(string) {}

func (a *testAgent) Info() agent.AgentInfo {
	return agent.AgentInfo{Name: "test-agent", Type: "test"}
}

func newTestHandler() *Handler {
	return &Handler{agents: make(map[string]agent.Agent), allowAllSenders: true}
}

func TestParseCommand_NoPrefix(t *testing.T) {
	h := newTestHandler()
	names, msg := h.parseCommand("hello world")
	if len(names) != 0 {
		t.Errorf("expected nil names, got %v", names)
	}
	if msg != "hello world" {
		t.Errorf("expected full text, got %q", msg)
	}
}

func TestParseCommand_SlashWithAgent(t *testing.T) {
	h := newTestHandler()
	names, msg := h.parseCommand("/claude explain this code")
	if len(names) != 1 || names[0] != "claude" {
		t.Errorf("expected [claude], got %v", names)
	}
	if msg != "explain this code" {
		t.Errorf("expected 'explain this code', got %q", msg)
	}
}

func TestParseCommand_MultiAgent(t *testing.T) {
	h := newTestHandler()
	names, msg := h.parseCommand("/cc /cx hello")
	if len(names) != 2 || names[0] != "claude" || names[1] != "codex" {
		t.Errorf("expected [claude codex], got %v", names)
	}
	if msg != "hello" {
		t.Errorf("expected 'hello', got %q", msg)
	}
}

func TestParseCommand_MultiAgentDedup(t *testing.T) {
	h := newTestHandler()
	names, msg := h.parseCommand("/cc /cc hello")
	if len(names) != 1 || names[0] != "claude" {
		t.Errorf("expected [claude] (deduped), got %v", names)
	}
	if msg != "hello" {
		t.Errorf("expected 'hello', got %q", msg)
	}
}

func TestParseCommand_SwitchOnly(t *testing.T) {
	h := newTestHandler()
	names, msg := h.parseCommand("/claude")
	if len(names) != 1 || names[0] != "claude" {
		t.Errorf("expected [claude], got %v", names)
	}
	if msg != "" {
		t.Errorf("expected empty message, got %q", msg)
	}
}

func TestParseCommand_Alias(t *testing.T) {
	h := newTestHandler()
	names, msg := h.parseCommand("/cc write a function")
	if len(names) != 1 || names[0] != "claude" {
		t.Errorf("expected [claude] from /cc alias, got %v", names)
	}
	if msg != "write a function" {
		t.Errorf("expected 'write a function', got %q", msg)
	}
}

func TestParseCommand_CustomAlias(t *testing.T) {
	h := newTestHandler()
	h.customAliases = map[string]string{"ai": "claude", "c": "claude"}
	names, msg := h.parseCommand("/ai hello")
	if len(names) != 1 || names[0] != "claude" {
		t.Errorf("expected [claude] from custom alias, got %v", names)
	}
	if msg != "hello" {
		t.Errorf("expected 'hello', got %q", msg)
	}
}

func TestResolveAlias(t *testing.T) {
	h := newTestHandler()
	tests := map[string]string{
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
	for alias, want := range tests {
		got := h.resolveAlias(alias)
		if got != want {
			t.Errorf("resolveAlias(%q) = %q, want %q", alias, got, want)
		}
	}
	// Unknown alias returns itself
	if got := h.resolveAlias("unknown"); got != "unknown" {
		t.Errorf("resolveAlias(unknown) = %q, want %q", got, "unknown")
	}
	// Custom alias takes priority over built-in
	h.customAliases = map[string]string{"cc": "custom-claude"}
	if got := h.resolveAlias("cc"); got != "custom-claude" {
		t.Errorf("resolveAlias(cc) with custom = %q, want custom-claude", got)
	}
}

func TestWrapAnswer(t *testing.T) {
	got := wrapAnswer("hello")
	if got != "--------answer--------\nhello\n---------end----------" {
		t.Errorf("unexpected wrap: %q", got)
	}
}

func TestBuildHelpText(t *testing.T) {
	text := buildHelpText()
	if text == "" {
		t.Error("help text is empty")
	}
	if !strings.Contains(text, "/info") {
		t.Error("help text should mention /info")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{90 * time.Second, "1m 30s"},
		{2*time.Hour + 15*time.Minute, "2h 15m 0s"},
		{25*time.Hour + 30*time.Minute, "1d 1h 30m"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestHandleChatInfo_CurrentChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ringcentral.Chat{
			ID: "c1", Name: "General", Type: "Team",
			Members: []ringcentral.ChatMember{{ID: "u1"}, {ID: "u2"}, {ID: "u3"}},
		})
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	result := handleChatInfo(context.Background(), client, "c1", "/chatinfo")
	if !strings.Contains(result, "General") || !strings.Contains(result, "Team") || !strings.Contains(result, "3") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandleChatInfo_SpecificChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ringcentral.Chat{
			ID: "c2", Name: "Backend", Type: "Group",
			Members: []ringcentral.ChatMember{{ID: "u1"}},
		})
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	result := handleChatInfo(context.Background(), client, "c1", "/chatinfo c2")
	if !strings.Contains(result, "Backend") || !strings.Contains(result, "c2") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestBuildHelpText_IncludesChatinfo(t *testing.T) {
	help := buildHelpText()
	if !strings.Contains(help, "/chatinfo") {
		t.Error("help text should include /chatinfo")
	}
	if !strings.Contains(help, "lock") {
		t.Error("help text should include lock for notes")
	}
}

func TestRouteSummarize_GroupDisabled(t *testing.T) {
	var sentTexts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/posts") {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			sentTexts = append(sentTexts, req["text"])
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	h := newTestHandler()
	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("owner-1")

	handled := h.routeSummarize(context.Background(), bot, bot, ringcentral.Post{
		GroupID:   "group-1",
		CreatorID: "owner-1",
		Text:      "总结一下",
	}, true)
	if !handled {
		t.Fatal("expected summarize route to handle the message")
	}
	if len(sentTexts) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(sentTexts))
	}
	if !strings.Contains(sentTexts[0], "group_summary_group_id") {
		t.Fatalf("expected missing group id hint in reply, got %q", sentTexts[0])
	}
}

func TestRouteSummarize_GroupEnabledWithoutConfiguredGroupID(t *testing.T) {
	var sentTexts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/posts") {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			sentTexts = append(sentTexts, req["text"])
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	h := newTestHandler()
	h.SetGroupSummaryConfig("", 42)
	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("owner-1")

	handled := h.routeSummarize(context.Background(), bot, bot, ringcentral.Post{
		GroupID:   "group-1",
		CreatorID: "owner-1",
		Text:      "总结一下",
	}, true)
	if !handled {
		t.Fatal("expected summarize route to handle the message")
	}
	if len(sentTexts) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(sentTexts))
	}
	if !strings.Contains(sentTexts[0], "group_summary_group_id") {
		t.Fatalf("expected missing group id hint in reply, got %q", sentTexts[0])
	}
}

func TestRouteSummarize_GroupEnabledButWrongGroup(t *testing.T) {
	var sentTexts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/posts") {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			sentTexts = append(sentTexts, req["text"])
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	h := newTestHandler()
	h.SetGroupSummaryConfig("group-allowed", 42)
	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("owner-1")

	handled := h.routeSummarize(context.Background(), bot, bot, ringcentral.Post{
		GroupID:   "group-1",
		CreatorID: "owner-1",
		Text:      "总结一下",
	}, true)
	if !handled {
		t.Fatal("expected summarize route to handle the message")
	}
	if len(sentTexts) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(sentTexts))
	}
	if !strings.Contains(sentTexts[0], "group-allowed") {
		t.Fatalf("expected configured group id in reply, got %q", sentTexts[0])
	}
}

func TestRouteSummarize_GroupRejectsOtherUserTarget(t *testing.T) {
	var sentTexts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/posts") {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			sentTexts = append(sentTexts, req["text"])
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	h := newTestHandler()
	h.SetGroupSummaryConfig("group-1", 42)
	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("bot-1")

	handled := h.routeSummarize(context.Background(), bot, bot, ringcentral.Post{
		GroupID:   "group-1",
		CreatorID: "owner-1",
		Text:      "总结 ![:Person](user-2) 的消息",
		Mentions: []ringcentral.Mention{
			{ID: "bot-1", Type: "Person", Name: "bot"},
			{ID: "user-2", Type: "Person", Name: "alice"},
		},
	}, true)
	if !handled {
		t.Fatal("expected summarize route to handle the message")
	}
	if len(sentTexts) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(sentTexts))
	}
	if !strings.Contains(strings.ToLower(sentTexts[0]), "don't have permission") {
		t.Fatalf("expected permission denial, got %q", sentTexts[0])
	}
}

func TestRouteSummarize_GroupRejectsOtherGroupTarget(t *testing.T) {
	var sentTexts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/posts") {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			sentTexts = append(sentTexts, req["text"])
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	h := newTestHandler()
	h.SetGroupSummaryConfig("group-1", 42)
	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("bot-1")

	handled := h.routeSummarize(context.Background(), bot, bot, ringcentral.Post{
		GroupID:   "group-1",
		CreatorID: "owner-1",
		Text:      "总结其他群的消息",
	}, true)
	if !handled {
		t.Fatal("expected summarize route to handle the message")
	}
	if len(sentTexts) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(sentTexts))
	}
	if !strings.Contains(strings.ToLower(sentTexts[0]), "don't have permission") {
		t.Fatalf("expected permission denial, got %q", sentTexts[0])
	}
}

func TestRouteSummarize_GroupEnabledUsesConfiguredLimit(t *testing.T) {
	var gotRecordCount string
	var updatedText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/chats/group-1/posts":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "placeholder-1"})
		case r.Method == http.MethodGet && r.URL.Path == "/team-messaging/v1/chats/group-1":
			_ = json.NewEncoder(w).Encode(ringcentral.Chat{ID: "group-1", Name: "General"})
		case r.Method == http.MethodGet && r.URL.Path == "/team-messaging/v1/chats/group-1/posts":
			gotRecordCount = r.URL.Query().Get("recordCount")
			_ = json.NewEncoder(w).Encode(ringcentral.PostList{
				Records: []ringcentral.Post{
					{
						ID:           "m1",
						GroupID:      "group-1",
						Text:         "hello team",
						CreatorID:    "glip-user-1",
						CreationTime: time.Now().UTC().Format(time.RFC3339),
					},
				},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/team-messaging/v1/chats/group-1/posts/placeholder-1":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			updatedText = req["text"]
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "placeholder-1", Text: updatedText})
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer srv.Close()

	h := newTestHandler()
	h.SetGroupSummaryConfig("group-1", 42)
	h.SetDefaultAgent("test", &testAgent{reply: "group summary"})

	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("owner-1")

	handled := h.routeSummarize(context.Background(), bot, bot, ringcentral.Post{
		GroupID:   "group-1",
		CreatorID: "owner-1",
		Text:      "总结一下最近消息",
	}, true)
	if !handled {
		t.Fatal("expected summarize route to handle the message")
	}
	if gotRecordCount != "42" {
		t.Fatalf("expected recordCount=42, got %q", gotRecordCount)
	}
	if updatedText != "group summary" {
		t.Fatalf("expected final summarized reply, got %q", updatedText)
	}
}

func TestInferMediaType(t *testing.T) {
	tests := []struct {
		name, want string
	}{
		{"photo.png", "image/png"},
		{"photo.PNG", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"photo.gif", "image/gif"},
		{"photo.webp", "image/webp"},
		{"document.pdf", ""},
		{"file.txt", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := inferMediaType(tt.name); got != tt.want {
			t.Errorf("inferMediaType(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestImageMediaTypes(t *testing.T) {
	supported := []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/jpg"}
	for _, mt := range supported {
		if !imageMediaTypes[mt] {
			t.Errorf("expected %q to be supported", mt)
		}
	}
	unsupported := []string{"image/bmp", "application/pdf", "text/plain", ""}
	for _, mt := range unsupported {
		if imageMediaTypes[mt] {
			t.Errorf("expected %q to NOT be supported", mt)
		}
	}
}

func TestExtractImageAttachments(t *testing.T) {
	imgData := []byte("fake-png-data")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(imgData)
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	post := ringcentral.Post{
		Attachments: []ringcentral.Attachment{
			{ID: "a1", ContentURI: srv.URL + "/img1.png", Name: "screenshot.png", MediaType: "image/png"},
			{ID: "a2", ContentURI: srv.URL + "/img2.jpg", Name: "photo.jpg", MediaType: "image/jpeg"},
			{ID: "a3", ContentURI: "", Name: "nourl.png"},           // no URI — skipped
			{ID: "a4", ContentURI: srv.URL + "/f.pdf", Name: "f.pdf", MediaType: "application/pdf"}, // not image — skipped
		},
	}

	images := extractImageAttachments(context.Background(), client, post)
	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}
	if images[0].Name != "screenshot.png" {
		t.Errorf("expected screenshot.png, got %s", images[0].Name)
	}
}

func TestExtractImageAttachments_MaxLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("data"))
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	var atts []ringcentral.Attachment
	for i := 0; i < 10; i++ {
		atts = append(atts, ringcentral.Attachment{
			ID: "a", ContentURI: srv.URL + "/img.png", Name: "img.png", MediaType: "image/png",
		})
	}
	post := ringcentral.Post{Attachments: atts}

	images := extractImageAttachments(context.Background(), client, post)
	if len(images) != maxImages {
		t.Fatalf("expected %d images (max), got %d", maxImages, len(images))
	}
}

// mockAgent for testing chatWithAgentOrImages
type mockImageAgent struct {
	lastImages int
}

func (m *mockImageAgent) Chat(_ context.Context, _, msg string) (string, error) {
	return "text-only: " + msg, nil
}
func (m *mockImageAgent) ChatWithImages(_ context.Context, _, msg string, imgs []agent.ImageAttachment) (string, error) {
	m.lastImages = len(imgs)
	return "with-images: " + msg, nil
}
func (m *mockImageAgent) ResetSession(_ context.Context, _ string) (string, error) { return "", nil }
func (m *mockImageAgent) SetCwd(_ string)                                          {}
func (m *mockImageAgent) Info() agent.AgentInfo {
	return agent.AgentInfo{Name: "mock", Type: "test"}
}

type mockTextOnlyAgent struct{}

func (m *mockTextOnlyAgent) Chat(_ context.Context, _, msg string) (string, error) {
	return "text: " + msg, nil
}
func (m *mockTextOnlyAgent) ResetSession(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *mockTextOnlyAgent) SetCwd(_ string)               {}
func (m *mockTextOnlyAgent) Info() agent.AgentInfo {
	return agent.AgentInfo{Name: "text-only", Type: "test"}
}

func TestChatWithAgentOrImages_ImageSupporter(t *testing.T) {
	h := &Handler{}
	ag := &mockImageAgent{}
	imgs := []agent.ImageAttachment{{Data: []byte("x"), MediaType: "image/png", Name: "a.png"}}
	reply, err := h.chatWithAgentOrImages(context.Background(), ag, "conv1", "hello", imgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(reply, "with-images:") {
		t.Errorf("expected ChatWithImages path, got %q", reply)
	}
	if ag.lastImages != 1 {
		t.Errorf("expected 1 image passed, got %d", ag.lastImages)
	}
}

func TestChatWithAgentOrImages_TextFallback(t *testing.T) {
	h := &Handler{}
	ag := &mockTextOnlyAgent{}
	imgs := []agent.ImageAttachment{{Data: []byte("x"), MediaType: "image/png", Name: "a.png"}}
	reply, err := h.chatWithAgentOrImages(context.Background(), ag, "conv1", "hello", imgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(reply, "text:") {
		t.Errorf("expected text-only path, got %q", reply)
	}
	if !strings.Contains(reply, "1 image(s) were attached") {
		t.Errorf("expected fallback note, got %q", reply)
	}
}

func TestChatWithAgentOrImages_NoImages(t *testing.T) {
	h := &Handler{}
	ag := &mockImageAgent{}
	reply, err := h.chatWithAgentOrImages(context.Background(), ag, "conv1", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(reply, "text-only:") {
		t.Errorf("expected Chat path (no images), got %q", reply)
	}
}

// --- HandleMessage tests ---

// newTestRC creates an httptest.Server that handles common RC API patterns for testing.
// It records sent post texts and returns placeholder post IDs.
func newTestRC(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var sentTexts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/posts") {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			sentTexts = append(sentTexts, req["text"])
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
			return
		}
		if r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/posts/") {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			sentTexts = append(sentTexts, req["text"])
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1", Text: req["text"]})
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Adaptive card creation
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/adaptive-cards") {
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "card-1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	return srv, &sentTexts
}

func getSentTexts(texts *[]string) []string {
	// Small sleep to let async operations complete
	time.Sleep(50 * time.Millisecond)
	return *texts
}

func TestHandleMessage_EmptyText(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "msg-empty-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "",
	})

	got := getSentTexts(sentTexts)
	if len(got) != 0 {
		t.Errorf("expected no messages sent for empty text, got %d: %v", len(got), got)
	}
}

func TestHandleMessage_WhitespaceOnly(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "msg-ws-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "   \n  ",
	})

	got := getSentTexts(sentTexts)
	if len(got) != 0 {
		t.Errorf("expected no messages for whitespace-only text, got %d", len(got))
	}
}

func TestHandleMessage_Dedup(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	post := ringcentral.Post{
		ID:        "dedup-post-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/reload",
	}

	h.HandleMessage(context.Background(), client, client, post)
	time.Sleep(50 * time.Millisecond)
	firstCount := len(*sentTexts)
	if firstCount == 0 {
		t.Fatal("expected at least 1 message from first /reload")
	}

	// Send same post again — should be deduped
	h.HandleMessage(context.Background(), client, client, post)
	time.Sleep(50 * time.Millisecond)
	secondCount := len(*sentTexts)
	if secondCount != firstCount {
		t.Errorf("expected dedup (count=%d), but got %d messages after second send", firstCount, secondCount)
	}
}

func TestHandleMessage_BotMentionStripped(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	// /reload should route to built-in command even when prefixed with bot mention
	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "mention-strip-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "![:Person](bot-1) /reload",
	})

	got := getSentTexts(sentTexts)
	if len(got) == 0 {
		t.Fatal("expected a reload reply after bot mention stripped")
	}
	if !strings.Contains(got[0], "not available") {
		t.Errorf("expected 'not available' reply, got %q", got[0])
	}
}

func TestStripForwardedPrefix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "forwarded message",
			input:    "John Lin posted in ![:Team](156364201990)\n> 哦，那我针对 fyi 过来的消息不处理就是了",
			expected: "哦，那我针对 fyi 过来的消息不处理就是了",
		},
		{
			name:     "multi-line forwarded",
			input:    "Alice posted in ![:Team](123)\n> line one\n> line two",
			expected: "line one\nline two",
		},
		{
			name:     "normal message",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "empty after prefix",
			input:    "Bob posted in ![:Team](456)\n> ",
			expected: "Bob posted in ![:Team](456)\n> ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripForwardedPrefix(tt.input)
			if got != tt.expected {
				t.Errorf("stripForwardedPrefix(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestHandleMessage_HelpCommand(t *testing.T) {
	srv, _ := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	// /help should not error
	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "help-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/help",
	})
	// No crash = success. The card or text reply should be sent.
}

func TestHandleMessage_ReloadCommand(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "reload-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/reload",
	})

	got := getSentTexts(sentTexts)
	if len(got) == 0 {
		t.Fatal("expected a reload reply")
	}
	if !strings.Contains(got[0], "not available") {
		t.Errorf("expected 'not available' since no reload func set, got %q", got[0])
	}
}

func TestHandleMessage_NewCommand(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "new-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/new",
	})

	got := getSentTexts(sentTexts)
	if len(got) == 0 {
		t.Fatal("expected a reply to /new")
	}
	if !strings.Contains(got[0], "No agent") {
		t.Errorf("expected 'No agent running' since no default agent, got %q", got[0])
	}
}

func TestHandleMessage_SwitchToKnownAgent(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	ag := &testAgent{reply: "hello"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)
	h.SetAgentMetas([]AgentMeta{{Name: "claude", Type: "test"}})

	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	// "/claude" with no message = switch default
	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "switch-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/claude",
	})

	got := getSentTexts(sentTexts)
	if len(got) == 0 {
		t.Fatal("expected a switch reply")
	}
	if !strings.Contains(got[0], "switch to claude") {
		t.Errorf("expected switch confirmation, got %q", got[0])
	}
}

func TestHandleMessage_NamedAgentDispatch(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	ag := &testAgent{reply: "agent reply here"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)
	h.SetAgentMetas([]AgentMeta{{Name: "claude", Type: "test"}})

	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "named-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/claude explain this code",
	})

	got := getSentTexts(sentTexts)
	found := false
	for _, t := range got {
		if strings.Contains(t, "agent reply here") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected agent reply in sent texts, got %v", got)
	}
}

func TestHandleMessage_DefaultAgentEchoMode(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "echo-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "hello world",
	})

	got := getSentTexts(sentTexts)
	found := false
	for _, t := range got {
		if strings.Contains(t, "[echo]") && strings.Contains(t, "hello world") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected echo mode reply, got %v", got)
	}
}

func TestHandleMessage_ActionCommand(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	// /task without subcommand should return usage help
	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "action-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/task",
	})

	got := getSentTexts(sentTexts)
	if len(got) == 0 {
		t.Fatal("expected action help reply")
	}
	if !strings.Contains(got[0], "Usage") {
		t.Errorf("expected usage help, got %q", got[0])
	}
}

func TestHandleMessage_UnknownSlashSentToDefault(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	// /unknownagent with no known agents should forward to default (echo mode)
	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "unknown-slash-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/unknownagent",
	})

	got := getSentTexts(sentTexts)
	found := false
	for _, t := range got {
		if strings.Contains(t, "[echo]") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected echo mode for unknown single agent, got %v", got)
	}
}

// --- switchDefault tests ---

func TestSwitchDefault_KnownAgent(t *testing.T) {
	ag := &testAgent{reply: "hi"}
	factory := func(_ context.Context, name string) agent.Agent {
		if name == "claude" {
			return ag
		}
		return nil
	}

	var savedName string
	saveFn := func(name string) error {
		savedName = name
		return nil
	}

	h := NewHandler(factory, saveFn, "test")
	h.SetDefaultAgent("codex", ag)

	result := h.switchDefault(context.Background(), "claude")
	if !strings.Contains(result, "switch to claude") {
		t.Errorf("expected switch confirmation, got %q", result)
	}
	if savedName != "claude" {
		t.Errorf("expected saved name 'claude', got %q", savedName)
	}
}

func TestSwitchDefault_UnknownAgent(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	result := h.switchDefault(context.Background(), "nonexistent")
	if !strings.Contains(result, "Failed") {
		t.Errorf("expected failure message, got %q", result)
	}
}

func TestSwitchDefault_CLIAgent(t *testing.T) {
	ag := &testAgent{reply: "hi"}
	// Override Info to return CLI type
	cliAgent := &cliTestAgent{}
	factory := func(_ context.Context, name string) agent.Agent {
		if name == "codex" {
			return cliAgent
		}
		return ag
	}
	h := NewHandler(factory, nil, "test")
	h.SetDefaultAgent("claude", ag)

	result := h.switchDefault(context.Background(), "codex")
	if !strings.Contains(result, "CLI mode") {
		t.Errorf("expected CLI mode warning, got %q", result)
	}
}

type cliTestAgent struct{}

func (a *cliTestAgent) Chat(_ context.Context, _, _ string) (string, error) { return "hi", nil }
func (a *cliTestAgent) ResetSession(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (a *cliTestAgent) SetCwd(_ string)        {}
func (a *cliTestAgent) Info() agent.AgentInfo {
	return agent.AgentInfo{Name: "codex", Type: "cli"}
}

// --- resetDefaultSession tests ---

func TestResetDefaultSession_NoAgent(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	result := h.resetDefaultSession(context.Background(), "conv-1")
	if result != "No agent running." {
		t.Errorf("expected 'No agent running.', got %q", result)
	}
}

func TestResetDefaultSession_Success(t *testing.T) {
	ag := &testAgent{reply: "hi"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)

	result := h.resetDefaultSession(context.Background(), "conv-1")
	if !strings.Contains(result, "New") && !strings.Contains(result, "session") {
		t.Errorf("expected new session message, got %q", result)
	}
	if !strings.Contains(result, "test-agent") {
		t.Errorf("expected agent name in result, got %q", result)
	}
}

// --- handleReload tests ---

func TestHandleReload_NoReloadFunc(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	result := h.handleReload()
	if result != "Reload is not available." {
		t.Errorf("expected 'Reload is not available.', got %q", result)
	}
}

func TestHandleReload_NoNewAgents(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	h.SetReloadAgents(func() ([]AgentMeta, map[string]string, []string) {
		return []AgentMeta{{Name: "claude", Type: "test"}}, nil, nil
	})

	result := h.handleReload()
	if !strings.Contains(result, "No new agents") {
		t.Errorf("expected 'No new agents detected', got %q", result)
	}
}

func TestHandleReload_WithNewAgents(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	h.SetReloadAgents(func() ([]AgentMeta, map[string]string, []string) {
		return []AgentMeta{
			{Name: "claude", Type: "test"},
			{Name: "gemini", Type: "test"},
		}, map[string]string{"gm": "gemini"}, []string{"gemini"}
	})

	result := h.handleReload()
	if !strings.Contains(result, "gemini") {
		t.Errorf("expected new agent 'gemini' in result, got %q", result)
	}
}

// --- getAgent tests ---

func TestGetAgent_FromCache(t *testing.T) {
	ag := &testAgent{reply: "cached"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)

	got, err := h.getAgent(context.Background(), "claude")
	if err != nil {
		t.Fatal(err)
	}
	if got != ag {
		t.Error("expected cached agent")
	}
}

func TestGetAgent_FromFactory(t *testing.T) {
	ag := &testAgent{reply: "new"}
	factory := func(_ context.Context, name string) agent.Agent {
		if name == "gemini" {
			return ag
		}
		return nil
	}

	h := NewHandler(factory, nil, "test")
	got, err := h.getAgent(context.Background(), "gemini")
	if err != nil {
		t.Fatal(err)
	}
	if got != ag {
		t.Error("expected factory-created agent")
	}

	// Second call should return from cache
	got2, err := h.getAgent(context.Background(), "gemini")
	if err != nil {
		t.Fatal(err)
	}
	if got2 != ag {
		t.Error("expected cached agent on second call")
	}
}

func TestGetAgent_FactoryNil(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	_, err := h.getAgent(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error when factory is nil")
	}
	if !strings.Contains(err.Error(), "no factory") {
		t.Errorf("expected 'no factory' error, got %q", err.Error())
	}
}

func TestGetAgent_FactoryReturnsNil(t *testing.T) {
	factory := func(_ context.Context, name string) agent.Agent {
		return nil
	}

	h := NewHandler(factory, nil, "test")
	_, err := h.getAgent(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error when factory returns nil")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("expected 'not available' error, got %q", err.Error())
	}
}

// --- dispatchToAgent test ---

func TestDispatchToAgent_Success(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	ag := &testAgent{reply: "agent response"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)

	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.dispatchToAgent(context.Background(), client, client,
		ringcentral.Post{ID: "disp-1", GroupID: "dm-1", CreatorID: "user-1"},
		ag, "test message", "placeholder-1")

	got := getSentTexts(sentTexts)
	found := false
	for _, t := range got {
		if strings.Contains(t, "agent response") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'agent response' in sent texts, got %v", got)
	}
}

// --- sendToDefaultAgent tests ---

func TestSendToDefaultAgent_NilAgent(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.sendToDefaultAgent(context.Background(), client, client,
		ringcentral.Post{ID: "nil-ag-1", GroupID: "dm-1", CreatorID: "user-1"},
		"hello")

	got := getSentTexts(sentTexts)
	found := false
	for _, t := range got {
		if strings.Contains(t, "[echo]") && strings.Contains(t, "hello") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected echo mode reply, got %v", got)
	}
}

func TestSendToDefaultAgent_WithAgent(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	ag := &testAgent{reply: "smart reply"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)

	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.sendToDefaultAgent(context.Background(), client, client,
		ringcentral.Post{ID: "with-ag-1", GroupID: "dm-1", CreatorID: "user-1"},
		"hello")

	got := getSentTexts(sentTexts)
	found := false
	for _, t := range got {
		if strings.Contains(t, "smart reply") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'smart reply' in sent texts, got %v", got)
	}
}

// --- broadcastToAgents test ---

func TestBroadcastToAgents_MultiAgent(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	ag1 := &testAgent{reply: "reply-from-claude"}
	ag2 := &testAgent{reply: "reply-from-codex"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag1)
	h.mu.Lock()
	h.agents["codex"] = ag2
	h.mu.Unlock()

	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.broadcastToAgents(context.Background(), client, client,
		ringcentral.Post{ID: "bcast-1", GroupID: "dm-1", CreatorID: "user-1"},
		[]string{"claude", "codex"}, "hello broadcast")

	got := getSentTexts(sentTexts)
	if len(got) < 2 {
		t.Fatalf("expected at least 2 replies from broadcast, got %d: %v", len(got), got)
	}
	var foundClaude, foundCodex bool
	for _, txt := range got {
		if strings.Contains(txt, "[claude]") {
			foundClaude = true
		}
		if strings.Contains(txt, "[codex]") {
			foundCodex = true
		}
	}
	if !foundClaude || !foundCodex {
		t.Errorf("expected both [claude] and [codex] replies, got %v", got)
	}
}

// --- isKnownAgent tests ---

func TestIsKnownAgent_FromCache(t *testing.T) {
	ag := &testAgent{reply: "hi"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)

	if !h.isKnownAgent("claude") {
		t.Error("expected claude to be known (cached)")
	}
}

func TestIsKnownAgent_FromMetas(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	h.SetAgentMetas([]AgentMeta{{Name: "gemini", Type: "http"}})

	if !h.isKnownAgent("gemini") {
		t.Error("expected gemini to be known (from metas)")
	}
}

func TestIsKnownAgent_Unknown(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	if h.isKnownAgent("nonexistent") {
		t.Error("expected nonexistent to not be known")
	}
}

// --- conversationIDForPost test ---

func TestConversationIDForPost_DM(t *testing.T) {
	client := ringcentral.NewBotClient("http://localhost", "token")
	client.SetDMChatID("dm-1")
	post := ringcentral.Post{GroupID: "dm-1", CreatorID: "user-1"}
	id := conversationIDForPost(client, post)
	if !strings.HasPrefix(id, "rc:dm:") {
		t.Errorf("expected dm prefix, got %q", id)
	}
}

func TestConversationIDForPost_Group(t *testing.T) {
	client := ringcentral.NewBotClient("http://localhost", "token")
	client.SetDMChatID("dm-1")
	post := ringcentral.Post{GroupID: "group-1", CreatorID: "user-1"}
	id := conversationIDForPost(client, post)
	if !strings.HasPrefix(id, "rc:chat:") {
		t.Errorf("expected chat prefix, got %q", id)
	}
}

// TestConversationIDForPost_PerUserIsolation locks in the security
// invariant from review Finding #4: within the same group chat, two
// different users MUST receive distinct conversationIDs so they cannot
// share or hijack each other's agent session.
func TestConversationIDForPost_PerUserIsolation(t *testing.T) {
	client := ringcentral.NewBotClient("http://localhost", "token")
	client.SetDMChatID("dm-1")

	chatID := "group-shared"
	a := conversationIDForPost(client, ringcentral.Post{GroupID: chatID, CreatorID: "alice"})
	b := conversationIDForPost(client, ringcentral.Post{GroupID: chatID, CreatorID: "bob"})

	if a == b {
		t.Fatalf("expected distinct conversation IDs for different users in same chat, got %q == %q", a, b)
	}
	if !strings.Contains(a, "alice") {
		t.Errorf("expected conversation ID to bind to creator id, got %q", a)
	}
	if !strings.Contains(b, "bob") {
		t.Errorf("expected conversation ID to bind to creator id, got %q", b)
	}
}

// TestConversationIDForPost_PerChatIsolation ensures the same user in
// different chats receives distinct conversationIDs. Without this,
// private DM history could leak into a group chat (or vice versa).
func TestConversationIDForPost_PerChatIsolation(t *testing.T) {
	client := ringcentral.NewBotClient("http://localhost", "token")
	client.SetDMChatID("dm-1")

	user := "alice"
	g1 := conversationIDForPost(client, ringcentral.Post{GroupID: "group-A", CreatorID: user})
	g2 := conversationIDForPost(client, ringcentral.Post{GroupID: "group-B", CreatorID: user})

	if g1 == g2 {
		t.Fatalf("expected distinct conversation IDs for same user in different chats, got %q == %q", g1, g2)
	}
}

// TestConversationIDForPost_DMAndGroupNamespaces verifies that DM and
// group chat IDs occupy disjoint namespaces, so a renamed/recycled chat
// ID can never collide with an existing DM session.
func TestConversationIDForPost_DMAndGroupNamespaces(t *testing.T) {
	client := ringcentral.NewBotClient("http://localhost", "token")
	client.SetDMChatID("shared-id")

	dmPost := ringcentral.Post{GroupID: "shared-id", CreatorID: "alice"}
	groupPost := ringcentral.Post{GroupID: "shared-id", CreatorID: "alice"}
	// Force the second post to evaluate as a group chat.
	other := ringcentral.NewBotClient("http://localhost", "token")
	other.SetDMChatID("different-id")

	dmID := conversationIDForPost(client, dmPost)
	groupID := conversationIDForPost(other, groupPost)

	if dmID == groupID {
		t.Fatalf("expected DM and group chat IDs to live in different namespaces, got %q == %q", dmID, groupID)
	}
	if !strings.HasPrefix(dmID, "rc:dm:") {
		t.Errorf("expected dm prefix, got %q", dmID)
	}
	if !strings.HasPrefix(groupID, "rc:chat:") {
		t.Errorf("expected chat prefix, got %q", groupID)
	}
}

// --- cleanSeenMsgs test ---

func TestCleanSeenMsgs(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	// Store an old message and a recent one
	h.seenMsgs.Store("old-msg", time.Now().Add(-10*time.Minute))
	h.seenMsgs.Store("new-msg", time.Now())
	h.seenMsgCount = 2

	h.cleanSeenMsgs()

	if _, ok := h.seenMsgs.Load("old-msg"); ok {
		t.Error("expected old message to be cleaned")
	}
	if _, ok := h.seenMsgs.Load("new-msg"); !ok {
		t.Error("expected new message to remain")
	}
}

// --- sendReplyWithActions test ---

func TestSendReplyWithActions_EmptyReply_DeletesPlaceholder(t *testing.T) {
	var deletedPostID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			parts := strings.Split(r.URL.Path, "/")
			deletedPostID = parts[len(parts)-1]
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "p1"})
	}))
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	// Use bot client with DM chat to avoid mention prefix being added
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.sendReplyWithActions(context.Background(), client, client,
		ringcentral.Post{GroupID: "dm-1", CreatorID: "user-1"},
		"   ", "placeholder-1")

	time.Sleep(50 * time.Millisecond)
	if deletedPostID != "placeholder-1" {
		t.Errorf("expected placeholder deletion, got %q", deletedPostID)
	}
}

// --- isPrivilegedCommand tests ---

func TestIsPrivilegedCommand(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"/new", true},
		{"/clear", true},
		{"/cwd /tmp", true},
		{"/cron list", true},
		{"/reload", true},
		{"/help", false},
		{"/info", false},
		{"/task list", false},
		{"hello", false},
	}
	for _, tt := range tests {
		got := isPrivilegedCommand(tt.text)
		if got != tt.want {
			t.Errorf("isPrivilegedCommand(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

// --- handleCwd tests ---

func TestHandleCwd_NoArg(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	result := h.handleCwd("/cwd")
	if !strings.Contains(result, "No agent") {
		t.Errorf("expected 'No agent running', got %q", result)
	}
}

func TestHandleCwd_WithAgent_NoArg(t *testing.T) {
	ag := &testAgent{reply: "hi"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)

	result := h.handleCwd("/cwd")
	if !strings.Contains(result, "cwd:") {
		t.Errorf("expected cwd info, got %q", result)
	}
}

func TestHandleCwd_RestrictedPath(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	ag := &testAgent{reply: "hi"}
	h.SetDefaultAgent("claude", ag)

	result := h.handleCwd("/cwd ~/.ssh")
	if !strings.Contains(result, "Denied") {
		t.Errorf("expected restricted path denial, got %q", result)
	}
}

// --- buildStatus tests ---

func TestBuildStatus_NoAgent(t *testing.T) {
	h := NewHandler(nil, nil, "v1.0.0")
	status := h.buildStatus()
	if !strings.Contains(status, "v1.0.0") {
		t.Errorf("expected version in status, got %q", status)
	}
	if !strings.Contains(status, "echo mode") {
		t.Errorf("expected 'echo mode' when no default agent, got %q", status)
	}
}

func TestBuildStatus_WithAgent(t *testing.T) {
	ag := &testAgent{reply: "hi"}
	h := NewHandler(nil, nil, "v2.0.0")
	h.SetDefaultAgent("claude", ag)
	h.SetAgentMetas([]AgentMeta{{Name: "claude", Type: "test", Model: "sonnet"}})

	status := h.buildStatus()
	if !strings.Contains(status, "claude") {
		t.Errorf("expected agent name in status, got %q", status)
	}
	if !strings.Contains(status, "v2.0.0") {
		t.Errorf("expected version in status, got %q", status)
	}
}

func TestBuildStatusCard_NoAgent(t *testing.T) {
	h := NewHandler(nil, nil, "v1.0.0")
	card := h.buildStatusCard()
	if !json.Valid(card) {
		t.Error("expected valid JSON from buildStatusCard")
	}
	cardStr := string(card)
	if !strings.Contains(cardStr, "echo mode") {
		t.Errorf("expected 'echo mode' in card, got %s", cardStr)
	}
}

func TestBuildStatusCard_WithAgent(t *testing.T) {
	ag := &testAgent{reply: "hi"}
	h := NewHandler(nil, nil, "v1.0.0")
	h.SetDefaultAgent("claude", ag)
	h.SetAgentMetas([]AgentMeta{{Name: "claude", Type: "test", Model: "sonnet"}})

	card := h.buildStatusCard()
	if !json.Valid(card) {
		t.Error("expected valid JSON from buildStatusCard")
	}
	cardStr := string(card)
	if !strings.Contains(cardStr, "claude") {
		t.Errorf("expected agent name in card, got %s", cardStr)
	}
}

// --- HandleMessage: /info and /status ---

func TestHandleMessage_InfoCommand(t *testing.T) {
	srv, _ := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	// /info should not crash
	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "info-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/info",
	})
}

func TestHandleMessage_StatusCommand(t *testing.T) {
	srv, _ := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "status-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/status",
	})
}

func TestHandleMessage_ClearCommand(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "clear-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/clear",
	})

	got := getSentTexts(sentTexts)
	if len(got) == 0 {
		t.Fatal("expected a reply to /clear")
	}
}

func TestHandleMessage_CwdCommand(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "cwd-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/cwd",
	})

	got := getSentTexts(sentTexts)
	if len(got) == 0 {
		t.Fatal("expected a reply to /cwd")
	}
}

func TestHandleMessage_CronNilStore(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "cron-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/cron list",
	})

	got := getSentTexts(sentTexts)
	if len(got) == 0 {
		t.Fatal("expected a reply to /cron")
	}
	if !strings.Contains(got[0], "not configured") {
		t.Errorf("expected 'not configured' reply, got %q", got[0])
	}
}

func TestHandleMessage_ChatInfoCommand(t *testing.T) {
	srv, _ := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "chatinfo-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/chatinfo",
	})
}

// --- HandleMessage: group chat permission tests ---

func TestHandleMessage_GroupPrivilegedBlocked(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	// NOT a DM — this is a group
	readClient := ringcentral.NewBotClient(srv.URL, "token")
	readClient.SetOwnerID("owner-1")

	h.HandleMessage(context.Background(), client, readClient, ringcentral.Post{
		ID:        "priv-1",
		GroupID:   "group-1",
		CreatorID: "non-owner",
		Text:      "/reload",
	})

	got := getSentTexts(sentTexts)
	if len(got) == 0 {
		t.Fatal("expected a permission denial reply")
	}
	if !strings.Contains(got[0], "Only the bot owner") {
		t.Errorf("expected 'Only the bot owner' reply, got %q", got[0])
	}
}

// --- HandleMessage: multi-agent broadcast usage error ---

func TestHandleMessage_MultiAgentNoMessage(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	ag := &testAgent{reply: "hi"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)
	h.mu.Lock()
	h.agents["codex"] = ag
	h.mu.Unlock()
	h.SetAgentMetas([]AgentMeta{
		{Name: "claude", Type: "test"},
		{Name: "codex", Type: "test"},
	})

	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "multi-no-msg-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/claude /codex",
	})

	got := getSentTexts(sentTexts)
	if len(got) == 0 {
		t.Fatal("expected usage error reply")
	}
	if !strings.Contains(got[0], "Usage") {
		t.Errorf("expected 'Usage' reply, got %q", got[0])
	}
}

// --- isGenericCurrentGroupSummaryTarget tests ---

func TestIsGenericCurrentGroupSummaryTarget(t *testing.T) {
	tests := []struct {
		target string
		want   bool
	}{
		{"", true},
		{"这个", true},
		{"当前", true},
		{"this", true},
		{"current", true},
		{"here", true},
		{"john", false},
		{"alice", false},
	}
	for _, tt := range tests {
		got := isGenericCurrentGroupSummaryTarget(tt.target)
		if got != tt.want {
			t.Errorf("isGenericCurrentGroupSummaryTarget(%q) = %v, want %v", tt.target, got, tt.want)
		}
	}
}

// --- denyGroupCrossTargetSummary tests ---

func TestDenyGroupCrossTargetSummary_AllowCurrentGroup(t *testing.T) {
	result := denyGroupCrossTargetSummary("总结一下最近消息", nil, "bot-1")
	if result != "" {
		t.Errorf("expected empty (allow), got %q", result)
	}
}

func TestDenyGroupCrossTargetSummary_DenyOtherGroup(t *testing.T) {
	result := denyGroupCrossTargetSummary("总结其他群的消息", nil, "bot-1")
	if result == "" {
		t.Error("expected denial for 其他群")
	}
}

func TestDenyGroupCrossTargetSummary_DenyTeamMention(t *testing.T) {
	result := denyGroupCrossTargetSummary("总结一下",
		[]ringcentral.Mention{{ID: "team-1", Type: "Team", Name: "Other Team"}},
		"bot-1")
	if result == "" {
		t.Error("expected denial for Team mention")
	}
}

func TestDenyGroupCrossTargetSummary_AllowBotSelfMention(t *testing.T) {
	result := denyGroupCrossTargetSummary("总结一下",
		[]ringcentral.Mention{{ID: "bot-1", Type: "Person", Name: "Bot"}},
		"bot-1")
	if result != "" {
		t.Errorf("expected allow for bot self-mention, got %q", result)
	}
}

func TestDenyGroupCrossTargetSummary_DenyPersonMention(t *testing.T) {
	result := denyGroupCrossTargetSummary("总结 alice 的消息",
		[]ringcentral.Mention{{ID: "user-2", Type: "Person", Name: "Alice"}},
		"bot-1")
	if result == "" {
		t.Error("expected denial for non-bot Person mention")
	}
}

// --- sendReplyWithActions: non-bot wraps with answer ---

func TestSendReplyWithActions_NonBotWrapsAnswer(t *testing.T) {
	var updatedText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPatch {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			updatedText = req["text"]
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "p1"})
	}))
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	// Non-bot client (private app)
	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	h.sendReplyWithActions(context.Background(), client, client,
		ringcentral.Post{GroupID: "g1", CreatorID: "user-1"},
		"hello reply", "placeholder-1")

	time.Sleep(50 * time.Millisecond)
	if !strings.Contains(updatedText, "answer") {
		t.Errorf("expected wrapped answer for non-bot client, got %q", updatedText)
	}
}

// --- sendReplyWithActions: group chat adds mention ---

func TestSendReplyWithActions_GroupChatAddsMention(t *testing.T) {
	var updatedText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPatch {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			updatedText = req["text"]
		}
		_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
	}))
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	// Group chat (not DM)

	h.sendReplyWithActions(context.Background(), client, client,
		ringcentral.Post{GroupID: "group-1", CreatorID: "user-1"},
		"reply text", "placeholder-1")

	time.Sleep(50 * time.Millisecond)
	if !strings.Contains(updatedText, "![:Person](user-1)") {
		t.Errorf("expected mention prefix in group chat, got %q", updatedText)
	}
}

// --- sendReplyWithActions: no placeholder sends new post ---

func TestSendReplyWithActions_NoPlaceholder(t *testing.T) {
	var mu sync.Mutex
	var sentTexts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/posts") {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			sentTexts = append(sentTexts, req["text"])
			mu.Unlock()
		}
		_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
	}))
	defer srv.Close()

	h := NewHandler(nil, nil, "test")
	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.sendReplyWithActions(context.Background(), client, client,
		ringcentral.Post{GroupID: "dm-1", CreatorID: "user-1"},
		"reply text", "")

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(sentTexts) == 0 {
		t.Fatal("expected a new post sent")
	}
	if !strings.Contains(sentTexts[0], "reply text") {
		t.Errorf("expected reply text, got %q", sentTexts[0])
	}
}

// --- SetCustomAliases / SetCronStore ---

func TestSetCustomAliases(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	h.SetCustomAliases(map[string]string{"ai": "claude"})
	if h.resolveAlias("ai") != "claude" {
		t.Error("expected custom alias to work")
	}
}

func TestSetCronStore(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	store := &CronStore{}
	h.SetCronStore(store)
	if h.cronStore != store {
		t.Error("expected cron store to be set")
	}
}

// --- GetDefaultAgent / GetAgent exported wrappers ---

func TestGetDefaultAgent_Exported(t *testing.T) {
	ag := &testAgent{reply: "hi"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)

	got := h.GetDefaultAgent()
	if got != ag {
		t.Error("expected exported GetDefaultAgent to return default agent")
	}
}

func TestGetAgent_Exported(t *testing.T) {
	ag := &testAgent{reply: "hi"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)

	got, err := h.GetAgent(context.Background(), "claude")
	if err != nil {
		t.Fatal(err)
	}
	if got != ag {
		t.Error("expected exported GetAgent to return agent")
	}
}

func TestValidateCwdPath_Extended(t *testing.T) {
	tests := []struct {
		path    string
		wantErr bool
	}{
		{"/home/user/project", false},
		{"/tmp/workspace", false},
		{"/home/user/.ssh", true},
		{"/home/user/.ssh/keys", true},
		{"/home/user/.gnupg", true},
		{"/home/user/.ringclaw", true},
		{"/home/user/.aws/config", true},
	}
	for _, tt := range tests {
		err := validateCwdPath(tt.path)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateCwdPath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
		}
	}
}
