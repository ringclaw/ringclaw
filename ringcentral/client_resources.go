package ringcentral

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

func (c *Client) ListTasks(ctx context.Context, chatID string) (*TaskList, error) {
	path := fmt.Sprintf("/team-messaging/v1/chats/%s/tasks?recordCount=250", chatID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	var list TaskList
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse task list: %w", err)
	}
	return &list, nil
}

func (c *Client) CreateTask(ctx context.Context, chatID string, req *CreateTaskRequest) (*Task, error) {
	data, _ := json.Marshal(req)
	path := fmt.Sprintf("/team-messaging/v1/chats/%s/tasks", chatID)
	resp, err := c.doRequest(ctx, http.MethodPost, path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var task Task
	if err := json.Unmarshal(resp, &task); err != nil {
		return nil, fmt.Errorf("parse task: %w", err)
	}
	return &task, nil
}

func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	path := fmt.Sprintf("/team-messaging/v1/tasks/%s", taskID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	var task Task
	if err := json.Unmarshal(resp, &task); err != nil {
		return nil, fmt.Errorf("parse task: %w", err)
	}
	return &task, nil
}

func (c *Client) UpdateTask(ctx context.Context, taskID string, req *UpdateTaskRequest) (*Task, error) {
	data, _ := json.Marshal(req)
	path := fmt.Sprintf("/team-messaging/v1/tasks/%s", taskID)
	resp, err := c.doRequest(ctx, http.MethodPatch, path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var task Task
	if err := json.Unmarshal(resp, &task); err != nil {
		return nil, fmt.Errorf("parse task: %w", err)
	}
	return &task, nil
}

func (c *Client) DeleteTask(ctx context.Context, taskID string) error {
	path := fmt.Sprintf("/team-messaging/v1/tasks/%s", taskID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, "", nil)
	return err
}

func (c *Client) CompleteTask(ctx context.Context, taskID string) error {
	path := fmt.Sprintf("/team-messaging/v1/tasks/%s/complete", taskID)
	body := map[string]string{"status": "Completed"}
	data, _ := json.Marshal(body)
	_, err := c.doRequest(ctx, http.MethodPost, path, "application/json", bytes.NewReader(data))
	return err
}

// --- Note CRUD ---

func (c *Client) ListNotes(ctx context.Context, chatID string) (*NoteList, error) {
	path := fmt.Sprintf("/team-messaging/v1/chats/%s/notes?recordCount=250", chatID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	var list NoteList
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse note list: %w", err)
	}
	return &list, nil
}

func (c *Client) CreateNote(ctx context.Context, chatID string, req *CreateNoteRequest) (*Note, error) {
	data, _ := json.Marshal(req)
	path := fmt.Sprintf("/team-messaging/v1/chats/%s/notes", chatID)
	resp, err := c.doRequest(ctx, http.MethodPost, path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var note Note
	if err := json.Unmarshal(resp, &note); err != nil {
		return nil, fmt.Errorf("parse note: %w", err)
	}
	return &note, nil
}

func (c *Client) GetNote(ctx context.Context, noteID string) (*Note, error) {
	path := fmt.Sprintf("/team-messaging/v1/notes/%s", noteID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	var note Note
	if err := json.Unmarshal(resp, &note); err != nil {
		return nil, fmt.Errorf("parse note: %w", err)
	}
	return &note, nil
}

func (c *Client) UpdateNote(ctx context.Context, noteID string, req *UpdateNoteRequest) (*Note, error) {
	data, _ := json.Marshal(req)
	path := fmt.Sprintf("/team-messaging/v1/notes/%s", noteID)
	resp, err := c.doRequest(ctx, http.MethodPatch, path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var note Note
	if err := json.Unmarshal(resp, &note); err != nil {
		return nil, fmt.Errorf("parse note: %w", err)
	}
	return &note, nil
}

func (c *Client) DeleteNote(ctx context.Context, noteID string) error {
	path := fmt.Sprintf("/team-messaging/v1/notes/%s", noteID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, "", nil)
	return err
}

func (c *Client) PublishNote(ctx context.Context, noteID string) error {
	path := fmt.Sprintf("/team-messaging/v1/notes/%s/publish", noteID)
	_, err := c.doRequest(ctx, http.MethodPost, path, "", nil)
	return err
}

// --- Event CRUD ---

func (c *Client) ListEvents(ctx context.Context) (*EventList, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/team-messaging/v1/events?recordCount=250", "", nil)
	if err != nil {
		return nil, err
	}
	var list EventList
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse event list: %w", err)
	}
	return &list, nil
}

func (c *Client) CreateEvent(ctx context.Context, req *CreateEventRequest) (*Event, error) {
	data, _ := json.Marshal(req)
	resp, err := c.doRequest(ctx, http.MethodPost, "/team-messaging/v1/events", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var event Event
	if err := json.Unmarshal(resp, &event); err != nil {
		return nil, fmt.Errorf("parse event: %w", err)
	}
	return &event, nil
}

func (c *Client) GetEvent(ctx context.Context, eventID string) (*Event, error) {
	path := fmt.Sprintf("/team-messaging/v1/events/%s", eventID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	var event Event
	if err := json.Unmarshal(resp, &event); err != nil {
		return nil, fmt.Errorf("parse event: %w", err)
	}
	return &event, nil
}

func (c *Client) UpdateEvent(ctx context.Context, eventID string, req *UpdateEventRequest) (*Event, error) {
	data, _ := json.Marshal(req)
	path := fmt.Sprintf("/team-messaging/v1/events/%s", eventID)
	resp, err := c.doRequest(ctx, http.MethodPut, path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var event Event
	if err := json.Unmarshal(resp, &event); err != nil {
		return nil, fmt.Errorf("parse event: %w", err)
	}
	return &event, nil
}

func (c *Client) DeleteEvent(ctx context.Context, eventID string) error {
	path := fmt.Sprintf("/team-messaging/v1/events/%s", eventID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, "", nil)
	return err
}

// --- Adaptive Card CRUD ---

func (c *Client) CreateAdaptiveCard(ctx context.Context, chatID string, card json.RawMessage) (*AdaptiveCard, error) {
	path := fmt.Sprintf("/team-messaging/v1/chats/%s/adaptive-cards", chatID)
	resp, err := c.doRequest(ctx, http.MethodPost, path, "application/json", bytes.NewReader(card))
	if err != nil {
		return nil, err
	}
	var ac AdaptiveCard
	if err := json.Unmarshal(resp, &ac); err != nil {
		return nil, fmt.Errorf("parse adaptive card: %w", err)
	}
	return &ac, nil
}

func (c *Client) GetAdaptiveCard(ctx context.Context, cardID string) (*AdaptiveCard, error) {
	path := fmt.Sprintf("/team-messaging/v1/adaptive-cards/%s", cardID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	var ac AdaptiveCard
	if err := json.Unmarshal(resp, &ac); err != nil {
		return nil, fmt.Errorf("parse adaptive card: %w", err)
	}
	return &ac, nil
}

func (c *Client) UpdateAdaptiveCard(ctx context.Context, cardID string, card json.RawMessage) (*AdaptiveCard, error) {
	path := fmt.Sprintf("/team-messaging/v1/adaptive-cards/%s", cardID)
	resp, err := c.doRequest(ctx, http.MethodPut, path, "application/json", bytes.NewReader(card))
	if err != nil {
		return nil, err
	}
	var ac AdaptiveCard
	if err := json.Unmarshal(resp, &ac); err != nil {
		return nil, fmt.Errorf("parse adaptive card: %w", err)
	}
	return &ac, nil
}

func (c *Client) DeleteAdaptiveCard(ctx context.Context, cardID string) error {
	path := fmt.Sprintf("/team-messaging/v1/adaptive-cards/%s", cardID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, "", nil)
	return err
}

// --- Additional API methods ---

// GetChat returns information about a specific chat.
func (c *Client) GetChat(ctx context.Context, chatID string) (*Chat, error) {
	path := fmt.Sprintf("/team-messaging/v1/chats/%s", chatID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	var chat Chat
	if err := json.Unmarshal(resp, &chat); err != nil {
		return nil, fmt.Errorf("parse chat: %w", err)
	}
	return &chat, nil
}

// GetPost returns a specific post by ID.
func (c *Client) GetPost(ctx context.Context, chatID, postID string) (*Post, error) {
	path := fmt.Sprintf("/team-messaging/v1/chats/%s/posts/%s", chatID, postID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	var post Post
	if err := json.Unmarshal(resp, &post); err != nil {
		return nil, fmt.Errorf("parse post: %w", err)
	}
	return &post, nil
}

// LockNote locks a note for exclusive editing (up to 5 hours).
func (c *Client) LockNote(ctx context.Context, noteID string) error {
	path := fmt.Sprintf("/team-messaging/v1/notes/%s/lock", noteID)
	_, err := c.doRequest(ctx, http.MethodPost, path, "", nil)
	return err
}

// UnlockNote releases a lock on a note.
func (c *Client) UnlockNote(ctx context.Context, noteID string) error {
	path := fmt.Sprintf("/team-messaging/v1/notes/%s/unlock", noteID)
	_, err := c.doRequest(ctx, http.MethodPost, path, "", nil)
	return err
}

// GetPresence returns the presence status of an extension.
// Requires ReadPresence permission (Private App only).
func (c *Client) GetPresence(ctx context.Context, extensionID string) (*PresenceInfo, error) {
	path := fmt.Sprintf("/restapi/v1.0/account/~/extension/%s/presence", extensionID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	var info PresenceInfo
	if err := json.Unmarshal(resp, &info); err != nil {
		return nil, fmt.Errorf("parse presence: %w", err)
	}
	return &info, nil
}

// ListRecentChats returns chats sorted by lastModifiedTime (most recently active first).
func (c *Client) ListRecentChats(ctx context.Context, chatType string, recordCount int) (*ChatList, error) {
	params := url.Values{}
	if chatType != "" {
		params.Set("type", chatType)
	}
	if recordCount > 0 {
		params.Set("recordCount", fmt.Sprintf("%d", recordCount))
	}
	path := "/team-messaging/v1/recent/chats"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	var list ChatList
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse recent chats: %w", err)
	}
	return &list, nil
}

// ListGroupEvents returns calendar events for a specific chat/group.
func (c *Client) ListGroupEvents(ctx context.Context, groupID string) (*EventList, error) {
	path := fmt.Sprintf("/team-messaging/v1/groups/%s/events?recordCount=250", groupID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	var list EventList
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse group events: %w", err)
	}
	return &list, nil
}

