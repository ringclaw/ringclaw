package messaging

import (
	"context"
	"log/slog"
	"sync"
)

// ActionEvent is the sanitized action outcome emitted by the runtime.
// It intentionally carries metadata only; action bodies, tokens, phone
// numbers, and other sensitive payloads must stay out of this struct.
type ActionEvent struct {
	Type    string
	Status  string
	Details map[string]any
}

// ActionEventRecorder receives action outcomes for an external control plane.
// Recorder failures must be handled by the recorder implementation so action
// execution itself is not coupled to telemetry availability.
type ActionEventRecorder func(context.Context, ActionEvent)

var actionEventRecorderState struct {
	mu       sync.RWMutex
	recorder ActionEventRecorder
}

// SetActionEventRecorder installs a process-wide recorder and returns a restore
// function. It is used by managed runtime mode; local ringclaw start leaves it
// unset and continues without control-plane action event reporting.
func SetActionEventRecorder(recorder ActionEventRecorder) func() {
	actionEventRecorderState.mu.Lock()
	previous := actionEventRecorderState.recorder
	actionEventRecorderState.recorder = recorder
	actionEventRecorderState.mu.Unlock()
	return func() {
		actionEventRecorderState.mu.Lock()
		actionEventRecorderState.recorder = previous
		actionEventRecorderState.mu.Unlock()
	}
}

func recordAgentActionEvent(ctx context.Context, event ActionEvent) {
	actionEventRecorderState.mu.RLock()
	recorder := actionEventRecorderState.recorder
	actionEventRecorderState.mu.RUnlock()
	if recorder == nil {
		return
	}
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Warn("action event recorder panicked", "component", "action-event", "error", recovered)
			}
		}()
		recorder(ctx, event)
	}()
}

func actionEventDetails(originChat, targetChat string, crossChat bool, extra map[string]any) map[string]any {
	details := map[string]any{
		"origin_chat": originChat,
	}
	if targetChat != "" {
		details["target_chat"] = targetChat
	}
	if crossChat {
		details["cross_chat"] = true
	}
	for key, value := range extra {
		if value != nil && value != "" {
			details[key] = value
		}
	}
	return details
}
