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
	PlanID       string                    `json:"plan_id,omitempty"`
	AccountID    string                    `json:"account_id,omitempty"`
	FromAgentID  string                    `json:"from_agent_id,omitempty"`
	ToAgentID    string                    `json:"to_agent_id,omitempty"`
	ToRoleID     string                    `json:"to_role_id,omitempty"`
	TraceID      string                    `json:"trace_id,omitempty"`
	Intent       string                    `json:"intent"`
	Title        string                    `json:"title,omitempty"`
	Instructions string                    `json:"instructions,omitempty"`
	Status       string                    `json:"status,omitempty"`
	Context      MeshRuntimeContextPackage `json:"context,omitempty"`
	RoutePlan    MeshRuntimeRoutePlan      `json:"route_plan,omitempty"`
}

type MeshRuntimeRoutePlan struct {
	PlanID          string                         `json:"plan_id,omitempty"`
	TraceID         string                         `json:"trace_id,omitempty"`
	FromAgentID     string                         `json:"from_agent_id,omitempty"`
	ToAgentID       string                         `json:"to_agent_id,omitempty"`
	ToRoleID        string                         `json:"to_role_id,omitempty"`
	TargetBotID     string                         `json:"target_bot_id,omitempty"`
	Intent          string                         `json:"intent,omitempty"`
	RoutingDecision MeshRuntimeRoutingDecision     `json:"routing_decision,omitempty"`
	VisibleDelivery MeshRuntimeVisibleDeliveryPlan `json:"visible_delivery,omitempty"`
	CallbackPolicy  MeshRuntimeCallbackPolicy      `json:"callback_policy,omitempty"`
}

type MeshRuntimeRoutingDecision struct {
	Mode             string   `json:"mode,omitempty"`
	Intent           string   `json:"intent,omitempty"`
	RequestedRoleID  string   `json:"requested_role_id,omitempty"`
	SelectedRoleID   string   `json:"selected_role_id,omitempty"`
	CandidateRoleIDs []string `json:"candidate_role_ids,omitempty"`
	Reason           string   `json:"reason,omitempty"`
}

type MeshRuntimeVisibleDeliveryPlan struct {
	Enabled           bool   `json:"enabled,omitempty"`
	Transport         string `json:"transport,omitempty"`
	ChatID            string `json:"chat_id,omitempty"`
	MentionPersonID   string `json:"mention_person_id,omitempty"`
	MentionLabel      string `json:"mention_label,omitempty"`
	TargetExtensionID string `json:"target_extension_id,omitempty"`
	TargetRoleID      string `json:"target_role_id,omitempty"`
	TargetAgentID     string `json:"target_agent_id,omitempty"`
	TargetBotID       string `json:"target_bot_id,omitempty"`
}

type MeshRuntimeCallbackPolicy struct {
	NotifyOwnerDM    bool `json:"notify_owner_dm,omitempty"`
	NotifyOriginChat bool `json:"notify_origin_chat,omitempty"`
}

type MeshRuntimeTaskPollRequest struct {
	Limit int `json:"limit,omitempty"`
}

type MeshRuntimeTaskCreateRequest struct {
	PlanID       string                    `json:"plan_id,omitempty"`
	ToRoleID     string                    `json:"to_role_id"`
	Intent       string                    `json:"intent"`
	Title        string                    `json:"title,omitempty"`
	Instructions string                    `json:"instructions,omitempty"`
	Context      MeshRuntimeContextPackage `json:"context,omitempty"`
}

type MeshRuntimeOrchestrationStartRequest struct {
	PlanID  string                    `json:"plan_id,omitempty"`
	Intent  string                    `json:"intent"`
	Title   string                    `json:"title,omitempty"`
	Context MeshRuntimeContextPackage `json:"context,omitempty"`
}

type MeshRuntimeOrchestrationStartResult struct {
	PlanID  string                    `json:"plan_id"`
	TraceID string                    `json:"trace_id,omitempty"`
	Context MeshRuntimeContextPackage `json:"context,omitempty"`
}

type MeshRuntimeOrchestrationDispatchRequest struct {
	PlanID       string                    `json:"plan_id,omitempty"`
	ToRoleID     string                    `json:"to_role_id,omitempty"`
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

type MeshOrchestrationStarter interface {
	StartOrchestration(context.Context, MeshRuntimeOrchestrationStartRequest) (MeshRuntimeOrchestrationStartResult, error)
}

type MeshOrchestrationDispatcher interface {
	DispatchOrchestration(context.Context, MeshRuntimeOrchestrationDispatchRequest) (MeshRuntimeTask, error)
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
	prompt := r.buildMeshTaskPrompt(task)
	reply, err := r.agent.Chat(ctx, "mesh/"+task.ID, prompt)
	if err != nil {
		return r.client.RespondMeshTask(ctx, task.ID, MeshRuntimeTaskResponse{
			Status: MeshRuntimeTaskStatusFailed,
			Result: err.Error(),
		})
	}
	cleanReply, actions := ParseAgentActions(reply)
	if requiredAction := r.coverageRequiredActionType(task); requiredAction != "" && !hasMeshActionType(actions, requiredAction) {
		correctionPrompt := r.buildCoverageSMSCorrectionPrompt(task, reply)
		retryReply, err := r.agent.Chat(ctx, "mesh/"+task.ID, correctionPrompt)
		if err != nil {
			return r.client.RespondMeshTask(ctx, task.ID, MeshRuntimeTaskResponse{
				Status: MeshRuntimeTaskStatusFailed,
				Result: fmt.Sprintf("coverage.transfer requires ACTION:%s but corrective retry failed: %s", requiredAction, err.Error()),
			})
		}
		reply = retryReply
		cleanReply, actions = ParseAgentActions(reply)
	}
	status, result := parseMeshRuntimeStatus(cleanReply)
	cleanResult := strings.TrimSpace(result)
	actions, blockedActions := r.filterAllowedActions(actions)
	actionEvents := r.blockedActionEvents(task, blockedActions)
	missingRequiredEvents := r.missingRequiredActionEvents(task, actions)
	actionEvents = append(actionEvents, missingRequiredEvents...)
	if len(missingRequiredEvents) > 0 {
		status = MeshRuntimeTaskStatusFailed
		result = strings.TrimSpace(result + "\ncoverage.transfer requires an executable " + missingRequiredEvents[0].Type + " action before it can wait for backup replies.")
		cleanResult = strings.TrimSpace(result)
	}
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
		var orchestrationDispatcher MeshOrchestrationDispatcher
		if dispatcher, ok := r.client.(MeshOrchestrationDispatcher); ok {
			orchestrationDispatcher = dispatcher
		}
		actionResults := ExecuteAgentActions(ctx, r.replyClient, r.actionClient, r.defaultChatID, actions, ActionContext{
			OriginIsOwner:           true,
			RequesterID:             "mesh-runtime",
			OwnerID:                 ownerID,
			OwnerDMChat:             ownerDMChat,
			Capabilities:            r.capabilities,
			SourceTaskID:            task.ID,
			SourceAgentID:           r.sourceAgentID,
			PlanID:                  task.PlanID,
			MeshTaskCreator:         r.client,
			OrchestrationDispatcher: orchestrationDispatcher,
			RolePeers:               cloneRolePeers(r.rolePeers),
		})
		restore()
		if len(actionResults) > 0 {
			result = strings.TrimSpace(result + "\n" + strings.Join(actionResults, "\n"))
		}
	}
	if shouldPostMeshCleanReplyFallback(cleanResult, actionEvents) && r.replyClient != nil && strings.TrimSpace(r.defaultChatID) != "" {
		if err := SendTextReply(ctx, r.replyClient, r.defaultChatID, cleanResult); err != nil {
			details := r.meshTaskActionDetails(task, map[string]any{
				"mesh_clean_reply_fallback": true,
				"error":                     err.Error(),
			})
			actionEvents = append(actionEvents, MeshRuntimeTaskActionEvent{
				Type:    "MESSAGE",
				Status:  "failed",
				Details: convertMeshActionDetails(actionEventDetails(r.defaultChatID, r.defaultChatID, false, details)),
			})
		} else {
			details := r.meshTaskActionDetails(task, map[string]any{
				"mesh_clean_reply_fallback": true,
			})
			actionEvents = append(actionEvents, MeshRuntimeTaskActionEvent{
				Type:    "MESSAGE",
				Status:  "completed",
				Details: convertMeshActionDetails(actionEventDetails(r.defaultChatID, r.defaultChatID, false, details)),
			})
		}
		ownerDMChat := strings.TrimSpace(r.replyClient.DMChatID())
		if ownerDMChat != "" && ownerDMChat != strings.TrimSpace(r.defaultChatID) {
			if err := SendTextReply(ctx, r.replyClient, ownerDMChat, cleanResult); err != nil {
				details := r.meshTaskActionDetails(task, map[string]any{
					"mesh_owner_dm_update": true,
					"error":                err.Error(),
				})
				actionEvents = append(actionEvents, MeshRuntimeTaskActionEvent{
					Type:    "MESSAGE",
					Status:  "failed",
					Details: convertMeshActionDetails(actionEventDetails(r.defaultChatID, ownerDMChat, true, details)),
				})
			} else {
				details := r.meshTaskActionDetails(task, map[string]any{
					"mesh_owner_dm_update": true,
				})
				actionEvents = append(actionEvents, MeshRuntimeTaskActionEvent{
					Type:    "MESSAGE",
					Status:  "completed",
					Details: convertMeshActionDetails(actionEventDetails(r.defaultChatID, ownerDMChat, true, details)),
				})
			}
		}
	}
	return r.client.RespondMeshTask(ctx, task.ID, MeshRuntimeTaskResponse{
		Status:       status,
		Result:       strings.TrimSpace(result),
		ActionEvents: actionEvents,
	})
}

func (r *MeshRunner) coverageRequiredActionType(task MeshRuntimeTask) string {
	if !strings.EqualFold(strings.TrimSpace(task.Intent), "coverage.transfer") {
		return ""
	}
	if containsMeshAllowedAction(r.allowedActions, "CARD") {
		return "CARD"
	}
	if containsMeshAllowedAction(r.allowedActions, "SMS") {
		return "SMS"
	}
	return ""
}

func hasMeshActionType(actions []AgentAction, actionType string) bool {
	for _, action := range actions {
		if strings.EqualFold(strings.TrimSpace(action.Type), strings.TrimSpace(actionType)) {
			return true
		}
	}
	return false
}

func (r *MeshRunner) buildCoverageSMSCorrectionPrompt(task MeshRuntimeTask, previousReply string) string {
	requiredAction := r.coverageRequiredActionType(task)
	var b strings.Builder
	b.WriteString(r.buildMeshTaskPrompt(task))
	b.WriteString(fmt.Sprintf("\n\nPrevious mesh response did not include an executable ACTION:%s, so the coverage transfer cannot advance to waiting state yet.\n", requiredAction))
	b.WriteString("Return a corrected response now. ")
	if requiredAction == "CARD" {
		b.WriteString("Include ACTION:CARD with an Adaptive Card status update in the shared/admin group chat. The card should show the coverage request, Jennifer/Karen candidates, response window, and current waiting status. ")
		b.WriteString("Do not use message, task, mesh delegation, or SMS outreach as a substitute for this required card update. ")
	} else {
		b.WriteString("Include ACTION:SMS blocks for the backup coverage outreach using the phone numbers from DOMAIN.md. ")
		b.WriteString("Do not use ACTION:MESSAGE, ACTION:TASK, ACTION:MESH_TASK, or ACTION:CARD as a substitute for this required SMS outreach. ")
	}
	b.WriteString(fmt.Sprintf("After the ACTION:%s block(s), use MESH_STATUS: waiting only if you are waiting for candidate replies.\n", requiredAction))
	if previous := strings.TrimSpace(previousReply); previous != "" {
		b.WriteString("\nPrevious response:\n")
		b.WriteString(previous)
		b.WriteString("\n")
	}
	return b.String()
}

func (r *MeshRunner) missingRequiredActionEvents(task MeshRuntimeTask, actions []AgentAction) []MeshRuntimeTaskActionEvent {
	requiredAction := r.coverageRequiredActionType(task)
	if requiredAction == "" || hasMeshActionType(actions, requiredAction) {
		return nil
	}
	details := r.meshTaskActionDetails(task, map[string]any{
		"reason":          "required_" + strings.ToLower(requiredAction) + "_action_missing",
		"required_action": requiredAction,
		"allowed_actions": append([]string(nil), r.allowedActions...),
	})
	return []MeshRuntimeTaskActionEvent{{
		Type:    requiredAction,
		Status:  "blocked",
		Details: convertMeshActionDetails(details),
	}}
}

func (r *MeshRunner) blockedActionEvents(task MeshRuntimeTask, actions []AgentAction) []MeshRuntimeTaskActionEvent {
	if len(actions) == 0 {
		return nil
	}
	events := make([]MeshRuntimeTaskActionEvent, 0, len(actions))
	for _, action := range actions {
		details := r.meshTaskActionDetails(task, map[string]any{
			"reason":          "mesh_action_not_allowed",
			"allowed_actions": append([]string(nil), r.allowedActions...),
			"action_type":     strings.ToUpper(strings.TrimSpace(action.Type)),
		})
		if len(action.Params) > 0 {
			details["action_params"] = action.Params
		}
		if body := strings.TrimSpace(action.Body); body != "" {
			details["action_body"] = body
		}
		events = append(events, MeshRuntimeTaskActionEvent{
			Type:    strings.ToUpper(strings.TrimSpace(action.Type)),
			Status:  "blocked",
			Details: convertMeshActionDetails(details),
		})
	}
	return events
}

func (r *MeshRunner) meshTaskActionDetails(task MeshRuntimeTask, extra map[string]any) map[string]any {
	details := map[string]any{
		"task_id":         task.ID,
		"plan_id":         task.PlanID,
		"trace_id":        task.TraceID,
		"intent":          task.Intent,
		"from_agent_id":   task.FromAgentID,
		"to_agent_id":     task.ToAgentID,
		"to_role_id":      task.ToRoleID,
		"source_agent_id": r.sourceAgentID,
	}
	for key, value := range extra {
		details[key] = value
	}
	return details
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

func shouldPostMeshCleanReplyFallback(cleanReply string, events []MeshRuntimeTaskActionEvent) bool {
	if strings.TrimSpace(cleanReply) == "" {
		return false
	}
	for _, event := range events {
		if strings.EqualFold(strings.TrimSpace(event.Type), "MESSAGE") && strings.EqualFold(strings.TrimSpace(event.Status), "completed") {
			return false
		}
	}
	return true
}

func (r *MeshRunner) buildMeshTaskPrompt(task MeshRuntimeTask) string {
	if r == nil {
		return buildMeshTaskPrompt(task)
	}
	return buildMeshTaskPromptWithAllowedActions(task, r.allowedActions)
}

func buildMeshTaskPrompt(task MeshRuntimeTask) string {
	return buildMeshTaskPromptWithAllowedActions(task, nil)
}

func buildMeshTaskPromptWithAllowedActions(task MeshRuntimeTask, allowedActions []string) string {
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
	if len(allowedActions) > 0 {
		b.WriteString("Allowed ACTION types for this target role: " + strings.Join(allowedActions, ", ") + ". ")
		b.WriteString("Do not output ACTION types outside this list; if required work is not allowed, explain what is waiting instead.\n")
	}
	if strings.EqualFold(strings.TrimSpace(task.Intent), "coverage.transfer") && containsMeshAllowedAction(allowedActions, "CARD") {
		b.WriteString("For coverage.transfer, first post an ACTION:CARD status update in the shared/admin group chat using details from DOMAIN.md, including Jennifer/Karen candidates, response window, and current waiting status. ")
		b.WriteString("Do not use message, task, mesh delegation, or SMS outreach as a substitute for the required status card; after emitting the card, use MESH_STATUS: waiting if you are waiting for replies.\n")
	} else if strings.EqualFold(strings.TrimSpace(task.Intent), "coverage.transfer") && containsMeshAllowedAction(allowedActions, "SMS") {
		b.WriteString("For coverage.transfer, first execute backup coverage outreach with ACTION:SMS to the phone numbers in DOMAIN.md. ")
		b.WriteString("Do not use ACTION:MESSAGE or ACTION:TASK as a substitute for SMS outreach; after emitting SMS or timeout actions, use MESH_STATUS: waiting if you are waiting for replies.\n")
	}
	b.WriteString("Return useful text plus ACTION blocks when work must be executed. ")
	b.WriteString("If you claim an SMS was sent, a task was created, a message was posted, or a card was created, you MUST include the matching ACTION block so the runtime can execute it and record action_events. ")
	b.WriteString("If you cannot or should not execute the action yet, say it is prepared or waiting; do not say it has already been sent or created. ")
	b.WriteString("You are the target agent for this task. If the instructions or context say to contact, coordinate with, or notify your own role/name, treat it as your own assignment. Do not say you will contact yourself; say what you are doing directly. ")
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

func (r *MeshRunner) filterAllowedActions(actions []AgentAction) ([]AgentAction, []AgentAction) {
	if len(r.allowedActions) == 0 {
		return actions, nil
	}
	var out []AgentAction
	var blocked []AgentAction
	for _, action := range actions {
		if containsMeshAllowedAction(r.allowedActions, action.Type) {
			out = append(out, action)
		} else {
			blocked = append(blocked, action)
		}
	}
	return out, blocked
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
