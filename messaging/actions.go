package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/internal/util"
	"github.com/ringclaw/ringclaw/messaging/oob"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// AgentAction represents a parsed action from the agent's response.
type AgentAction struct {
	Type   string // "NOTE", "TASK", "EVENT", "CARD", "MESSAGE"
	Params map[string]string
	Body   string
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

	parts := strings.SplitN(header, " ", 2)
	actionType := strings.TrimSpace(parts[0])

	params := make(map[string]string)
	if len(parts) > 1 {
		for _, p := range parseActionParams(parts[1]) {
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

// parseActionParams parses "title=xxx start=2026-01-01T10:00:00Z end=2026-01-01T11:00:00Z"
func parseActionParams(s string) []keyValue {
	var result []keyValue
	keys := []string{"title", "subject", "start", "end", "chatid", "assignee"}
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
// OOB and OwnerDMChat are the Phase 2 wiring for PIN-gated approvals.
// When OOB is non-nil and OwnerDMChat is set, owner-initiated cross-chat
// actions (where chatid != origin) are gated on a PIN approval card
// posted to the owner's bot DM. RequesterID identifies the originating
// user for the approval cache key. Leaving OOB nil reverts to the
// Phase 1 warn-log behavior so library callers (tests, /api/send) keep
// working without forcing every entry point through OOB plumbing.
type ActionContext struct {
	OriginIsOwner bool
	OOB           *oob.Manager
	OwnerDMChat   string
	RequesterID   string
}

// ExecuteAgentActions executes parsed actions against the RC API.
func ExecuteAgentActions(ctx context.Context, replyClient, actionClient *ringcentral.Client, chatID string, actions []AgentAction, opts ActionContext) []string {
	var results []string
	for _, a := range actions {
		targetChat := chatID
		if cid := a.Params["chatid"]; cid != "" {
			if !opts.OriginIsOwner {
				slog.Warn("action: ignoring chatid override from non-owner sender; forcing origin chat",
					"type", a.Type, "requested", cid, "origin", chatID)
			} else {
				resolved, err := resolveChatParam(ctx, actionClient, cid, chatID)
				if err != nil {
					slog.Error("action: failed to resolve chatid", "chatid", cid, "error", err)
					results = append(results, fmt.Sprintf("Failed to resolve chat '%s': %v", cid, err))
					continue
				}
				if resolved != chatID {
					approved, gateErr := authorizeCrossChat(ctx, opts, replyClient, a.Type, chatID, resolved)
					if gateErr != nil {
						slog.Error("action: cross-chat OOB error",
							"type", a.Type, "from", chatID, "to", resolved, "error", gateErr)
						results = append(results, fmt.Sprintf("Cross-chat %s blocked: %v", a.Type, gateErr))
						continue
					}
					if !approved {
						slog.Warn("action: cross-chat dispatch denied by OOB",
							"type", a.Type, "from", chatID, "to", resolved)
						results = append(results, fmt.Sprintf("Cross-chat %s requires PIN approval (denied or expired).", a.Type))
						continue
					}
				}
				targetChat = resolved
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
				results = append(results, fmt.Sprintf("Failed to create note: %v", err))
				continue
			}
			if pubErr := actionClient.PublishNote(ctx, note.ID); pubErr != nil {
				slog.Error("action: publish note failed", "noteID", note.ID, "error", pubErr)
			}
			slog.Info("action: created note", "noteID", note.ID, "chatID", targetChat, "title", title)

		case "TASK":
			subject := a.Params["subject"]
			if subject == "" {
				continue
			}
			req := &ringcentral.CreateTaskRequest{Subject: subject}
			if aid := a.Params["assignee"]; aid != "" {
				resolvedID, err := resolveAssigneeParam(ctx, actionClient, aid)
				if err != nil {
					slog.Error("action: failed to resolve assignee", "assignee", aid, "error", err)
					results = append(results, fmt.Sprintf("Failed to resolve assignee '%s': %v", aid, err))
					continue
				}
				req.Assignees = []ringcentral.TaskAssignee{{ID: resolvedID}}
			}
			task, err := actionClient.CreateTask(ctx, targetChat, req)
			if err != nil {
				slog.Error("action: create task failed", "error", err)
				results = append(results, fmt.Sprintf("Failed to create task: %v", err))
				continue
			}
			slog.Info("action: created task", "taskID", task.ID, "chatID", targetChat, "subject", subject)

		case "EVENT":
			title := a.Params["title"]
			startTime := a.Params["start"]
			endTime := a.Params["end"]
			if title == "" || startTime == "" || endTime == "" {
				continue
			}
			event, err := actionClient.CreateEvent(ctx, &ringcentral.CreateEventRequest{
				Title:     title,
				StartTime: startTime,
				EndTime:   endTime,
			})
			if err != nil {
				slog.Error("action: create event failed", "error", err)
				results = append(results, fmt.Sprintf("Failed to create event: %v", err))
				continue
			}
			slog.Info("action: created event", "eventID", event.ID, "title", title)

		case "CARD":
			cardJSON := a.Body
			if cardJSON == "" {
				continue
			}
			if !json.Valid([]byte(cardJSON)) {
				slog.Error("action: invalid adaptive card JSON")
				results = append(results, "Failed to create card: invalid JSON")
				continue
			}
			cardClient := selectCardClient(replyClient, actionClient, targetChat)
			card, err := cardClient.CreateAdaptiveCard(ctx, targetChat, json.RawMessage(cardJSON))
			if err != nil {
				slog.Error("action: create adaptive card failed", "error", err)
				results = append(results, fmt.Sprintf("Failed to create card: %v", err))
				continue
			}
			slog.Info("action: created adaptive card", "cardID", card.ID, "chatID", targetChat)

		case "MESSAGE":
			body := strings.TrimSpace(a.Body)
			if body == "" {
				continue
			}
			if err := SendTextReply(ctx, actionClient, targetChat, body); err != nil {
				slog.Error("action: send message failed", "error", err, "chatID", targetChat)
				results = append(results, fmt.Sprintf("Failed to send message: %v", err))
				continue
			}
			slog.Info("action: sent message", "chatID", targetChat, "text", util.Truncate(body, 60))

		default:
			slog.Warn("action: unknown action type, sending body as message", "type", a.Type)
			body := strings.TrimSpace(a.Body)
			if body != "" {
				if err := SendTextReply(ctx, replyClient, chatID, fmt.Sprintf("[%s] %s", a.Type, body)); err != nil {
					slog.Error("action: failed to send unknown action as message", "error", err)
				}
			}
			results = append(results, fmt.Sprintf("Unknown action type: %s", a.Type))
		}
	}
	return results
}

// crossChatApprovalTTL bounds how long we wait for the operator to enter
// the PIN before the cross-chat action gives up. Five minutes matches
// oob.DefaultChallengeTTL but is restated here so changing one does not
// silently change the other. Declared as a var (not const) so tests can
// shorten the wait without touching production behavior.
var crossChatApprovalTTL = 5 * time.Minute

// authorizeCrossChat resolves the OOB gate decision for a single
// cross-chat action. Returns (true, nil) when the action may proceed
// (manager not configured -> Phase 1 warn-log fallback, or operator
// approved the PIN challenge). Returns (false, nil) when the operator
// denied or the challenge expired. Non-nil err signals an unexpected
// failure that should be surfaced to the caller.
func authorizeCrossChat(ctx context.Context, opts ActionContext, replyClient *ringcentral.Client, actionType, originChat, resolvedChat string) (bool, error) {
	if opts.OOB == nil || opts.OwnerDMChat == "" {
		// Phase 1 fallback: log loudly so audit trails still capture the
		// dispatch even when no operator is on-call to type a PIN.
		slog.Warn("action: owner cross-chat dispatch (OOB not configured)",
			"type", actionType, "from", originChat, "to", resolvedChat)
		return true, nil
	}
	intent := fmt.Sprintf("%s cross-chat from %s to %s", actionType, originChat, resolvedChat)
	requesterID := opts.RequesterID
	if requesterID == "" {
		requesterID = "unknown"
	}
	approved, err := opts.OOB.Authorize(ctx, oob.AuthorizeOptions{
		RequesterID:  requesterID,
		Intent:       intent,
		OriginChatID: originChat,
		OwnerDMChat:  opts.OwnerDMChat,
		Client:       newOOBClient(replyClient),
		TTL:          crossChatApprovalTTL,
	})
	if err != nil {
		// Treat ErrChallengeExpired as a non-error denial so the result
		// message reads "denied or expired" rather than surfacing the
		// internal sentinel to the user.
		if err == oob.ErrChallengeExpired {
			return false, nil
		}
		return false, err
	}
	if approved {
		slog.Info("action: cross-chat dispatch approved by OOB",
			"type", actionType, "from", originChat, "to", resolvedChat, "requesterID", requesterID)
	}
	return approved, nil
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
