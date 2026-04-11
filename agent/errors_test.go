package agent

import (
	"errors"
	"fmt"
	"testing"
)

func TestAgentError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *AgentError
		want string
	}{
		{"timeout with cause", Timeout(fmt.Errorf("context deadline exceeded")), "AGENT_TIMEOUT: agent timed out: context deadline exceeded"},
		{"crash with cause", Crash(fmt.Errorf("process exited")), "AGENT_CRASH: agent encountered an error: process exited"},
		{"empty no cause", Empty(), "AGENT_EMPTY: agent returned an empty response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAgentError_Unwrap(t *testing.T) {
	cause := fmt.Errorf("root cause")
	ae := Timeout(cause)
	if !errors.Is(ae, cause) {
		t.Error("Unwrap should allow errors.Is to find cause")
	}

	empty := Empty()
	if empty.Unwrap() != nil {
		t.Error("Empty().Unwrap() should be nil")
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"timeout", Timeout(nil), true},
		{"crash", Crash(nil), false},
		{"empty", Empty(), true},
		{"plain error", fmt.Errorf("something"), false},
		{"nil", nil, false},
		{"wrapped timeout", fmt.Errorf("wrap: %w", Timeout(nil)), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUserMessage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"timeout", Timeout(nil), "agent timed out. Please try again."},
		{"crash", Crash(nil), "agent encountered an error"},
		{"empty", Empty(), "agent returned an empty response. Please try again."},
		{"plain error", fmt.Errorf("something broke"), "Error: something broke"},
		{"wrapped crash", fmt.Errorf("wrap: %w", Crash(nil)), "agent encountered an error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UserMessage(tt.err); got != tt.want {
				t.Errorf("UserMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAgentError_Codes(t *testing.T) {
	if Timeout(nil).Code != ErrCodeTimeout {
		t.Error("Timeout code mismatch")
	}
	if Crash(nil).Code != ErrCodeCrash {
		t.Error("Crash code mismatch")
	}
	if Empty().Code != ErrCodeEmpty {
		t.Error("Empty code mismatch")
	}
}
