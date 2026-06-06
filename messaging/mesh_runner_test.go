package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"
)

type meshTestAgent struct {
	reply      string
	lastPrompt string
}

func (a *meshTestAgent) Chat(_ context.Context, _ string, message string) (string, error) {
	a.lastPrompt = message
	return a.reply, nil
}

func (a *meshTestAgent) ResetSession(context.Context, string) (string, error) { return "", nil }
func (a *meshTestAgent) SetCwd(string)                                        {}
func (a *meshTestAgent) Info() agent.AgentInfo {
	return agent.AgentInfo{Name: "mesh-test", Type: "test"}
}

type meshTaskClientStub struct {
	tasks     []MeshRuntimeTask
	responses []MeshRuntimeTaskResponse
	created   []MeshRuntimeTaskCreateRequest
}

func (c *meshTaskClientStub) PollMeshTasks(context.Context, MeshRuntimeTaskPollRequest) ([]MeshRuntimeTask, error) {
	return c.tasks, nil
}

func (c *meshTaskClientStub) RespondMeshTask(_ context.Context, taskID string, resp MeshRuntimeTaskResponse) error {
	resp.TaskID = taskID
	c.responses = append(c.responses, resp)
	return nil
}

func (c *meshTaskClientStub) CreateMeshTask(_ context.Context, req MeshRuntimeTaskCreateRequest) (MeshRuntimeTask, error) {
	c.created = append(c.created, req)
	return MeshRuntimeTask{ID: "delegated-task-1", Intent: req.Intent, ToRoleID: req.ToRoleID, Title: req.Title}, nil
}

func TestMeshRunnerExecutesAgentActionsAndCompletesTask(t *testing.T) {
	var sent []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/team-messaging/v1/chats/admin-chat/posts" {
			var body struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode post: %v", err)
			}
			sent = append(sent, body.Text)
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "post-1", Text: body.Text})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	botClient := ringcentral.NewBotClient(server.URL, "bot-token")
	botClient.SetOwnerID("bot-1")
	ag := &meshTestAgent{reply: "I will notify the admin channel.\n\nACTION:MESSAGE\nCoverage transfer accepted.\nEND_ACTION"}
	taskClient := &meshTaskClientStub{tasks: []MeshRuntimeTask{{
		ID:           "task-1",
		Intent:       "coverage.transfer",
		Title:        "Alexis absence coverage",
		Instructions: "Transfer Alexis task queue.",
		Context: MeshRuntimeContextPackage{
			Summary: "Alexis is absent today and task coverage is required.",
		},
	}}}
	runner := NewMeshRunner(MeshRunnerOptions{
		Client:        taskClient,
		Agent:         ag,
		ReplyClient:   botClient,
		ActionClient:  botClient,
		DefaultChatID: "admin-chat",
		Capabilities:  []string{"message", "summary", "phone", "video"},
	})

	if err := runner.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}
	if !strings.Contains(ag.lastPrompt, "coverage.transfer") || !strings.Contains(ag.lastPrompt, "Alexis is absent today") {
		t.Fatalf("prompt did not include task context: %s", ag.lastPrompt)
	}
	if len(sent) != 1 || sent[0] != "Coverage transfer accepted." {
		t.Fatalf("sent messages = %#v", sent)
	}
	if len(taskClient.responses) != 1 {
		t.Fatalf("responses = %#v", taskClient.responses)
	}
	resp := taskClient.responses[0]
	if resp.Status != MeshRuntimeTaskStatusCompleted {
		t.Fatalf("response status = %q", resp.Status)
	}
	if !strings.Contains(resp.Result, "I will notify") {
		t.Fatalf("response result = %q", resp.Result)
	}
	if len(resp.ActionEvents) != 1 || resp.ActionEvents[0].Type != "MESSAGE" {
		t.Fatalf("action events = %#v", resp.ActionEvents)
	}
}

func TestMeshRunnerPostsCleanReplyWhenMessageActionFails(t *testing.T) {
	sentByPath := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/directory/entries/search"):
			_ = json.NewEncoder(w).Encode(ringcentral.DirectorySearchResult{})
			return
		case r.Method == http.MethodPost && (r.URL.Path == "/team-messaging/v1/chats/admin-chat/posts" || r.URL.Path == "/team-messaging/v1/chats/owner-dm/posts"):
			var body struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode post: %v", err)
			}
			sentByPath[r.URL.Path] = body.Text
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "post-1", Text: body.Text})
			return
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	botClient := ringcentral.NewBotClient(server.URL, "bot-token")
	botClient.SetDMChatID("owner-dm")
	visibleReply := "Hi Jennifer, 今天 Alexis 请病假，需要你帮忙覆盖一部分病人事务。"
	ag := &meshTestAgent{reply: visibleReply + "\n\nACTION:MESSAGE chatid=Jennifer\n" + visibleReply + "\nEND_ACTION"}
	taskClient := &meshTaskClientStub{tasks: []MeshRuntimeTask{{
		ID:           "task-clean-fallback",
		Intent:       "coverage.transfer",
		Title:        "Alexis absence coverage",
		Instructions: "Coordinate Jennifer/Karen coverage.",
	}}}
	runner := NewMeshRunner(MeshRunnerOptions{
		Client:        taskClient,
		Agent:         ag,
		ReplyClient:   botClient,
		ActionClient:  botClient,
		DefaultChatID: "admin-chat",
		Capabilities:  []string{"message", "summary"},
	})

	if err := runner.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}
	if sentByPath["/team-messaging/v1/chats/admin-chat/posts"] != visibleReply {
		t.Fatalf("shared chat message = %q, want clean reply fallback; all=%#v", sentByPath["/team-messaging/v1/chats/admin-chat/posts"], sentByPath)
	}
	if sentByPath["/team-messaging/v1/chats/owner-dm/posts"] != visibleReply {
		t.Fatalf("owner DM message = %q, want clean reply fallback; all=%#v", sentByPath["/team-messaging/v1/chats/owner-dm/posts"], sentByPath)
	}
	if len(taskClient.responses) != 1 {
		t.Fatalf("responses = %#v", taskClient.responses)
	}
	events := taskClient.responses[0].ActionEvents
	var sawFailedAction bool
	var sawCleanFallback bool
	var sawDMUpdate bool
	for _, event := range events {
		if event.Type == "MESSAGE" && event.Status == "failed" {
			sawFailedAction = true
		}
		if event.Type == "MESSAGE" && event.Status == "completed" && event.Details["mesh_clean_reply_fallback"] == true {
			sawCleanFallback = true
		}
		if event.Type == "MESSAGE" && event.Status == "completed" && event.Details["mesh_owner_dm_update"] == true {
			sawDMUpdate = true
		}
	}
	if !sawFailedAction || !sawCleanFallback || !sawDMUpdate {
		t.Fatalf("action events = %#v", events)
	}
}

func TestMeshRunnerPassesRolePeersToAgentActions(t *testing.T) {
	var sentPath string
	var sentText string
	var mentionIDs []int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/group/31462793222/post" {
			var body struct {
				Text           string  `json:"text"`
				MentionItemIDs []int64 `json:"mention_item_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode group post: %v", err)
			}
			sentPath = r.URL.Path
			sentText = body.Text
			mentionIDs = append(mentionIDs, body.MentionItemIDs...)
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "post-1", Text: body.Text})
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/team-messaging/v1/chats/clinical-shared-chat" {
			_ = json.NewEncoder(w).Encode(ringcentral.Chat{
				ID: "clinical-shared-chat", Type: "Team",
				Members: []ringcentral.ChatMember{{
					ID:        "clinical-person",
					FirstName: "clinical-bot",
				}},
			})
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/directory/entries/search") {
			_ = json.NewEncoder(w).Encode(ringcentral.DirectorySearchResult{Records: []ringcentral.DirectoryEntry{{
				ID:        "clinical-person",
				FirstName: "clinical-bot",
			}}})
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/posts") {
			var body struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode post: %v", err)
			}
			sentPath = r.URL.Path
			sentText = body.Text
			_ = json.NewEncoder(w).Encode(ringcentral.Post{ID: "post-1", Text: body.Text})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	botClient := ringcentral.NewBotClient(server.URL, "bot-token")
	botClient.SetOwnerID("nursecoord-ext")
	ag := &meshTestAgent{reply: "I will ask clinical-bot to review.\n\nACTION:MESSAGE to_role_id=role-clinical-bot\nPlease review the clinical follow-up package.\nEND_ACTION"}
	taskClient := &meshTaskClientStub{tasks: []MeshRuntimeTask{{
		ID:     "task-role-message",
		Intent: "coverage.transfer",
		Title:  "Coverage transfer",
		Context: MeshRuntimeContextPackage{
			Summary: "Nurse coordinator needs clinical bot support.",
		},
	}}}
	runner := NewMeshRunner(MeshRunnerOptions{
		Client:        taskClient,
		Agent:         ag,
		ReplyClient:   botClient,
		ActionClient:  botClient,
		DefaultChatID: "nursecoord-chat",
		RolePeers: map[string]RolePeer{
			"role-clinical-bot": {
				RoleID:        "role-clinical-bot",
				DisplayName:   "clinical-bot",
				ExtensionID:   "clinical-ext",
				PersonID:      "87368474627",
				SharedChatIDs: []string{"31462793222"},
			},
		},
	})

	if err := runner.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}
	if sentPath != "/api/group/31462793222/post" {
		t.Fatalf("sent path = %q", sentPath)
	}
	if len(mentionIDs) != 1 || mentionIDs[0] != 87368474627 {
		t.Fatalf("mention_item_ids = %#v", mentionIDs)
	}
	if !strings.Contains(sentText, `rel='{"id":87368474627}'`) {
		t.Fatalf("sent text = %q", sentText)
	}
	if len(taskClient.responses) != 1 {
		t.Fatalf("responses = %#v", taskClient.responses)
	}
	events := taskClient.responses[0].ActionEvents
	if len(events) != 1 || events[0].Details["to_role_id"] != "role-clinical-bot" {
		t.Fatalf("action events = %#v", events)
	}
}

func TestMeshRunnerSupportsWaitingStatus(t *testing.T) {
	ag := &meshTestAgent{reply: "MESH_STATUS: waiting\nWaiting for Jennifer to reply YES within 15 minutes."}
	taskClient := &meshTaskClientStub{tasks: []MeshRuntimeTask{{ID: "task-2", Intent: "coverage.transfer", Title: "Wait for backup"}}}
	runner := NewMeshRunner(MeshRunnerOptions{
		Client:        taskClient,
		Agent:         ag,
		DefaultChatID: "admin-chat",
	})

	if err := runner.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}
	if len(taskClient.responses) != 1 {
		t.Fatalf("responses = %#v", taskClient.responses)
	}
	resp := taskClient.responses[0]
	if resp.Status != MeshRuntimeTaskStatusWaiting {
		t.Fatalf("response status = %q", resp.Status)
	}
	if strings.Contains(resp.Result, "MESH_STATUS") || !strings.Contains(resp.Result, "Waiting for Jennifer") {
		t.Fatalf("response result = %q", resp.Result)
	}
}

func TestMeshRunnerAllowsTargetAgentToDelegateMeshTask(t *testing.T) {
	ag := &meshTestAgent{reply: "Clinical handoff is needed.\n\nACTION:MESH_TASK to_role_id=role-clinical-bot intent=clinical.handoff title=\"Clinical handoff\"\nShare coverage context with clinical team.\nEND_ACTION"}
	taskClient := &meshTaskClientStub{tasks: []MeshRuntimeTask{{
		ID:     "task-3",
		Intent: "coverage.transfer",
		Title:  "Coverage transfer",
		Context: MeshRuntimeContextPackage{
			Summary: "Alexis is absent and clinical follow-up is needed.",
		},
	}}}
	runner := NewMeshRunner(MeshRunnerOptions{
		Client:        taskClient,
		Agent:         ag,
		DefaultChatID: "admin-chat",
	})

	if err := runner.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}
	if len(taskClient.created) != 1 {
		t.Fatalf("created mesh tasks = %#v", taskClient.created)
	}
	created := taskClient.created[0]
	if created.ToRoleID != "role-clinical-bot" || created.Intent != "clinical.handoff" {
		t.Fatalf("created mesh task = %#v", created)
	}
	if len(taskClient.responses) != 1 {
		t.Fatalf("responses = %#v", taskClient.responses)
	}
	resp := taskClient.responses[0]
	if strings.Contains(resp.Result, "delegated-task-1") {
		t.Fatalf("response leaked delegated task id: %#v", resp)
	}
	if len(resp.ActionEvents) != 1 || resp.ActionEvents[0].Type != "MESH_TASK" || resp.ActionEvents[0].Status != "completed" {
		t.Fatalf("action events = %#v", resp.ActionEvents)
	}
}

func TestMeshRunnerDelegatedTaskCarriesSourceTaskAndAgent(t *testing.T) {
	ag := &meshTestAgent{reply: "ACTION:MESH_TASK to_role_id=role-clinical-bot intent=clinical.handoff title=\"Clinical handoff\"\nShare clinical follow-up context.\nEND_ACTION"}
	taskClient := &meshTaskClientStub{tasks: []MeshRuntimeTask{{
		ID:     "task-3",
		Intent: "coverage.transfer",
		Title:  "Coverage transfer",
	}}}
	runner := NewMeshRunner(MeshRunnerOptions{
		Client:        taskClient,
		Agent:         ag,
		DefaultChatID: "admin-chat",
		SourceAgentID: "mesh-agent-nursecoord",
	})

	if err := runner.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}
	if len(taskClient.created) != 1 {
		t.Fatalf("created mesh tasks = %#v", taskClient.created)
	}
	data := taskClient.created[0].Context.Data
	if got, _ := data["source_task_id"].(string); got != "task-3" {
		t.Fatalf("source_task_id = %#v; data=%#v", data["source_task_id"], data)
	}
	if got, _ := data["source_agent_id"].(string); got != "mesh-agent-nursecoord" {
		t.Fatalf("source_agent_id = %#v; data=%#v", data["source_agent_id"], data)
	}
	if got, _ := data["origin_chat_id"].(string); got != "admin-chat" {
		t.Fatalf("origin_chat_id = %#v; data=%#v", data["origin_chat_id"], data)
	}
}

func TestBuildMeshTaskPromptRequiresActionBlocksForExecutedWork(t *testing.T) {
	got := buildMeshTaskPrompt(MeshRuntimeTask{ID: "task-1", Intent: "coverage.transfer"})
	if !strings.Contains(got, "If you claim an SMS was sent") ||
		!strings.Contains(got, "MUST include the matching ACTION block") ||
		!strings.Contains(got, "record action_events") {
		t.Fatalf("mesh task prompt missing execution/action_event guard: %s", got)
	}
	if !strings.Contains(got, "ACTION:MESSAGE") ||
		!strings.Contains(got, "ACTION:SMS") ||
		!strings.Contains(got, "ACTION:MESH_TASK") {
		t.Fatalf("mesh task prompt missing full action protocol: %s", got)
	}
}
