package agent

import (
	"encoding/json"
	"log/slog"
)

// codeNonOwnerDenied is the JSON-RPC error code ringclaw returns when
// the Layer-B gate refuses an fs/* or terminal/* request because the
// originating session belongs to a non-owner sender. -32001 is in the
// reserved JSON-RPC implementation range.
const codeNonOwnerDenied = -32001

// gateNonOwnerToolCall enforces the Layer-B (client-side, fail-closed)
// part of the v0.4.3 non-owner restriction model. It is invoked from
// every fs/* and terminal/* handler immediately after the request has
// been parsed.
//
// Behavior:
//
//   - sessionID matched and the session is owner-driven → returns
//     false (caller proceeds with the original implementation).
//   - sessionID matched and the session is non-owner-driven → bot
//     replies with a JSON-RPC error using codeNonOwnerDenied and
//     returns true (caller MUST `return` immediately).
//   - sessionID unknown → fail-closed: bot also denies. This covers
//     races where a tool call arrives before the session has been
//     registered or after a reset; agents should never see this in
//     practice.
//
// Deny-once-per-(session, method) WARN dedupe is applied so a runaway
// agent does not flood the log.
func (a *ACPAgent) gateNonOwnerToolCall(reqID json.RawMessage, method, sessionID string) bool {
	v, ok := a.sessionRoles.Load(sessionID)
	if !ok {
		a.sendErrorResponse(reqID, codeNonOwnerDenied,
			"ringclaw: tool call denied — unknown session (fail-closed)")
		a.warnDeniedOnce(sessionID, method, "reason", "unknown_session")
		return true
	}
	origin, _ := v.(Origin)
	if origin.IsOwner {
		return false
	}
	a.sendErrorResponse(reqID, codeNonOwnerDenied,
		"ringclaw: "+method+" is denied for non-owner senders (v0.4.3 fail-closed isolation)")
	a.warnDeniedOnce(sessionID, method,
		"reason", "non_owner",
		"sender_id", origin.SenderID,
		"sender_reason", origin.Reason,
	)
	return true
}

func (a *ACPAgent) warnDeniedOnce(sessionID, method string, kv ...interface{}) {
	key := sessionID + "|" + method
	if _, loaded := a.deniedToolWarned.LoadOrStore(key, true); loaded {
		return
	}
	args := append([]interface{}{
		"component", "acp",
		"event", "tool_call_denied",
		"method", method,
		"session", sessionID,
		"command", a.command,
	}, kv...)
	slog.Warn("acp non-owner tool call denied", args...)
}
