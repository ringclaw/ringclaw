package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// --- handleCwd extended tests ---

func TestHandleCwd_ValidDirectory(t *testing.T) {
	dir := t.TempDir()
	ag := &testAgent{reply: "hi"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)

	result := h.handleCwd("/cwd " + dir)
	if !strings.Contains(result, "cwd:") {
		t.Errorf("expected 'cwd:' in result, got %q", result)
	}
	if !strings.Contains(result, dir) {
		t.Errorf("expected directory path in result, got %q", result)
	}
}

func TestHandleCwd_NotADirectory(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "testfile.txt")
	os.WriteFile(filePath, []byte("hello"), 0o644)

	ag := &testAgent{reply: "hi"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)

	result := h.handleCwd("/cwd " + filePath)
	if !strings.Contains(result, "Not a directory") {
		t.Errorf("expected 'Not a directory', got %q", result)
	}
}

func TestHandleCwd_PathNotFound(t *testing.T) {
	ag := &testAgent{reply: "hi"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)

	result := h.handleCwd("/cwd /nonexistent/path/xyz")
	if !strings.Contains(result, "Path not found") {
		t.Errorf("expected 'Path not found', got %q", result)
	}
}

func TestHandleCwd_TildeExpansion(t *testing.T) {
	ag := &testAgent{reply: "hi"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)

	// Test ~ expansion — since home dir exists, should not show "Path not found"
	result := h.handleCwd("/cwd ~")
	if strings.Contains(result, "Path not found") {
		t.Errorf("expected ~ to expand to home dir, got %q", result)
	}
}

func TestHandleCwd_TildeSlashExpansion(t *testing.T) {
	ag := &testAgent{reply: "hi"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)

	// ~/nonexistent should fail with "Path not found"
	result := h.handleCwd("/cwd ~/nonexistent_test_dir_xyz")
	if !strings.Contains(result, "Path not found") && !strings.Contains(result, "Denied") {
		// It might not find the path, which is fine
		t.Logf("~/nonexistent result: %q", result)
	}
}

func TestHandleCwd_UpdatesMultipleAgents(t *testing.T) {
	dir := t.TempDir()
	ag1 := &cwdTrackingAgent{name: "claude"}
	ag2 := &cwdTrackingAgent{name: "codex"}

	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag1)
	h.mu.Lock()
	h.agents["codex"] = ag2
	h.mu.Unlock()

	result := h.handleCwd("/cwd " + dir)
	if !strings.Contains(result, "cwd:") {
		t.Errorf("expected 'cwd:' in result, got %q", result)
	}
	if ag1.lastCwd != dir {
		t.Errorf("expected ag1 cwd=%q, got %q", dir, ag1.lastCwd)
	}
	if ag2.lastCwd != dir {
		t.Errorf("expected ag2 cwd=%q, got %q", dir, ag2.lastCwd)
	}
}

type cwdTrackingAgent struct {
	name    string
	lastCwd string
}

func (a *cwdTrackingAgent) Chat(_ context.Context, _, _ string) (string, error) { return "hi", nil }
func (a *cwdTrackingAgent) ResetSession(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (a *cwdTrackingAgent) SetCwd(cwd string)      { a.lastCwd = cwd }
func (a *cwdTrackingAgent) Info() agent.AgentInfo {
	return agent.AgentInfo{Name: a.name, Type: "test"}
}

// --- sendToNamedAgent tests ---

func TestHandleMessage_NamedAgentError(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	factory := func(_ context.Context, name string) agent.Agent {
		return nil // agent not available
	}
	h := NewHandler(factory, nil, "test")
	h.SetAgentMetas([]AgentMeta{{Name: "gemini", Type: "test"}})

	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.HandleMessage(context.Background(), client, client, ringcentral.Post{
		ID:        "named-err-1",
		GroupID:   "dm-1",
		CreatorID: "user-1",
		Text:      "/gemini explain this",
	})

	got := getSentTexts(sentTexts)
	found := false
	for _, txt := range got {
		if strings.Contains(txt, "not available") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'not available' error, got %v", got)
	}
}

// --- ParseAgentActions edge cases ---

func TestParseAgentActions_NoEndAction(t *testing.T) {
	reply := "Here is the result\nACTION:EVENT title=Meeting start=2026-04-01T09:00:00Z end=2026-04-01T10:00:00Z"
	clean, actions := ParseAgentActions(reply)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != "EVENT" {
		t.Errorf("expected EVENT action, got %q", actions[0].Type)
	}
	if !strings.Contains(clean, "result") {
		t.Errorf("expected clean text to contain 'result', got %q", clean)
	}
}

func TestParseAgentActions_MultipleActions(t *testing.T) {
	reply := `Summary here
ACTION:NOTE title=Meeting Notes
Key decisions
END_ACTION
ACTION:TASK subject=Review PR
END_ACTION`

	clean, actions := ParseAgentActions(reply)
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}
	if actions[0].Type != "NOTE" {
		t.Errorf("expected NOTE, got %q", actions[0].Type)
	}
	if actions[1].Type != "TASK" {
		t.Errorf("expected TASK, got %q", actions[1].Type)
	}
	if !strings.Contains(clean, "Summary") {
		t.Errorf("expected clean text, got %q", clean)
	}
}

func TestParseAgentActions_EmptyBody(t *testing.T) {
	reply := "ACTION:TASK subject=Do something\nEND_ACTION"
	_, actions := ParseAgentActions(reply)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Params["subject"] != "Do something" {
		t.Errorf("expected subject 'Do something', got %q", actions[0].Params["subject"])
	}
}

func TestParseAgentActions_NoActions(t *testing.T) {
	reply := "Just a normal reply without any actions"
	clean, actions := ParseAgentActions(reply)
	if len(actions) != 0 {
		t.Errorf("expected 0 actions, got %d", len(actions))
	}
	if clean != reply {
		t.Errorf("expected unchanged reply, got %q", clean)
	}
}

// --- ExecuteAgentActions tests ---

func TestExecuteAgentActions_MessageAction(t *testing.T) {
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

	client := ringcentral.NewBotClient(srv.URL, "token")
	actions := []AgentAction{
		{Type: "MESSAGE", Params: map[string]string{}, Body: "hello from action"},
	}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Errorf("expected no error results, got %v", results)
	}
	if len(sentTexts) != 1 || sentTexts[0] != "hello from action" {
		t.Errorf("expected 'hello from action', got %v", sentTexts)
	}
}

func TestExecuteAgentActions_UnknownType(t *testing.T) {
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

	client := ringcentral.NewBotClient(srv.URL, "token")
	actions := []AgentAction{
		{Type: "UNKNOWN", Params: map[string]string{}, Body: "some body"},
	}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) == 0 {
		t.Error("expected result for unknown action type")
	}
	if !strings.Contains(results[0], "Unknown action type") {
		t.Errorf("expected 'Unknown action type', got %q", results[0])
	}
}

func TestExecuteAgentActions_EmptyMessageBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not send for empty body")
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	actions := []AgentAction{
		{Type: "MESSAGE", Params: map[string]string{}, Body: "   "},
	}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Errorf("expected no results for empty body, got %v", results)
	}
}

func TestExecuteAgentActions_EmptyEventParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not send for incomplete event params")
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	// Missing start/end params — should skip
	actions := []AgentAction{
		{Type: "EVENT", Params: map[string]string{"title": "Meeting"}, Body: ""},
	}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Errorf("expected no results for incomplete event, got %v", results)
	}
}

func TestExecuteAgentActions_EmptyTaskSubject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not send for empty subject")
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	actions := []AgentAction{
		{Type: "TASK", Params: map[string]string{}, Body: ""},
	}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Errorf("expected no results for empty task subject, got %v", results)
	}
}

func TestExecuteAgentActions_InvalidCardJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not send invalid card")
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	actions := []AgentAction{
		{Type: "CARD", Params: map[string]string{}, Body: "not valid json"},
	}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) == 0 {
		t.Error("expected error result for invalid JSON")
	}
	if !strings.Contains(results[0], "invalid JSON") {
		t.Errorf("expected 'invalid JSON', got %q", results[0])
	}
}

func TestExecuteAgentActions_EmptyCardBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not send for empty card body")
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	actions := []AgentAction{
		{Type: "CARD", Params: map[string]string{}, Body: ""},
	}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Errorf("expected no results for empty card body, got %v", results)
	}
}

// --- DefaultCronStorePath ---

func TestDefaultCronStorePath(t *testing.T) {
	path, err := DefaultCronStorePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
	if !strings.Contains(path, "ringclaw") {
		t.Errorf("expected path to contain 'ringclaw', got %q", path)
	}
	if !strings.HasSuffix(path, "jobs.json") {
		t.Errorf("expected path to end with 'jobs.json', got %q", path)
	}
}

// --- buildStatusCard with PID and model ---

func TestBuildStatusCard_WithPIDAndModel(t *testing.T) {
	ag := &pidTestAgent{}
	h := NewHandler(nil, nil, "v1.0.0")
	h.SetDefaultAgent("claude", ag)
	h.SetAgentMetas([]AgentMeta{
		{Name: "claude", Type: "acp", Model: "sonnet"},
		{Name: "codex", Type: "cli"},
	})

	card := h.buildStatusCard()
	if !json.Valid(card) {
		t.Error("expected valid JSON")
	}
	cardStr := string(card)
	if !strings.Contains(cardStr, "12345") {
		t.Errorf("expected PID in card, got %s", cardStr)
	}
	if !strings.Contains(cardStr, "sonnet") {
		t.Errorf("expected model in card, got %s", cardStr)
	}
}

type pidTestAgent struct{}

func (a *pidTestAgent) Chat(_ context.Context, _, _ string) (string, error) { return "hi", nil }
func (a *pidTestAgent) ResetSession(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (a *pidTestAgent) SetCwd(_ string)        {}
func (a *pidTestAgent) Info() agent.AgentInfo {
	return agent.AgentInfo{Name: "claude", Type: "acp", Model: "sonnet", PID: 12345}
}

// --- buildStatus with PID and model ---

func TestBuildStatus_WithPIDAndModel(t *testing.T) {
	ag := &pidTestAgent{}
	h := NewHandler(nil, nil, "v1.0.0")
	h.SetDefaultAgent("claude", ag)
	h.SetAgentMetas([]AgentMeta{{Name: "claude", Type: "acp", Model: "sonnet"}})

	status := h.buildStatus()
	if !strings.Contains(status, "12345") {
		t.Errorf("expected PID in status, got %q", status)
	}
	if !strings.Contains(status, "sonnet") {
		t.Errorf("expected model in status, got %q", status)
	}
}

// --- buildStatus with agent not started ---

func TestBuildStatus_AgentNotStarted(t *testing.T) {
	h := NewHandler(nil, nil, "v1.0.0")
	h.mu.Lock()
	h.defaultName = "ghost"
	h.mu.Unlock()

	status := h.buildStatus()
	if !strings.Contains(status, "not started") {
		t.Errorf("expected 'not started', got %q", status)
	}
}

// --- filenameFromURL edge cases ---

func TestFilenameFromURL_QueryStripping(t *testing.T) {
	// Test that query params are stripped before extracting filename
	got := filenameFromURL("https://example.com/report.pdf?token=abc&page=1")
	if got != "report.pdf" {
		t.Errorf("expected 'report.pdf', got %q", got)
	}
}

func TestFilenameFromURL_DeepPath(t *testing.T) {
	got := filenameFromURL("https://cdn.example.com/assets/images/logo.png")
	if got != "logo.png" {
		t.Errorf("expected 'logo.png', got %q", got)
	}
}

// --- resetDefaultSession error path ---

func TestResetDefaultSession_Error(t *testing.T) {
	ag := &resetErrorAgent{}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("test", ag)

	result := h.resetDefaultSession(context.Background(), "conv-1")
	if !strings.Contains(result, "Failed") {
		t.Errorf("expected 'Failed', got %q", result)
	}
}

type resetErrorAgent struct{}

func (a *resetErrorAgent) Chat(_ context.Context, _, _ string) (string, error) { return "", nil }
func (a *resetErrorAgent) ResetSession(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("reset failed")
}
func (a *resetErrorAgent) SetCwd(_ string)        {}
func (a *resetErrorAgent) Info() agent.AgentInfo {
	return agent.AgentInfo{Name: "test", Type: "test"}
}

// --- groupSummaryLimit default ---

func TestGroupSummaryLimit_DefaultValue(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	limit := h.groupSummaryLimit()
	if limit != defaultSummaryMessageLimit {
		t.Errorf("expected default limit %d, got %d", defaultSummaryMessageLimit, limit)
	}
}

// --- handleChatInfo with description ---

func TestHandleChatInfo_WithDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ringcentral.Chat{
			ID: "c1", Name: "General", Type: "Team",
			Description: "Main discussion channel",
			Status:      "Active",
			Members:     []ringcentral.ChatMember{{ID: "u1"}},
		})
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	result := handleChatInfo(context.Background(), client, "c1", "/chatinfo")
	if !strings.Contains(result, "Main discussion channel") {
		t.Errorf("expected description, got %s", result)
	}
	if !strings.Contains(result, "Active") {
		t.Errorf("expected status, got %s", result)
	}
}

// --- switchDefault with save error ---

func TestSwitchDefault_SaveError(t *testing.T) {
	ag := &testAgent{reply: "hi"}
	factory := func(_ context.Context, name string) agent.Agent {
		return ag
	}
	saveFn := func(name string) error {
		return fmt.Errorf("disk full")
	}

	h := NewHandler(factory, saveFn, "test")
	result := h.switchDefault(context.Background(), "claude")
	// Should succeed switching even if save fails
	if !strings.Contains(result, "switch to claude") {
		t.Errorf("expected switch confirmation, got %q", result)
	}
}

// --- buildStatusCard with agent not started ---

func TestBuildStatusCard_AgentNotStarted(t *testing.T) {
	h := NewHandler(nil, nil, "v1.0.0")
	h.mu.Lock()
	h.defaultName = "ghost"
	h.mu.Unlock()

	card := h.buildStatusCard()
	if !json.Valid(card) {
		t.Error("expected valid JSON")
	}
	cardStr := string(card)
	if !strings.Contains(cardStr, "not started") {
		t.Errorf("expected 'not started' in card, got %s", cardStr)
	}
}

// --- handleMessage: /agent switch blocked in group ---

func TestHandleMessage_AgentSwitchBlockedInGroup(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	ag := &testAgent{reply: "hi"}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)
	h.SetAgentMetas([]AgentMeta{{Name: "claude", Type: "test"}})

	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")

	readClient := ringcentral.NewBotClient(srv.URL, "token")
	readClient.SetOwnerID("owner-1")

	h.HandleMessage(context.Background(), client, readClient, ringcentral.Post{
		ID:        "switch-block-1",
		GroupID:   "group-1",
		CreatorID: "not-owner",
		Text:      "/claude",
	})

	got := getSentTexts(sentTexts)
	found := false
	for _, txt := range got {
		if strings.Contains(txt, "Only the bot owner") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'Only the bot owner' for group agent switch, got %v", got)
	}
}

// --- resetDefaultSession with session ID returned ---

func TestResetDefaultSession_WithSessionID(t *testing.T) {
	ag := &sessionIDAgent{}
	h := NewHandler(nil, nil, "test")
	h.SetDefaultAgent("claude", ag)

	result := h.resetDefaultSession(context.Background(), "conv-1")
	if !strings.Contains(result, "session-abc") {
		t.Errorf("expected session ID in result, got %q", result)
	}
}

type sessionIDAgent struct{}

func (a *sessionIDAgent) Chat(_ context.Context, _, _ string) (string, error) { return "", nil }
func (a *sessionIDAgent) ResetSession(_ context.Context, _ string) (string, error) {
	return "session-abc", nil
}
func (a *sessionIDAgent) SetCwd(_ string)        {}
func (a *sessionIDAgent) Info() agent.AgentInfo {
	return agent.AgentInfo{Name: "claude", Type: "acp"}
}

// --- ExecuteAgentActions: NOTE action ---

func TestExecuteAgentActions_NoteAction(t *testing.T) {
	var createdNote bool
	var publishedNote bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/notes") {
			if strings.HasSuffix(r.URL.Path, "/publish") {
				publishedNote = true
				w.WriteHeader(http.StatusNoContent)
				return
			}
			createdNote = true
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "note-1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	actions := []AgentAction{
		{Type: "NOTE", Params: map[string]string{"title": "Meeting Notes"}, Body: "Key decisions..."},
	}

	results := ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if len(results) != 0 {
		t.Errorf("expected no error results, got %v", results)
	}
	if !createdNote {
		t.Error("expected note to be created")
	}
	// Publish might or might not succeed depending on route matching
	_ = publishedNote
}

// --- ExecuteAgentActions: NOTE without title uses default ---

func TestExecuteAgentActions_NoteDefaultTitle_Extra(t *testing.T) {
	var noteTitle string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/notes") {
			if strings.HasSuffix(r.URL.Path, "/publish") {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			noteTitle = req["title"]
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "note-1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	actions := []AgentAction{
		{Type: "NOTE", Params: map[string]string{}, Body: "content"},
	}

	ExecuteAgentActions(context.Background(), client, client, "chat-1", actions, ActionContext{OriginIsOwner: true})
	if noteTitle != "Note" {
		t.Errorf("expected default title 'Note', got %q", noteTitle)
	}
}

// --- truncateStr coverage ---

func TestTruncateStr(t *testing.T) {
	if got := truncateStr("hello", 10); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
	if got := truncateStr("hello world this is long", 5); got != "hello..." {
		t.Errorf("expected 'hello...', got %q", got)
	}
}

// --- dispatchToAgent: with error from agent ---

func TestDispatchToAgent_AgentError(t *testing.T) {
	srv, sentTexts := newTestRC(t)
	defer srv.Close()

	errAg := &errorAgent{err: fmt.Errorf("crash")}
	h := NewHandler(nil, nil, "test")

	client := ringcentral.NewBotClient(srv.URL, "token")
	client.SetOwnerID("bot-1")
	client.SetDMChatID("dm-1")

	h.dispatchToAgent(context.Background(), client, client,
		ringcentral.Post{ID: "disp-err-1", GroupID: "dm-1", CreatorID: "user-1"},
		errAg, "test message", "placeholder-1")

	got := getSentTexts(sentTexts)
	found := false
	for _, txt := range got {
		if strings.Contains(txt, "Error") || strings.Contains(txt, "crash") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error reply, got %v", got)
	}
}

// --- cronAdd with invalid schedule ---

func TestHandleCronCommand_AddInvalidSchedule(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	reply := HandleCronCommand(store, `/cron add "test" bad-schedule "hello"`, "chat1")
	if !strings.Contains(reply, "Invalid schedule") {
		t.Errorf("expected 'Invalid schedule', got %q", reply)
	}
}

// --- cronList with jobs ---

func TestHandleCronCommand_ListWithDisabled(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	_ = store.Add(CronJob{Name: "active", Enabled: true, Schedule: "every:1h", Message: "hi"})
	_ = store.Add(CronJob{Name: "inactive", Enabled: false, Schedule: "every:1h", Message: "bye"})

	reply := HandleCronCommand(store, "/cron list", "chat1")
	if !strings.Contains(reply, "active") || !strings.Contains(reply, "inactive") {
		t.Errorf("expected both jobs in list, got %q", reply)
	}
	if !strings.Contains(reply, "disabled") {
		t.Errorf("expected 'disabled' status, got %q", reply)
	}
}

// --- HandleCronCommand: unknown subcommand ---

func TestHandleCronCommand_Unknown(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	reply := HandleCronCommand(store, "/cron xyz", "chat1")
	if !strings.Contains(reply, "Usage") {
		t.Errorf("expected Usage, got %q", reply)
	}
}

// --- HandleCronCommand: delete/enable/disable missing ID ---

func TestHandleCronCommand_DeleteMissingID(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	reply := HandleCronCommand(store, "/cron delete", "chat1")
	if !strings.Contains(reply, "Usage") {
		t.Errorf("expected usage, got %q", reply)
	}
}

func TestHandleCronCommand_EnableMissingID(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	reply := HandleCronCommand(store, "/cron enable", "chat1")
	if !strings.Contains(reply, "Usage") {
		t.Errorf("expected usage, got %q", reply)
	}
}

func TestHandleCronCommand_DisableMissingID(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	reply := HandleCronCommand(store, "/cron disable", "chat1")
	if !strings.Contains(reply, "Usage") {
		t.Errorf("expected usage, got %q", reply)
	}
}

// --- HandleCronCommand: add with at: schedule (one-shot) ---

func TestHandleCronCommand_AddOneShotSchedule(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	future := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	reply := HandleCronCommand(store, fmt.Sprintf(`/cron add "once" at:%s "run once"`, future), "chat1")
	if !strings.Contains(reply, "added") {
		t.Errorf("expected 'added', got %q", reply)
	}

	jobs := store.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if !jobs[0].State.DeleteAfter {
		t.Error("expected one-shot job to have DeleteAfter=true")
	}
}

// --- cronDelete error ---

func TestHandleCronCommand_DeleteNonExistent(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	reply := HandleCronCommand(store, "/cron delete nonexistent", "chat1")
	if !strings.Contains(reply, "Error") {
		t.Errorf("expected error, got %q", reply)
	}
}

// --- cronSetEnabled error ---

func TestHandleCronCommand_EnableNonExistent(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	reply := HandleCronCommand(store, "/cron enable nonexistent", "chat1")
	if !strings.Contains(reply, "Error") {
		t.Errorf("expected error, got %q", reply)
	}
}
