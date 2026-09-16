package agent

import "context"

// progressCtxKey is the context key for the optional progress callback.
type progressCtxKey struct{}

// ProgressFunc receives short, human-readable descriptions of what an agent
// is currently doing (e.g. "bash · git status"). It must be cheap and
// non-blocking; RingClaw throttles how often the description is surfaced.
type ProgressFunc func(text string)

// WithProgress attaches a progress callback to ctx. Agents that support
// progress reporting (currently ACP) invoke it as they work. Agents that do
// not support it simply ignore the callback, so callers can attach it
// unconditionally.
func WithProgress(ctx context.Context, fn ProgressFunc) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, progressCtxKey{}, fn)
}

// reportProgress invokes the ctx progress callback, if any.
func reportProgress(ctx context.Context, text string) {
	if text == "" {
		return
	}
	if fn, ok := ctx.Value(progressCtxKey{}).(ProgressFunc); ok && fn != nil {
		fn(text)
	}
}
