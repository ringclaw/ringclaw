package agent

import (
	"testing"
)

func TestNewACPAgent_FullAccessDowngradedWithoutAck(t *testing.T) {
	t.Setenv(fullAccessAckEnv, "")
	ResetFullAccessAck()
	t.Cleanup(ResetFullAccessAck)

	a := NewACPAgent(ACPAgentConfig{
		Command:    "echo",
		Cwd:        t.TempDir(),
		FullAccess: true,
	})
	if a.fullAccess {
		t.Errorf("expected fullAccess to be downgraded to false when neither config nor %s is set", fullAccessAckEnv)
	}
}

func TestNewACPAgent_FullAccessHonoredWithAckEnv(t *testing.T) {
	t.Setenv(fullAccessAckEnv, "1")
	ResetFullAccessAck()
	t.Cleanup(ResetFullAccessAck)

	a := NewACPAgent(ACPAgentConfig{
		Command:    "echo",
		Cwd:        t.TempDir(),
		FullAccess: true,
	})
	if !a.fullAccess {
		t.Errorf("expected fullAccess to be honored when %s=1", fullAccessAckEnv)
	}
}

func TestNewACPAgent_FullAccessOffStaysOff(t *testing.T) {
	t.Setenv(fullAccessAckEnv, "1") // even with ack, off stays off
	ResetFullAccessAck()
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

// TestNewACPAgent_FullAccessConfigWinsOverEnv locks in the documented
// precedence: an explicit config acknowledgement (set via
// SetFullAccessAck) WINS over the RINGCLAW_FULL_ACCESS_ACK env var.
func TestNewACPAgent_FullAccessConfigWinsOverEnv(t *testing.T) {
	t.Cleanup(ResetFullAccessAck)

	t.Run("config_true_overrides_env_unset", func(t *testing.T) {
		t.Setenv(fullAccessAckEnv, "")
		SetFullAccessAck(true)
		t.Cleanup(ResetFullAccessAck)

		a := NewACPAgent(ACPAgentConfig{
			Command:    "echo",
			Cwd:        t.TempDir(),
			FullAccess: true,
		})
		if !a.fullAccess {
			t.Error("expected fullAccess honored when config ack=true even with env unset")
		}
	})

	t.Run("config_false_overrides_env_set", func(t *testing.T) {
		t.Setenv(fullAccessAckEnv, "1")
		SetFullAccessAck(false)
		t.Cleanup(ResetFullAccessAck)

		a := NewACPAgent(ACPAgentConfig{
			Command:    "echo",
			Cwd:        t.TempDir(),
			FullAccess: true,
		})
		if a.fullAccess {
			t.Error("expected fullAccess refused when config ack=false even with env=1")
		}
	})

	t.Run("config_unset_falls_back_to_env", func(t *testing.T) {
		t.Setenv(fullAccessAckEnv, "1")
		ResetFullAccessAck()
		t.Cleanup(ResetFullAccessAck)

		a := NewACPAgent(ACPAgentConfig{
			Command:    "echo",
			Cwd:        t.TempDir(),
			FullAccess: true,
		})
		if !a.fullAccess {
			t.Error("expected fullAccess honored when config unset and env=1")
		}
	})
}
