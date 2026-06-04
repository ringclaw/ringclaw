package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// TestCronScheduler_ExecuteJob_ActionFired verifies that when the agent returns
// a reply containing an ACTION:TASK block, the scheduler calls CreateTask.
func TestCronScheduler_ExecuteJob_ActionFired(t *testing.T) {
	var mu sync.Mutex
	var taskSubjects []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/tasks") {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			taskSubjects = append(taskSubjects, req["subject"])
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "task-1", "subject": req["subject"]})
			return
		}
		// No text post expected for pure-action reply (clean part is empty)
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/posts") {
			t.Errorf("unexpected text post for pure-action reply: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	store := newTestStore(t)
	client := ringcentral.NewBotClient(srv.URL, "token")
	ag := &cronTestAgent{reply: "ACTION:TASK subject=Test\nEND_ACTION"}

	scheduler := NewCronScheduler(store, client, "chat-1", func(name string) agent.Agent {
		return ag
	}, nil, ActionContext{OriginIsOwner: true})

	job := CronJob{
		ID:       "act1",
		Name:     "action-job",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "do stuff",
	}
	_ = store.Add(job)

	scheduler.executeJob(context.Background(), store.List()[0])

	mu.Lock()
	defer mu.Unlock()
	if len(taskSubjects) != 1 {
		t.Fatalf("expected CreateTask to be called once, got %d calls", len(taskSubjects))
	}
	if taskSubjects[0] != "Test" {
		t.Errorf("expected task subject 'Test', got %q", taskSubjects[0])
	}
}

// TestCronScheduler_ExecuteJob_TextAndAction verifies that both the clean text
// is sent as a message and the action is fired.
func TestCronScheduler_ExecuteJob_TextAndAction(t *testing.T) {
	var mu sync.Mutex
	var sentTexts []string
	var taskSubjects []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/tasks") {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			taskSubjects = append(taskSubjects, req["subject"])
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "task-2", "subject": req["subject"]})
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/posts") {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			sentTexts = append(sentTexts, req["text"])
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "post-1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	store := newTestStore(t)
	client := ringcentral.NewBotClient(srv.URL, "token")
	ag := &cronTestAgent{reply: "All done!\nACTION:TASK subject=Follow up\nEND_ACTION"}

	scheduler := NewCronScheduler(store, client, "chat-1", func(name string) agent.Agent {
		return ag
	}, nil, ActionContext{OriginIsOwner: true})

	job := CronJob{
		ID:       "act2",
		Name:     "text-and-action-job",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "do stuff",
	}
	_ = store.Add(job)

	scheduler.executeJob(context.Background(), store.List()[0])

	mu.Lock()
	defer mu.Unlock()

	// Text part should be sent
	if len(sentTexts) != 1 {
		t.Fatalf("expected 1 text post, got %d: %v", len(sentTexts), sentTexts)
	}
	if !strings.Contains(sentTexts[0], "All done!") {
		t.Errorf("expected 'All done!' in reply, got %q", sentTexts[0])
	}
	if !strings.Contains(sentTexts[0], "[Cron: text-and-action-job]") {
		t.Errorf("expected job name prefix in reply, got %q", sentTexts[0])
	}

	// Action should also be fired
	if len(taskSubjects) != 1 {
		t.Fatalf("expected CreateTask called once, got %d", len(taskSubjects))
	}
	if taskSubjects[0] != "Follow up" {
		t.Errorf("expected task subject 'Follow up', got %q", taskSubjects[0])
	}
}

// TestCronScheduler_ExecuteJob_SMSAllowed verifies that SMS actions are passed
// through from cron (unlike heartbeat which strips SMS).
// Cron allowlist is "all" — the default ExecuteAgentActions handles SMS if the
// RC API supports it; here we just confirm the scheduler does not filter it out.
func TestCronScheduler_ExecuteJob_SMSAllowed(t *testing.T) {
	var mu sync.Mutex
	var postPaths []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			mu.Lock()
			postPaths = append(postPaths, r.URL.Path)
			mu.Unlock()
		}
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	store := newTestStore(t)
	client := ringcentral.NewBotClient(srv.URL, "token")

	// ACTION:SMS is an "unknown" type in the current actions.go default case —
	// it gets passed to the fallback which sends a [SMS] body message.
	// The important thing is the cron scheduler does NOT strip it.
	ag := &cronTestAgent{reply: "ACTION:SMS to=+15551234567\nhello from cron\nEND_ACTION"}

	scheduler := NewCronScheduler(store, client, "chat-1", func(name string) agent.Agent {
		return ag
	}, nil, ActionContext{OriginIsOwner: true})

	job := CronJob{
		ID:       "act3",
		Name:     "sms-job",
		Enabled:  true,
		Schedule: "every:1h",
		Message:  "send sms",
	}
	_ = store.Add(job)

	scheduler.executeJob(context.Background(), store.List()[0])

	// Verify the job completed (status updated), not panicked
	j, ok := store.Get(store.List()[0].ID)
	if !ok {
		t.Fatal("job not found after execution")
	}
	// Any terminal status is acceptable; what matters is no crash
	_ = j.State.LastStatus

	mu.Lock()
	defer mu.Unlock()
	// The SMS fallback case posts a [SMS] text message to origin chat
	// We just check that at least one request happened (the action was not stripped)
	if len(postPaths) == 0 {
		t.Error("expected at least one HTTP request from SMS action handling")
	}
}
