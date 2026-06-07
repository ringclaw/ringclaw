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

func TestRuntimeMeshTaskClientStartsOrchestration(t *testing.T) {
	var sawStart bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/runtime/v1/orchestrations/start" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		sawStart = true
		var req struct {
			BotID          string `json:"bot_id"`
			BootstrapToken string `json:"bootstrap_token"`
			Intent         string `json:"intent"`
			Title          string `json:"title"`
			Context        struct {
				Data map[string]any `json:"data"`
			} `json:"context"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode start: %v", err)
		}
		if req.BotID != "bot-1" || req.BootstrapToken != "boot-1" || req.Intent != "coverage.transfer" || req.Title != "Coverage handoff" {
			t.Fatalf("start request = %#v", req)
		}
		if req.Context.Data["source_post_id"] != "post-123" {
			t.Fatalf("start context data = %#v", req.Context.Data)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"plan_id":  "plan-post-123",
			"trace_id": "trace-post-123",
			"context": map[string]any{
				"data": map[string]any{
					"plan_id":  "plan-post-123",
					"trace_id": "trace-post-123",
				},
			},
		})
	}))
	defer server.Close()

	client := runtimeMeshTaskClient{
		controlPlaneURL: server.URL,
		botID:           "bot-1",
		bootstrapToken:  "boot-1",
	}
	result, err := client.StartOrchestration(context.Background(), messaging.MeshRuntimeOrchestrationStartRequest{
		Intent: "coverage.transfer",
		Title:  "Coverage handoff",
		Context: messaging.MeshRuntimeContextPackage{Data: map[string]interface{}{
			"source_post_id": "post-123",
		}},
	})
	if err != nil {
		t.Fatalf("StartOrchestration() error = %v", err)
	}
	if !sawStart || result.PlanID != "plan-post-123" || result.TraceID != "trace-post-123" {
		t.Fatalf("sawStart=%v result=%#v", sawStart, result)
	}
}

func TestRuntimeMeshTaskClientCreatesTaskAsSourceRuntime(t *testing.T) {
	var sawCreate bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/runtime/v1/mesh/tasks/create" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		sawCreate = true
		var req struct {
			BotID          string `json:"bot_id"`
			BootstrapToken string `json:"bootstrap_token"`
			PlanID         string `json:"plan_id"`
			ToRoleID       string `json:"to_role_id"`
			Intent         string `json:"intent"`
			Title          string `json:"title"`
			Instructions   string `json:"instructions"`
			Context        struct {
				Data map[string]any `json:"data"`
			} `json:"context"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode create: %v", err)
		}
		if req.BotID != "bot-1" || req.BootstrapToken != "boot-1" || req.ToRoleID != "nurse-coordinator" || req.Intent != "coverage.transfer" {
			t.Fatalf("create request = %#v", req)
		}
		if req.PlanID != "plan-absence-1" {
			t.Fatalf("create plan_id = %q", req.PlanID)
		}
		if req.Context.Data["source_post_id"] != "post-123" {
			t.Fatalf("create context data = %#v", req.Context.Data)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"plan_id":  "plan-absence-1",
			"trace_id": "trace-post-123",
			"route_plan": map[string]any{
				"plan_id":    "plan-absence-1",
				"trace_id":   "trace-post-123",
				"to_role_id": req.ToRoleID,
				"routing_decision": map[string]any{
					"mode":               "explicit_role",
					"intent":             "coverage.transfer",
					"requested_role_id":  "nurse-coordinator",
					"selected_role_id":   "nurse-coordinator",
					"candidate_role_ids": []string{"nurse-coordinator"},
					"reason":             "explicit target role requested",
				},
				"visible_delivery": map[string]any{
					"enabled":           true,
					"transport":         "shared_chat",
					"chat_id":           "chat-admin",
					"mention_person_id": "person-nursecoord",
				},
			},
			"task": map[string]any{
				"id":         "task-1",
				"plan_id":    "plan-absence-1",
				"intent":     req.Intent,
				"to_role_id": req.ToRoleID,
				"title":      req.Title,
			},
		})
	}))
	defer server.Close()

	client := runtimeMeshTaskClient{
		controlPlaneURL: server.URL,
		botID:           "bot-1",
		bootstrapToken:  "boot-1",
	}
	task, err := client.CreateMeshTask(context.Background(), messaging.MeshRuntimeTaskCreateRequest{
		PlanID:       "plan-absence-1",
		ToRoleID:     "nurse-coordinator",
		Intent:       "coverage.transfer",
		Title:        "Alexis absence coverage",
		Instructions: "Transfer Alexis task queue.",
		Context: messaging.MeshRuntimeContextPackage{
			Data: map[string]interface{}{"source_post_id": "post-123"},
		},
	})
	if err != nil {
		t.Fatalf("CreateMeshTask() error = %v", err)
	}
	if task.ID != "task-1" || task.Intent != "coverage.transfer" {
		t.Fatalf("created task = %#v", task)
	}
	if task.TraceID != "trace-post-123" || task.RoutePlan.VisibleDelivery.ChatID != "chat-admin" {
		t.Fatalf("created task route plan = %#v", task)
	}
	if task.PlanID != "plan-absence-1" || task.RoutePlan.PlanID != "plan-absence-1" {
		t.Fatalf("created task plan = %#v", task)
	}
	if task.RoutePlan.RoutingDecision.Mode != "explicit_role" ||
		task.RoutePlan.RoutingDecision.SelectedRoleID != "nurse-coordinator" {
		t.Fatalf("created task routing decision = %#v", task.RoutePlan.RoutingDecision)
	}
	if !sawCreate {
		t.Fatal("create endpoint was not called")
	}
}
