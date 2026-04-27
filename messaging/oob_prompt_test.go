package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
)

func newOOBPromptTestClient(serverURL string) *ringcentral.Client {
	c := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: serverURL,
	})
	c.Auth().SetTokenForTest("token", time.Now().Add(time.Hour))
	return c
}

func TestResolveRequesterLabel(t *testing.T) {
	t.Run("empty userID returns empty", func(t *testing.T) {
		if got := resolveRequesterLabel(context.Background(), nil, "  "); got != "" {
			t.Fatalf("empty userID expected empty, got %q", got)
		}
	})
	t.Run("nil readClient returns bare ID", func(t *testing.T) {
		if got := resolveRequesterLabel(context.Background(), nil, "user-1"); got != "user-1" {
			t.Fatalf("nil client expected bare ID, got %q", got)
		}
	})
	t.Run("lookup error returns bare ID", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)
		got := resolveRequesterLabel(context.Background(), newOOBPromptTestClient(srv.URL), "user-1")
		if got != "user-1" {
			t.Fatalf("error path expected bare ID, got %q", got)
		}
	})
	cases := []struct {
		name  string
		first string
		last  string
		email string
		want  string
	}{
		{"name+email", "Alice", "Cross", "alice@example.com", "Alice Cross <alice@example.com> (id=user-1)"},
		{"name only", "Alice", "Cross", "", "Alice Cross (id=user-1)"},
		{"email only", "", "", "alice@example.com", "alice@example.com (id=user-1)"},
		{"empty fields fall back to ID", "", "", "", "user-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(ringcentral.PersonInfo{
					ID: "user-1", FirstName: tc.first, LastName: tc.last, Email: tc.email,
				})
			}))
			t.Cleanup(srv.Close)
			got := resolveRequesterLabel(context.Background(), newOOBPromptTestClient(srv.URL), "user-1")
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveChatLabel(t *testing.T) {
	t.Run("empty chatID returns empty", func(t *testing.T) {
		if got := resolveChatLabel(context.Background(), nil, "  "); got != "" {
			t.Fatalf("empty chatID expected empty, got %q", got)
		}
	})
	t.Run("nil readClient returns bare ID", func(t *testing.T) {
		if got := resolveChatLabel(context.Background(), nil, "chat-1"); got != "chat-1" {
			t.Fatalf("nil client expected bare ID, got %q", got)
		}
	})
	t.Run("lookup error returns bare ID", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)
		got := resolveChatLabel(context.Background(), newOOBPromptTestClient(srv.URL), "chat-1")
		if got != "chat-1" {
			t.Fatalf("error path expected bare ID, got %q", got)
		}
	})
	t.Run("named chat", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ringcentral.Chat{ID: "chat-1", Name: "Engineering", Type: "Team"})
		}))
		t.Cleanup(srv.Close)
		got := resolveChatLabel(context.Background(), newOOBPromptTestClient(srv.URL), "chat-1")
		if !strings.Contains(got, "Engineering") || !strings.Contains(got, "id=chat-1") {
			t.Fatalf("expected name+id, got %q", got)
		}
	})
	t.Run("empty name falls back to type", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ringcentral.Chat{ID: "chat-1", Name: "", Type: "Direct"})
		}))
		t.Cleanup(srv.Close)
		got := resolveChatLabel(context.Background(), newOOBPromptTestClient(srv.URL), "chat-1")
		if !strings.Contains(got, "Direct") {
			t.Fatalf("expected fallback to type, got %q", got)
		}
	})
	t.Run("empty name and type fall back to bare ID", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ringcentral.Chat{ID: "chat-1"})
		}))
		t.Cleanup(srv.Close)
		got := resolveChatLabel(context.Background(), newOOBPromptTestClient(srv.URL), "chat-1")
		if got != "chat-1" {
			t.Fatalf("expected bare ID, got %q", got)
		}
	})
}

func TestCollectAuthorizeMeta_Branches(t *testing.T) {
	t.Run("nil readClient returns empty meta", func(t *testing.T) {
		h := newTestHandler()
		got := h.collectAuthorizeMeta(context.Background(), nil, ringcentral.Post{GroupID: "c1", CreatorID: "u1"})
		if got.ChatName != "" || got.DisplayName != "" || got.Email != "" {
			t.Fatalf("expected zero-value meta, got %+v", got)
		}
	})

	t.Run("lookup errors leave fields blank without panicking", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)
		client := newOOBPromptTestClient(srv.URL)
		h := newTestHandler()
		got := h.collectAuthorizeMeta(context.Background(), client, ringcentral.Post{GroupID: "c1", CreatorID: "u1"})
		if got.ChatName != "" || got.Email != "" || got.DisplayName != "" {
			t.Fatalf("expected blank meta on error, got %+v", got)
		}
	})

	t.Run("formatPersonLabel covers all branches", func(t *testing.T) {
		cases := []struct {
			name, displayName, email, userID, want string
		}{
			{name: "empty userID returns empty"},
			{name: "trim-only userID returns empty", userID: "  "},
			{name: "name+email", displayName: "Alice", email: "a@example.com", userID: "u1", want: "Alice <a@example.com> (id=u1)"},
			{name: "name only", displayName: "Alice", userID: "u1", want: "Alice (id=u1)"},
			{name: "email only", email: "a@example.com", userID: "u1", want: "a@example.com (id=u1)"},
			{name: "bare ID fallback", userID: "u1", want: "u1"},
			{name: "trims fields", displayName: "  Alice  ", email: "  a@example.com  ", userID: "u1", want: "Alice <a@example.com> (id=u1)"},
		}
		for _, tc := range cases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				if got := formatPersonLabel(tc.displayName, tc.email, tc.userID); got != tc.want {
					t.Errorf("got %q, want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("formatChatLabel covers all branches", func(t *testing.T) {
		cases := []struct {
			name, chatName, chatID, want string
		}{
			{name: "empty chatID returns empty"},
			{name: "trim-only chatID returns empty", chatID: "  "},
			{name: "named", chatName: "Engineering", chatID: "c1", want: "Engineering (id=c1)"},
			{name: "bare ID fallback", chatID: "c1", want: "c1"},
			{name: "trims name", chatName: "  Eng  ", chatID: "c1", want: "Eng (id=c1)"},
		}
		for _, tc := range cases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				if got := formatChatLabel(tc.chatName, tc.chatID); got != tc.want {
					t.Errorf("got %q, want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("name composed from first and last; type fallback for chat name", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case strings.Contains(r.URL.Path, "/persons/"):
				_ = json.NewEncoder(w).Encode(ringcentral.PersonInfo{
					ID: "u1", FirstName: "Alice", LastName: "Cross", Email: "alice@example.com",
				})
			case strings.Contains(r.URL.Path, "/chats/"):
				_ = json.NewEncoder(w).Encode(ringcentral.Chat{ID: "c1", Name: "", Type: "Direct"})
			default:
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "x"})
			}
		}))
		t.Cleanup(srv.Close)
		client := newOOBPromptTestClient(srv.URL)
		h := newTestHandler()
		got := h.collectAuthorizeMeta(context.Background(), client, ringcentral.Post{GroupID: "c1", CreatorID: "u1"})
		if got.DisplayName != "Alice Cross" {
			t.Errorf("DisplayName = %q, want %q", got.DisplayName, "Alice Cross")
		}
		if got.Email != "alice@example.com" {
			t.Errorf("Email = %q, want alice@example.com", got.Email)
		}
		if got.ChatName != "Direct" {
			t.Errorf("ChatName = %q, want Direct (type fallback)", got.ChatName)
		}
	})
}
