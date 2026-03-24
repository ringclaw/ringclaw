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
	chatID     string
	auth       *Auth
	httpClient *http.Client
	ownerID    string
	monitor    *Monitor
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
		serverURL:  serverURL,
		chatID:     creds.ChatID,
		auth:       auth,
		httpClient: &http.Client{},
	}
}

// Authenticate performs JWT authentication. Must be called before other methods.
func (c *Client) Authenticate() error {
	return c.auth.Authenticate()
}

// ChatID returns the configured chat ID.
func (c *Client) ChatID() string {
	return c.chatID
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

// FindDirectChatByMember searches Direct chats for one containing the given member ID.
func (c *Client) FindDirectChatByMember(ctx context.Context, memberID string) (*Chat, error) {
	chats, err := c.ListChats(ctx, "Direct")
	if err != nil {
		return nil, err
	}
	for _, chat := range chats.Records {
		for _, m := range chat.Members {
			if m.ID == memberID {
				return &chat, nil
			}
		}
	}
	return nil, fmt.Errorf("no Direct chat found with member %s", memberID)
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
