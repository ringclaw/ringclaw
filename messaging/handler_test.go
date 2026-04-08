package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	return &Handler{agents: make(map[string]agent.Agent)}
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
