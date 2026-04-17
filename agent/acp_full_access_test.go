package agent

import (
	"testing"
)

func TestNewACPAgent_FullAccessDowngradedWithoutAck(t *testing.T) {
	ResetFullAccessAck()
	t.Cleanup(ResetFullAccessAck)

	a := NewACPAgent(ACPAgentConfig{
		Command:    "echo",
		Cwd:        t.TempDir(),
		FullAccess: true,
	})
	if a.fullAccess {
		t.Errorf("expected fullAccess to be downgraded to false when full_access_ack is not set in config")
	}
}

func TestNewACPAgent_FullAccessHonoredWithConfigAck(t *testing.T) {
	SetFullAccessAck(true)
	t.Cleanup(ResetFullAccessAck)

	a := NewACPAgent(ACPAgentConfig{
		Command:    "echo",
		Cwd:        t.TempDir(),
		FullAccess: true,
	})
	if !a.fullAccess {
		t.Errorf("expected fullAccess to be honored when full_access_ack=true")
	}
}

func TestNewACPAgent_FullAccessOffStaysOff(t *testing.T) {
	SetFullAccessAck(true) // even with ack, off stays off
	t.Cleanup(ResetFullAccessAck)

	a := NewACPAgent(ACPAgentConfig{
		Command:    "echo",
		Cwd:        t.TempDir(),
		FullAccess: false,
	})
	if a.fullAccess {
		t.Errorf("expected fullAccess to remain false when not requested")
	}
}

// TestNewACPAgent_FullAccessConfigOnly pins the post-migration behavior:
// config.json (via SetFullAccessAck) is the SOLE source for the
// acknowledgement. The former RINGCLAW_FULL_ACCESS_ACK env var is
// silently ignored.
func TestNewACPAgent_FullAccessConfigOnly(t *testing.T) {
	t.Cleanup(ResetFullAccessAck)

	t.Run("config_true_enables", func(t *testing.T) {
		SetFullAccessAck(true)
		t.Cleanup(ResetFullAccessAck)

		a := NewACPAgent(ACPAgentConfig{
			Command:    "echo",
			Cwd:        t.TempDir(),
			FullAccess: true,
		})
		if !a.fullAccess {
			t.Error("expected fullAccess honored when config ack=true")
		}
	})

	t.Run("config_false_disables", func(t *testing.T) {
		SetFullAccessAck(false)
		t.Cleanup(ResetFullAccessAck)

		a := NewACPAgent(ACPAgentConfig{
			Command:    "echo",
			Cwd:        t.TempDir(),
			FullAccess: true,
		})
		if a.fullAccess {
			t.Error("expected fullAccess refused when config ack=false")
		}
	})

	t.Run("env_is_ignored", func(t *testing.T) {
		t.Setenv("RINGCLAW_FULL_ACCESS_ACK", "1")
		ResetFullAccessAck()
		t.Cleanup(ResetFullAccessAck)

		a := NewACPAgent(ACPAgentConfig{
			Command:    "echo",
			Cwd:        t.TempDir(),
			FullAccess: true,
		})
		if a.fullAccess {
			t.Error("expected fullAccess refused: RINGCLAW_FULL_ACCESS_ACK env must be ignored")
		}
	})
}
