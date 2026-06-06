package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/internal/util"
	"github.com/ringclaw/ringclaw/messaging/oob"
	"github.com/ringclaw/ringclaw/paths"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// crossChatNoticeTimeout bounds how long we wait for the synchronous
// owner-DM heads-up before giving up and refusing the cross-chat
// ACTION. Declared as a var so tests can shrink the wait without
// changing production behavior.
var crossChatNoticeTimeout = 5 * time.Second

// AgentAction represents a parsed action from the agent's response.
type AgentAction struct {
	Type   string // "NOTE", "TASK", "EVENT", "CARD", "MESSAGE", "VIDEO", "VIDEO_LIST", "PHONE_CALL", "PHONE_CALLLOG", "SMS"
	Params map[string]string
	Body   string
}

type RolePeer struct {
	RoleID        string
	RoleName      string
	BotID         string
	DisplayName   string
	ExtensionID   string
	PersonID      string
	SharedChatIDs []string
}

type MeshTaskCreator interface {
	CreateMeshTask(context.Context, MeshRuntimeTaskCreateRequest) (MeshRuntimeTask, error)
}

// ParseAgentActions extracts ACTION blocks from the agent's response and returns
// the clean reply text (without ACTION blocks) and the parsed actions.
func ParseAgentActions(reply string) (string, []AgentAction) {
	var actions []AgentAction
	clean := reply

	for {
		startIdx := strings.Index(clean, "ACTION:")
		if startIdx < 0 {
			break
		}
		endIdx := strings.Index(clean[startIdx:], "END_ACTION")
		if endIdx < 0 {
			// No END_ACTION: treat the single line as a complete action (e.g. EVENT).
			lineEnd := strings.Index(clean[startIdx:], "\n")
			if lineEnd < 0 {
				lineEnd = len(clean) - startIdx
			}
			block := clean[startIdx : startIdx+lineEnd]
			action := parseActionBlock(block)
			if action != nil {
				actions = append(actions, *action)
			}
			clean = clean[:startIdx] + clean[startIdx+lineEnd:]
			continue
		}
		endIdx += startIdx + len("END_ACTION")

		block := clean[startIdx:endIdx]
		action := parseActionBlock(block)
		if action != nil {
			actions = append(actions, *action)
		}

		clean = clean[:startIdx] + clean[endIdx:]
	}

	clean = strings.TrimSpace(clean)
	return clean, actions
}

func parseActionBlock(block string) *AgentAction {
	lines := strings.SplitN(block, "\n", 2)
	if len(lines) == 0 {
		return nil
	}

	header := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(header, "ACTION:") {
		return nil
	}
	header = header[len("ACTION:"):]

	actionType, remainder := parseActionHeader(header)

	params := make(map[string]string)
	if remainder != "" {
		for _, p := range parseActionParams(remainder) {
			params[p.key] = p.value
		}
	}

	body := ""
	if len(lines) > 1 {
		body = strings.TrimSuffix(lines[1], "END_ACTION")
		body = strings.TrimSpace(body)
	}

	return &AgentAction{
		Type:   actionType,
		Params: params,
		Body:   body,
	}
}

func parseActionHeader(header string) (string, string) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", ""
	}

	splitIdx := len(header)
	for i, r := range header {
		if (r >= 'A' && r <= 'Z') || r == '_' {
			continue
		}
		splitIdx = i
		break
	}

	actionType := strings.TrimSpace(header[:splitIdx])
	remainder := strings.TrimSpace(header[splitIdx:])
	remainder = strings.TrimLeft(remainder, ",，:：;-")
	remainder = strings.TrimSpace(remainder)
	return actionType, remainder
}

// parseActionParams parses "title=xxx start=2026-01-01T10:00:00Z end=2026-01-01T11:00:00Z"
func parseActionParams(s string) []keyValue {
	var result []keyValue
	keys := []string{"title", "subject", "start", "end", "chatid", "assignee", "type", "from", "to", "callerid", "playprompt", "scope", "important", "limit", "missing", "summary", "next_actions", "direction", "result", "view", "days", "date_from", "date_to", "to_role_id", "to_role", "role_id", "role", "intent", "task_intent", "instructions", "instruction", "context_summary"}
	remaining := s
	for len(remaining) > 0 {
		remaining = strings.TrimSpace(remaining)
		matched := false
		for _, key := range keys {
			prefix := key + "="
			if strings.HasPrefix(remaining, prefix) {
				remaining = remaining[len(prefix):]
				nextIdx := len(remaining)
				for _, k := range keys {
					idx := strings.Index(remaining, " "+k+"=")
					if idx >= 0 && idx < nextIdx {
						nextIdx = idx
					}
				}
				value := strings.TrimSpace(remaining[:nextIdx])
				result = append(result, keyValue{key: key, value: value})
				remaining = remaining[nextIdx:]
				matched = true
				break
			}
		}
		if !matched {
			break
		}
	}
	return result
}

// ActionContext describes who sent the originating message so that
// ExecuteAgentActions can decide whether to honor cross-chat targeting.
//
// OriginIsOwner is true when the message originated from the trusted machine
// owner (Private App owner OR an entry in source_user_ids). When false, any
// AI-emitted `chatid=` parameter is ignored and the action is forced to run
// in the origin chat to prevent lateral movement / data exfiltration
// (Finding #5 in the security review).
//
// OwnerDMChat and RequesterID drive the fail-closed cross-chat
// pre-dispatch heads-up: when an owner-initiated action is delivered
// to a chat other than the origin AND other than the owner's own DM,
// a metadata-only notice is posted SYNCHRONOUSLY to the owner DM
// before the action fires. If OwnerDMChat is empty, or the notice
// send fails (timeout / 5xx / transport error), the action is
// refused — callers see a "Refused cross-chat …" entry in the
// returned results slice and nothing is dispatched to the target
// chat. OOB is retained so the rest of the system (notably
// /full-access) can still access the manager through ActionContext,
// but ExecuteAgentActions itself does not consult it.
//
// OwnerID is the machine owner's user ID (Private App owner). It is
// used when a non-owner sender triggers a cross-chat ACTION — a
// challenge is issued and the owner must approve via /approval in
// their bot DM before the action executes. Empty when OOB is not
// configured (falls back to forcing origin chat).
type ActionContext struct {
	OriginIsOwner bool
	OOB           *oob.Manager
	OwnerDMChat   string
	RequesterID   string
	OwnerID       string
	Mentions      []ringcentral.Mention
	// RelayCollaborator identifies the "other bot" in a current-group
	// bot-to-bot handoff like "@me @otherBot ...". When set, current-chat
	// ACTION:MESSAGE replies are normalized to keep the collaborator mention
	// alive even if the model forgets it.
	RelayCollaborator *ringcentral.Mention
	// Capabilities is the runtime capability set selected during onboarding.
	// Empty means legacy behavior: allow all optional actions.
	Capabilities []string
	// OriginalText is the user message that produced the agent reply. It lets
	// action execution enforce workflow-specific safety rails even when the
	// model emits the wrong ACTION blocks.
	OriginalText string
	// SourcePostID identifies the RingCentral post that produced this action.
	// Mesh task creation carries it into Control Plane so duplicate processing
	// of the same post can be deduped across pods/restarts.
	SourcePostID string
	// SourceTaskID identifies the upstream mesh task when one agent delegates
	// to another while processing Agent Mesh work.
	SourceTaskID string
	// SourceAgentID identifies the current runtime mesh agent when available.
	SourceAgentID string
	// MeshTaskCreator lets a source RingClaw runtime delegate work into
	// AVA Control Plane Agent Mesh without carrying an admin token.
	MeshTaskCreator MeshTaskCreator
	// RolePeers maps AgentsMesh role IDs to concrete RingCentral bot
	// mention targets for real RC bot-to-bot messages.
	RolePeers map[string]RolePeer
}

// ExecuteAgentActions executes parsed actions against the RC API.
func ExecuteAgentActions(ctx context.Context, replyClient, actionClient *ringcentral.Client, chatID string, actions []AgentAction, opts ActionContext) []string {
	var results []string
	initialClinicalRefill := isInitialClinicalRefillRequest(opts.OriginalText)
	forceClinicalRefillApproval := shouldForceClinicalRefillApproval(opts.OriginalText, actions)
	if forceClinicalRefillApproval {
		actions = append([]AgentAction{buildClinicalRefillApprovalCardAction(opts.OriginalText, opts)}, actions...)
	}
	actions = normalizeLegacyMeshDelegationActions(actions, opts)
	if actionClient != nil {
		actions = ensureCoverageHandoffNoteActions(actions, opts)
	}
	for _, a := range actions {
		record := func(status string, targetChat string, crossChat bool, extra map[string]any) {
			recordAgentActionEvent(ctx, ActionEvent{
				Type:    a.Type,
				Status:  status,
				Details: actionEventDetails(chatID, targetChat, crossChat, extra),
			})
		}
		if initialClinicalRefill && isClinicalRefillProhibitedAction(a.Type) {
			record("blocked", chatID, false, map[string]any{"reason": "clinical_refill_requires_provider_card"})
			slog.Warn("action: blocked premature clinical refill action",
				"type", a.Type, "chatID", chatID, "requesterID", opts.RequesterID)
			continue
		}
		if forceClinicalRefillApproval && strings.EqualFold(a.Type, "CARD") && !cardHasClinicalRefillSubmitActions(a) {
			record("blocked", chatID, false, map[string]any{"reason": "clinical_refill_card_missing_submit_actions"})
			slog.Warn("action: replaced clinical refill card without submit actions",
				"chatID", chatID, "requesterID", opts.RequesterID)
			continue
		}
		if capability := actionCapability(a.Type); capability != "" && !isActionCapabilityAllowed(opts.Capabilities, capability) {
			record("blocked", "", false, map[string]any{"reason": "capability_disabled", "capability": capability})
			results = append(results, capabilityDisabledMessage(capability))
			continue
		}
		if a.Type == "MESH_TASK" {
			if !opts.OriginIsOwner {
				record("blocked", "", false, map[string]any{"reason": "owner_required"})
				results = append(results, "Refused mesh task: only trusted senders can delegate agent tasks.")
				continue
			}
			if opts.MeshTaskCreator == nil {
				record("failed", "", false, map[string]any{"reason": "mesh_not_configured"})
				results = append(results, "Failed to create mesh task: Agent Mesh is not configured for this bot.")
				continue
			}
			req, err := meshTaskRequestFromAction(a, opts, chatID)
			if err != nil {
				record("failed", "", false, map[string]any{"error": err.Error()})
				results = append(results, fmt.Sprintf("Failed to create mesh task: %v", err))
				continue
			}
			task, err := opts.MeshTaskCreator.CreateMeshTask(ctx, req)
			if err != nil {
				record("failed", "", false, map[string]any{"error": err.Error(), "to_role_id": req.ToRoleID, "intent": req.Intent})
				results = append(results, fmt.Sprintf("Failed to create mesh task: %v", err))
				continue
			}
			record("completed", "", false, map[string]any{"task_id": task.ID, "to_role_id": task.ToRoleID, "intent": task.Intent})
			if notified, err := notifyRolePeerForMeshTask(ctx, replyClient, actionClient, chatID, a, req, task, opts.RolePeers); err != nil {
				recordAgentActionEvent(ctx, ActionEvent{
					Type:   "MESSAGE",
					Status: "failed",
					Details: actionEventDetails(chatID, "", false, map[string]any{
						"error":      err.Error(),
						"reason":     "mesh_task_role_peer_notify_failed",
						"task_id":    task.ID,
						"to_role_id": req.ToRoleID,
					}),
				})
				slog.Warn("action: mesh task role peer notification failed", "taskID", task.ID, "toRoleID", req.ToRoleID, "error", err)
			} else if notified {
				slog.Info("action: notified role peer for mesh task", "taskID", task.ID, "toRoleID", req.ToRoleID)
			}
			continue
		}
		if a.Type == "PHONE_CALL" || a.Type == "RINGOUT" {
			if !opts.OriginIsOwner {
				record("blocked", "", false, map[string]any{"reason": "owner_required"})
				results = append(results, "Refused phone call: only the owner can start phone calls.")
				continue
			}
			req, err := phoneClientCallFromParams(ctx, actionClient, a.Params)
			if err != nil {
				record("failed", "", false, map[string]any{"error": err.Error()})
				results = append(results, fmt.Sprintf("Failed to prepare FIJI phone call: %v", err))
				continue
			}
			slog.Info("action: requested FIJI client phone call", "target", req.TargetLabel)
			record("client_action_required", "", false, map[string]any{
				"client_action": "make_call",
				"to_number":     req.ToNumber,
				"target_label":  req.TargetLabel,
				"requester_id":  opts.RequesterID,
			})
			results = append(results, formatPhoneClientCallMessage(req.TargetLabel))
			continue
		}

		targetChat := chatID
		crossChat := false
		var currentChatMention *ringcentral.Mention
		rolePeer, hasRolePeer := rolePeerForAction(a, opts.RolePeers)
		mentionID := ""
		mentionSource := ""
		targetChatSource := ""
		if hasRolePeer {
			if !opts.OriginIsOwner {
				record("blocked", "", false, map[string]any{"reason": "owner_required", "to_role_id": rolePeer.RoleID})
				results = append(results, "Refused role message: only trusted senders can message another bot role.")
				continue
			}
			delivery, err := resolveRolePeerDelivery(ctx, replyClient, rolePeer)
			if err != nil {
				record("failed", "", false, map[string]any{"reason": "resolve_role_peer_chat_failed", "error": err.Error(), "to_role_id": rolePeer.RoleID})
				results = append(results, fmt.Sprintf("Failed to message role %s: %v", rolePeer.RoleID, err))
				continue
			}
			mentionID = delivery.MentionID
			mentionSource = delivery.MentionSource
			targetChat = delivery.TargetChat
			targetChatSource = delivery.TargetChatSource
			crossChat = targetChat != chatID
		} else if cid := a.Params["chatid"]; cid != "" {
			if a.Type == "MESSAGE" {
				if mention := resolveCurrentChatMention(cid, opts.Mentions); mention != nil {
					currentChatMention = mention
					slog.Info("action: resolved message target mention to current chat",
						"chatid", cid, "mentionID", mention.ID, "mentionName", mention.Name, "chatID", chatID)
				}
			}
			if currentChatMention != nil {
				targetChat = chatID
			} else if !opts.OriginIsOwner {
				// Non-owner cross-chat: issue OOB challenge when
				// the manager is wired; otherwise force to origin
				// chat (legacy silent-override path).
				if opts.OOB != nil && opts.OwnerDMChat != "" && opts.OwnerID != "" {
					resolved, err := resolveChatParam(ctx, actionClient, cid, chatID)
					if err != nil {
						slog.Error("action: failed to resolve chatid", "chatid", cid, "error", err)
						record("failed", chatID, false, map[string]any{"error": err.Error(), "reason": "resolve_chat_failed"})
						results = append(results, fmt.Sprintf("Failed to resolve chat '%s': %v", cid, err))
						continue
					}
					targetChat = resolved
					if resolved != chatID {
						record("approval_required", targetChat, true, map[string]any{"requester_id": opts.RequesterID})
						results = append(results, crossChatOOBChallenge(ctx, actionClient, a, chatID, targetChat, opts))
						continue
					}
				} else {
					slog.Warn("action: ignoring chatid override from non-owner sender; forcing origin chat",
						"type", a.Type, "requested", cid, "origin", chatID)
				}
			} else {
				resolved, err := resolveChatParam(ctx, actionClient, cid, chatID)
				if err != nil {
					slog.Error("action: failed to resolve chatid", "chatid", cid, "error", err)
					record("failed", chatID, false, map[string]any{"error": err.Error(), "reason": "resolve_chat_failed"})
					results = append(results, fmt.Sprintf("Failed to resolve chat '%s': %v", cid, err))
					continue
				}
				targetChat = resolved
				crossChat = resolved != chatID
			}
		}

		// Fail-closed cross-chat gate. Owner-initiated ACTIONs that
		// leave both the origin chat AND the owner's own DM must
		// first land a heads-up notice in the owner DM — that is
		// the audit trail the operator relies on to detect hijacked
		// cross-chat dispatch. If the notice cannot be delivered
		// (no owner DM wired, or RC transport failure), the action
		// is refused entirely; a silent cross-chat write with no
		// audit record is not an acceptable failure mode.
		// Role-peer messages are provisioned by the Control Plane with
		// an explicit shared chat + target bot extension. They are the
		// bot-to-bot collaboration path, so they do not need the owner DM
		// pre-notice that guards free-form chatid= cross-chat writes.
		if crossChat && targetChat != opts.OwnerDMChat && !hasRolePeer {
			if err := announceCrossChatOrRefuse(ctx, replyClient, opts, a.Type, chatID, targetChat); err != nil {
				slog.Warn("action: cross-chat ACTION refused (fail-closed on pre-notice)",
					"type", a.Type, "origin", chatID, "target", targetChat,
					"requesterID", opts.RequesterID, "error", err)
				record("blocked", targetChat, true, map[string]any{"error": err.Error(), "reason": "cross_chat_notice_failed"})
				results = append(results, fmt.Sprintf("Refused cross-chat %s: %v", a.Type, err))
				continue
			}
		}

		switch a.Type {
		case "NOTE":
			title := a.Params["title"]
			if title == "" {
				title = "Note"
			}
			note, err := actionClient.CreateNote(ctx, targetChat, &ringcentral.CreateNoteRequest{
				Title: title,
				Body:  a.Body,
			})
			if err != nil {
				slog.Error("action: create note failed", "error", err)
				record("failed", targetChat, crossChat, map[string]any{"error": err.Error()})
				results = append(results, fmt.Sprintf("Failed to create note: %v", err))
				continue
			}
			if pubErr := actionClient.PublishNote(ctx, note.ID); pubErr != nil {
				slog.Error("action: publish note failed", "noteID", note.ID, "error", pubErr)
			}
			slog.Info("action: created note", "noteID", note.ID, "chatID", targetChat, "title", title)
			record("completed", targetChat, crossChat, map[string]any{"note_id": note.ID})

		case "TASK":
			subject := a.Params["subject"]
			if subject == "" {
				record("skipped", targetChat, crossChat, map[string]any{"reason": "missing_subject"})
				continue
			}
			taskClient := selectCurrentChatPostClient(replyClient, actionClient, targetChat, chatID)
			req := &ringcentral.CreateTaskRequest{Subject: subject}
			if aid := a.Params["assignee"]; aid != "" {
				resolvedID, err := resolveAssigneeParam(ctx, actionClient, aid)
				if err != nil {
					slog.Error("action: failed to resolve assignee", "assignee", aid, "error", err)
					record("failed", targetChat, crossChat, map[string]any{"error": err.Error(), "reason": "resolve_assignee_failed"})
					results = append(results, fmt.Sprintf("Failed to resolve assignee '%s': %v", aid, err))
					continue
				}
				req.Assignees = []ringcentral.TaskAssignee{{ID: resolvedID}}
			}
			task, err := taskClient.CreateTask(ctx, targetChat, req)
			if err != nil {
				slog.Error("action: create task failed", "error", err)
				record("failed", targetChat, crossChat, map[string]any{"error": err.Error()})
				results = append(results, fmt.Sprintf("Failed to create task: %v", err))
				continue
			}
			slog.Info("action: created task", "taskID", task.ID, "chatID", targetChat, "subject", subject)
			record("completed", targetChat, crossChat, map[string]any{"task_id": task.ID})

		case "EVENT":
			title := a.Params["title"]
			startTime := a.Params["start"]
			endTime := a.Params["end"]
			if title == "" || startTime == "" || endTime == "" {
				record("skipped", targetChat, crossChat, map[string]any{"reason": "missing_event_fields"})
				continue
			}
			startTime, endTime, err := normalizeEventDateTimes(startTime, endTime)
			if err != nil {
				record("failed", targetChat, crossChat, map[string]any{"error": err.Error(), "reason": "invalid_event_time"})
				results = append(results, fmt.Sprintf("Failed to create event: %v", err))
				continue
			}
			event, err := actionClient.CreateEvent(ctx, &ringcentral.CreateEventRequest{
				Title:     title,
				StartTime: startTime,
				EndTime:   endTime,
			})
			if err != nil {
				slog.Error("action: create event failed", "error", err)
				record("failed", targetChat, crossChat, map[string]any{"error": err.Error()})
				results = append(results, fmt.Sprintf("Failed to create event: %v", err))
				continue
			}
			slog.Info("action: created event", "eventID", event.ID, "title", title)
			record("completed", targetChat, crossChat, map[string]any{"event_id": event.ID})

		case "CARD":
			cardJSON := a.Body
			if cardJSON == "" {
				record("skipped", targetChat, crossChat, map[string]any{"reason": "empty_card"})
				continue
			}
			if !json.Valid([]byte(cardJSON)) {
				slog.Error("action: invalid adaptive card JSON")
				record("failed", targetChat, crossChat, map[string]any{"reason": "invalid_card_json"})
				results = append(results, "Failed to create card: invalid JSON")
				continue
			}
			cardClient := selectCardClient(replyClient, actionClient, targetChat, chatID)
			card, err := cardClient.CreateAdaptiveCard(ctx, targetChat, json.RawMessage(cardJSON))
			if err != nil {
				slog.Error("action: create adaptive card failed", "error", err)
				record("failed", targetChat, crossChat, map[string]any{"error": err.Error()})
				results = append(results, fmt.Sprintf("Failed to create card: %v", err))
				continue
			}
			slog.Info("action: created adaptive card", "cardID", card.ID, "chatID", targetChat)
			record("completed", targetChat, crossChat, map[string]any{"card_id": card.ID})

		case "MESSAGE":
			body := strings.TrimSpace(a.Body)
			messageClient := selectCurrentChatPostClient(replyClient, actionClient, targetChat, chatID)
			currentBotID := ""
			if replyClient != nil {
				currentBotID = strings.TrimSpace(replyClient.OwnerID())
			}
			rolePeerSenderIdentity := ""
			if hasRolePeer {
				messageClient, rolePeerSenderIdentity = selectRolePeerVisibleMessageClient(replyClient, actionClient)
				mentionID, mentionSource = rolePeerVisibleMentionID(rolePeer, mentionID, mentionSource, rolePeerSenderIdentity, targetChatSource)
				body = ensurePersonMentionPrefix(body, mentionID)
			} else if currentChatMention != nil {
				relayBotID := ""
				if replyClient != nil {
					candidate := strings.TrimSpace(replyClient.OwnerID())
					if candidate != "" && candidate != currentChatMention.ID {
						if mention := resolveCurrentChatMention(candidate, opts.Mentions); mention != nil {
							relayBotID = mention.ID
						}
					}
				}
				body = ensureRelayMentionPrefixes(body, currentChatMention.ID, relayBotID)
				messageClient = replyClient
			} else if targetChat == chatID && opts.RelayCollaborator != nil && currentBotID != "" {
				body = ensureCurrentChatRelay(body, opts.RelayCollaborator.ID, currentBotID)
				messageClient = replyClient
			}
			if body == "" {
				record("skipped", targetChat, crossChat, map[string]any{"reason": "empty_message"})
				continue
			}
			var deliveryDetails map[string]any
			var err error
			if hasRolePeer {
				deliveryDetails, err = sendRolePeerVisibleMessage(ctx, messageClient, targetChat, body, rolePeer, targetChatSource, rolePeerSenderIdentity)
			} else {
				deliveryDetails, err = sendRoleAwareMessage(ctx, messageClient, targetChat, body, false, rolePeer, targetChatSource)
			}
			if err != nil {
				slog.Error("action: send message failed", "error", err, "chatID", targetChat)
				record("failed", targetChat, crossChat, map[string]any{"error": err.Error()})
				results = append(results, fmt.Sprintf("Failed to send message: %v", err))
				continue
			}
			slog.Info("action: sent message", "chatID", targetChat, "text", util.Truncate(body, 60))
			details := map[string]any(nil)
			if hasRolePeer {
				details = map[string]any{
					"role_peer":           true,
					"to_role_id":          rolePeer.RoleID,
					"target_extension_id": rolePeer.ExtensionID,
					"target_person_id":    rolePeer.PersonID,
					"target_mention_id":   mentionID,
					"mention_source":      mentionSource,
					"target_chat_source":  targetChatSource,
				}
				if rolePeer.DisplayName != "" {
					details["target_display_name"] = rolePeer.DisplayName
				}
				for key, value := range deliveryDetails {
					details[key] = value
				}
			}
			record("completed", targetChat, crossChat, details)

		case "VIDEO":
			title := a.Params["title"]
			if title == "" {
				title = "RingClaw Meeting"
			}
			bridgeType := a.Params["type"]
			if bridgeType == "" {
				bridgeType = "Instant"
			}
			bridge, event, err := createVideoMeeting(ctx, actionClient, videoCreateOptions{
				Title:      title,
				BridgeType: bridgeType,
				StartTime:  a.Params["start"],
				EndTime:    a.Params["end"],
			})
			if err != nil {
				slog.Error("action: create video bridge failed", "error", err)
				details := map[string]any{"error": err.Error()}
				if bridge != nil && bridge.ID != "" {
					details["bridge_id"] = bridge.ID
				}
				record("failed", targetChat, crossChat, details)
				results = append(results, fmt.Sprintf("Failed to create video meeting: %v", err))
				continue
			}
			text := formatVideoMeetingMessage(bridge, event)
			postClient := selectCurrentChatPostClient(replyClient, actionClient, targetChat, chatID)
			if err := SendTextReply(ctx, postClient, targetChat, text); err != nil {
				slog.Error("action: send video bridge link failed", "error", err, "chatID", targetChat)
				details := map[string]any{"error": err.Error(), "bridge_id": bridge.ID, "reason": "post_video_link_failed"}
				if event != nil && event.ID != "" {
					details["event_id"] = event.ID
				}
				record("failed", targetChat, crossChat, details)
				results = append(results, fmt.Sprintf("Created video meeting but failed to post link: %v", err))
				continue
			}
			slog.Info("action: created video bridge", "bridgeID", bridge.ID, "chatID", targetChat, "title", title)
			details := map[string]any{"bridge_id": bridge.ID}
			if event != nil && event.ID != "" {
				details["event_id"] = event.ID
			}
			record("completed", targetChat, crossChat, details)

		case "VIDEO_LIST":
			text, count, err := videoListFromParams(ctx, actionClient, a.Params, opts.RequesterID)
			if err != nil {
				slog.Error("action: list video meetings failed", "error", err)
				record("failed", targetChat, crossChat, map[string]any{"error": err.Error()})
				results = append(results, fmt.Sprintf("Failed to list video meetings: %s", friendlyVideoAPIError(err)))
				continue
			}
			postClient := selectCurrentChatPostClient(replyClient, actionClient, targetChat, chatID)
			if err := SendTextReply(ctx, postClient, targetChat, text); err != nil {
				slog.Error("action: send video meeting list failed", "error", err, "chatID", targetChat)
				record("failed", targetChat, crossChat, map[string]any{"error": err.Error(), "reason": "post_video_list_failed"})
				results = append(results, fmt.Sprintf("Listed video meetings but failed to post result: %v", err))
				continue
			}
			slog.Info("action: listed video meetings", "chatID", targetChat, "count", count)
			record("completed", targetChat, crossChat, map[string]any{"count": count})

		case "PHONE_CALLLOG":
			text, count, err := phoneCallLogFromParams(ctx, actionClient, a.Params, opts.RequesterID)
			if err != nil {
				slog.Error("action: list call log failed", "error", err)
				record("failed", targetChat, crossChat, map[string]any{"error": err.Error()})
				results = append(results, fmt.Sprintf("Failed to list call log: %s", friendlyPhoneAPIError(err)))
				continue
			}
			postClient := selectCurrentChatPostClient(replyClient, actionClient, targetChat, chatID)
			if err := SendTextReply(ctx, postClient, targetChat, text); err != nil {
				slog.Error("action: send call log summary failed", "error", err, "chatID", targetChat)
				record("failed", targetChat, crossChat, map[string]any{"error": err.Error(), "reason": "post_call_log_failed"})
				results = append(results, fmt.Sprintf("Listed call log but failed to post result: %v", err))
				continue
			}
			slog.Info("action: listed call log", "chatID", targetChat, "count", count)
			record("completed", targetChat, crossChat, map[string]any{"count": count})

		case "SMS":
			params := make(map[string]string, len(a.Params)+1)
			for k, v := range a.Params {
				params[k] = v
			}
			params["text"] = strings.TrimSpace(a.Body)
			resolveClinicalRefillSMSTargetFromMemory(params, opts)
			result := smsSend(ctx, actionClient, params)
			if strings.HasPrefix(result, "Error: ") || strings.HasPrefix(result, "Usage: ") {
				record("failed", targetChat, crossChat, map[string]any{"error": result})
				results = append(results, strings.TrimPrefix(result, "Error: "))
				continue
			}
			postClient := selectCurrentChatPostClient(replyClient, actionClient, targetChat, chatID)
			if err := SendTextReply(ctx, postClient, targetChat, result); err != nil {
				slog.Error("action: send sms confirmation failed", "error", err, "chatID", targetChat)
				record("failed", targetChat, crossChat, map[string]any{"error": err.Error(), "reason": "post_sms_confirmation_failed"})
				results = append(results, fmt.Sprintf("Sent SMS but failed to post confirmation: %v", err))
				continue
			}
			slog.Info("action: sent sms", "chatID", targetChat, "to", params["to"])
			record("completed", targetChat, crossChat, map[string]any{"to": params["to"]})

		default:
			slog.Warn("action: unknown action type, sending body as message", "type", a.Type)
			body := strings.TrimSpace(a.Body)
			if body != "" {
				if err := SendTextReply(ctx, replyClient, chatID, fmt.Sprintf("[%s] %s", a.Type, body)); err != nil {
					slog.Error("action: failed to send unknown action as message", "error", err)
				}
			}
			record("skipped", targetChat, crossChat, map[string]any{"reason": "unknown_action_type"})
			results = append(results, fmt.Sprintf("Unknown action type: %s", a.Type))
		}
	}
	return results
}

var (
	clinicalPatientIDPattern = regexp.MustCompile(`(?i)\bAX-?(\d{3,})\b`)
	clinicalPhonePattern     = regexp.MustCompile(`\+?\d[\d .()/-]{6,}\d`)
)

type clinicalPatientEntityMemory struct {
	PatientID string
	Name      string
	Phone     string
}

func resolveClinicalRefillSMSTargetFromMemory(params map[string]string, opts ActionContext) {
	if params == nil {
		return
	}
	target := strings.TrimSpace(params["to"])
	if target == "" || looksLikePhoneNumber(target) {
		return
	}
	patientID := clinicalRefillSMSPatientID(target, opts.OriginalText)
	if patientID == "" {
		return
	}
	entity, err := loadClinicalPatientEntityMemory(patientID)
	if err != nil {
		slog.Warn("action: clinical refill sms memory lookup failed", "patientID", patientID, "target", target, "error", err)
		return
	}
	if entity.Phone == "" || !targetMatchesClinicalPatient(target, entity) {
		return
	}
	params["to"] = entity.Phone
	if entity.Name != "" {
		params["_target_label"] = entity.Name
	}
	slog.Info("action: resolved clinical refill sms target from memory",
		"patientID", patientID, "target", target, "patient", entity.Name, "phone", entity.Phone)
}

func clinicalRefillSMSPatientID(target string, originalText string) string {
	if patientID := normalizeClinicalPatientID(target); patientID != "" {
		return patientID
	}
	return clinicalRefillDecisionPatientID(originalText)
}

func clinicalRefillDecisionPatientID(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	fields := strings.Fields(text)
	if len(fields) == 0 || !isClinicalRefillDecisionVerb(fields[0]) {
		return ""
	}
	return normalizeClinicalPatientID(text)
}

func isClinicalRefillDecisionVerb(value string) bool {
	value = strings.ToLower(strings.Trim(strings.TrimSpace(value), ".,:;()[]{}"))
	switch value {
	case "approve", "approved", "followup", "follow-up", "follow_up", "deny", "denied", "reject", "rejected":
		return true
	default:
		return false
	}
}

func normalizeClinicalPatientID(value string) string {
	match := clinicalPatientIDPattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return ""
	}
	return "AX-" + match[1]
}

func loadClinicalPatientEntityMemory(patientID string) (clinicalPatientEntityMemory, error) {
	patientID = normalizeClinicalPatientID(patientID)
	if patientID == "" {
		return clinicalPatientEntityMemory{}, fmt.Errorf("patient id is required")
	}
	memoryDir := clinicalRefillMemoryDir()
	if memoryDir == "" {
		return clinicalPatientEntityMemory{}, fmt.Errorf("memory dir is not configured")
	}
	path := filepath.Join(memoryDir, "entities", patientID+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return clinicalPatientEntityMemory{}, fmt.Errorf("read %s: %w", path, err)
	}
	entity := clinicalPatientEntityMemory{PatientID: patientID}
	for _, line := range strings.Split(string(data), "\n") {
		label, value, ok := splitMemoryField(line)
		if !ok {
			continue
		}
		normalizedLabel := strings.ToLower(strings.TrimSpace(label))
		switch {
		case entity.Name == "" && (strings.Contains(normalizedLabel, "患者姓名") ||
			strings.Contains(normalizedLabel, "patient name") ||
			normalizedLabel == "name" || normalizedLabel == "姓名"):
			entity.Name = strings.TrimSpace(value)
		case entity.Phone == "" && (strings.Contains(normalizedLabel, "手机号") ||
			strings.Contains(normalizedLabel, "phone") ||
			strings.Contains(normalizedLabel, "mobile")):
			entity.Phone = extractClinicalPhoneNumber(value)
		}
	}
	if entity.Phone == "" {
		return entity, fmt.Errorf("patient %s memory does not contain a phone number", patientID)
	}
	return entity, nil
}

func clinicalRefillMemoryDir() string {
	dir, err := paths.ResolveEnvOrDefault("RINGCLAW_MEMORY_DIR", "workspace", "memory")
	if err != nil || strings.TrimSpace(dir) == "" {
		return ""
	}
	return dir
}

func splitMemoryField(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	if label, value, ok := strings.Cut(line, "："); ok {
		return strings.TrimSpace(label), strings.TrimSpace(value), true
	}
	if label, value, ok := strings.Cut(line, ":"); ok {
		return strings.TrimSpace(label), strings.TrimSpace(value), true
	}
	return "", "", false
}

func extractClinicalPhoneNumber(value string) string {
	match := clinicalPhonePattern.FindString(value)
	if match == "" {
		return ""
	}
	var b strings.Builder
	for i, r := range match {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
			continue
		}
		if r == '+' && i == 0 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func targetMatchesClinicalPatient(target string, entity clinicalPatientEntityMemory) bool {
	if normalizeClinicalPatientID(target) == entity.PatientID {
		return true
	}
	targetKey := normalizeClinicalTarget(target)
	if targetKey == "patient" || targetKey == "thepatient" || targetKey == "患者" {
		return true
	}
	nameKey := normalizeClinicalTarget(entity.Name)
	return nameKey != "" && targetKey == nameKey
}

func normalizeClinicalTarget(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "", "\t", "", "\n", "", "-", "", "_", "", ".", "", ",", "", "(", "", ")", "", "[", "", "]", "", "{", "", "}", "")
	return replacer.Replace(value)
}

func shouldForceClinicalRefillApproval(originalText string, actions []AgentAction) bool {
	if !isInitialClinicalRefillRequest(originalText) {
		return false
	}
	for _, action := range actions {
		if cardHasClinicalRefillSubmitActions(action) {
			return false
		}
	}
	return true
}

func cardHasClinicalRefillSubmitActions(action AgentAction) bool {
	if !strings.EqualFold(action.Type, "CARD") {
		return false
	}
	var card struct {
		Actions []struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(action.Body), &card); err != nil {
		return false
	}
	required := map[string]bool{
		"approve":  false,
		"followup": false,
		"deny":     false,
	}
	for _, action := range card.Actions {
		if !strings.EqualFold(strings.TrimSpace(action.Type), "Action.Submit") {
			continue
		}
		value, _ := action.Data["action"].(string)
		key := strings.ToLower(strings.TrimSpace(value))
		if _, ok := required[key]; ok {
			required[key] = true
		}
	}
	for _, found := range required {
		if !found {
			return false
		}
	}
	return true
}

func isInitialClinicalRefillRequest(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, "approve ") ||
		strings.Contains(normalized, "followup ") ||
		strings.Contains(normalized, "deny ") {
		return false
	}
	fields := strings.Fields(normalized)
	hasRefill := false
	hasPatientID := false
	for _, field := range fields {
		field = strings.Trim(field, ".,:;()[]{}")
		if field == "refill" {
			hasRefill = true
		}
		if strings.HasPrefix(field, "ax-") && len(field) > len("ax-") {
			hasPatientID = true
		}
	}
	return hasRefill && hasPatientID
}

func isClinicalRefillProhibitedAction(actionType string) bool {
	switch strings.ToUpper(strings.TrimSpace(actionType)) {
	case "SMS", "TASK":
		return true
	default:
		return false
	}
}

func containsClinicalRefillProhibitedAction(actions []AgentAction) bool {
	for _, action := range actions {
		if isClinicalRefillProhibitedAction(action.Type) {
			return true
		}
	}
	return false
}

func clinicalRefillGuardReply(originalText string) string {
	req := parseClinicalRefillRequest(originalText)
	if req.PatientID == "" {
		return "已拦截提前执行的续剂动作，并改为发送 provider 审批卡。请等待 provider 在卡片上选择 approve / followup / deny。"
	}
	return fmt.Sprintf("已发送 %s 的续剂审批卡，等待 %s 操作 approve / followup / deny。未发送 SMS，未创建 Task。", req.PatientID, req.ProviderName)
}

func buildClinicalRefillApprovalCardAction(originalText string, opts ActionContext) AgentAction {
	req := parseClinicalRefillRequest(originalText)
	if req.PatientID == "" {
		req.PatientID = "Unknown"
	}
	if req.ProviderName == "" {
		req.ProviderName = "provider"
	}
	rxID := fmt.Sprintf("RX-%s-%s", time.Now().Format("20060102"), strings.ReplaceAll(req.PatientID, "-", ""))
	card := map[string]any{
		"$schema":      "http://adaptivecards.io/schemas/adaptive-card.json",
		"type":         "AdaptiveCard",
		"version":      "1.3",
		"fallbackText": fmt.Sprintf("Refill approval requested: %s %s", req.PatientID, req.Medication),
		"body": []map[string]any{
			{
				"type":   "TextBlock",
				"text":   "Refill approval requested",
				"size":   "Small",
				"color":  "Attention",
				"weight": "Bolder",
			},
			{
				"type":   "TextBlock",
				"text":   strings.TrimSpace(fmt.Sprintf("%s - %s %s", rxID, req.PatientID, req.Medication)),
				"size":   "Large",
				"color":  "Accent",
				"weight": "Bolder",
				"wrap":   true,
			},
			{
				"type": "FactSet",
				"facts": []map[string]string{
					{"title": "Provider", "value": req.ProviderName},
					{"title": "Patient", "value": req.PatientID},
					{"title": "Medication", "value": fallbackString(req.Medication, "See request")},
					{"title": "Status", "value": "Waiting for provider decision"},
				},
			},
			{
				"type":      "TextBlock",
				"text":      fmt.Sprintf("@%s please review this refill request. Runtime blocked premature SMS/Task actions until provider approval.", req.ProviderName),
				"wrap":      true,
				"separator": true,
			},
		},
		"actions": []map[string]any{
			refillSubmitAction("Approve", "approve", rxID, req, opts),
			refillSubmitAction("Need follow-up", "followup", rxID, req, opts),
			refillSubmitAction("Deny", "deny", rxID, req, opts),
		},
	}
	body, err := json.Marshal(card)
	if err != nil {
		body = []byte(`{"type":"AdaptiveCard","version":"1.3","body":[{"type":"TextBlock","text":"Refill approval requested"}]}`)
	}
	return AgentAction{Type: "CARD", Body: string(body)}
}

func refillSubmitAction(title, action, rxID string, req clinicalRefillRequest, opts ActionContext) map[string]any {
	data := map[string]any{
		"action":     action,
		"rx_id":      rxID,
		"patient_id": req.PatientID,
		"medication": req.Medication,
	}
	if req.ProviderName != "" {
		data["provider_name"] = req.ProviderName
	}
	if botID := refillApprovalBotID(); botID != "" {
		data["bot_id"] = botID
	}
	if providerID := providerUserID(req.ProviderName, opts); providerID != "" {
		data["provider_user_id"] = providerID
	}
	return map[string]any{
		"type":  "Action.Submit",
		"title": title,
		"data":  data,
	}
}

func refillApprovalBotID() string {
	for _, key := range []string{"RINGCLAW_BOT_ID", "BOT_ID"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func providerUserID(providerName string, opts ActionContext) string {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	for _, mention := range opts.Mentions {
		if strings.ToLower(strings.TrimSpace(mention.Name)) == providerName && mention.ID != "" {
			return mention.ID
		}
	}
	return ""
}

type clinicalRefillRequest struct {
	PatientID    string
	Medication   string
	ProviderName string
}

func parseClinicalRefillRequest(text string) clinicalRefillRequest {
	fields := strings.Fields(strings.TrimSpace(text))
	for i, field := range fields {
		if !strings.EqualFold(strings.Trim(field, ".,:;()[]{}"), "refill") {
			continue
		}
		if i+1 >= len(fields) {
			return clinicalRefillRequest{}
		}
		req := clinicalRefillRequest{PatientID: strings.Trim(fields[i+1], ".,:;()[]{}")}
		rest := fields[i+2:]
		providerStart := len(rest)
		for j := 0; j+1 < len(rest); j++ {
			if looksLikeNameToken(rest[j]) && looksLikeNameToken(rest[j+1]) {
				providerStart = j
				break
			}
		}
		if providerStart < len(rest) {
			req.ProviderName = strings.Join(rest[providerStart:], " ")
			rest = rest[:providerStart]
		}
		req.Medication = strings.Join(rest, " ")
		return req
	}
	return clinicalRefillRequest{}
}

func normalizeLegacyMeshDelegationActions(actions []AgentAction, opts ActionContext) []AgentAction {
	if opts.MeshTaskCreator == nil || !opts.OriginIsOwner || len(actions) == 0 {
		return actions
	}
	out := make([]AgentAction, 0, len(actions))
	for _, action := range actions {
		if converted, ok := legacyDelegationActionToMeshTask(action, opts); ok {
			if strings.EqualFold(strings.TrimSpace(action.Type), "NOTE") {
				out = append(out, legacyDelegationNoteForOriginChat(action))
			}
			out = append(out, converted)
			continue
		}
		out = append(out, action)
	}
	return out
}

func ensureCoverageHandoffNoteActions(actions []AgentAction, opts ActionContext) []AgentAction {
	if opts.MeshTaskCreator == nil || !opts.OriginIsOwner || opts.SourceTaskID != "" || len(actions) == 0 || hasActionType(actions, "NOTE") {
		return actions
	}
	insertAt := -1
	var source AgentAction
	for i, action := range actions {
		if isCoverageHandoffMeshTask(action, opts.OriginalText) {
			insertAt = i
			source = action
			break
		}
	}
	if insertAt < 0 {
		return actions
	}
	note := coverageHandoffNoteAction(source, opts.OriginalText)
	out := make([]AgentAction, 0, len(actions)+1)
	out = append(out, actions[:insertAt]...)
	out = append(out, note)
	out = append(out, actions[insertAt:]...)
	return out
}

func hasActionType(actions []AgentAction, actionType string) bool {
	actionType = strings.ToUpper(strings.TrimSpace(actionType))
	for _, action := range actions {
		if strings.ToUpper(strings.TrimSpace(action.Type)) == actionType {
			return true
		}
	}
	return false
}

func isCoverageHandoffMeshTask(action AgentAction, originalText string) bool {
	if strings.ToUpper(strings.TrimSpace(action.Type)) != "MESH_TASK" {
		return false
	}
	actionText := strings.ToLower(strings.Join([]string{
		action.Params["intent"],
		action.Params["title"],
		action.Params["context_summary"],
		action.Body,
		originalText,
	}, "\n"))
	if !strings.Contains(actionText, "coverage.transfer") &&
		!strings.Contains(actionText, "coverage") &&
		!strings.Contains(actionText, "handoff") &&
		!strings.Contains(actionText, "交接") {
		return false
	}
	return strings.Contains(actionText, "缺勤") ||
		strings.Contains(actionText, "absence") ||
		strings.Contains(actionText, "sick") ||
		strings.Contains(actionText, "coverage") ||
		strings.Contains(actionText, "handoff") ||
		strings.Contains(actionText, "交接")
}

func coverageHandoffNoteAction(action AgentAction, originalText string) AgentAction {
	summary := firstNonEmptyLine(firstActionParam(action.Params, "context_summary", "title"))
	if summary == "" {
		summary = strings.TrimSpace(originalText)
	}
	if summary == "" {
		summary = "Coverage handoff requested."
	}
	details := strings.TrimSpace(action.Body)
	if details == "" || strings.EqualFold(details, summary) {
		details = summary
	}
	body := strings.TrimSpace(strings.Join([]string{
		"**缺勤交接摘要**",
		"",
		summary,
		"",
		"**待移交工作**",
		details,
		"",
		"**跟进要求**",
		"- 需要下游协调 Bot 确认覆盖并回写进展",
		"",
		"**来源请求**",
		strings.TrimSpace(originalText),
	}, "\n"))
	return AgentAction{
		Type: "NOTE",
		Params: map[string]string{
			"title": "缺勤交接文档 - " + time.Now().Format("2006-01-02"),
		},
		Body: body,
	}
}

func legacyDelegationNoteForOriginChat(action AgentAction) AgentAction {
	note := action
	note.Params = make(map[string]string, len(action.Params))
	for key, value := range action.Params {
		if strings.EqualFold(strings.TrimSpace(key), "chatid") {
			continue
		}
		note.Params[key] = value
	}
	return note
}

func legacyDelegationActionToMeshTask(action AgentAction, opts ActionContext) (AgentAction, bool) {
	switch strings.ToUpper(strings.TrimSpace(action.Type)) {
	case "MESSAGE", "NOTE", "TASK", "CARD":
	default:
		return AgentAction{}, false
	}
	originalText := opts.OriginalText
	chatID := strings.ToLower(strings.TrimSpace(action.Params["chatid"]))
	body := fallbackString(strings.TrimSpace(action.Body), firstActionParam(action.Params, "title", "subject"))
	actionText := strings.TrimSpace(strings.Join([]string{
		body,
		firstActionParam(action.Params, "title", "subject"),
		firstActionParam(action.Params, "assignee"),
	}, "\n"))
	lowerBody := strings.ToLower(actionText)
	lowerOriginal := strings.ToLower(strings.TrimSpace(originalText))
	toRoleID := firstActionParam(action.Params, "to_role_id", "to_role", "role_id", "role")
	if toRoleID == "" {
		toRoleID = inferDelegationRoleID(lowerBody, opts.RolePeers)
	}
	if toRoleID == "" && looksLikeLegacyDelegationTarget(chatID) && len(opts.RolePeers) == 1 {
		toRoleID = onlyRolePeerID(opts.RolePeers)
	}
	if !looksLikeLegacyDelegationTarget(chatID) && !strings.Contains(lowerBody, "task_handoff_request") && toRoleID == "" {
		return AgentAction{}, false
	}
	if !strings.Contains(lowerBody, "task_handoff_request") &&
		!strings.Contains(lowerBody, "handoff") &&
		!strings.Contains(lowerBody, "coverage") &&
		!strings.Contains(lowerBody, "交接") &&
		!strings.Contains(lowerOriginal, "交接") &&
		!strings.Contains(lowerOriginal, "缺勤") &&
		!strings.Contains(lowerOriginal, "absence") &&
		!strings.Contains(lowerOriginal, "handoff") {
		return AgentAction{}, false
	}
	if toRoleID == "" {
		return AgentAction{}, false
	}
	summary := firstNonEmptyLine(body)
	if summary == "" {
		summary = strings.TrimSpace(originalText)
	}
	return AgentAction{
		Type: "MESH_TASK",
		Params: map[string]string{
			"to_role_id":      toRoleID,
			"intent":          "coverage.transfer",
			"title":           "Coverage handoff",
			"context_summary": summary,
		},
		Body: fallbackString(body, "Coordinate coverage transfer and report completion."),
	}, true
}

func looksLikeLegacyDelegationTarget(chatID string) bool {
	chatID = strings.TrimPrefix(strings.TrimSpace(chatID), "#")
	switch chatID {
	case "admin", "admins", "admin-channel", "admin chat":
		return true
	default:
		return false
	}
}

func inferDelegationRoleID(lowerBody string, peers map[string]RolePeer) string {
	lowerBody = strings.ToLower(strings.TrimSpace(lowerBody))
	if lowerBody == "" || len(peers) == 0 {
		return ""
	}
	for roleID, peer := range peers {
		roleID = strings.TrimSpace(firstNonEmptyString(peer.RoleID, roleID))
		if roleID == "" {
			continue
		}
		for _, query := range []string{roleID, peer.RoleName, peer.DisplayName, peer.BotID, peer.ExtensionID} {
			query = strings.ToLower(strings.TrimSpace(query))
			if query != "" && strings.Contains(lowerBody, query) {
				return roleID
			}
		}
	}
	return ""
}

func onlyRolePeerID(peers map[string]RolePeer) string {
	for roleID, peer := range peers {
		return strings.TrimSpace(firstNonEmptyString(peer.RoleID, roleID))
	}
	return ""
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func rolePeerForAction(action AgentAction, peers map[string]RolePeer) (RolePeer, bool) {
	if strings.ToUpper(strings.TrimSpace(action.Type)) != "MESSAGE" || len(peers) == 0 {
		return RolePeer{}, false
	}
	roleID := firstActionParam(action.Params, "to_role_id", "to_role", "role_id", "role")
	if roleID == "" {
		return RolePeer{}, false
	}
	peer, ok := peers[roleID]
	if !ok {
		return RolePeer{}, false
	}
	if peer.RoleID == "" {
		peer.RoleID = roleID
	}
	return peer, true
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func notifyRolePeerForMeshTask(ctx context.Context, replyClient, actionClient *ringcentral.Client, originChat string, action AgentAction, req MeshRuntimeTaskCreateRequest, task MeshRuntimeTask, peers map[string]RolePeer) (bool, error) {
	peer, ok := rolePeerFromRoutePlan(task.RoutePlan)
	if !ok {
		peer, ok = rolePeerForID(req.ToRoleID, peers)
	}
	if !ok {
		return false, nil
	}
	messageClient, senderIdentity := selectRolePeerVisibleMessageClient(replyClient, actionClient)
	if messageClient == nil {
		return false, fmt.Errorf("message client is not configured")
	}
	delivery, err := resolveRolePeerDelivery(ctx, messageClient, peer)
	if err != nil {
		return false, err
	}
	mentionID := delivery.MentionID
	mentionSource := delivery.MentionSource
	targetChat := delivery.TargetChat
	targetChatSource := delivery.TargetChatSource
	mentionID, mentionSource = rolePeerVisibleMentionID(peer, mentionID, mentionSource, senderIdentity, targetChatSource)
	body := meshTaskRolePeerMessage(action, req, task)
	if body == "" {
		return false, fmt.Errorf("message body is empty")
	}
	body = ensurePersonMentionPrefix(body, mentionID)
	deliveryDetails, err := sendRolePeerVisibleMessage(ctx, messageClient, targetChat, body, peer, targetChatSource, senderIdentity)
	if err != nil {
		return false, err
	}
	extra := map[string]any{
		"role_peer":           true,
		"trace_id":            firstNonEmptyString(task.TraceID, task.RoutePlan.TraceID),
		"task_id":             task.ID,
		"to_role_id":          peer.RoleID,
		"target_extension_id": peer.ExtensionID,
		"target_person_id":    peer.PersonID,
		"target_mention_id":   mentionID,
		"mention_source":      mentionSource,
		"target_chat_source":  targetChatSource,
		"target_display_name": peer.DisplayName,
	}
	for key, value := range deliveryDetails {
		extra[key] = value
	}
	recordAgentActionEvent(ctx, ActionEvent{
		Type:    "MESSAGE",
		Status:  "completed",
		Details: actionEventDetails(originChat, targetChat, targetChat != originChat, extra),
	})
	return true, nil
}

func rolePeerFromRoutePlan(plan MeshRuntimeRoutePlan) (RolePeer, bool) {
	visible := plan.VisibleDelivery
	if !visible.Enabled {
		return RolePeer{}, false
	}
	peer := RolePeer{
		RoleID:      firstNonEmptyString(visible.TargetRoleID, plan.ToRoleID),
		BotID:       firstNonEmptyString(visible.TargetBotID, plan.TargetBotID),
		DisplayName: visible.MentionLabel,
		ExtensionID: visible.TargetExtensionID,
		PersonID:    visible.MentionPersonID,
	}
	if strings.EqualFold(strings.TrimSpace(visible.Transport), "shared_chat") && strings.TrimSpace(visible.ChatID) != "" {
		peer.SharedChatIDs = []string{strings.TrimSpace(visible.ChatID)}
	}
	if peer.RoleID == "" && peer.BotID == "" && peer.DisplayName == "" && peer.ExtensionID == "" && peer.PersonID == "" && len(peer.SharedChatIDs) == 0 {
		return RolePeer{}, false
	}
	return peer, true
}

func sendRoleAwareMessage(ctx context.Context, client *ringcentral.Client, chatID, body string, hasRolePeer bool, peer RolePeer, targetChatSource string) (map[string]any, error) {
	if hasRolePeer &&
		targetChatSource == "shared_chat" &&
		isNumericID(chatID) &&
		isNumericID(peer.PersonID) {
		groupBody := stripPersonMentionPrefix(body, peer.PersonID)
		_, err := client.SendGroupMentionPost(ctx, chatID, peer.PersonID, rolePeerDisplayLabel(peer), groupBody)
		if err != nil {
			details, fallbackErr := sendRolePeerDirectFallback(ctx, client, groupBody, peer, err)
			if fallbackErr == nil {
				return details, nil
			}
			return nil, fallbackErr
		}
		slog.Info("sent role peer group mention",
			"component", "sender",
			"chatID", chatID,
			"roleID", peer.RoleID,
			"personID", peer.PersonID,
			"text", util.Truncate(groupBody, 50))
		return map[string]any{
			"role_peer_delivery":        "group_mention",
			"actual_target_chat":        chatID,
			"actual_target_chat_source": "shared_chat",
			"mention_transport":         "group_post",
		}, nil
	}
	if err := SendTextReply(ctx, client, chatID, body); err != nil {
		return nil, err
	}
	return nil, nil
}

func selectRolePeerVisibleMessageClient(replyClient, actionClient *ringcentral.Client) (*ringcentral.Client, string) {
	if replyClient != nil {
		return replyClient, "reply_client"
	}
	return actionClient, "action_client"
}

func rolePeerVisibleMentionID(peer RolePeer, resolvedMentionID, resolvedMentionSource, senderIdentity, targetChatSource string) (string, string) {
	if senderIdentity == "action_client" && targetChatSource != "shared_chat" {
		if extensionID := strings.TrimSpace(peer.ExtensionID); extensionID != "" {
			return extensionID, "extension_id"
		}
	}
	return resolvedMentionID, resolvedMentionSource
}

func sendRolePeerVisibleMessage(ctx context.Context, client *ringcentral.Client, chatID, body string, peer RolePeer, targetChatSource, senderIdentity string) (map[string]any, error) {
	if senderIdentity == "action_client" {
		if targetChatSource == "shared_chat" && isNumericID(chatID) && isNumericID(peer.PersonID) {
			details, err := sendRoleAwareMessage(ctx, client, chatID, body, true, peer, targetChatSource)
			if err != nil {
				return nil, err
			}
			if details == nil {
				details = map[string]any{}
			}
			details["sender_identity"] = senderIdentity
			return details, nil
		}
		if err := SendTextReply(ctx, client, chatID, body); err != nil {
			return nil, err
		}
		return map[string]any{
			"role_peer_delivery":        "user_message",
			"actual_target_chat":        chatID,
			"actual_target_chat_source": targetChatSource,
			"mention_transport":         "team_messaging_post",
			"sender_identity":           senderIdentity,
		}, nil
	}
	details, err := sendRoleAwareMessage(ctx, client, chatID, body, true, peer, targetChatSource)
	if err != nil {
		return nil, err
	}
	if details == nil {
		details = map[string]any{}
	}
	details["sender_identity"] = senderIdentity
	return details, nil
}

func sendRolePeerDirectFallback(ctx context.Context, client *ringcentral.Client, body string, peer RolePeer, groupErr error) (map[string]any, error) {
	if client == nil {
		return nil, fmt.Errorf("send group mention message: %w; fallback direct chat: reply client is not configured", groupErr)
	}
	var lastErr error
	for _, memberID := range rolePeerDirectMemberCandidates(peer) {
		chat, err := client.CreateConversation(ctx, []string{memberID})
		if err != nil {
			lastErr = err
			continue
		}
		directChatID := strings.TrimSpace(chat.ID)
		if directChatID == "" {
			lastErr = fmt.Errorf("direct bot chat ID is empty")
			continue
		}
		if err := SendTextReply(ctx, client, directChatID, body); err != nil {
			lastErr = err
			continue
		}
		slog.Warn("role peer group mention failed; sent direct fallback",
			"component", "sender",
			"roleID", peer.RoleID,
			"memberID", memberID,
			"directChatID", directChatID,
			"groupError", groupErr)
		return map[string]any{
			"role_peer_delivery":        "direct_chat_fallback",
			"actual_target_chat":        directChatID,
			"actual_target_chat_source": "direct_chat_fallback",
			"direct_member_id":          memberID,
			"group_notify_error":        groupErr.Error(),
		}, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("send group mention message: %w; fallback direct chat: %w", groupErr, lastErr)
	}
	return nil, fmt.Errorf("send group mention message: %w; fallback direct chat: target bot member ID is empty", groupErr)
}

func rolePeerDirectMemberCandidates(peer RolePeer) []string {
	seen := map[string]bool{}
	var candidates []string
	for _, value := range []string{peer.ExtensionID, peer.BotID, peer.PersonID} {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		candidates = append(candidates, value)
	}
	return candidates
}

func stripPersonMentionPrefix(body, personID string) string {
	body = strings.TrimSpace(body)
	personID = strings.TrimSpace(personID)
	if body == "" || personID == "" {
		return body
	}
	prefix := "![:Person](" + personID + ")"
	if strings.HasPrefix(body, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(body, prefix))
	}
	return body
}

func rolePeerDisplayLabel(peer RolePeer) string {
	return firstNonEmptyString(peer.DisplayName, peer.RoleName, peer.BotID, peer.ExtensionID, peer.PersonID)
}

func rolePeerForID(roleID string, peers map[string]RolePeer) (RolePeer, bool) {
	roleID = strings.TrimSpace(roleID)
	if roleID == "" || len(peers) == 0 {
		return RolePeer{}, false
	}
	peer, ok := peers[roleID]
	if !ok {
		return RolePeer{}, false
	}
	if peer.RoleID == "" {
		peer.RoleID = roleID
	}
	return peer, true
}

func meshTaskRolePeerMessage(_ AgentAction, req MeshRuntimeTaskCreateRequest, task MeshRuntimeTask) string {
	intent := firstNonEmptyString(task.Intent, req.Intent)
	title := firstNonEmptyString(task.Title, req.Title, "Mesh task")
	var lines []string
	if intent != "" {
		lines = append(lines, "New mesh task: "+intent)
	} else {
		lines = append(lines, "New mesh task")
	}
	lines = append(lines, "Title: "+title)
	if task.ID != "" {
		lines = append(lines, "Task ID: "+task.ID)
	}
	if toRoleID := firstNonEmptyString(task.ToRoleID, req.ToRoleID); toRoleID != "" {
		lines = append(lines, "To role: "+toRoleID)
	}
	if summary := strings.TrimSpace(req.Context.Summary); summary != "" {
		lines = append(lines, "Summary: "+summary)
	}
	return strings.Join(lines, "\n")
}

func looksLikeNameToken(token string) bool {
	token = strings.Trim(token, ".,:;()[]{}")
	if token == "" {
		return false
	}
	r := rune(token[0])
	return r >= 'A' && r <= 'Z'
}

func fallbackString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func meshTaskRequestFromAction(a AgentAction, opts ActionContext, originChatID string) (MeshRuntimeTaskCreateRequest, error) {
	toRoleID := firstActionParam(a.Params, "to_role_id", "to_role", "role_id", "role")
	intent := firstActionParam(a.Params, "intent", "task_intent")
	intent = canonicalMeshTaskIntent(intent)
	title := strings.TrimSpace(a.Params["title"])
	instructions := firstActionParam(a.Params, "instructions", "instruction")
	if instructions == "" {
		instructions = strings.TrimSpace(a.Body)
	}
	summary := firstActionParam(a.Params, "context_summary", "summary")
	if toRoleID == "" {
		return MeshRuntimeTaskCreateRequest{}, fmt.Errorf("to_role_id is required")
	}
	if intent == "" {
		return MeshRuntimeTaskCreateRequest{}, fmt.Errorf("intent is required")
	}
	data := map[string]interface{}{}
	for key, value := range a.Params {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value == "" || !strings.HasPrefix(key, "context_") || key == "context_summary" {
			continue
		}
		data[strings.TrimPrefix(key, "context_")] = value
	}
	putMeshContextData(data, "origin_chat_id", originChatID)
	putMeshContextData(data, "source_post_id", opts.SourcePostID)
	putMeshContextData(data, "source_task_id", opts.SourceTaskID)
	putMeshContextData(data, "source_agent_id", opts.SourceAgentID)
	putMeshContextData(data, "requester_id", opts.RequesterID)
	putMeshContextData(data, "to_role_id", toRoleID)
	putMeshContextData(data, "intent", intent)
	if len(data) == 0 {
		data = nil
	}
	return MeshRuntimeTaskCreateRequest{
		ToRoleID:     toRoleID,
		Intent:       intent,
		Title:        title,
		Instructions: instructions,
		Context: MeshRuntimeContextPackage{
			Summary: summary,
			Data:    data,
		},
	}, nil
}

func canonicalMeshTaskIntent(intent string) string {
	intent = strings.TrimSpace(intent)
	key := strings.ToLower(intent)
	key = strings.NewReplacer("_", ".", "-", ".", " ", ".").Replace(key)
	switch key {
	case "coverage.transfer",
		"coverage.handoff",
		"coverage.request",
		"coverage.transfer.request",
		"absence.coverage",
		"absence.coverage.request",
		"absence.handoff",
		"absence.handoff.request",
		"handoff.coverage":
		return "coverage.transfer"
	default:
		return intent
	}
}

func putMeshContextData(data map[string]interface{}, key, value string) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return
	}
	if _, exists := data[key]; !exists {
		data[key] = value
	}
}

func firstActionParam(params map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(params[key]); value != "" {
			return value
		}
	}
	return ""
}

func actionCapability(actionType string) string {
	switch strings.ToUpper(strings.TrimSpace(actionType)) {
	case "VIDEO", "VIDEO_LIST":
		return "video"
	case "PHONE_CALL", "RINGOUT":
		return "phone"
	case "PHONE_CALLLOG":
		return "phone"
	case "SMS":
		return "sms"
	default:
		return ""
	}
}

func isActionCapabilityAllowed(capabilities []string, capability string) bool {
	if len(capabilities) == 0 {
		return true
	}
	capability = strings.ToLower(strings.TrimSpace(capability))
	for _, item := range defaultEnabledCapabilities {
		if item == capability {
			return true
		}
	}
	for _, item := range capabilities {
		if strings.EqualFold(strings.TrimSpace(item), capability) {
			return true
		}
	}
	return false
}

func formatVideoBridgeMessage(bridge *ringcentral.VideoBridge) string {
	if bridge == nil {
		return "Video meeting created."
	}
	title := bridge.Name
	if title == "" {
		title = "Video meeting"
	}
	if bridge.Discovery.Web != "" {
		return fmt.Sprintf("Video meeting created: **%s**\n%s", title, bridge.Discovery.Web)
	}
	return fmt.Sprintf("Video meeting created: **%s** (`%s`)", title, bridge.ID)
}

func formatVideoMeetingMessage(bridge *ringcentral.VideoBridge, event *ringcentral.Event) string {
	if event == nil {
		return formatVideoBridgeMessage(bridge)
	}
	title := strings.TrimSpace(event.Title)
	if title == "" && bridge != nil {
		title = strings.TrimSpace(bridge.Name)
	}
	if title == "" {
		title = "Video meeting"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Scheduled video meeting created: **%s**", title))
	if event.ID != "" {
		sb.WriteString(fmt.Sprintf(" (`%s`)", event.ID))
	}
	if event.StartTime != "" || event.EndTime != "" {
		sb.WriteString(fmt.Sprintf("\nTime: %s ~ %s", event.StartTime, event.EndTime))
	}
	if bridge != nil && strings.TrimSpace(bridge.Discovery.Web) != "" {
		sb.WriteString(fmt.Sprintf("\nJoin: %s", strings.TrimSpace(bridge.Discovery.Web)))
	}
	return sb.String()
}

func createVideoMeeting(ctx context.Context, client *ringcentral.Client, opts videoCreateOptions) (*ringcentral.VideoBridge, *ringcentral.Event, error) {
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "RingClaw Meeting"
	}
	bridgeType := strings.TrimSpace(opts.BridgeType)
	if bridgeType == "" {
		bridgeType = "Instant"
	}
	bridge, err := client.CreateVideoBridge(ctx, &ringcentral.CreateVideoBridgeRequest{
		Name: title,
		Type: bridgeType,
	})
	if err != nil {
		return nil, nil, err
	}
	if !strings.EqualFold(bridgeType, "Scheduled") {
		return bridge, nil, nil
	}
	startTime := strings.TrimSpace(opts.StartTime)
	endTime := strings.TrimSpace(opts.EndTime)
	if startTime == "" && endTime == "" {
		return bridge, nil, nil
	}
	if startTime == "" || endTime == "" {
		return bridge, nil, fmt.Errorf("scheduled video meeting requires both start and end times")
	}
	startTime, endTime, err = normalizeEventDateTimes(startTime, endTime)
	if err != nil {
		return bridge, nil, err
	}
	event, err := client.CreateEvent(ctx, &ringcentral.CreateEventRequest{
		Title:       title,
		StartTime:   startTime,
		EndTime:     endTime,
		Location:    "RingCentral Video",
		Description: buildScheduledVideoEventDescription(bridge),
	})
	if err != nil {
		return bridge, nil, fmt.Errorf("create scheduled event: %w", err)
	}
	return bridge, event, nil
}

func buildScheduledVideoEventDescription(bridge *ringcentral.VideoBridge) string {
	if bridge == nil {
		return "RingCentral Video meeting"
	}
	url := strings.TrimSpace(bridge.Discovery.Web)
	if url == "" {
		if bridge.ID == "" {
			return "RingCentral Video meeting"
		}
		return fmt.Sprintf("RingCentral Video bridge ID: %s", bridge.ID)
	}
	if bridge.ID == "" {
		return fmt.Sprintf("RingCentral Video join link: %s", url)
	}
	return fmt.Sprintf("RingCentral Video join link: %s\nBridge ID: %s", url, bridge.ID)
}

func videoListFromParams(ctx context.Context, client *ringcentral.Client, params map[string]string, requesterID string) (string, int, error) {
	if err := ensureAuthenticatedRequester(client, requesterID, "Video meetings"); err != nil {
		return "", 0, err
	}
	limit := parsePositiveInt(params["limit"])
	if limit == 0 {
		limit = 10
	}
	scope := strings.ToLower(strings.TrimSpace(params["scope"]))
	important := strings.EqualFold(strings.TrimSpace(params["important"]), "true")
	if scope == "today" || scope == "upcoming" {
		meetings, err := listUpcomingMeetingEvents(ctx, client, scope, limit)
		if err != nil {
			return "", 0, err
		}
		return formatUpcomingMeetingEvents(meetings, scope, important), len(meetings), nil
	}
	list, err := client.ListVideoMeetingHistory(ctx, ringcentral.VideoMeetingHistoryOptions{
		Type:    "All",
		PerPage: limit,
	})
	if err != nil {
		return "", 0, err
	}
	meetings := filterVideoMeetingHistory(list.Meetings, scope, limit)
	return formatVideoMeetingHistoryList(meetings, scope, important), len(meetings), nil
}

type upcomingMeetingEvent struct {
	ID          string
	Title       string
	StartTime   string
	EndTime     string
	Location    string
	Description string
	Source      string
}

func listUpcomingMeetingEvents(ctx context.Context, client *ringcentral.Client, scope string, limit int) ([]upcomingMeetingEvent, error) {
	events, err := listCloudCalendarMeetings(ctx, client, scope, limit)
	if err != nil {
		return nil, err
	}
	return events, nil
}

func listCloudCalendarMeetings(ctx context.Context, client *ringcentral.Client, scope string, limit int) ([]upcomingMeetingEvent, error) {
	windowStart, windowEnd := upcomingMeetingWindow(scope, time.Now())
	calendars, err := client.ListCloudCalendars(ctx, true)
	if err != nil {
		return nil, err
	}
	events := make([]upcomingMeetingEvent, 0)
	for _, calendar := range calendars.Records {
		if !calendar.Connected {
			continue
		}
		providerID := strings.TrimSpace(calendar.ProviderID)
		if providerID == "" {
			providerID = strings.TrimSpace(calendar.ID)
		}
		calendarID := strings.TrimSpace(calendar.CalendarID)
		if calendarID == "" {
			calendarID = strings.TrimSpace(calendar.ID)
		}
		if providerID == "" || calendarID == "" {
			continue
		}
		list, err := client.ListCloudCalendarEvents(ctx, providerID, calendarID, ringcentral.CloudCalendarEventOptions{
			StartTimeFrom:      windowStart,
			StartTimeTo:        windowEnd,
			IncludeNonRCEvents: true,
			PerPage:            100,
		})
		if err != nil {
			return nil, err
		}
		events = append(events, cloudEventsToUpcomingMeetings(list.Records, providerID, calendarID)...)
	}
	return filterUpcomingMeetingEvents(events, scope, limit), nil
}

func upcomingMeetingWindow(scope string, now time.Time) (string, string) {
	loc := now.Location()
	start := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, loc)
	end := start.Add(24 * time.Hour)
	if scope == "upcoming" {
		start = now.In(loc)
		end = start.AddDate(0, 0, 14)
	}
	return start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339)
}

func isCloudCalendarUnavailable(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "ManageCloudCalendars") ||
		strings.Contains(message, "VideoInternal") ||
		strings.Contains(message, "HTTP 403") ||
		strings.Contains(message, "HTTP 404")
}

func ensureAuthenticatedRequester(client *ringcentral.Client, requesterID string, capability string) error {
	requesterID = strings.TrimSpace(requesterID)
	if requesterID == "" || client == nil {
		return nil
	}
	ownerID := strings.TrimSpace(client.OwnerID())
	if ownerID == "" || ownerID == requesterID {
		return nil
	}
	return fmt.Errorf("%s is user-scoped to the authenticated RingCentral user. The FIJI requester extension is %s, but the configured Private JWT owner is %s. Re-onboard or rotate the Runtime JWT so this bot uses the current FIJI user's JWT; RingClaw will not fall back to company-level history", capability, requesterID, ownerID)
}

func filterVideoBridges(records []ringcentral.VideoBridge, scope string, limit int) []ringcentral.VideoBridge {
	out := make([]ringcentral.VideoBridge, 0, len(records))
	for _, bridge := range records {
		if scope == "today" && !videoBridgeIsTodayOrUndated(bridge, time.Now()) {
			continue
		}
		out = append(out, bridge)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func videoBridgeIsTodayOrUndated(bridge ringcentral.VideoBridge, now time.Time) bool {
	for _, value := range []string{bridge.CreateTime, bridge.UpdateTime} {
		if t, ok := parseRFC3339Time(value); ok {
			y1, m1, d1 := t.In(now.Location()).Date()
			y2, m2, d2 := now.Date()
			return y1 == y2 && m1 == m2 && d1 == d2
		}
	}
	return bridge.CreateTime == "" && bridge.UpdateTime == ""
}

func parseRFC3339Time(value string) (time.Time, bool) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func parsePositiveInt(value string) int {
	var out int
	if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &out); err != nil || out < 1 {
		return 0
	}
	return out
}

func formatVideoBridgeList(bridges []ringcentral.VideoBridge, scope string, important bool) string {
	if len(bridges) == 0 {
		if scope == "today" {
			return "No video meetings found for today."
		}
		return "No video meetings found."
	}
	header := "Video meetings"
	if scope == "today" && important {
		header = "Important video meetings today"
	} else if scope == "today" {
		header = "Video meetings today"
	} else if scope == "recent" && important {
		header = "Important recent video meetings"
	} else if scope == "recent" {
		header = "Recent video meetings"
	} else if important {
		header = "Important video meetings"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**%s** (%d)\n", header, len(bridges)))
	for _, bridge := range bridges {
		name := strings.TrimSpace(bridge.Name)
		if name == "" {
			name = "Untitled meeting"
		}
		sb.WriteString(fmt.Sprintf("- `%s` **%s**", bridge.ID, name))
		if bridge.Type != "" {
			sb.WriteString(fmt.Sprintf(" [%s]", bridge.Type))
		}
		if bridge.CreateTime != "" {
			sb.WriteString(fmt.Sprintf(" created=%s", bridge.CreateTime))
		}
		if bridge.Discovery.Web != "" {
			sb.WriteString(fmt.Sprintf(" — %s", bridge.Discovery.Web))
		}
		sb.WriteByte('\n')
	}
	return strings.TrimSpace(sb.String())
}

func teamEventsToUpcomingMeetings(records []ringcentral.Event) []upcomingMeetingEvent {
	out := make([]upcomingMeetingEvent, 0, len(records))
	for _, event := range records {
		out = append(out, upcomingMeetingEvent{
			ID:          event.ID,
			Title:       event.Title,
			StartTime:   event.StartTime,
			EndTime:     event.EndTime,
			Location:    event.Location,
			Description: event.Description,
			Source:      "team_messaging_event",
		})
	}
	return out
}

func cloudEventsToUpcomingMeetings(records []ringcentral.CloudCalendarEvent, providerID, calendarID string) []upcomingMeetingEvent {
	out := make([]upcomingMeetingEvent, 0, len(records))
	for _, event := range records {
		if event.Cancelled || event.IsCancelled {
			continue
		}
		out = append(out, upcomingMeetingEvent{
			ID:          firstNonEmpty(event.ID, providerID+"/"+calendarID),
			Title:       event.Subject,
			StartTime:   normalizeCloudEventTime(event.StartTime, event.Start.DateTime),
			EndTime:     normalizeCloudEventTime(event.EndTime, event.End.DateTime),
			Location:    event.Location,
			Description: event.Description,
			Source:      "cloud_calendar",
		})
	}
	return out
}

func filterUpcomingMeetingEvents(records []upcomingMeetingEvent, scope string, limit int) []upcomingMeetingEvent {
	out := make([]upcomingMeetingEvent, 0, len(records))
	deduped := make(map[string]bool, len(records))
	now := time.Now()
	for _, event := range records {
		if scope == "today" && !eventIsTodayOrUndated(event, now) {
			continue
		}
		if scope == "upcoming" && !eventIsUpcomingOrUndated(event, now) {
			continue
		}
		key := upcomingMeetingDedupKey(event)
		if deduped[key] {
			continue
		}
		deduped[key] = true
		out = append(out, event)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].StartTime < out[j].StartTime
	})
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func normalizeCloudEventTime(values ...string) string {
	value := firstNonEmpty(values...)
	if value == "" || strings.HasSuffix(value, "Z") {
		return value
	}
	if strings.Contains(value, "+") || strings.LastIndex(value, "-") > len("2006-01-02") {
		return value
	}
	return value + "Z"
}

func upcomingMeetingDedupKey(event upcomingMeetingEvent) string {
	title := strings.ToLower(strings.Join(strings.Fields(event.Title), " "))
	location := strings.ToLower(strings.Join(strings.Fields(event.Location), " "))
	return title + "|" + event.StartTime + "|" + event.EndTime + "|" + location
}

func eventIsTodayOrUndated(event upcomingMeetingEvent, now time.Time) bool {
	if t, ok := parseRFC3339Time(event.StartTime); ok {
		y1, m1, d1 := t.In(now.Location()).Date()
		y2, m2, d2 := now.Date()
		return y1 == y2 && m1 == m2 && d1 == d2
	}
	return strings.TrimSpace(event.StartTime) == ""
}

func eventIsUpcomingOrUndated(event upcomingMeetingEvent, now time.Time) bool {
	if t, ok := parseRFC3339Time(event.EndTime); ok {
		return !t.Before(now)
	}
	if t, ok := parseRFC3339Time(event.StartTime); ok {
		return !t.Before(now)
	}
	return strings.TrimSpace(event.StartTime) == ""
}

func formatUpcomingMeetingEvents(events []upcomingMeetingEvent, scope string, important bool) string {
	if len(events) == 0 {
		if scope == "today" {
			return "No upcoming meetings found for today."
		}
		return "No upcoming meetings found."
	}
	header := "Upcoming meetings"
	if scope == "today" {
		header = "Upcoming meetings today"
	}
	if important {
		header += " (important candidates)"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**%s** (%d)\n", header, len(events)))
	for _, event := range events {
		title := strings.TrimSpace(event.Title)
		if title == "" {
			title = "Untitled meeting"
		}
		sb.WriteString(fmt.Sprintf("- Title: **%s** (`%s`)", title, event.ID))
		if event.StartTime != "" {
			sb.WriteString(fmt.Sprintf(" start=%s", event.StartTime))
		}
		if event.EndTime != "" {
			sb.WriteString(fmt.Sprintf(" end=%s", event.EndTime))
		}
		if event.Location != "" {
			sb.WriteString(fmt.Sprintf(" location=%s", event.Location))
		}
		if description := compactText(event.Description, 220); description != "" {
			sb.WriteString(fmt.Sprintf("\n  Description: %s", description))
			if important {
				sb.WriteString(fmt.Sprintf("\n  Important info: %s", description))
			}
		}
		sb.WriteByte('\n')
	}
	return strings.TrimSpace(sb.String())
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func compactText(value string, max int) string {
	compact := strings.Join(strings.Fields(value), " ")
	if max > 0 && len(compact) > max {
		return compact[:max] + "..."
	}
	return compact
}

func filterVideoMeetingHistory(records []ringcentral.VideoMeetingHistory, scope string, limit int) []ringcentral.VideoMeetingHistory {
	out := make([]ringcentral.VideoMeetingHistory, 0, len(records))
	for _, meeting := range records {
		if scope == "today" && !videoMeetingHistoryIsTodayOrUndated(meeting, time.Now()) {
			continue
		}
		out = append(out, meeting)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func videoMeetingHistoryIsTodayOrUndated(meeting ringcentral.VideoMeetingHistory, now time.Time) bool {
	if t, ok := parseRFC3339Time(meeting.StartTime); ok {
		y1, m1, d1 := t.In(now.Location()).Date()
		y2, m2, d2 := now.Date()
		return y1 == y2 && m1 == m2 && d1 == d2
	}
	return strings.TrimSpace(meeting.StartTime) == ""
}

func formatVideoMeetingHistoryList(meetings []ringcentral.VideoMeetingHistory, scope string, important bool) string {
	if len(meetings) == 0 {
		if scope == "today" {
			return "No video meeting records found for today."
		}
		return "No video meeting records found."
	}
	header := "Video meeting records"
	if scope == "today" {
		header = "Video meeting records today"
	}
	if important {
		header += " (important candidates)"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**%s**\n", header))
	for _, meeting := range meetings {
		title := strings.TrimSpace(meeting.DisplayName)
		if title == "" {
			title = "Video meeting"
		}
		status := strings.TrimSpace(meeting.Status)
		if status == "" {
			status = "Unknown"
		}
		host := strings.TrimSpace(meeting.HostInfo.DisplayName)
		if host == "" {
			host = "Unknown host"
		}
		sb.WriteString(fmt.Sprintf("- `%s` %s [%s] host=%s duration=%ds participants=%d recordings=%d\n",
			meeting.ID, title, status, host, meeting.Duration, len(meeting.Participants), len(meeting.Recordings)))
		if meeting.StartTime != "" {
			sb.WriteString(fmt.Sprintf("  start: %s\n", meeting.StartTime))
		}
	}
	return strings.TrimSpace(sb.String())
}

type phoneClientCallRequest struct {
	ToNumber    string
	TargetLabel string
}

func phoneClientCallFromParams(ctx context.Context, client *ringcentral.Client, params map[string]string) (*phoneClientCallRequest, error) {
	to := strings.TrimSpace(params["to"])
	if to == "" {
		return nil, fmt.Errorf("missing to phone number or contact name")
	}
	if looksLikePhoneNumber(to) {
		return &phoneClientCallRequest{
			ToNumber:    to,
			TargetLabel: to,
		}, nil
	}
	number, label, err := resolveNameToPhoneNumber(ctx, client, to)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(label) == "" {
		label = to
	}
	return &phoneClientCallRequest{
		ToNumber:    number,
		TargetLabel: label,
	}, nil
}

func formatPhoneClientCallMessage(targetLabel string) string {
	targetLabel = strings.TrimSpace(targetLabel)
	if targetLabel == "" {
		targetLabel = "the selected number"
	}
	return fmt.Sprintf("Prepared a FIJI phone call to %s. FIJI will use the current signed-in user's Phone client to place the call.", targetLabel)
}

func ringOutRequestFromParams(ctx context.Context, client *ringcentral.Client, params map[string]string) (*ringcentral.CreateRingOutRequest, error) {
	from := strings.TrimSpace(params["from"])
	to := strings.TrimSpace(params["to"])
	if to == "" {
		return nil, fmt.Errorf("missing to phone number or contact name")
	}
	if isExtensionOnlyRingOutFrom(from) {
		return nil, fmt.Errorf("from=%s looks like an extension. RingOut `from` must be a reachable forwarding/callback phone number, preferably E.164 such as +14155550100; use a configured RingOut callback number instead", from)
	}
	if from == "" {
		number, err := defaultRingOutFromNumber(ctx, client)
		if err != nil {
			return nil, err
		}
		from = number
	}
	if !looksLikePhoneNumber(to) {
		number, label, err := resolveNameToPhoneNumber(ctx, client, to)
		if err != nil {
			return nil, err
		}
		slog.Info("action: resolved ringout target", "target", to, "match", label, "phone", number)
		to = number
	}
	req := &ringcentral.CreateRingOutRequest{
		To: ringcentral.PhoneNumberRef{PhoneNumber: to},
	}
	if from != "" {
		req.From = &ringcentral.PhoneNumberRef{PhoneNumber: from}
	}
	if callerID := strings.TrimSpace(params["callerid"]); callerID != "" {
		req.CallerID = &ringcentral.PhoneNumberRef{PhoneNumber: callerID}
	}
	if playPrompt := strings.ToLower(strings.TrimSpace(params["playprompt"])); playPrompt == "true" || playPrompt == "1" || playPrompt == "yes" {
		req.PlayPrompt = true
	}
	return req, nil
}

func defaultRingOutFromNumber(ctx context.Context, client *ringcentral.Client) (string, error) {
	if client == nil {
		return "", fmt.Errorf("missing RingCentral client for default RingOut callback number")
	}
	list, err := client.ListForwardingNumbers(ctx)
	if err != nil {
		return "", fmt.Errorf("list current extension forwarding numbers: %w", err)
	}
	number := bestForwardingNumber(list.Records)
	if number == "" {
		return "", fmt.Errorf("current extension has no RingOut forwarding/callback number; configure a RingOut callback number in RingCentral call handling or provide from=<callback phone> explicitly")
	}
	return number, nil
}

func bestForwardingNumber(records []ringcentral.ForwardingNumber) string {
	for _, record := range records {
		if !forwardingNumberUsable(record) {
			continue
		}
		if hasFeature(record.Features, "RingOut") {
			return strings.TrimSpace(record.PhoneNumber)
		}
	}
	for _, record := range records {
		if forwardingNumberUsable(record) {
			return strings.TrimSpace(record.PhoneNumber)
		}
	}
	return ""
}

func forwardingNumberUsable(record ringcentral.ForwardingNumber) bool {
	if strings.TrimSpace(record.PhoneNumber) == "" {
		return false
	}
	status := strings.TrimSpace(record.Status)
	if status != "" && !strings.EqualFold(status, "Normal") {
		return false
	}
	if record.Hidden {
		return false
	}
	return true
}

func hasFeature(features []string, want string) bool {
	for _, feature := range features {
		if strings.EqualFold(strings.TrimSpace(feature), want) {
			return true
		}
	}
	return false
}

func bestExtensionPhoneNumber(records []ringcentral.ExtensionPhoneNumber) string {
	preferredUsage := []string{"DirectNumber", "MainCompanyNumber", "CompanyNumber", "AdditionalCompanyNumber"}
	for _, usage := range preferredUsage {
		for _, record := range records {
			if !extensionPhoneNumberUsable(record) {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(record.UsageType), usage) {
				return strings.TrimSpace(record.PhoneNumber)
			}
		}
	}
	for _, record := range records {
		if extensionPhoneNumberUsable(record) {
			return strings.TrimSpace(record.PhoneNumber)
		}
	}
	return ""
}

func extensionPhoneNumberUsable(record ringcentral.ExtensionPhoneNumber) bool {
	if strings.TrimSpace(record.PhoneNumber) == "" {
		return false
	}
	status := strings.TrimSpace(record.Status)
	if status != "" && !strings.EqualFold(status, "Normal") {
		return false
	}
	return true
}

func looksLikePhoneNumber(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	hasDigit := false
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case strings.ContainsRune("+()-. #", r):
		default:
			return false
		}
	}
	return hasDigit
}

func phoneCallLogFromParams(ctx context.Context, client *ringcentral.Client, params map[string]string, requesterID string) (string, int, error) {
	scope := strings.ToLower(strings.TrimSpace(params["scope"]))
	missing := truthyParam(params["missing"])
	days := parsePositiveInt(params["days"])
	opts := ringcentral.CallLogOptions{
		RecordCount: parsePositiveInt(params["limit"]),
		ExtensionID: strings.TrimSpace(requesterID),
		View:        strings.TrimSpace(params["view"]),
		Direction:   strings.TrimSpace(params["direction"]),
		Result:      strings.TrimSpace(params["result"]),
		DateFrom:    strings.TrimSpace(params["date_from"]),
		DateTo:      strings.TrimSpace(params["date_to"]),
	}
	if opts.RecordCount == 0 {
		opts.RecordCount = 10
	}
	if missing {
		if opts.Direction == "" {
			opts.Direction = "Inbound"
		}
		if opts.Result == "" {
			opts.Result = "Missed"
		}
		if opts.RecordCount < 100 {
			opts.RecordCount = 100
		}
	}
	if scope == "today" && (opts.DateFrom == "" || opts.DateTo == "") {
		opts.DateFrom, opts.DateTo = todayRFC3339Range(time.Now())
	}
	if scope == "recent" && days == 0 && opts.DateFrom == "" && opts.DateTo == "" {
		days = 15
	}
	if days > 0 && (opts.DateFrom == "" || opts.DateTo == "") {
		opts.DateFrom, opts.DateTo = recentDaysRFC3339Range(time.Now(), days)
	}
	list, err := client.ListExtensionCallLog(ctx, opts)
	if err != nil {
		return "", 0, err
	}
	records := filterPhoneCallLogRecords(list.Records, opts, scope, days, time.Now())
	summary := truthyParam(params["summary"])
	nextActions := truthyParam(params["next_actions"])
	var followUps []missedCallFollowUpStatus
	if nextActions {
		enrichMissedCallCallbackNumbers(ctx, client, records)
		followUps = sendMissedCallFollowUpSMS(ctx, client, records)
	}
	return formatPhoneCallLogSummary(records, scope, missing, summary, nextActions, followUps), len(records), nil
}

func enrichMissedCallCallbackNumbers(ctx context.Context, client *ringcentral.Client, records []ringcentral.CallLogRecord) {
	resolved := make(map[string]string)
	for i := range records {
		rec := &records[i]
		if !strings.EqualFold(strings.TrimSpace(rec.Result), "Missed") {
			continue
		}
		name := strings.TrimSpace(rec.From.Name)
		if name == "" || strings.TrimSpace(rec.From.PhoneNumber) != "" {
			continue
		}
		number, ok := resolved[name]
		if !ok {
			var label string
			var err error
			number, label, err = resolveNameToPhoneNumber(ctx, client, name)
			if err != nil {
				slog.Debug("call log missed caller phone lookup failed", "component", "actions", "name", name, "error", err)
				resolved[name] = ""
				continue
			}
			if label != "" {
				rec.From.Name = label
			}
			resolved[name] = number
		}
		if number != "" {
			rec.From.PhoneNumber = number
		}
	}
}

func recentDaysRFC3339Range(now time.Time, days int) (string, string) {
	if days < 1 {
		days = 1
	}
	end := now.Local()
	start := end.AddDate(0, 0, -days)
	return start.Format(time.RFC3339), end.Format(time.RFC3339)
}

func todayRFC3339Range(now time.Time) (string, string) {
	local := now.Local()
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	end := start.Add(24*time.Hour - time.Nanosecond)
	return start.Format(time.RFC3339), end.Format(time.RFC3339)
}

func filterPhoneCallLogRecords(records []ringcentral.CallLogRecord, opts ringcentral.CallLogOptions, scope string, days int, now time.Time) []ringcentral.CallLogRecord {
	out := make([]ringcentral.CallLogRecord, 0, len(records))
	for _, rec := range records {
		if opts.Direction != "" && !strings.EqualFold(strings.TrimSpace(rec.Direction), opts.Direction) {
			continue
		}
		if opts.Result != "" && !strings.EqualFold(strings.TrimSpace(rec.Result), opts.Result) {
			continue
		}
		if scope == "today" && !callLogRecordIsTodayOrUndated(rec, now) {
			continue
		}
		if days > 0 && !callLogRecordIsWithinDaysOrUndated(rec, now, days) {
			continue
		}
		out = append(out, rec)
	}
	return out
}

func callLogRecordIsWithinDaysOrUndated(rec ringcentral.CallLogRecord, now time.Time, days int) bool {
	if days < 1 {
		days = 1
	}
	if t, ok := parseRFC3339Time(rec.StartTime); ok {
		return !t.Before(now.AddDate(0, 0, -days)) && !t.After(now)
	}
	return strings.TrimSpace(rec.StartTime) == ""
}

func callLogRecordIsTodayOrUndated(rec ringcentral.CallLogRecord, now time.Time) bool {
	if t, ok := parseRFC3339Time(rec.StartTime); ok {
		y1, m1, d1 := t.In(now.Location()).Date()
		y2, m2, d2 := now.Date()
		return y1 == y2 && m1 == m2 && d1 == d2
	}
	return strings.TrimSpace(rec.StartTime) == ""
}

func truthyParam(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "y":
		return true
	default:
		return false
	}
}

func formatPhoneCallLogSummary(records []ringcentral.CallLogRecord, scope string, missing bool, summary bool, nextActions bool, followUps []missedCallFollowUpStatus) string {
	if len(records) == 0 {
		if scope == "today" {
			return "No call log records found for today."
		}
		return "No call log records found."
	}
	stats := summarizeCallLogs(records)
	header := "Call summary"
	if scope == "today" {
		header = "Call summary today"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**%s**\n", header))
	if summary || missing {
		sb.WriteString(fmt.Sprintf("- Total calls: %d\n", len(records)))
		sb.WriteString(fmt.Sprintf("- Missed calls: %d\n", stats.missed))
		sb.WriteString(fmt.Sprintf("- Inbound: %d\n", stats.inbound))
		sb.WriteString(fmt.Sprintf("- Outbound: %d\n", stats.outbound))
		sb.WriteString(fmt.Sprintf("- Answered/accepted: %d\n", stats.answered))
	}
	sb.WriteString("\n**Calls**\n")
	for _, rec := range records {
		result := strings.TrimSpace(rec.Result)
		if result == "" {
			result = "Unknown"
		}
		sb.WriteString(fmt.Sprintf("- `%s` %s %s [%s] %s -> %s (%ds)\n",
			rec.ID, rec.StartTime, rec.Direction, result, callLogPartyLabel(rec.From), callLogPartyLabel(rec.To), rec.Duration))
	}
	if nextActions {
		sb.WriteString("\n**Next actions**\n")
		if stats.missed == 0 {
			sb.WriteString("- No missed-call follow-up needed.\n")
		} else if len(followUps) > 0 {
			for _, item := range followUps {
				if item.Success {
					sb.WriteString(fmt.Sprintf("- SMS sent to %s. Status: %s", item.Label, item.Status))
					if item.MessageID != "" {
						sb.WriteString(fmt.Sprintf(" (id `%s`)", item.MessageID))
					}
					sb.WriteString(".\n")
				} else {
					sb.WriteString(fmt.Sprintf("- SMS follow-up failed for %s: %s.\n", item.Label, item.Error))
				}
				sb.WriteString(fmt.Sprintf("- You can also directly click/call %s.\n", item.Label))
			}
		} else {
			for _, rec := range records {
				if strings.EqualFold(strings.TrimSpace(rec.Result), "Missed") {
					sb.WriteString(fmt.Sprintf("- Follow up missed call from %s.\n", callLogPartyLabel(rec.From)))
				}
			}
		}
		if stats.outbound > 0 {
			sb.WriteString("- Review outbound calls and add notes/tasks for open follow-ups.\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

type missedCallFollowUpStatus struct {
	Label     string
	Number    string
	Success   bool
	Status    string
	MessageID string
	Error     string
}

func sendMissedCallFollowUpSMS(ctx context.Context, client *ringcentral.Client, records []ringcentral.CallLogRecord) []missedCallFollowUpStatus {
	targets := uniqueMissedCallFollowUpTargets(records)
	if len(targets) == 0 {
		return nil
	}
	from, err := defaultSMSSenderNumber(ctx, client)
	if err != nil {
		out := make([]missedCallFollowUpStatus, 0, len(targets))
		for _, target := range targets {
			out = append(out, missedCallFollowUpStatus{
				Label: target.Label,
				Error: err.Error(),
			})
		}
		return out
	}
	const body = "Sorry I missed your call. What is this regarding?"
	out := make([]missedCallFollowUpStatus, 0, len(targets))
	for _, target := range targets {
		status := missedCallFollowUpStatus{
			Label:  target.Label,
			Number: target.Number,
		}
		if strings.TrimSpace(target.Number) == "" {
			status.Error = "no reachable phone number found"
			out = append(out, status)
			continue
		}
		msg, err := client.SendSMS(ctx, &ringcentral.CreateSMSRequest{
			From: ringcentral.PhoneNumberRef{PhoneNumber: from},
			To:   []ringcentral.PhoneNumberRef{{PhoneNumber: target.Number}},
			Text: body,
		})
		if err != nil {
			slog.Error("missed-call sms follow-up failed", "component", "actions", "to", target.Number, "label", target.Label, "error", err)
			status.Error = friendlyPhoneAPIError(err)
			out = append(out, status)
			continue
		}
		status.Success = true
		status.Status = strings.TrimSpace(msg.MessageStatus)
		if status.Status == "" {
			status.Status = "Queued"
		}
		status.MessageID = ringcentral.FormatResourceID(msg.ID)
		slog.Info("missed-call sms follow-up sent", "component", "actions", "to", target.Number, "label", target.Label, "messageID", status.MessageID, "status", status.Status)
		out = append(out, status)
	}
	return out
}

type missedCallFollowUpTarget struct {
	Label  string
	Number string
}

func uniqueMissedCallFollowUpTargets(records []ringcentral.CallLogRecord) []missedCallFollowUpTarget {
	seen := make(map[string]bool)
	out := make([]missedCallFollowUpTarget, 0, len(records))
	for _, rec := range records {
		if !strings.EqualFold(strings.TrimSpace(rec.Result), "Missed") {
			continue
		}
		label := callLogPartyLabel(rec.From)
		number := strings.TrimSpace(rec.From.PhoneNumber)
		key := strings.ToLower(strings.TrimSpace(number))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(label))
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, missedCallFollowUpTarget{
			Label:  label,
			Number: number,
		})
	}
	return out
}

type callLogStats struct {
	missed   int
	inbound  int
	outbound int
	answered int
}

func summarizeCallLogs(records []ringcentral.CallLogRecord) callLogStats {
	var stats callLogStats
	for _, rec := range records {
		if strings.EqualFold(strings.TrimSpace(rec.Direction), "Inbound") {
			stats.inbound++
		}
		if strings.EqualFold(strings.TrimSpace(rec.Direction), "Outbound") {
			stats.outbound++
		}
		result := strings.TrimSpace(rec.Result)
		if strings.EqualFold(result, "Missed") {
			stats.missed++
		}
		if strings.EqualFold(result, "Accepted") || strings.EqualFold(result, "Call connected") || strings.EqualFold(result, "Answered") {
			stats.answered++
		}
	}
	return stats
}

func callLogPartyLabel(p ringcentral.CallLogParty) string {
	name := strings.TrimSpace(p.Name)
	number := strings.TrimSpace(p.PhoneNumber)
	switch {
	case name != "" && number != "":
		return fmt.Sprintf("%s (%s)", name, number)
	case name != "":
		return name
	case number != "":
		return number
	default:
		return "Unknown"
	}
}

func isExtensionOnlyRingOutFrom(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "+") {
		return false
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, value)
	return digits == value && len(digits) >= 2 && len(digits) <= 6
}

func friendlyPhoneAPIError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "permissionName\":\"RingOut") ||
		strings.Contains(msg, "[RingOut] permission") ||
		strings.Contains(msg, "permissionName: RingOut"):
		return "RingOut permission is missing. Ask an admin to add `RingOut` to the Private JWT App, regenerate or rotate the JWT token, then rerun RC JWT preflight/onboarding."
	case strings.Contains(msg, "permissionName\":\"ReadCallLog") ||
		strings.Contains(msg, "[ReadCallLog] permission") ||
		strings.Contains(msg, "permissionName: ReadCallLog"):
		return "ReadCallLog permission is missing. Ask an admin to add `ReadCallLog` to the Private JWT App, regenerate or rotate the JWT token, then rerun RC JWT preflight/onboarding."
	case strings.Contains(msg, "permissionName\":\"SMS") ||
		strings.Contains(msg, "[SMS] permission") ||
		strings.Contains(msg, "permissionName: SMS"):
		return "SMS permission is missing. Ask an admin to add `SMS` to the Private JWT App, regenerate or rotate the JWT token, then rerun RC JWT preflight/onboarding."
	default:
		return msg
	}
}

// announceCrossChatOrRefuse posts a metadata-only heads-up to the
// owner's bot DM BEFORE an AI-triggered cross-chat ACTION is
// dispatched. The action lands only if this pre-notice succeeds;
// every other path returns an error and the caller refuses the
// action. This is the fail-closed replacement for the earlier
// best-effort async notice — see Finding 2 in the Phase 2 follow-up
// review.
//
// The notice intentionally carries no body/title/content — just the
// action type, requester, timestamp and the origin / target chat
// IDs. The operator has to open the target chat (or the audit log)
// to see what was actually written.
//
// A target chat that equals the owner's own DM is handled by the
// caller (the heads-up would be noise because the owner already
// sees the action land in their own timeline). Callers MUST guard
// that case before invoking this function.
func announceCrossChatOrRefuse(ctx context.Context, replyClient *ringcentral.Client, opts ActionContext, actionType, originChat, targetChat string) error {
	if opts.OwnerDMChat == "" {
		return fmt.Errorf("no owner DM audit channel configured")
	}
	requester := opts.RequesterID
	if requester == "" {
		requester = "unknown"
	}
	msg := fmt.Sprintf("[notice] %s by %s at %s: origin=%s target=%s",
		actionType, requester, time.Now().UTC().Format(time.RFC3339),
		originChat, targetChat,
	)
	client := newOOBClient(replyClient)
	if client == nil {
		return fmt.Errorf("no reply client available for audit channel")
	}
	// Cap the wait so a stuck RC endpoint cannot wedge the whole
	// prompt pipeline. We still respect the caller's ctx via
	// context.WithTimeout, so an upstream cancel is honored too.
	sendCtx, cancel := context.WithTimeout(ctx, crossChatNoticeTimeout)
	defer cancel()
	if err := client.SendText(sendCtx, opts.OwnerDMChat, msg); err != nil {
		return fmt.Errorf("audit notice delivery failed: %w", err)
	}
	slog.Info("action: cross-chat notice sent (pre-dispatch)",
		"type", actionType, "from", originChat, "to", targetChat,
		"ownerDMChat", opts.OwnerDMChat, "requesterID", requester)
	return nil
}

// extractChatID extracts a numeric chat ID from various formats:
// "12345", "![:Team](12345)", "![:Person](12345)"
func extractChatID(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "("); idx >= 0 {
		end := strings.Index(s[idx:], ")")
		if end > 0 {
			return s[idx+1 : idx+end]
		}
	}
	return s
}

// crossChatOOBChallenge issues a challenge to the owner for a
// non-owner cross-chat ACTION, posts the prompt in the owner DM, and
// returns a "pending" message for the origin chat. A background
// goroutine awaits the challenge resolution and executes or drops the
// action accordingly.
func crossChatOOBChallenge(ctx context.Context, actionClient *ringcentral.Client, a AgentAction, originChat, targetChat string, opts ActionContext) string {
	intent := fmt.Sprintf("cross-chat %s by %s: origin=%s target=%s",
		a.Type, opts.RequesterID, originChat, targetChat)
	if a.Body != "" {
		intent += " body=" + util.Truncate(a.Body, 80)
	}
	challenge, err := opts.OOB.Issue(opts.RequesterID, intent, originChat,
		opts.OwnerDMChat, oob.IssueOptions{
			TTL:     oob.DefaultChallengeTTL,
			OwnerID: opts.OwnerID,
		})
	if err != nil {
		slog.Error("action: cross-chat OOB challenge issue failed",
			"type", a.Type, "requester", opts.RequesterID, "error", err)
		return fmt.Sprintf("Cross-chat %s failed: challenge error", a.Type)
	}
	if err := postCrossChatPrompt(ctx, actionClient, challenge, a, originChat, targetChat, opts); err != nil {
		slog.Error("action: cross-chat OOB challenge prompt failed",
			"challengeID", challenge.ID, "error", err)
		opts.OOB.Deny(challenge.ID)
		return fmt.Sprintf("Cross-chat %s failed: %v", a.Type, err)
	}
	go awaitCrossChatOOB(actionClient, challenge, a, originChat, targetChat, opts)
	return fmt.Sprintf("Cross-chat %s pending approval (challenge %s).", a.Type, challenge.ID)
}

// postCrossChatPrompt posts the rich cross-chat ACTION challenge
// prompt to the operator's DM. Surfaces the action type, requester
// identity, origin and target chat names, the most relevant action
// param (Title/Subject), and a body preview so the operator can
// audit the request without opening the chat. Best-effort lookups
// for human-readable labels — the prompt always ships even when the
// directory API is flaky.
func postCrossChatPrompt(ctx context.Context, actionClient *ringcentral.Client, c *oob.Challenge, a AgentAction, originChat, targetChat string, opts ActionContext) error {
	requesterLabel := resolveRequesterLabel(ctx, actionClient, opts.RequesterID)
	if requesterLabel == "" {
		requesterLabel = opts.RequesterID
	}
	originLabel := resolveChatLabel(ctx, actionClient, originChat)
	if originLabel == "" {
		originLabel = originChat
	}
	targetLabel := resolveChatLabel(ctx, actionClient, targetChat)
	if targetLabel == "" {
		targetLabel = targetChat
	}

	var paramLine string
	if title := strings.TrimSpace(a.Params["title"]); title != "" {
		paramLine = "Title: " + title + "\n"
	}
	if subject := strings.TrimSpace(a.Params["subject"]); subject != "" {
		paramLine += "Subject: " + subject + "\n"
	}
	if assignee := strings.TrimSpace(a.Params["assignee"]); assignee != "" {
		paramLine += "Assignee: " + assignee + "\n"
	}

	body := util.Truncate(strings.TrimSpace(a.Body), 200)
	if body == "" {
		body = "(empty)"
	}

	expiresIn := time.Until(c.ExpiresAt).Round(time.Second)
	if expiresIn < 0 {
		expiresIn = 0
	}

	msg := fmt.Sprintf(
		"Pending approval (challenge `%s`).\n"+
			"Action: Cross-chat %s\n"+
			"Requester: %s\n"+
			"Origin chat: %s\n"+
			"Target chat: %s\n"+
			"%sBody: %s\n\n"+
			"Effect: bot will write a %s into the target chat on the requester's behalf.\n\n"+
			"Run on the host:\n"+
			"  ringclaw approval %s        (approve)\n"+
			"  ringclaw approval deny %s   (deny)\n\n"+
			"Expires in %s.",
		c.ID, a.Type, requesterLabel, originLabel, targetLabel,
		paramLine, body, a.Type, c.ID, c.ID, expiresIn,
	)
	return SendTextReply(ctx, actionClient, c.OwnerDMChat, msg)
}

// awaitCrossChatOOB blocks on the challenge resolution. On approval
// the action is executed in the target chat; on denial or expiry a
// notice is posted to the origin chat.
func awaitCrossChatOOB(actionClient *ringcentral.Client, challenge *oob.Challenge, a AgentAction, originChat, targetChat string, opts ActionContext) {
	mgr := opts.OOB
	if mgr == nil {
		return
	}
	timeout := time.Until(challenge.ExpiresAt) + 5*time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	approved, err := challenge.Wait(ctx, mgr)
	switch {
	case err != nil && err != oob.ErrChallengeExpired:
		slog.Warn("action: cross-chat OOB wait failed",
			"challengeID", challenge.ID, "error", err)
		logSendError(SendTextReply(ctx, actionClient, originChat,
			fmt.Sprintf("Cross-chat %s cancelled: %v", a.Type, err)))
		return
	case err == oob.ErrChallengeExpired:
		logSendError(SendTextReply(ctx, actionClient, originChat,
			fmt.Sprintf("Cross-chat %s expired without approval (challenge %s).", a.Type, challenge.ID)))
		return
	case !approved:
		logSendError(SendTextReply(ctx, actionClient, originChat,
			fmt.Sprintf("Cross-chat %s denied (challenge %s).", a.Type, challenge.ID)))
		return
	}

	switch a.Type {
	case "NOTE":
		title := a.Params["title"]
		if title == "" {
			title = "Note"
		}
		note, err := actionClient.CreateNote(ctx, targetChat, &ringcentral.CreateNoteRequest{
			Title: title,
			Body:  a.Body,
		})
		if err != nil {
			logSendError(SendTextReply(ctx, actionClient, originChat,
				fmt.Sprintf("Cross-chat %s failed: create note: %v", a.Type, err)))
			return
		}
		if pubErr := actionClient.PublishNote(ctx, note.ID); pubErr != nil {
			slog.Error("action: publish note failed", "noteID", note.ID, "error", pubErr)
		}
		slog.Info("action: cross-chat OOB approved - created note", "noteID", note.ID, "chatID", targetChat, "title", title)
		logSendError(SendTextReply(ctx, actionClient, originChat,
			fmt.Sprintf("Cross-chat %s approved — note \"%s\" created in target chat.", a.Type, title)))
	case "TASK":
		subject := a.Params["subject"]
		if subject == "" {
			logSendError(SendTextReply(ctx, actionClient, originChat,
				fmt.Sprintf("Cross-chat %s failed: missing subject.", a.Type)))
			return
		}
		req := &ringcentral.CreateTaskRequest{Subject: subject}
		if aid := a.Params["assignee"]; aid != "" {
			resolvedID, err := resolveAssigneeParam(ctx, actionClient, aid)
			if err != nil {
				logSendError(SendTextReply(ctx, actionClient, originChat,
					fmt.Sprintf("Cross-chat %s failed: resolve assignee: %v", a.Type, err)))
				return
			}
			req.Assignees = []ringcentral.TaskAssignee{{ID: resolvedID}}
		}
		task, err := actionClient.CreateTask(ctx, targetChat, req)
		if err != nil {
			logSendError(SendTextReply(ctx, actionClient, originChat,
				fmt.Sprintf("Cross-chat %s failed: create task: %v", a.Type, err)))
			return
		}
		slog.Info("action: cross-chat OOB approved - created task", "taskID", task.ID, "chatID", targetChat, "subject", subject)
		logSendError(SendTextReply(ctx, actionClient, originChat,
			fmt.Sprintf("Cross-chat %s approved — task \"%s\" created in target chat.", a.Type, subject)))
	case "EVENT":
		title := a.Params["title"]
		startTime := a.Params["start"]
		endTime := a.Params["end"]
		if title == "" || startTime == "" || endTime == "" {
			logSendError(SendTextReply(ctx, actionClient, originChat,
				fmt.Sprintf("Cross-chat %s failed: missing title/time.", a.Type)))
			return
		}
		startTime, endTime, err := normalizeEventDateTimes(startTime, endTime)
		if err != nil {
			logSendError(SendTextReply(ctx, actionClient, originChat,
				fmt.Sprintf("Cross-chat %s failed: invalid event time: %v", a.Type, err)))
			return
		}
		event, err := actionClient.CreateEvent(ctx, &ringcentral.CreateEventRequest{
			Title:     title,
			StartTime: startTime,
			EndTime:   endTime,
		})
		if err != nil {
			logSendError(SendTextReply(ctx, actionClient, originChat,
				fmt.Sprintf("Cross-chat %s failed: create event: %v", a.Type, err)))
			return
		}
		slog.Info("action: cross-chat OOB approved - created event", "eventID", event.ID, "title", title)
		logSendError(SendTextReply(ctx, actionClient, originChat,
			fmt.Sprintf("Cross-chat %s approved — event \"%s\" created.", a.Type, title)))
	case "CARD":
		cardJSON := a.Body
		if cardJSON == "" {
			logSendError(SendTextReply(ctx, actionClient, originChat,
				fmt.Sprintf("Cross-chat %s failed: empty card body.", a.Type)))
			return
		}
		if !json.Valid([]byte(cardJSON)) {
			logSendError(SendTextReply(ctx, actionClient, originChat,
				fmt.Sprintf("Cross-chat %s failed: invalid card JSON.", a.Type)))
			return
		}
		cardClient := selectCardClient(actionClient, actionClient, targetChat, originChat) // OOB path has no separate replyClient; use actionClient for both roles.
		card, err := cardClient.CreateAdaptiveCard(ctx, targetChat, json.RawMessage(cardJSON))
		if err != nil {
			logSendError(SendTextReply(ctx, actionClient, originChat,
				fmt.Sprintf("Cross-chat %s failed: create card: %v", a.Type, err)))
			return
		}
		slog.Info("action: cross-chat OOB approved - created card", "cardID", card.ID, "chatID", targetChat)
		logSendError(SendTextReply(ctx, actionClient, originChat,
			fmt.Sprintf("Cross-chat %s approved — card created in target chat.", a.Type)))
	case "MESSAGE":
		body := strings.TrimSpace(a.Body)
		if body == "" {
			logSendError(SendTextReply(ctx, actionClient, originChat,
				fmt.Sprintf("Cross-chat %s failed: empty message body.", a.Type)))
			return
		}
		if err := SendTextReply(ctx, actionClient, targetChat, body); err != nil {
			slog.Error("action: cross-chat OOB message send failed", "error", err, "chatID", targetChat)
			logSendError(SendTextReply(ctx, actionClient, originChat,
				fmt.Sprintf("Cross-chat %s failed: send: %v", a.Type, err)))
			return
		}
		slog.Info("action: cross-chat OOB approved - sent message", "chatID", targetChat,
			"text", util.Truncate(body, 60))
		logSendError(SendTextReply(ctx, actionClient, originChat,
			fmt.Sprintf("Cross-chat %s approved — message delivered to target chat.", a.Type)))
	case "VIDEO":
		title := a.Params["title"]
		if title == "" {
			title = "RingClaw Meeting"
		}
		bridgeType := a.Params["type"]
		if bridgeType == "" {
			bridgeType = "Instant"
		}
		bridge, event, err := createVideoMeeting(ctx, actionClient, videoCreateOptions{
			Title:      title,
			BridgeType: bridgeType,
			StartTime:  a.Params["start"],
			EndTime:    a.Params["end"],
		})
		if err != nil {
			logSendError(SendTextReply(ctx, actionClient, originChat,
				fmt.Sprintf("Cross-chat %s failed: create video meeting: %v", a.Type, err)))
			return
		}
		if err := SendTextReply(ctx, actionClient, targetChat, formatVideoMeetingMessage(bridge, event)); err != nil {
			logSendError(SendTextReply(ctx, actionClient, originChat,
				fmt.Sprintf("Cross-chat %s failed: post video link: %v", a.Type, err)))
			return
		}
		slog.Info("action: cross-chat OOB approved - created video bridge", "bridgeID", bridge.ID, "chatID", targetChat, "title", title)
		summary := fmt.Sprintf("Cross-chat %s approved — video meeting \"%s\" created in target chat.", a.Type, title)
		if event != nil && event.ID != "" {
			summary = fmt.Sprintf("Cross-chat %s approved — scheduled video meeting \"%s\" created in target chat as event `%s`.", a.Type, title, event.ID)
		}
		logSendError(SendTextReply(ctx, actionClient, originChat, summary))
	default:
		logSendError(SendTextReply(ctx, actionClient, originChat,
			fmt.Sprintf("Cross-chat %s cancelled: unsupported type for OOB approval.", a.Type)))
	}
}
