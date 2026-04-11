// Package testutil provides test doubles for RingClaw packages.
//
// This package is only intended for use in tests. Import it only from _test.go
// files or test-only packages.
package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ringclaw/ringclaw/ringcentral"
)

// MockSender implements ringcentral.MessageSender for testing.
// All method calls are recorded for assertion.
type MockSender struct {
	mu sync.Mutex

	Posts    []*ringcentral.Post
	Updated  []*ringcentral.Post
	Deleted  []string // post IDs
	Uploads  []MockUpload
	Cards    []MockCard
	Errors   map[string]error // method name -> error to return
}

// MockUpload records a file upload call.
type MockUpload struct {
	ChatID   string
	FileName string
	Data     []byte
}

// MockCard records an adaptive card creation call.
type MockCard struct {
	ChatID string
	Card   json.RawMessage
}

// NewMockSender creates a ready-to-use MockSender.
func NewMockSender() *MockSender {
	return &MockSender{
		Errors: make(map[string]error),
	}
}

// SendPost records a post creation.
func (m *MockSender) SendPost(_ context.Context, chatID, text string) (*ringcentral.Post, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.Errors["SendPost"]; ok {
		return nil, err
	}
	post := &ringcentral.Post{ID: fmt.Sprintf("post-%d", len(m.Posts)+1), GroupID: chatID, Text: text}
	m.Posts = append(m.Posts, post)
	return post, nil
}

// UpdatePost records a post update.
func (m *MockSender) UpdatePost(_ context.Context, chatID, postID, text string) (*ringcentral.Post, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.Errors["UpdatePost"]; ok {
		return nil, err
	}
	post := &ringcentral.Post{ID: postID, GroupID: chatID, Text: text}
	m.Updated = append(m.Updated, post)
	return post, nil
}

// DeletePost records a post deletion.
func (m *MockSender) DeletePost(_ context.Context, _, postID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.Errors["DeletePost"]; ok {
		return err
	}
	m.Deleted = append(m.Deleted, postID)
	return nil
}

// UploadFile records a file upload.
func (m *MockSender) UploadFile(_ context.Context, chatID, fileName string, data []byte) (*ringcentral.FileUploadResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.Errors["UploadFile"]; ok {
		return nil, err
	}
	m.Uploads = append(m.Uploads, MockUpload{ChatID: chatID, FileName: fileName, Data: data})
	return &ringcentral.FileUploadResponse{ID: fmt.Sprintf("file-%d", len(m.Uploads))}, nil
}

// DownloadAttachment returns an error by default (override via Errors map).
func (m *MockSender) DownloadAttachment(_ context.Context, _ string) ([]byte, string, error) {
	if err, ok := m.Errors["DownloadAttachment"]; ok {
		return nil, "", err
	}
	return nil, "", fmt.Errorf("DownloadAttachment not mocked")
}

// CreateAdaptiveCard records a card creation.
func (m *MockSender) CreateAdaptiveCard(_ context.Context, chatID string, card json.RawMessage) (*ringcentral.AdaptiveCard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.Errors["CreateAdaptiveCard"]; ok {
		return nil, err
	}
	m.Cards = append(m.Cards, MockCard{ChatID: chatID, Card: card})
	return &ringcentral.AdaptiveCard{ID: fmt.Sprintf("card-%d", len(m.Cards))}, nil
}

// PostTexts returns the text of all sent posts.
func (m *MockSender) PostTexts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	texts := make([]string, len(m.Posts))
	for i, p := range m.Posts {
		texts[i] = p.Text
	}
	return texts
}
