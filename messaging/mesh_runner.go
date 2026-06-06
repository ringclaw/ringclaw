package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"
)

const (
	MeshRuntimeTaskStatusWaiting   = "waiting"
	MeshRuntimeTaskStatusCompleted = "completed"
	MeshRuntimeTaskStatusFailed    = "failed"
)

type MeshRuntimeContextPackage struct {
	Summary    string                 `json:"summary,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"`
	MemoryRefs []MeshRuntimeMemoryRef `json:"memory_refs,omitempty"`
}

type MeshRuntimeMemoryRef struct {
	Namespace string `json:"namespace,omitempty"`
	EntityID  string `json:"entity_id"`
	Purpose   string `json:"purpose,omitempty"`
}

type MeshRuntimeTask struct {
	ID           string                    `json:"id"`
	AccountID    string                    `json:"account_id,omitempty"`
	FromAgentID  string                    `json:"from_agent_id,omitempty"`
	ToAgentID    string                    `json:"to_agent_id,omitempty"`
	ToRoleID     string                    `json:"to_role_id,omitempty"`
	Intent       string                    `json:"intent"`
	Title        string                    `json:"title,omitempty"`
	Instructions string                    `json:"instructions,omitempty"`
	Status       string                    `json:"status,omitempty"`
	Context      MeshRuntimeContextPackage `json:"context,omitempty"`
}

type MeshRuntimeTaskPollRequest struct {
	Limit int `json:"limit,omitempty"`
}

type MeshRuntimeTaskCreateRequest struct {
	ToRoleID     string                    `json:"to_role_id"`
	Intent       string                    `json:"intent"`
	Title        string                    `json:"title,omitempty"`
	Instructions string                    `json:"instructions,omitempty"`
	Context      MeshRuntimeContextPackage `json:"context,omitempty"`
}

type MeshRuntimeTaskActionEvent struct {
	Type    string                 `json:"type"`
	Status  string                 `json:"status"`
	Details map[string]interface{} `json:"details,omitempty"`
}

type MeshRuntimeTaskResponse struct {
	TaskID       string                       `json:"task_id,omitempty"`
	Status       string                       `json:"status"`
	Result       string                       `json:"result,omitempty"`
	ActionEvents []MeshRuntimeTaskActionEvent `json:"action_events,omitempty"`
	Details      map[string]interface{}       `json:"details,omitempty"`
}

type MeshTaskClient interface {
	PollMeshTasks(context.Context, MeshRuntimeTaskPollRequest) ([]MeshRuntimeTask, error)
	CreateMeshTask(context.Context, MeshRuntimeTaskCreateRequest) (MeshRuntimeTask, error)
	RespondMeshTask(context.Context, string, MeshRuntimeTaskResponse) error
}

type MeshRunnerOptions struct {
	Client         MeshTaskClient
	Agent          agent.Agent
	ReplyClient    *ringcentral.Client
	ActionClient   *ringcentral.Client
	DefaultChatID  string
	Capabilities   []string
	AllowedActions []string
	RolePeers      map[string]RolePeer
	SourceAgentID  string
}

type MeshRunner struct {
	client         MeshTaskClient
	agent          agent.Agent
	replyClient    *ringcentral.Client
	actionClient   *ringcentral.Client
	defaultChatID  string
	capabilities   []string
	allowedActions []string
	rolePeers      map[string]RolePeer
	sourceAgentID  string
}

func NewMeshRunner(opts MeshRunnerOptions) *MeshRunner {
	return &MeshRunner{
		client:         opts.Client,
		agent:          opts.Agent,
		replyClient:    opts.ReplyClient,
		actionClient:   opts.ActionClient,
		defaultChatID:  strings.TrimSpace(opts.DefaultChatID),
		capabilities:   append([]string(nil), opts.Capabilities...),
		allowedActions: normalizeMeshAllowedActions(opts.AllowedActions),
		rolePeers:      cloneRolePeers(opts.RolePeers),
		sourceAgentID:  strings.TrimSpace(opts.SourceAgentID),
	}
}

func (r *MeshRunner) ProcessOnce(ctx context.Context) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("mesh task client is required")
	}
	if r.agent == nil {
		return fmt.Errorf("mesh agent runtime is not ready")
	}
	tasks, err := r.client.PollMeshTasks(ctx, MeshRuntimeTaskPollRequest{Limit: 10})
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if err := r.processTask(ctx, task); err != nil {
			return err
		}
	}
	return nil
}

func (r *MeshRunner) processTask(ctx context.Context, task MeshRuntimeTask) error {
	prompt := buildMeshTaskPrompt(task)
	reply, err := r.agent.Chat(ctx, "mesh/"+task.ID, prompt)
	if err != nil {
		return r.client.RespondMeshTask(ctx, task.ID, MeshRuntimeTaskResponse{
			Status: MeshRuntimeTaskStatusFailed,
			Result: err.Error(),
		})
	}
	cleanReply, actions := ParseAgentActions(reply)
	status, result := parseMeshRuntimeStatus(cleanReply)
	actions = r.filterAllowedActions(actions)
	actionEvents := []MeshRuntimeTaskActionEvent{}
	if len(actions) > 0 {
		if (r.replyClient == nil || r.actionClient == nil) && hasNonMeshTaskAction(actions) {
			return r.client.RespondMeshTask(ctx, task.ID, MeshRuntimeTaskResponse{
				Status: MeshRuntimeTaskStatusFailed,
				Result: "mesh task produced ACTION blocks but RingCentral clients are not ready",
			})
		}
		restore := SetActionEventRecorder(func(_ context.Context, event ActionEvent) {
			actionEvents = append(actionEvents, MeshRuntimeTaskActionEvent{
				Type:    event.Type,
				Status:  event.Status,
				Details: convertMeshActionDetails(event.Details),
			})
		})
		ownerID := ""
		ownerDMChat := ""
		if r.replyClient != nil {
			ownerID = r.replyClient.OwnerID()
			ownerDMChat = r.replyClient.DMChatID()
		}
		actionResults := ExecuteAgentActions(ctx, r.replyClient, r.actionClient, r.defaultChatID, actions, ActionContext{
			OriginIsOwner:   true,
			RequesterID:     "mesh-runtime",
			OwnerID:         ownerID,
			OwnerDMChat:     ownerDMChat,
			Capabilities:    r.capabilities,
			SourceTaskID:    task.ID,
			SourceAgentID:   r.sourceAgentID,
			MeshTaskCreator: r.client,
			RolePeers:       cloneRolePeers(r.rolePeers),
		})
		restore()
		if len(actionResults) > 0 {
			result = strings.TrimSpace(result + "\n" + strings.Join(actionResults, "\n"))
		}
	}
	return r.client.RespondMeshTask(ctx, task.ID, MeshRuntimeTaskResponse{
		Status:       status,
		Result:       strings.TrimSpace(result),
		ActionEvents: actionEvents,
	})
}

func cloneRolePeers(peers map[string]RolePeer) map[string]RolePeer {
	if len(peers) == 0 {
		return nil
	}
	out := make(map[string]RolePeer, len(peers))
	for roleID, peer := range peers {
		peer.SharedChatIDs = append([]string(nil), peer.SharedChatIDs...)
		out[roleID] = peer
	}
	return out
}

func hasNonMeshTaskAction(actions []AgentAction) bool {
	for _, action := range actions {
		if strings.ToUpper(strings.TrimSpace(action.Type)) != "MESH_TASK" {
			return true
		}
	}
	return false
}

func buildMeshTaskPrompt(task MeshRuntimeTask) string {
	var b strings.Builder
	b.WriteString("You are executing an AVA Agent Mesh task.\n")
	if task.Intent != "" {
		b.WriteString("Intent: " + task.Intent + "\n")
	}
	if task.Title != "" {
		b.WriteString("Title: " + task.Title + "\n")
	}
	if task.Instructions != "" {
		b.WriteString("Instructions: " + task.Instructions + "\n")
	}
	if task.Context.Summary != "" {
		b.WriteString("Context summary: " + task.Context.Summary + "\n")
	}
	if len(task.Context.MemoryRefs) > 0 || len(task.Context.Data) > 0 {
		if data, err := json.Marshal(task.Context); err == nil {
			b.WriteString("Context package JSON: " + string(data) + "\n")
		}
	}
	b.WriteString("Return useful text plus ACTION blocks when work must be executed. ")
	b.WriteString("If you claim an SMS was sent, a task was created, a message was posted, or a card was created, you MUST include the matching ACTION block so the runtime can execute it and record action_events. ")
	b.WriteString("If you cannot or should not execute the action yet, say it is prepared or waiting; do not say it has already been sent or created. ")
	b.WriteString("Use `MESH_STATUS: waiting` only when the task is waiting for an external response or deadline.\n")
	b.WriteString(ActionPrompt())
	return b.String()
}

func parseMeshRuntimeStatus(reply string) (string, string) {
	status := MeshRuntimeTaskStatusCompleted
	var lines []string
	for _, line := range strings.Split(reply, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "mesh_status:") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, strings.Split(trimmed, ":")[0]+":"))
			switch strings.ToLower(value) {
			case MeshRuntimeTaskStatusWaiting:
				status = MeshRuntimeTaskStatusWaiting
			case MeshRuntimeTaskStatusFailed:
				status = MeshRuntimeTaskStatusFailed
			case MeshRuntimeTaskStatusCompleted:
				status = MeshRuntimeTaskStatusCompleted
			}
			continue
		}
		lines = append(lines, line)
	}
	return status, strings.TrimSpace(strings.Join(lines, "\n"))
}

func (r *MeshRunner) filterAllowedActions(actions []AgentAction) []AgentAction {
	if len(r.allowedActions) == 0 {
		return actions
	}
	var out []AgentAction
	for _, action := range actions {
		if containsMeshAllowedAction(r.allowedActions, action.Type) {
			out = append(out, action)
		}
	}
	return out
}

func normalizeMeshAllowedActions(values []string) []string {
	var out []string
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value != "" && !containsMeshAllowedAction(out, value) {
			out = append(out, value)
		}
	}
	return out
}

func containsMeshAllowedAction(values []string, value string) bool {
	value = strings.ToUpper(strings.TrimSpace(value))
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func convertMeshActionDetails(in map[string]any) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
