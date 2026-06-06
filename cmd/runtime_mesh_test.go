package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ringclaw/ringclaw/messaging"
)

func TestRuntimeMeshTaskClientPollsAndResponds(t *testing.T) {
	var sawPoll bool
	var sawRespond bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/runtime/v1/mesh/tasks":
			sawPoll = true
			var req struct {
				BotID          string `json:"bot_id"`
				BootstrapToken string `json:"bootstrap_token"`
				Limit          int    `json:"limit"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode poll: %v", err)
			}
			if req.BotID != "bot-1" || req.BootstrapToken != "boot-1" || req.Limit != 10 {
				t.Fatalf("poll request = %#v", req)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tasks": []map[string]any{{
					"id":     "task-1",
					"intent": "coverage.transfer",
					"title":  "Alexis absence coverage",
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/runtime/v1/mesh/tasks/task-1/respond":
			sawRespond = true
			var req struct {
				BotID          string `json:"bot_id"`
				BootstrapToken string `json:"bootstrap_token"`
				Status         string `json:"status"`
				Result         string `json:"result"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode respond: %v", err)
			}
			if req.BotID != "bot-1" || req.BootstrapToken != "boot-1" || req.Status != messaging.MeshRuntimeTaskStatusCompleted {
				t.Fatalf("respond request = %#v", req)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-1", "status": req.Status})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := runtimeMeshTaskClient{
		controlPlaneURL: server.URL,
		botID:           "bot-1",
		bootstrapToken:  "boot-1",
	}
	tasks, err := client.PollMeshTasks(context.Background(), messaging.MeshRuntimeTaskPollRequest{Limit: 10})
	if err != nil {
		t.Fatalf("PollMeshTasks() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "task-1" {
		t.Fatalf("tasks = %#v", tasks)
	}
	if err := client.RespondMeshTask(context.Background(), "task-1", messaging.MeshRuntimeTaskResponse{
		Status: messaging.MeshRuntimeTaskStatusCompleted,
		Result: "done",
	}); err != nil {
		t.Fatalf("RespondMeshTask() error = %v", err)
	}
	if !sawPoll || !sawRespond {
		t.Fatalf("sawPoll=%v sawRespond=%v", sawPoll, sawRespond)
	}
}
