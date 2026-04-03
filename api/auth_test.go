package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthMiddleware_ValidToken(t *testing.T) {
	s := &Server{token: "secret-token"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-RingClaw-Token", "secret-token")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	s := &Server{token: "secret-token"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-RingClaw-Token", "wrong-token")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	s := &Server{token: "secret-token"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_EmptyServerToken(t *testing.T) {
	s := &Server{token: ""}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when server has no token, got %d", w.Code)
	}
}

func TestHostGuard_Localhost(t *testing.T) {
	handler := hostGuard(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		host string
		want int
	}{
		{"localhost:18011", http.StatusOK},
		{"127.0.0.1:18011", http.StatusOK},
		{"[::1]:18011", http.StatusOK},
		{"localhost", http.StatusOK},
		{"127.0.0.1", http.StatusOK},
		{"::1", http.StatusOK},
		{"evil.com:18011", http.StatusForbidden},
		{"attacker.local:18011", http.StatusForbidden},
		{"192.168.1.1:18011", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tt.host
			w := httptest.NewRecorder()
			handler(w, req)
			if w.Code != tt.want {
				t.Errorf("host %q: expected %d, got %d", tt.host, tt.want, w.Code)
			}
		})
	}
}

func TestLoadOrCreateToken(t *testing.T) {
	// Override HOME to temp dir
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	rcDir := filepath.Join(tmpDir, ".ringclaw")
	os.MkdirAll(rcDir, 0o700)

	// First call should create token
	token1, err := LoadOrCreateToken()
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if token1 == "" {
		t.Fatal("expected non-empty token")
	}
	if len(token1) != 64 { // 32 bytes = 64 hex chars
		t.Errorf("expected 64-char token, got %d chars", len(token1))
	}

	// Second call should return same token
	token2, err := LoadOrCreateToken()
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if token1 != token2 {
		t.Errorf("expected same token, got %q vs %q", token1, token2)
	}
}
