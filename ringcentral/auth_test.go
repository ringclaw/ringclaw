package ringcentral

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// timeNow is a helper so tests don't rely on wall clock precision.
func timeNow() time.Time { return time.Now() }

func TestAuthenticate_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "test-token",
			ExpiresIn:   3600,
		})
	}))
	defer srv.Close()

	auth := NewAuth("client-id", "client-secret", "jwt-token", srv.URL)
	auth.httpClient = srv.Client()

	if err := auth.Authenticate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	token, err := auth.AccessToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "test-token" {
		t.Errorf("expected test-token, got %q", token)
	}
}

func TestAuthenticate_InvalidCreds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid_grant"}`))
	}))
	defer srv.Close()

	auth := NewAuth("bad", "bad", "bad", srv.URL)
	auth.httpClient = srv.Client()

	if err := auth.Authenticate(); err == nil {
		t.Fatal("expected error for invalid credentials")
	}
}

func TestAccessToken_AutoRefresh(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "refreshed-token",
			ExpiresIn:   3600,
		})
	}))
	defer srv.Close()

	auth := NewAuth("id", "secret", "jwt", srv.URL)
	auth.httpClient = srv.Client()

	// Set an expired token
	auth.mu.Lock()
	auth.accessToken = "expired"
	auth.expiresAt = time.Now().Add(-1 * time.Minute)
	auth.mu.Unlock()

	token, err := auth.AccessToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "refreshed-token" {
		t.Errorf("expected refreshed-token, got %q", token)
	}
}

func TestAccessToken_ConcurrentAccess(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		time.Sleep(10 * time.Millisecond) // simulate latency
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "concurrent-token",
			ExpiresIn:   3600,
		})
	}))
	defer srv.Close()

	auth := NewAuth("id", "secret", "jwt", srv.URL)
	auth.httpClient = srv.Client()

	// Set expired token to force refresh
	auth.mu.Lock()
	auth.accessToken = "expired"
	auth.expiresAt = time.Now().Add(-1 * time.Minute)
	auth.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := auth.AccessToken()
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if token != "concurrent-token" {
				t.Errorf("expected concurrent-token, got %q", token)
			}
		}()
	}
	wg.Wait()

	// Before P0 fix: callCount could be up to 10
	// After P0 fix with singleflight: should be 1
	t.Logf("concurrent refresh call count: %d (will be 1 after singleflight fix)", callCount.Load())
}

func TestInvalidateToken_ForcesRefresh(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "fresh-token",
			ExpiresIn:   3600,
		})
	}))
	defer srv.Close()

	auth := NewAuth("id", "secret", "jwt", srv.URL)
	auth.httpClient = srv.Client()

	// Set a "valid" token (not expired locally)
	auth.mu.Lock()
	auth.accessToken = "stale-token"
	auth.expiresAt = time.Now().Add(30 * time.Minute)
	auth.mu.Unlock()

	// Should return cached token without calling server
	token, err := auth.AccessToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "stale-token" {
		t.Errorf("expected stale-token, got %q", token)
	}
	if callCount.Load() != 0 {
		t.Errorf("expected 0 server calls, got %d", callCount.Load())
	}

	// Invalidate and verify next call refreshes
	auth.InvalidateToken()

	token, err = auth.AccessToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "fresh-token" {
		t.Errorf("expected fresh-token, got %q", token)
	}
	if callCount.Load() != 1 {
		t.Errorf("expected 1 server call after invalidation, got %d", callCount.Load())
	}
}

func TestGetWSToken_RetryOn401(t *testing.T) {
	var tokenCallCount, wsCallCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/restapi/oauth/token":
			tokenCallCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(TokenResponse{
				AccessToken: "fresh-token",
				ExpiresIn:   3600,
			})
		case "/restapi/oauth/wstoken":
			count := wsCallCount.Add(1)
			auth := r.Header.Get("Authorization")
			if count == 1 && auth == "Bearer stale-token" {
				// First attempt with stale token: reject
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"errorCode":"TokenInvalid","message":"Token not found"}`))
				return
			}
			// Retry with fresh token: succeed
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(WSTokenResponse{
				WSAccessToken: "ws-token-ok",
				URI:           "wss://example.com/ws",
				ExpiresIn:     300,
			})
		}
	}))
	defer srv.Close()

	auth := NewAuth("id", "secret", "jwt", srv.URL)
	auth.httpClient = srv.Client()

	// Set a locally-valid but server-revoked token
	auth.mu.Lock()
	auth.accessToken = "stale-token"
	auth.expiresAt = time.Now().Add(30 * time.Minute)
	auth.mu.Unlock()

	wsResp, err := auth.GetWSToken()
	if err != nil {
		t.Fatalf("expected retry to succeed, got error: %v", err)
	}
	if wsResp.WSAccessToken != "ws-token-ok" {
		t.Errorf("expected ws-token-ok, got %q", wsResp.WSAccessToken)
	}
	if wsCallCount.Load() != 2 {
		t.Errorf("expected 2 wstoken calls (1 fail + 1 retry), got %d", wsCallCount.Load())
	}
	if tokenCallCount.Load() != 1 {
		t.Errorf("expected 1 token refresh call, got %d", tokenCallCount.Load())
	}
}

func TestRefreshToken_429Retry(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n <= 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"errorCode":"CMN-301","message":"Request rate exceeded"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "retry-token",
			ExpiresIn:   3600,
		})
	}))
	defer srv.Close()

	auth := NewAuth("id", "secret", "jwt", srv.URL)
	auth.httpClient = srv.Client()

	if err := auth.Authenticate(); err != nil {
		t.Fatalf("expected success after 429 retry, got: %v", err)
	}
	token, err := auth.AccessToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "retry-token" {
		t.Errorf("expected retry-token, got %q", token)
	}
	if callCount.Load() != 2 {
		t.Errorf("expected 2 token calls (1 x 429 + 1 success), got %d", callCount.Load())
	}
}

func TestRefreshToken_429ExhaustedRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"errorCode":"CMN-301","message":"Request rate exceeded"}`))
	}))
	defer srv.Close()

	auth := NewAuth("id", "secret", "jwt", srv.URL)
	auth.httpClient = srv.Client()

	err := auth.Authenticate()
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("expected '429' in error, got: %v", err)
	}
}
