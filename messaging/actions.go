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

// crossChatNoticeTimeout bounds how long we wait for the synchronous
// owner-DM heads-up before giving up and refusing the cross-chat
// ACTION. Declared as a var so tests can shrink the wait without
// changing production behavior.
var crossChatNoticeTimeout = 5 * time.Second

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
}

// ExecuteAgentActions executes parsed actions against the RC API.
func ExecuteAgentActions(ctx context.Context, replyClient, actionClient *ringcentral.Client, chatID string, actions []AgentAction, opts ActionContext) []string {
	var results []string
	for _, a := range actions {
		targetChat := chatID
		crossChat := false
		if cid := a.Params["chatid"]; cid != "" {
			if !opts.OriginIsOwner {
				// Non-owner cross-chat: issue OOB challenge when
				// the manager is wired; otherwise force to origin
				// chat (legacy silent-override path).
				if opts.OOB != nil && opts.OwnerDMChat != "" && opts.OwnerID != "" {
					resolved, err := resolveChatParam(ctx, actionClient, cid, chatID)
					if err != nil {
						slog.Error("action: failed to resolve chatid", "chatid", cid, "error", err)
						results = append(results, fmt.Sprintf("Failed to resolve chat '%s': %v", cid, err))
						continue
					}
					targetChat = resolved
					if resolved != chatID {
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
		if crossChat && targetChat != opts.OwnerDMChat {
			if err := announceCrossChatOrRefuse(ctx, replyClient, opts, a.Type, chatID, targetChat); err != nil {
				slog.Warn("action: cross-chat ACTION refused (fail-closed on pre-notice)",
					"type", a.Type, "origin", chatID, "target", targetChat,
					"requesterID", opts.RequesterID, "error", err)
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
		cardClient := selectCardClient(actionClient, actionClient, targetChat) // OOB path has no separate replyClient; use actionClient for both roles.
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
	default:
		logSendError(SendTextReply(ctx, actionClient, originChat,
			fmt.Sprintf("Cross-chat %s cancelled: unsupported type for OOB approval.", a.Type)))
	}
}
