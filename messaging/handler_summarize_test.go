package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// intentTestAgent lets tests control the reply based on conversationID.
type intentTestAgent struct {
	intentReply string
	chatReply   string
	chatErr     error
}

func (a *intentTestAgent) Chat(_ context.Context, convID, _ string) (string, error) {
	if convID == intentConversationID {
		return a.intentReply, nil
	}
	if a.chatErr != nil {
		return "", a.chatErr
	}
	return a.chatReply, nil
}
func (a *intentTestAgent) ResetSession(_ context.Context, _ string) (string, error) { return "", nil }
func (a *intentTestAgent) SetCwd(_ string)                                          {}
func (a *intentTestAgent) Info() agent.AgentInfo {
	return agent.AgentInfo{Name: "intent-test", Type: "test"}
}

func TestClassifyAndRoute_SummarizeKeywordFastPath(t *testing.T) {
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
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	h := newTestHandler()
	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("bot-1")

	// "总结" is a summarize keyword — fast path should handle it without AI classification
	handled := h.classifyAndRoute(context.Background(), bot, bot,
		ringcentral.Post{GroupID: "dm-1", CreatorID: "user-1", Text: "总结一下"},
		"总结一下", false)
	if !handled {
		t.Fatal("expected classifyAndRoute to handle summarize keyword")
	}
	// Should have sent some reply (error about no private app)
	if len(sentTexts) == 0 {
		t.Fatal("expected at least one reply")
	}
}

func TestClassifyAndRoute_NoAgent(t *testing.T) {
	h := newTestHandler()
	// No default agent -> should return false
	handled := h.classifyAndRoute(context.Background(), nil, nil,
		ringcentral.Post{GroupID: "dm-1", CreatorID: "user-1"},
		"create a task for john", false)
	if handled {
		t.Fatal("expected false when no agent")
	}
}

func TestClassifyAndRoute_IntentTask(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	ag := &intentTestAgent{intentReply: "task", chatReply: "task created"}
	h := newTestHandler()
	h.SetDefaultAgent("test", ag)

	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("bot-1")
	bot.SetDMChatID("dm-1")

	handled := h.classifyAndRoute(context.Background(), bot, bot,
		ringcentral.Post{GroupID: "dm-1", CreatorID: "user-1"},
		"create a task for john", false)
	if !handled {
		t.Fatal("expected task intent to be handled")
	}
	got := getSentTexts(sentTexts)
	if len(got) == 0 {
		t.Fatal("expected a reply for task intent")
	}
}

func TestClassifyAndRoute_IntentNote(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	ag := &intentTestAgent{intentReply: "note", chatReply: "note created"}
	h := newTestHandler()
	h.SetDefaultAgent("test", ag)

	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("bot-1")
	bot.SetDMChatID("dm-1")

	handled := h.classifyAndRoute(context.Background(), bot, bot,
		ringcentral.Post{GroupID: "dm-1", CreatorID: "user-1"},
		"create a note about meeting", false)
	if !handled {
		t.Fatal("expected note intent to be handled")
	}
	got := getSentTexts(sentTexts)
	if len(got) == 0 {
		t.Fatal("expected a reply for note intent")
	}
}

func TestClassifyAndRoute_IntentEvent(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	ag := &intentTestAgent{intentReply: "event", chatReply: "event created"}
	h := newTestHandler()
	h.SetDefaultAgent("test", ag)

	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("bot-1")
	bot.SetDMChatID("dm-1")

	handled := h.classifyAndRoute(context.Background(), bot, bot,
		ringcentral.Post{GroupID: "dm-1", CreatorID: "user-1"},
		"schedule a meeting tomorrow", false)
	if !handled {
		t.Fatal("expected event intent to be handled")
	}
	got := getSentTexts(sentTexts)
	if len(got) == 0 {
		t.Fatal("expected a reply for event intent")
	}
}

func TestClassifyAndRoute_IntentChat(t *testing.T) {
	ag := &intentTestAgent{intentReply: "chat", chatReply: "hello"}
	h := newTestHandler()
	h.SetDefaultAgent("test", ag)

	handled := h.classifyAndRoute(context.Background(), nil, nil,
		ringcentral.Post{GroupID: "dm-1", CreatorID: "user-1"},
		"hello world", false)
	if handled {
		t.Fatal("expected chat intent to NOT be handled (returns false)")
	}
}

func TestClassifyAndRoute_IntentSummarize(t *testing.T) {
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
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	ag := &intentTestAgent{intentReply: "summarize", chatReply: "summary"}
	h := newTestHandler()
	h.SetDefaultAgent("test", ag)

	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("bot-1")

	// When intent is summarize, routeSummarize should be called.
	// In DM mode with bot == readClient, it should complain about no Private App.
	handled := h.classifyAndRoute(context.Background(), bot, bot,
		ringcentral.Post{GroupID: "dm-1", CreatorID: "user-1"},
		"请帮我总结一下聊天", false)
	if !handled {
		t.Fatal("expected summarize intent to be handled")
	}
	if len(sentTexts) == 0 {
		t.Fatal("expected a reply")
	}
}

func TestHandleSummarize_WithMention(t *testing.T) {
	var sentTexts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/posts"):
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			sentTexts = append(sentTexts, req["text"])
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/chats") && strings.Contains(r.URL.RawQuery, "type=Direct"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"records": []map[string]interface{}{
					{"id": "dm-with-alice", "type": "Direct", "members": []map[string]string{{"id": "alice-1"}, {"id": "me"}}},
				},
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/posts"):
			_ = json.NewEncoder(w).Encode(ringcentral.PostList{
				Records: []ringcentral.Post{
					{
						ID: "m1", GroupID: "dm-with-alice", Text: "hello alice",
						CreatorID: "alice-1", CreationTime: time.Now().UTC().Format(time.RFC3339),
					},
				},
			})
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/posts/"):
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			sentTexts = append(sentTexts, req["text"])
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1", Text: req["text"]})
		default:
			_ = json.NewEncoder(w).Encode(map[string]string{})
		}
	}))
	defer srv.Close()

	ag := &testAgent{reply: "Here is the summary of your chat with Alice"}
	h := newTestHandler()
	h.SetDefaultAgent("test", ag)

	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("bot-1")
	readClient := ringcentral.NewBotClient(srv.URL, "read-token")

	h.handleSummarize(context.Background(), bot, readClient, ringcentral.Post{
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "总结 alice 的消息",
		Mentions:  []ringcentral.Mention{{ID: "alice-1", Type: "Person", Name: "Alice"}},
	})

	time.Sleep(100 * time.Millisecond)
	found := false
	for _, txt := range sentTexts {
		if strings.Contains(txt, "summary") || strings.Contains(txt, "Summary") || strings.Contains(txt, "Alice") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected summary reply, got %v", sentTexts)
	}
}

func TestHandleSummarize_ResolveFails(t *testing.T) {
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
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	h := newTestHandler()
	bot := ringcentral.NewBotClient(srv.URL, "token")
	readClient := ringcentral.NewBotClient(srv.URL, "read-token")

	h.handleSummarize(context.Background(), bot, readClient, ringcentral.Post{
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "summarize",
	})

	time.Sleep(100 * time.Millisecond)
	if len(sentTexts) == 0 {
		t.Fatal("expected error reply")
	}
	if !strings.Contains(sentTexts[0], "Error") {
		t.Errorf("expected Error in reply, got %q", sentTexts[0])
	}
}

func TestExecuteSummarize_NoAgent(t *testing.T) {
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
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/posts") {
			_ = json.NewEncoder(w).Encode(ringcentral.PostList{
				Records: []ringcentral.Post{
					{
						ID: "m1", GroupID: "c1", Text: "test msg",
						CreatorID: "glip-user-1", CreationTime: time.Now().UTC().Format(time.RFC3339),
					},
				},
			})
			return
		}
		if r.Method == http.MethodPatch {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			sentTexts = append(sentTexts, req["text"])
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	h := newTestHandler() // no default agent
	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("bot-1")

	h.executeSummarize(context.Background(), bot, bot,
		ringcentral.Post{GroupID: "c1", CreatorID: "user-1"},
		&SummarizeRequest{
			ChatID:      "c1",
			ChatName:    "Test",
			TimeFrom:    time.Now().Add(-time.Hour),
			UserRequest: "summarize",
		})

	time.Sleep(100 * time.Millisecond)
	found := false
	for _, txt := range sentTexts {
		if strings.Contains(txt, "no agent") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'no agent' error, got %v", sentTexts)
	}
}

func TestExecuteSummarize_AgentError(t *testing.T) {
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
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/posts") {
			_ = json.NewEncoder(w).Encode(ringcentral.PostList{
				Records: []ringcentral.Post{
					{
						ID: "m1", GroupID: "c1", Text: "test msg",
						CreatorID: "glip-user-1", CreationTime: time.Now().UTC().Format(time.RFC3339),
					},
				},
			})
			return
		}
		if r.Method == http.MethodPatch {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			sentTexts = append(sentTexts, req["text"])
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	errAgent := &errorAgent{err: fmt.Errorf("agent crashed")}
	h := newTestHandler()
	h.SetDefaultAgent("test", errAgent)

	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("bot-1")

	h.executeSummarize(context.Background(), bot, bot,
		ringcentral.Post{GroupID: "c1", CreatorID: "user-1"},
		&SummarizeRequest{
			ChatID:      "c1",
			ChatName:    "Test",
			TimeFrom:    time.Now().Add(-time.Hour),
			UserRequest: "summarize",
		})

	time.Sleep(100 * time.Millisecond)
	found := false
	for _, txt := range sentTexts {
		if strings.Contains(txt, "Error") || strings.Contains(txt, "crashed") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error reply, got %v", sentTexts)
	}
}

func TestExecuteSummarize_BuildPromptError(t *testing.T) {
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
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/posts") {
			// Return empty posts list to trigger error
			_ = json.NewEncoder(w).Encode(ringcentral.PostList{Records: []ringcentral.Post{}})
			return
		}
		if r.Method == http.MethodPatch {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			sentTexts = append(sentTexts, req["text"])
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	ag := &testAgent{reply: "should not be called"}
	h := newTestHandler()
	h.SetDefaultAgent("test", ag)

	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("bot-1")

	h.executeSummarize(context.Background(), bot, bot,
		ringcentral.Post{GroupID: "c1", CreatorID: "user-1"},
		&SummarizeRequest{
			ChatID:      "c1",
			ChatName:    "Test",
			TimeFrom:    time.Now().Add(-time.Hour),
			UserRequest: "summarize",
		})

	time.Sleep(100 * time.Millisecond)
	found := false
	for _, txt := range sentTexts {
		if strings.Contains(txt, "Error") || strings.Contains(txt, "no messages") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error about no messages, got %v", sentTexts)
	}
}

func TestExecuteSummarize_WithActions(t *testing.T) {
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
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/posts") {
			_ = json.NewEncoder(w).Encode(ringcentral.PostList{
				Records: []ringcentral.Post{
					{
						ID: "m1", GroupID: "c1", Text: "discussion",
						CreatorID: "glip-user-1", CreationTime: time.Now().UTC().Format(time.RFC3339),
					},
				},
			})
			return
		}
		if r.Method == http.MethodPatch {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			sentTexts = append(sentTexts, req["text"])
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	// Agent reply includes an ACTION block
	ag := &testAgent{reply: "Here is the summary\nACTION:MESSAGE chatid=c1\nfollow up\nEND_ACTION"}
	h := newTestHandler()
	h.SetDefaultAgent("test", ag)

	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("bot-1")

	h.executeSummarize(context.Background(), bot, bot,
		ringcentral.Post{GroupID: "c1", CreatorID: "user-1"},
		&SummarizeRequest{
			ChatID:      "c1",
			ChatName:    "Test",
			TimeFrom:    time.Now().Add(-time.Hour),
			UserRequest: "summarize",
		})

	time.Sleep(100 * time.Millisecond)
	found := false
	for _, txt := range sentTexts {
		if strings.Contains(txt, "summary") || strings.Contains(txt, "Summary") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected summary text in replies, got %v", sentTexts)
	}
}

func TestExecuteSummarize_NonBotWraps(t *testing.T) {
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
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/posts") {
			_ = json.NewEncoder(w).Encode(ringcentral.PostList{
				Records: []ringcentral.Post{
					{
						ID: "m1", GroupID: "c1", Text: "hello",
						CreatorID: "glip-user-1", CreationTime: time.Now().UTC().Format(time.RFC3339),
					},
				},
			})
			return
		}
		if r.Method == http.MethodPatch {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			sentTexts = append(sentTexts, req["text"])
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	ag := &testAgent{reply: "summary result"}
	h := newTestHandler()
	h.SetDefaultAgent("test", ag)

	// Non-bot client
	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	h.executeSummarize(context.Background(), client, client,
		ringcentral.Post{GroupID: "c1", CreatorID: "user-1"},
		&SummarizeRequest{
			ChatID:      "c1",
			ChatName:    "Test",
			TimeFrom:    time.Now().Add(-time.Hour),
			UserRequest: "summarize",
		})

	time.Sleep(100 * time.Millisecond)
	found := false
	for _, txt := range sentTexts {
		if strings.Contains(txt, "answer") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected wrapped answer for non-bot, got %v", sentTexts)
	}
}

// errorAgent always returns an error from Chat.
type errorAgent struct {
	err error
}

func (a *errorAgent) Chat(_ context.Context, _, _ string) (string, error) { return "", a.err }
func (a *errorAgent) ResetSession(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (a *errorAgent) SetCwd(_ string)        {}
func (a *errorAgent) Info() agent.AgentInfo {
	return agent.AgentInfo{Name: "error-agent", Type: "test"}
}

func TestRouteSummarize_DM_NeedsPrivateApp(t *testing.T) {
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
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	h := newTestHandler()
	bot := ringcentral.NewBotClient(srv.URL, "token")
	bot.SetOwnerID("bot-1")

	// In DM mode, when readClient == client (same bot), it should say Private App needed
	handled := h.routeSummarize(context.Background(), bot, bot,
		ringcentral.Post{GroupID: "dm-1", CreatorID: "user-1", Text: "summarize"},
		false)
	if !handled {
		t.Fatal("expected handled")
	}
	if len(sentTexts) == 0 {
		t.Fatal("expected reply")
	}
	if !strings.Contains(sentTexts[0], "Private App") {
		t.Errorf("expected Private App message, got %q", sentTexts[0])
	}
}
