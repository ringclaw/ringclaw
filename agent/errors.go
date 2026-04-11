package agent

import (
	"errors"
	"fmt"
)

// AgentErrorCode is a stable, machine-readable error code.
type AgentErrorCode string

const (
	ErrCodeTimeout AgentErrorCode = "AGENT_TIMEOUT"
	ErrCodeCrash   AgentErrorCode = "AGENT_CRASH"
	ErrCodeEmpty   AgentErrorCode = "AGENT_EMPTY"
)

// AgentError is a structured error with a stable code and retryable flag.
type AgentError struct {
	Code      AgentErrorCode
	Message   string
	Retryable bool
	Cause     error
}

func (e *AgentError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AgentError) Unwrap() error { return e.Cause }

// Timeout creates a retryable timeout error.
func Timeout(cause error) *AgentError {
	return &AgentError{
		Code:      ErrCodeTimeout,
		Message:   "agent timed out",
		Retryable: true,
		Cause:     cause,
	}
}

// Crash creates a non-retryable crash error.
func Crash(cause error) *AgentError {
	return &AgentError{
		Code:      ErrCodeCrash,
		Message:   "agent encountered an error",
		Retryable: false,
		Cause:     cause,
	}
}

// Empty creates a retryable empty-response error.
func Empty() *AgentError {
	return &AgentError{
		Code:      ErrCodeEmpty,
		Message:   "agent returned an empty response",
		Retryable: true,
	}
}

// IsRetryable reports whether the error is retryable.
// Returns false for non-AgentError errors.
func IsRetryable(err error) bool {
	var ae *AgentError
	if errors.As(err, &ae) {
		return ae.Retryable
	}
	return false
}

// UserMessage returns a user-friendly message suitable for chat replies.
// For non-AgentError errors, falls back to the raw error string.
func UserMessage(err error) string {
	var ae *AgentError
	if errors.As(err, &ae) {
		if ae.Retryable {
			return ae.Message + ". Please try again."
		}
		return ae.Message
	}
	return fmt.Sprintf("Error: %v", err)
}
