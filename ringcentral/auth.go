package ringcentral

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Auth manages OAuth token lifecycle using JWT grant type.
type Auth struct {
	clientID     string
	clientSecret string
	jwtToken     string
	serverURL    string

	mu          sync.RWMutex
	accessToken string
	expiresAt   time.Time
	httpClient  *http.Client
	refreshMu   sync.Mutex // prevents thundering herd on concurrent token refresh
}

// NewAuth creates a new Auth manager.
func NewAuth(clientID, clientSecret, jwtToken, serverURL string) *Auth {
	if serverURL == "" {
		serverURL = defaultServerURL
	}
	return &Auth{
		clientID:     clientID,
		clientSecret: clientSecret,
		jwtToken:     jwtToken,
		serverURL:    serverURL,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

// Authenticate performs initial JWT authentication.
func (a *Auth) Authenticate() error {
	return a.refreshToken()
}

// AccessToken returns a valid access token, refreshing if needed.
// Uses refreshMu to prevent thundering herd when multiple goroutines
// detect an expired token simultaneously.
func (a *Auth) AccessToken() (string, error) {
	a.mu.RLock()
	token := a.accessToken
	expires := a.expiresAt
	a.mu.RUnlock()

	if token != "" && time.Now().Before(expires.Add(-60*time.Second)) {
		return token, nil
	}

	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()

	// Double-check after acquiring lock: another goroutine may have refreshed already
	a.mu.RLock()
	token = a.accessToken
	expires = a.expiresAt
	a.mu.RUnlock()

	if token != "" && time.Now().Before(expires.Add(-60*time.Second)) {
		return token, nil
	}

	if err := a.refreshToken(); err != nil {
		return "", err
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.accessToken, nil
}

func (a *Auth) refreshToken() error {
	const maxRetries = 2
	for attempt := 0; attempt <= maxRetries; attempt++ {
		data := url.Values{
			"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
			"assertion":  {a.jwtToken},
		}

		req, err := http.NewRequest(http.MethodPost, a.serverURL+"/restapi/oauth/token", strings.NewReader(data.Encode()))
		if err != nil {
			return fmt.Errorf("create token request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(
			[]byte(a.clientID+":"+a.clientSecret)))

		resp, err := a.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("token request: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("read token response: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxRetries {
			wait := parseRetryAfter(resp.Header.Get("Retry-After"), 5*time.Second)
			if wait > 30*time.Second {
				wait = 30 * time.Second
			}
			slog.Warn("token endpoint rate limited, retrying",
				"component", "auth", "retry_after", wait, "attempt", attempt+1)
			time.Sleep(wait)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("token request failed HTTP %d: %s", resp.StatusCode, string(body))
		}

		var tokenResp TokenResponse
		if err := json.Unmarshal(body, &tokenResp); err != nil {
			return fmt.Errorf("parse token response: %w", err)
		}

		a.mu.Lock()
		a.accessToken = tokenResp.AccessToken
		a.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		a.mu.Unlock()

		return nil
	}
	return fmt.Errorf("token request rate limited after %d retries", maxRetries)
}

// InvalidateToken clears the cached access token, forcing the next
// AccessToken() call to re-authenticate via JWT.
func (a *Auth) InvalidateToken() {
	a.mu.Lock()
	a.accessToken = ""
	a.expiresAt = time.Time{}
	a.mu.Unlock()
}

// GetWSToken obtains a single-use WebSocket access token.
// If the wstoken request returns HTTP 401 (stale/revoked token),
// the access token is invalidated and a single retry is attempted
// with a freshly obtained token.
func (a *Auth) GetWSToken() (*WSTokenResponse, error) {
	return a.getWSTokenOnce(false)
}

func (a *Auth) getWSTokenOnce(isRetry bool) (*WSTokenResponse, error) {
	token, err := a.AccessToken()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, a.serverURL+"/restapi/oauth/wstoken", nil)
	if err != nil {
		return nil, fmt.Errorf("create wstoken request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wstoken request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read wstoken response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized && !isRetry {
		slog.Warn("access token rejected by wstoken endpoint, forcing re-auth via JWT",
			"component", "auth")
		a.InvalidateToken()
		return a.getWSTokenOnce(true)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wstoken request failed HTTP %d: %s", resp.StatusCode, string(body))
	}

	var wsResp WSTokenResponse
	if err := json.Unmarshal(body, &wsResp); err != nil {
		return nil, fmt.Errorf("parse wstoken response: %w", err)
	}
	return &wsResp, nil
}

// SetTokenForTest sets a token directly for testing purposes.
func (a *Auth) SetTokenForTest(token string, expiresAt time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.accessToken = token
	a.expiresAt = expiresAt
}
