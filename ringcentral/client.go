package ringcentral

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultServerURL = "https://platform.ringcentral.com"
	requestTimeout   = 30 * time.Second
)

// Client is a RingCentral Team Messaging REST API client.
type Client struct {
	serverURL      string
	videoServerURL string
	auth           *Auth
	httpClient     *http.Client
	ownerID        string
	monitor        *Monitor
	isBot          bool
	dmChatID       string
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

// DMChatID returns the bot's DM chat ID with the trusted owner (set
// via SetDMChatID at startup). Empty when the client is not a bot or
// the DM chat has not been resolved yet. Used by the OOB approval flow
// to choose where to deliver challenge cards.
func (c *Client) DMChatID() string {
	if !c.isBot {
		return ""
	}
	return c.dmChatID
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

// newHTTPClient creates a shared HTTP client with connection pooling.
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        20,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

// NewClient creates a new RingCentral API client.
func NewClient(creds *Credentials) *Client {
	serverURL := creds.ServerURL
	if serverURL == "" {
		serverURL = defaultServerURL
	}
	videoServerURL := creds.VideoServerURL
	if videoServerURL == "" {
		videoServerURL = defaultVideoServerURL(serverURL)
	}
	auth := NewAuth(creds.ClientID, creds.ClientSecret, creds.JWTToken, serverURL)
	return &Client{
		serverURL:      serverURL,
		videoServerURL: videoServerURL,
		auth:           auth,
		httpClient:     newHTTPClient(),
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
		serverURL:      serverURL,
		videoServerURL: defaultVideoServerURL(serverURL),
		auth:           auth,
		isBot:          true,
		httpClient:     newHTTPClient(),
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

// VideoServerURL returns the RingCentral Video API base URL.
func (c *Client) VideoServerURL() string {
	return c.videoServerURL
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

	path := teamMessagingChatPostsEndpoint(chatID)
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

	path := teamMessagingChatPostEndpoint(chatID, postID)
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
	path := teamMessagingChatPostEndpoint(chatID, postID)
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

const maxImageSize = 5 * 1024 * 1024 // 5MB

// DownloadAttachment downloads a file from a contentUri using auth token.
func (c *Client) DownloadAttachment(ctx context.Context, contentURI string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	token, err := c.auth.AccessToken()
	if err != nil {
		return nil, "", fmt.Errorf("get access token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, contentURI, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("download HTTP %d", resp.StatusCode)
	}

	mediaType := resp.Header.Get("Content-Type")

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("read body: %w", err)
	}
	if len(data) > maxImageSize {
		return nil, "", fmt.Errorf("file too large (>5MB)")
	}

	return data, mediaType, nil
}

func (c *Client) doRequest(ctx context.Context, method, path, contentType string, body io.Reader) ([]byte, error) {
	return c.doRequestBase(ctx, c.serverURL, method, path, contentType, body)
}

func (c *Client) doVideoRequest(ctx context.Context, method, path, contentType string, body io.Reader) ([]byte, error) {
	return c.doRequestBase(ctx, c.videoServerURL, method, path, contentType, body)
}

func (c *Client) doRequestBase(ctx context.Context, baseURL, method, path, contentType string, body io.Reader) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	// Buffer the body so it can be replayed on retry.
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
	}

	const maxRetries = 2
	for attempt := 0; attempt <= maxRetries; attempt++ {
		token, err := c.auth.AccessToken()
		if err != nil {
			return nil, fmt.Errorf("get access token: %w", err)
		}

		reqURL := strings.TrimRight(baseURL, "/") + path
		var reqBody io.Reader
		if bodyBytes != nil {
			reqBody = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
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

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxRetries {
			wait := parseRetryAfter(resp.Header.Get("Retry-After"), 5*time.Second)
			if wait > 30*time.Second {
				wait = 30 * time.Second
			}
			slog.Warn("API rate limited, retrying",
				"component", "rc", "method", method, "path", path,
				"retry_after", wait, "attempt", attempt+1)
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
		}

		return respBody, nil
	}
	return nil, fmt.Errorf("HTTP 429: rate limited after %d retries", maxRetries)
}

func defaultVideoServerURL(serverURL string) string {
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if serverURL == "" {
		return "https://api-meet.ringcentral.com"
	}
	u, err := url.Parse(serverURL)
	if err != nil || u.Host == "" {
		return serverURL
	}
	switch u.Host {
	case "platform.ringcentral.com":
		u.Host = "api-meet.ringcentral.com"
	case "platform.devtest.ringcentral.com":
		u.Host = "api-meet.devtest.ringcentral.com"
	default:
		return serverURL
	}
	return u.String()
}

// parseRetryAfter parses the Retry-After header value (in seconds) and
// returns a duration. Falls back to the provided default if the header
// is empty or unparseable.
func parseRetryAfter(header string, fallback time.Duration) time.Duration {
	if header == "" {
		return fallback
	}
	if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return fallback
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
