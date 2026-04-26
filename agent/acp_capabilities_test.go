package agent

import (
	"encoding/json"
	"testing"
)

func TestACPAgent_SupportsMedia_FactoryDroidShape(t *testing.T) {
	// This is the exact shape of the initialize result returned by
	// @factory/cli 0.109.1 — image is advertised but audio is not.
	// Captured from a live ACP handshake on 2026-04-26.
	raw := json.RawMessage(`{
		"protocolVersion": 1,
		"agentCapabilities": {
			"loadSession": true,
			"sessionCapabilities": {"list": {}, "resume": {}},
			"promptCapabilities": {"image": true, "embeddedContext": true},
			"_meta": {"terminal_output": true, "terminal-auth": true}
		},
		"agentInfo": {"name": "@factory/cli", "title": "Factory Droid", "version": "0.109.1"},
		"authMethods": []
	}`)

	a := &ACPAgent{}
	a.parseInitializeResult(raw)

	if !a.SupportsMedia(MediaKindImage) {
		t.Errorf("expected image to be supported (advertised true)")
	}
	if a.SupportsMedia(MediaKindAudio) {
		t.Errorf("expected audio to be unsupported (not advertised)")
	}
}

func TestACPAgent_SupportsMedia_BothAdvertised(t *testing.T) {
	raw := json.RawMessage(`{
		"agentCapabilities": {
			"promptCapabilities": {"image": true, "audio": true}
		}
	}`)

	a := &ACPAgent{}
	a.parseInitializeResult(raw)

	if !a.SupportsMedia(MediaKindImage) {
		t.Errorf("expected image supported")
	}
	if !a.SupportsMedia(MediaKindAudio) {
		t.Errorf("expected audio supported")
	}
}

func TestACPAgent_SupportsMedia_PresentButFalse(t *testing.T) {
	// agent advertises promptCapabilities but explicitly opts out
	raw := json.RawMessage(`{
		"agentCapabilities": {
			"promptCapabilities": {"image": false, "audio": false}
		}
	}`)

	a := &ACPAgent{}
	a.parseInitializeResult(raw)

	if a.SupportsMedia(MediaKindImage) {
		t.Errorf("expected image unsupported when advertised false")
	}
	if a.SupportsMedia(MediaKindAudio) {
		t.Errorf("expected audio unsupported when advertised false")
	}
}

func TestACPAgent_SupportsMedia_NotAdvertised(t *testing.T) {
	// Older ACP servers may not include promptCapabilities at all.
	// We must keep the legacy behavior of "assume supported" so we
	// don't regress agents that previously accepted images.
	raw := json.RawMessage(`{
		"agentCapabilities": {
			"loadSession": true
		}
	}`)

	a := &ACPAgent{}
	a.parseInitializeResult(raw)

	if !a.SupportsMedia(MediaKindImage) {
		t.Errorf("expected image supported when promptCapabilities absent")
	}
	if !a.SupportsMedia(MediaKindAudio) {
		t.Errorf("expected audio supported when promptCapabilities absent")
	}
}

func TestACPAgent_SupportsMedia_NoInitializeYet(t *testing.T) {
	// An agent on which parseInitializeResult was never called (e.g.
	// a startup error before the handshake completed) should also
	// fall back to "assume supported" rather than blocking media.
	a := &ACPAgent{}

	if !a.SupportsMedia(MediaKindImage) {
		t.Errorf("expected image supported pre-init")
	}
	if !a.SupportsMedia(MediaKindAudio) {
		t.Errorf("expected audio supported pre-init")
	}
}

func TestACPAgent_SupportsMedia_UnknownKind(t *testing.T) {
	// SupportsMedia should reject unknown media kinds when the agent
	// did advertise capabilities — only the well-known kinds are
	// considered supported. (When capabilities aren't advertised at
	// all, the legacy fallback returns true.)
	raw := json.RawMessage(`{
		"agentCapabilities": {
			"promptCapabilities": {"image": true, "audio": true}
		}
	}`)

	a := &ACPAgent{}
	a.parseInitializeResult(raw)

	if a.SupportsMedia("video") {
		t.Errorf("expected unknown kind 'video' to report unsupported")
	}
}

func TestACPAgent_SupportsMedia_MalformedJSON(t *testing.T) {
	// A malformed initialize result must not crash; we just leave
	// the capabilities unadvertised, which means SupportsMedia
	// degrades to "assume supported".
	a := &ACPAgent{}
	a.parseInitializeResult(json.RawMessage(`not json at all`))

	if !a.SupportsMedia(MediaKindImage) {
		t.Errorf("expected image supported when init parse fails")
	}
}
