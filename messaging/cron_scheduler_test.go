package messaging

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"

	"encoding/json"
	"net/http"
	"net/http/httptest"
)

// cronTestAgent is a mock agent for cron tests with configurable reply/error.
type cronTestAgent struct {
	reply string
	err   error
}

func (a *cronTestAgent) Chat(_ context.Context, _, _ string) (string, error) {
	if a.err != nil {
		return "", a.err
	}
	return a.reply, nil
}
func (a *cronTestAgent) ResetSession(_ context.Context, _ string) (string, error) { return "", nil }
func (a *cronTestAgent) SetCwd(_ string)                                          {}
func (a *cronTestAgent) Info() agent.AgentInfo {
	return agent.AgentInfo{Name: "cron-test", Type: "test"}
}

func newTestStore(t *testing.T) *CronStore {
	t.Helper()
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestExecuteJob_Success(t *testing.T) {
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
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	store := newTestStore(t)
	client := ringcentral.NewBotClient(srv.URL, "token")
	ag := &cronTestAgent{reply: "cron result"}

	scheduler := NewCronScheduler(store, client, "chat-1", func(name string) agent.Agent {
		return ag
	}, nil, ActionContext{})

	job := CronJob{
		ID:       "j1",
		Name:     "test-job",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "hello cron",
	}
	_ = store.Add(job)

	scheduler.executeJob(context.Background(), store.List()[0])

	mu.Lock()
	defer mu.Unlock()
	if len(sentTexts) != 1 {
		t.Fatalf("expected 1 sent text, got %d: %v", len(sentTexts), sentTexts)
	}
	if !strings.Contains(sentTexts[0], "cron result") {
		t.Errorf("expected 'cron result' in reply, got %q", sentTexts[0])
	}
	if !strings.Contains(sentTexts[0], "[Cron: test-job]") {
		t.Errorf("expected job name prefix, got %q", sentTexts[0])
	}

	// Verify state was updated
	j, ok := store.Get(store.List()[0].ID)
	if !ok {
		t.Fatal("job not found")
	}
	if j.State.LastStatus != "ok" {
		t.Errorf("expected status 'ok', got %q", j.State.LastStatus)
	}
	if j.State.RunCount != 1 {
		t.Errorf("expected RunCount=1, got %d", j.State.RunCount)
	}
}

func TestExecuteJob_NoAgent(t *testing.T) {
	store := newTestStore(t)
	client := ringcentral.NewBotClient("http://unused", "token")

	scheduler := NewCronScheduler(store, client, "chat-1", func(name string) agent.Agent {
		return nil // no agent
	}, nil, ActionContext{})

	job := CronJob{
		ID:       "j2",
		Name:     "no-agent-job",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "hello",
	}
	_ = store.Add(job)

	scheduler.executeJob(context.Background(), store.List()[0])

	// Should reschedule, not record error
	j, ok := store.Get(store.List()[0].ID)
	if !ok {
		t.Fatal("job not found")
	}
	// State should have NextRunAt set to near future
	if j.State.NextRunAt.IsZero() {
		t.Error("expected NextRunAt to be set")
	}
	if j.State.LastStatus == "error" {
		t.Error("expected no error status for no-agent case (reschedule)")
	}
}

func TestExecuteJob_AgentError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	store := newTestStore(t)
	client := ringcentral.NewBotClient(srv.URL, "token")
	ag := &cronTestAgent{err: fmt.Errorf("agent failed")}

	scheduler := NewCronScheduler(store, client, "chat-1", func(name string) agent.Agent {
		return ag
	}, nil, ActionContext{})

	job := CronJob{
		ID:       "j3",
		Name:     "error-job",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "hello",
	}
	_ = store.Add(job)

	scheduler.executeJob(context.Background(), store.List()[0])

	j, ok := store.Get(store.List()[0].ID)
	if !ok {
		t.Fatal("job not found")
	}
	if j.State.LastStatus != "error" {
		t.Errorf("expected status 'error', got %q", j.State.LastStatus)
	}
	if j.State.ErrorCount != 1 {
		t.Errorf("expected ErrorCount=1, got %d", j.State.ErrorCount)
	}
	if !strings.Contains(j.State.LastError, "agent failed") {
		t.Errorf("expected error message, got %q", j.State.LastError)
	}
}

func TestExecuteJob_RetryableError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	store := newTestStore(t)
	client := ringcentral.NewBotClient(srv.URL, "token")
	ag := &cronTestAgent{err: agent.Timeout(fmt.Errorf("timeout"))}

	scheduler := NewCronScheduler(store, client, "chat-1", func(name string) agent.Agent {
		return ag
	}, nil, ActionContext{})

	job := CronJob{
		ID:       "j4",
		Name:     "timeout-job",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "hello",
	}
	_ = store.Add(job)

	scheduler.executeJob(context.Background(), store.List()[0])

	j, ok := store.Get(store.List()[0].ID)
	if !ok {
		t.Fatal("job not found")
	}
	if j.State.LastStatus != "error" {
		t.Errorf("expected status 'error', got %q", j.State.LastStatus)
	}
}

func TestExecuteJob_EmptyReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not send any message for empty reply")
	}))
	defer srv.Close()

	store := newTestStore(t)
	client := ringcentral.NewBotClient(srv.URL, "token")
	ag := &cronTestAgent{reply: "   "} // whitespace-only reply

	scheduler := NewCronScheduler(store, client, "chat-1", func(name string) agent.Agent {
		return ag
	}, nil, ActionContext{})

	job := CronJob{
		ID:       "j5",
		Name:     "empty-reply-job",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "hello",
	}
	_ = store.Add(job)

	scheduler.executeJob(context.Background(), store.List()[0])

	j, ok := store.Get(store.List()[0].ID)
	if !ok {
		t.Fatal("job not found")
	}
	if j.State.LastStatus != "ok" {
		t.Errorf("expected status 'ok' even for empty reply, got %q", j.State.LastStatus)
	}
}

func TestExecuteJob_UsesChatIDFromJob(t *testing.T) {
	var mu sync.Mutex
	var postPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/posts") {
			mu.Lock()
			postPath = r.URL.Path
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	store := newTestStore(t)
	client := ringcentral.NewBotClient(srv.URL, "token")
	ag := &cronTestAgent{reply: "result"}

	scheduler := NewCronScheduler(store, client, "default-chat", func(name string) agent.Agent {
		return ag
	}, nil, ActionContext{})

	job := CronJob{
		ID:       "j6",
		Name:     "custom-chat",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "hello",
		ChatID:   "custom-chat-1",
	}
	_ = store.Add(job)

	scheduler.executeJob(context.Background(), store.List()[0])

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(postPath, "custom-chat-1") {
		t.Errorf("expected post to custom-chat-1, got path %q", postPath)
	}
}

func TestExecuteJob_FallbackToDefaultChat(t *testing.T) {
	var mu sync.Mutex
	var postPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/posts") {
			mu.Lock()
			postPath = r.URL.Path
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "p1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	store := newTestStore(t)
	client := ringcentral.NewBotClient(srv.URL, "token")
	ag := &cronTestAgent{reply: "result"}

	scheduler := NewCronScheduler(store, client, "default-chat", func(name string) agent.Agent {
		return ag
	}, nil, ActionContext{})

	job := CronJob{
		ID:       "j7",
		Name:     "no-chatid",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "hello",
		// ChatID is empty -> should use defaultChat
	}
	_ = store.Add(job)

	scheduler.executeJob(context.Background(), store.List()[0])

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(postPath, "default-chat") {
		t.Errorf("expected post to default-chat, got path %q", postPath)
	}
}

func TestRecordResult_Success(t *testing.T) {
	store := newTestStore(t)
	scheduler := NewCronScheduler(store, nil, "", nil, nil, ActionContext{})

	job := CronJob{
		ID:       "r1",
		Name:     "rec-job",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "hello",
	}
	_ = store.Add(job)

	scheduler.recordResult(store.List()[0], "ok", "")

	j, ok := store.Get(store.List()[0].ID)
	if !ok {
		t.Fatal("job not found")
	}
	if j.State.LastStatus != "ok" {
		t.Errorf("expected 'ok', got %q", j.State.LastStatus)
	}
	if j.State.RunCount != 1 {
		t.Errorf("expected RunCount=1, got %d", j.State.RunCount)
	}
	if j.State.ErrorCount != 0 {
		t.Errorf("expected ErrorCount=0, got %d", j.State.ErrorCount)
	}
	if j.State.LastError != "" {
		t.Errorf("expected empty LastError, got %q", j.State.LastError)
	}
}

func TestRecordResult_Error(t *testing.T) {
	store := newTestStore(t)
	scheduler := NewCronScheduler(store, nil, "", nil, nil, ActionContext{})

	job := CronJob{
		ID:       "r2",
		Name:     "err-job",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "hello",
	}
	_ = store.Add(job)

	scheduler.recordResult(store.List()[0], "error", "something broke")

	j, ok := store.Get(store.List()[0].ID)
	if !ok {
		t.Fatal("job not found")
	}
	if j.State.LastStatus != "error" {
		t.Errorf("expected 'error', got %q", j.State.LastStatus)
	}
	if j.State.ErrorCount != 1 {
		t.Errorf("expected ErrorCount=1, got %d", j.State.ErrorCount)
	}
	if j.State.LastError != "something broke" {
		t.Errorf("expected 'something broke', got %q", j.State.LastError)
	}
}

func TestRecordResult_OneShotDisables(t *testing.T) {
	store := newTestStore(t)
	scheduler := NewCronScheduler(store, nil, "", nil, nil, ActionContext{})

	job := CronJob{
		ID:       "r3",
		Name:     "oneshot",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "hello",
		State:    JobState{DeleteAfter: true},
	}
	_ = store.Add(job)

	scheduler.recordResult(store.List()[0], "ok", "")

	j, ok := store.Get(store.List()[0].ID)
	if !ok {
		t.Fatal("job not found")
	}
	if j.Enabled {
		t.Error("expected one-shot job to be disabled after run")
	}
}

func TestRecordResult_ClearsLastError(t *testing.T) {
	store := newTestStore(t)
	scheduler := NewCronScheduler(store, nil, "", nil, nil, ActionContext{})

	job := CronJob{
		ID:       "r4",
		Name:     "clear-err",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "hello",
		State:    JobState{LastError: "old error", ErrorCount: 1},
	}
	_ = store.Add(job)

	scheduler.recordResult(store.List()[0], "ok", "")

	j, ok := store.Get(store.List()[0].ID)
	if !ok {
		t.Fatal("job not found")
	}
	if j.State.LastError != "" {
		t.Errorf("expected LastError cleared, got %q", j.State.LastError)
	}
}

func TestInitNextRuns_SetsInitialTimes(t *testing.T) {
	store := newTestStore(t)
	scheduler := NewCronScheduler(store, nil, "", nil, nil, ActionContext{})

	job := CronJob{
		ID:       "init1",
		Name:     "init-job",
		Enabled:  true,
		Schedule: "every:5m",
		Message:  "hello",
	}
	_ = store.Add(job)

	scheduler.initNextRuns()

	j, ok := store.Get(store.List()[0].ID)
	if !ok {
		t.Fatal("job not found")
	}
	if j.State.NextRunAt.IsZero() {
		t.Error("expected NextRunAt to be set")
	}
}

func TestInitNextRuns_SkipsAlreadySet(t *testing.T) {
	store := newTestStore(t)
	scheduler := NewCronScheduler(store, nil, "", nil, nil, ActionContext{})

	existingNext := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	job := CronJob{
		ID:       "init2",
		Name:     "already-set",
		Enabled:  true,
		Schedule: "every:5m",
		Message:  "hello",
		State:    JobState{NextRunAt: existingNext},
	}
	_ = store.Add(job)

	scheduler.initNextRuns()

	j, ok := store.Get(store.List()[0].ID)
	if !ok {
		t.Fatal("job not found")
	}
	if !j.State.NextRunAt.Equal(existingNext) {
		t.Errorf("expected NextRunAt unchanged (%v), got %v", existingNext, j.State.NextRunAt)
	}
}

func TestInitNextRuns_DisablesInvalidSchedule(t *testing.T) {
	store := newTestStore(t)
	scheduler := NewCronScheduler(store, nil, "", nil, nil, ActionContext{})

	job := CronJob{
		ID:       "init3",
		Name:     "bad-schedule",
		Enabled:  true,
		Schedule: "invalid-schedule",
		Message:  "hello",
	}
	_ = store.Add(job)

	scheduler.initNextRuns()

	j, ok := store.Get(store.List()[0].ID)
	if !ok {
		t.Fatal("job not found")
	}
	if j.Enabled {
		t.Error("expected job to be disabled due to invalid schedule")
	}
}

func TestInitNextRuns_SkipsDisabledJobs(t *testing.T) {
	store := newTestStore(t)
	scheduler := NewCronScheduler(store, nil, "", nil, nil, ActionContext{})

	job := CronJob{
		ID:       "init4",
		Name:     "disabled-job",
		Enabled:  false,
		Schedule: "every:5m",
		Message:  "hello",
	}
	_ = store.Add(job)

	scheduler.initNextRuns()

	j, ok := store.Get(store.List()[0].ID)
	if !ok {
		t.Fatal("job not found")
	}
	if !j.State.NextRunAt.IsZero() {
		t.Error("expected NextRunAt to remain zero for disabled job")
	}
}

func TestTick_ExecutesDueJob(t *testing.T) {
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
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	store := newTestStore(t)
	client := ringcentral.NewBotClient(srv.URL, "token")
	ag := &cronTestAgent{reply: "tick result"}

	scheduler := NewCronScheduler(store, client, "chat-1", func(name string) agent.Agent {
		return ag
	}, nil, ActionContext{})

	// Add a job that's already due
	job := CronJob{
		ID:       "tick1",
		Name:     "due-job",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "hello tick",
		State:    JobState{NextRunAt: time.Now().Add(-time.Minute)},
	}
	_ = store.Add(job)

	scheduler.tick(context.Background())

	// Wait for goroutine to complete
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(sentTexts) == 0 {
		t.Fatal("expected tick to execute the due job")
	}
	if !strings.Contains(sentTexts[0], "tick result") {
		t.Errorf("expected 'tick result', got %q", sentTexts[0])
	}
}

func TestTick_SkipsNotDueJob(t *testing.T) {
	store := newTestStore(t)
	client := ringcentral.NewBotClient("http://unused", "token")

	scheduler := NewCronScheduler(store, client, "chat-1", func(name string) agent.Agent {
		t.Fatal("should not get agent for not-due job")
		return nil
	}, nil, ActionContext{})

	job := CronJob{
		ID:       "tick2",
		Name:     "future-job",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "hello",
		State:    JobState{NextRunAt: time.Now().Add(time.Hour)},
	}
	_ = store.Add(job)

	scheduler.tick(context.Background())
	// No crash = success
}

func TestTick_SkipsDisabledJob(t *testing.T) {
	store := newTestStore(t)
	client := ringcentral.NewBotClient("http://unused", "token")

	scheduler := NewCronScheduler(store, client, "chat-1", func(name string) agent.Agent {
		t.Fatal("should not get agent for disabled job")
		return nil
	}, nil, ActionContext{})

	job := CronJob{
		ID:       "tick3",
		Name:     "disabled-job",
		Enabled:  false,
		Schedule: "every:1h",
		Message:  "hello",
		State:    JobState{NextRunAt: time.Now().Add(-time.Minute)},
	}
	_ = store.Add(job)

	scheduler.tick(context.Background())
	// No crash = success
}

func TestStartStop_Lifecycle(t *testing.T) {
	store := newTestStore(t)
	client := ringcentral.NewBotClient("http://unused", "token")

	scheduler := NewCronScheduler(store, client, "chat-1", func(name string) agent.Agent {
		return nil
	}, nil, ActionContext{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		scheduler.Start(ctx)
		close(done)
	}()

	// Cancel immediately — Start should return
	cancel()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not stop within timeout")
	}
}
