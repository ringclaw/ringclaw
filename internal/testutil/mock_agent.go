package testutil

import (
	"context"
	"fmt"
	"sync"

	"github.com/ringclaw/ringclaw/agent"
)

// MockAgent implements agent.Agent for testing.
type MockAgent struct {
	mu       sync.Mutex
	Chats    []MockChatCall
	Info_    agent.AgentInfo
	Sessions []string // conversation IDs that were reset
	Err      error    // return this error on Chat if set
}

// MockChatCall records a single Chat invocation.
type MockChatCall struct {
	ConversationID string
	Message        string
}

// NewMockAgent creates a mock agent that echoes messages.
func NewMockAgent(name string) *MockAgent {
	return &MockAgent{
		Info_: agent.AgentInfo{Name: name, Type: "mock"},
	}
}

// Chat records the call and returns the message prefixed with "echo: ".
func (m *MockAgent) Chat(_ context.Context, conversationID, message string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return "", m.Err
	}
	m.Chats = append(m.Chats, MockChatCall{ConversationID: conversationID, Message: message})
	return fmt.Sprintf("echo: %s", message), nil
}

// ResetSession records the reset and returns a fake session ID.
func (m *MockAgent) ResetSession(_ context.Context, conversationID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Sessions = append(m.Sessions, conversationID)
	return fmt.Sprintf("session-%d", len(m.Sessions)), nil
}

// SetCwd is a no-op for the mock.
func (m *MockAgent) SetCwd(_ string) {}

// Info returns the mock agent info.
func (m *MockAgent) Info() agent.AgentInfo {
	return m.Info_
}

// ChatCount returns the number of recorded Chat calls.
func (m *MockAgent) ChatCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Chats)
}

// LastChat returns the most recent Chat call, or nil if none.
func (m *MockAgent) LastChat() *MockChatCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Chats) == 0 {
		return nil
	}
	return &m.Chats[len(m.Chats)-1]
}

// Compile-time check.
var _ agent.Agent = (*MockAgent)(nil)
