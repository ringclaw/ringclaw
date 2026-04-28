package agent

import "context"

// Origin describes the authority level of the message that ultimately
// drives an ACP prompt. It is propagated through the request context
// so the agent layer can decide whether to apply a restricted ACP
// mode and whether to deny client-side tool calls (fs/* and
// terminal/*) for the duration of that session.
//
// Two-state model (v0.4.3):
//
//   - IsOwner == true  → full agent capability (legacy behavior).
//   - IsOwner == false → restricted: bot will request a read-only
//     ACP mode (`spec` for droid, `plan` for claude/gemini/qwen/cursor)
//     AND fail-closed deny every fs/* + terminal/* tool call from
//     this session.
//
// The Reason field is informational only — used by audit logs to
// distinguish "owner because source_user_ids", "owner because DM to
// bot", "non-owner because chat_user_allow", etc.
type Origin struct {
	IsOwner  bool
	SenderID string
	Reason   string
}

type originCtxKey struct{}

// WithOrigin returns a derived context that carries the supplied
// Origin. Callers building an ACP prompt MUST attach this before
// invoking Chat / ChatWithImages / ChatWithAudio so the agent layer
// can apply non-owner restrictions.
func WithOrigin(ctx context.Context, o Origin) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, originCtxKey{}, o)
}

// OriginFromContext extracts the Origin attached by WithOrigin.
//
// Backwards-compatibility default: when no Origin has been attached
// (cron jobs, heartbeat probes, /api/send callers that haven't been
// migrated yet) the caller is treated as IsOwner=true. This preserves
// the v0.4.2 behavior for non-handler entry points that have always
// run with the operator's identity. New non-owner senders MUST go
// through messaging/handler.go which always attaches an explicit
// Origin.
func OriginFromContext(ctx context.Context) Origin {
	if ctx == nil {
		return Origin{IsOwner: true, Reason: "default"}
	}
	v, ok := ctx.Value(originCtxKey{}).(Origin)
	if !ok {
		return Origin{IsOwner: true, Reason: "default"}
	}
	return v
}
