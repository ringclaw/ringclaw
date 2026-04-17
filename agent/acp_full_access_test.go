package agent

import (
	"testing"
)

func TestNewACPAgent_FullAccessDowngradedWithoutAck(t *testing.T) {
	t.Setenv(fullAccessAckEnv, "")

	a := NewACPAgent(ACPAgentConfig{
		Command:    "echo",
		Cwd:        t.TempDir(),
		FullAccess: true,
	})
	if a.fullAccess {
		t.Errorf("expected fullAccess to be downgraded to false when %s is unset", fullAccessAckEnv)
	}
}

func TestNewACPAgent_FullAccessHonoredWithAck(t *testing.T) {
	t.Setenv(fullAccessAckEnv, "1")

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

	a := NewACPAgent(ACPAgentConfig{
		Command:    "echo",
		Cwd:        t.TempDir(),
		FullAccess: false,
	})
	if a.fullAccess {
		t.Errorf("expected fullAccess to remain false when not requested")
	}
}
