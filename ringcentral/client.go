package ringcentral

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultServerURL = "https://platform.ringcentral.com"
	requestTimeout   = 30 * time.Second
)

// Client is a RingCentral Team Messaging REST API client.
type Client struct {
	serverURL  string
	auth       *Auth
	httpClient *http.Client
	ownerID    string
	monitor    *Monitor
	isBot      bool
	dmChatID   string
}

// IsBot returns true if this client uses a bot token.
func (c *Client) IsBot() bool {
	return c.isBot
}

// SetDMChatID sets the bot's direct message chat ID.
func (c *Client) SetDMChatID(id string) {
	c.dmChatID = id
}

// IsBotDM returns true if the given chatID is the bot's DM chat.
func (c *Client) IsBotDM(chatID string) bool {
	return c.isBot && c.dmChatID != "" && chatID == c.dmChatID
}

// SetMonitor links a monitor for tracking sent posts.
func (c *Client) SetMonitor(m *Monitor) {
	c.monitor = m
}

// markSentPost records a post ID as sent by the bot.
func (c *Client) markSentPost(id string) {
	if c.monitor != nil {
		c.monitor.MarkSentPost(id)
	}
}

// NewClient creates a new RingCentral API client.
func NewClient(creds *Credentials) *Client {
	serverURL := creds.ServerURL
	if serverURL == "" {
		serverURL = defaultServerURL
	}
	auth := NewAuth(creds.ClientID, creds.ClientSecret, creds.JWTToken, serverURL)
	return &Client{
		serverURL: serverURL,
		auth:      auth,
		httpClient: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// NewBotClient creates a Client that uses a static bot access token
// (obtained from the RC Developer Console when installing a private bot).
// Bot tokens are long-lived and don't require JWT refresh.
func NewBotClient(serverURL, botToken string) *Client {
	if serverURL == "" {
		serverURL = defaultServerURL
	}
	auth := NewAuth("", "", "", serverURL)
	// Set the bot token with a far-future expiry so AccessToken() never triggers refresh
	auth.SetTokenForTest(botToken, time.Now().Add(365*24*time.Hour))
	return &Client{
		serverURL: serverURL,
		auth:      auth,
		isBot:     true,
		httpClient: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// Authenticate performs JWT authentication. Must be called before other methods.
func (c *Client) Authenticate() error {
	return c.auth.Authenticate()
}

// Auth returns the auth manager (used by monitor for WS token).
func (c *Client) Auth() *Auth {
	return c.auth
}

// ServerURL returns the server base URL.
func (c *Client) ServerURL() string {
	return c.serverURL
}

// SetOwnerID sets the bot's own user ID (to filter self-messages).
func (c *Client) SetOwnerID(id string) {
	c.ownerID = id
}

// OwnerID returns the bot's own user ID.
func (c *Client) OwnerID() string {
	return c.ownerID
}

// SendPost creates a new post in a chat.
func (c *Client) SendPost(ctx context.Context, chatID, text string) (*Post, error) {
	reqBody := CreatePostRequest{Text: text}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal post: %w", err)
	}

	path := fmt.Sprintf("/team-messaging/v1/chats/%s/posts", chatID)
	respBody, err := c.doRequest(ctx, http.MethodPost, path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	var post Post
	if err := json.Unmarshal(respBody, &post); err != nil {
		return nil, fmt.Errorf("parse post response: %w", err)
	}
	c.markSentPost(post.ID)
	return &post, nil
}

// UpdatePost updates an existing post's text.
func (c *Client) UpdatePost(ctx context.Context, chatID, postID, text string) (*Post, error) {
	reqBody := UpdatePostRequest{Text: text}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal update: %w", err)
	}

	path := fmt.Sprintf("/team-messaging/v1/chats/%s/posts/%s", chatID, postID)
	respBody, err := c.doRequest(ctx, http.MethodPatch, path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	var post Post
	if err := json.Unmarshal(respBody, &post); err != nil {
		return nil, fmt.Errorf("parse update response: %w", err)
	}
	return &post, nil
}

// DeletePost deletes a post by ID.
func (c *Client) DeletePost(ctx context.Context, chatID, postID string) error {
	path := fmt.Sprintf("/team-messaging/v1/chats/%s/posts/%s", chatID, postID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, "", nil)
	return err
}

// UploadFile uploads a file to a chat.
func (c *Client) UploadFile(ctx context.Context, chatID, fileName string, fileData []byte) (*FileUploadResponse, error) {
	ct := inferContentType(fileName)

	params := url.Values{
		"name":    {fileName},
		"groupId": {chatID},
	}
	path := "/team-messaging/v1/files?" + params.Encode()

	respBody, err := c.doRequest(ctx, http.MethodPost, path, ct, bytes.NewReader(fileData))
	if err != nil {
		return nil, err
	}

	// Response is an array of file objects
	var files []FileUploadResponse
	if err := json.Unmarshal(respBody, &files); err != nil {
		return nil, fmt.Errorf("parse upload response: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("empty upload response")
	}
	return &files[0], nil
}

// ListPosts fetches posts from a chat.
// Note: RingCentral API only supports recordCount and pageToken, not time filters.
func (c *Client) ListPosts(ctx context.Context, chatID string, opts ListPostsOpts) (*PostList, error) {
	params := url.Values{}
	if opts.RecordCount > 0 {
		params.Set("recordCount", fmt.Sprintf("%d", opts.RecordCount))
	} else {
		params.Set("recordCount", "250")
	}
	if opts.PageToken != "" {
		params.Set("pageToken", opts.PageToken)
	}

	path := fmt.Sprintf("/team-messaging/v1/chats/%s/posts?%s", chatID, params.Encode())
	respBody, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}

	var list PostList
	if err := json.Unmarshal(respBody, &list); err != nil {
		return nil, fmt.Errorf("parse posts response: %w", err)
	}
	return &list, nil
}

// GetPersonInfo fetches a person's profile by ID.
func (c *Client) GetPersonInfo(ctx context.Context, personID string) (*PersonInfo, error) {
	path := fmt.Sprintf("/team-messaging/v1/persons/%s", personID)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}

	var info PersonInfo
	if err := json.Unmarshal(respBody, &info); err != nil {
		return nil, fmt.Errorf("parse person info: %w", err)
	}
	return &info, nil
}

// ListChats fetches chats with optional type filter.
func (c *Client) ListChats(ctx context.Context, chatType string) (*ChatList, error) {
	params := url.Values{"recordCount": {"250"}}
	if chatType != "" {
		params.Set("type", chatType)
	}

	path := "/team-messaging/v1/chats?" + params.Encode()
	respBody, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}

	var list ChatList
	if err := json.Unmarshal(respBody, &list); err != nil {
		return nil, fmt.Errorf("parse chats response: %w", err)
	}
	return &list, nil
}

// SearchDirectory searches the company directory by name or email.
func (c *Client) SearchDirectory(ctx context.Context, searchString string) (*DirectorySearchResult, error) {
	body := map[string]string{"searchString": searchString}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal search body: %w", err)
	}

	path := "/restapi/v1.0/account/~/directory/entries/search"
	respBody, err := c.doRequest(ctx, http.MethodPost, path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	var result DirectorySearchResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse directory search: %w", err)
	}
	return &result, nil
}

// CreateConversation creates or finds an existing Direct chat with the given members.
// If a conversation already exists with those members, it is returned (idempotent).
func (c *Client) CreateConversation(ctx context.Context, memberIDs []string) (*Chat, error) {
	members := make([]ChatMember, len(memberIDs))
	for i, id := range memberIDs {
		members[i] = ChatMember{ID: id}
	}
	body := CreateChatRequest{Members: members}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal create chat: %w", err)
	}

	respBody, err := c.doRequest(ctx, http.MethodPost, "/team-messaging/v1/conversations", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	var chat Chat
	if err := json.Unmarshal(respBody, &chat); err != nil {
		return nil, fmt.Errorf("parse create chat: %w", err)
	}
	return &chat, nil
}


// GetExtensionInfo fetches current user's extension info to get the owner ID.
func (c *Client) GetExtensionInfo(ctx context.Context) (string, error) {
	respBody, err := c.doRequest(ctx, http.MethodGet, "/restapi/v1.0/account/~/extension/~", "", nil)
	if err != nil {
		return "", err
	}

	var info struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(respBody, &info); err != nil {
		return "", fmt.Errorf("parse extension info: %w", err)
	}
	return fmt.Sprintf("%d", info.ID), nil
}

// FindDirectChat finds or creates a Direct (1:1) chat between the current
// user and the given person. Returns the chat ID.
func (c *Client) FindDirectChat(ctx context.Context, personID string) (string, error) {
	chat, err := c.CreateConversation(ctx, []string{personID})
	if err != nil {
		return "", fmt.Errorf("find direct chat: %w", err)
	}
	return chat.ID, nil
}

// --- Task CRUD ---

func (c *Client) ListTasks(ctx context.Context, chatID string) (*TaskList, error) {
	path := fmt.Sprintf("/team-messaging/v1/chats/%s/tasks?recordCount=50", chatID)
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
	path := fmt.Sprintf("/team-messaging/v1/chats/%s/notes?recordCount=50", chatID)
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
	resp, err := c.doRequest(ctx, http.MethodGet, "/team-messaging/v1/events?recordCount=50", "", nil)
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

func (c *Client) doRequest(ctx context.Context, method, path, contentType string, body io.Reader) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	token, err := c.auth.AccessToken()
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	reqURL := c.serverURL + path
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func inferContentType(fileName string) string {
	ext := filepath.Ext(fileName)
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	lower := strings.ToLower(ext)
	switch lower {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".mp4":
		return "video/mp4"
	case ".pdf":
		return "application/pdf"
	}
	return "application/octet-stream"
}
