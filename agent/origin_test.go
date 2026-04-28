package agent

import (
	"context"
	"testing"
)

func TestWithOrigin_RoundTrip(t *testing.T) {
	ctx := WithOrigin(context.Background(), Origin{IsOwner: false, SenderID: "u1", Reason: "test"})
	got := OriginFromContext(ctx)
	if got.IsOwner || got.SenderID != "u1" || got.Reason != "test" {
		t.Errorf("unexpected origin: %+v", got)
	}
}

func TestOriginFromContext_DefaultsToOwner(t *testing.T) {
	got := OriginFromContext(context.Background())
	if !got.IsOwner {
		t.Error("default Origin must be owner-equivalent for backwards compatibility")
	}
	if got.Reason != "default" {
		t.Errorf("expected reason=default, got %q", got.Reason)
	}
}

func TestOriginFromContext_NilCtx(t *testing.T) {
	got := OriginFromContext(nil)
	if !got.IsOwner {
		t.Error("nil ctx must return owner default")
	}
}

func TestWithOrigin_NilCtxIsBackground(t *testing.T) {
	ctx := WithOrigin(nil, Origin{IsOwner: false})
	if ctx == nil {
		t.Fatal("WithOrigin must not return nil")
	}
	got := OriginFromContext(ctx)
	if got.IsOwner {
		t.Error("expected non-owner from explicit Origin")
	}
}
