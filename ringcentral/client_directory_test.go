package ringcentral

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSearchDirectory_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/restapi/v1.0/account/~/directory/entries/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DirectorySearchResult{
			Records: []DirectoryEntry{
				{ID: "123", FirstName: "Alice", LastName: "Smith", Email: "alice@example.com"},
				{ID: "456", FirstName: "Bob", LastName: "Jones", Email: "bob@example.com"},
			},
		})
	})
	defer srv.Close()

	result, err := client.SearchDirectory(context.Background(), "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(result.Records))
	}
	if result.Records[0].Email != "alice@example.com" {
		t.Errorf("expected alice@example.com, got %q", result.Records[0].Email)
	}
}

func TestSearchDirectory_Empty(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DirectorySearchResult{Records: []DirectoryEntry{}})
	})
	defer srv.Close()

	result, err := client.SearchDirectory(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Records) != 0 {
		t.Errorf("expected 0 records, got %d", len(result.Records))
	}
}

func TestSearchDirectory_HTTPError(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	})
	defer srv.Close()

	_, err := client.SearchDirectory(context.Background(), "alice")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestSearchContacts_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/restapi/v1.0/account/~/extension/~/address-book/contact" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("searchString") != "Grace He" || r.URL.Query().Get("perPage") != "20" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ContactList{
			Records: []Contact{{
				ID:            "contact-1",
				FirstName:     "Grace",
				LastName:      "He",
				BusinessPhone: "+12123753080",
			}},
		})
	})
	defer srv.Close()

	result, err := client.SearchContacts(context.Background(), "Grace He")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Records) != 1 || result.Records[0].BusinessPhone != "+12123753080" {
		t.Fatalf("unexpected contact result: %+v", result)
	}
}

func TestGetExtensionInfo_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/restapi/v1.0/account/~/extension/~" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 12345678}`))
	})
	defer srv.Close()

	id, err := client.GetExtensionInfo(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "12345678" {
		t.Errorf("expected 12345678, got %q", id)
	}
}

func TestGetExtensionInfo_HTTPError(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("unauthorized"))
	})
	defer srv.Close()

	_, err := client.GetExtensionInfo(context.Background())
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestCreateConversation_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/team-messaging/v1/conversations" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var reqBody CreateChatRequest
		json.NewDecoder(r.Body).Decode(&reqBody)
		if len(reqBody.Members) != 2 {
			t.Errorf("expected 2 members, got %d", len(reqBody.Members))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Chat{ID: "conv-1", Type: "Direct"})
	})
	defer srv.Close()

	chat, err := client.CreateConversation(context.Background(), []string{"user-1", "user-2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chat.ID != "conv-1" {
		t.Errorf("expected conv-1, got %s", chat.ID)
	}
	if chat.Type != "Direct" {
		t.Errorf("expected Direct, got %s", chat.Type)
	}
}

func TestCreateConversation_HTTPError(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	})
	defer srv.Close()

	_, err := client.CreateConversation(context.Background(), []string{"user-1"})
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}

func TestFindDirectChat_Success(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Chat{ID: "dm-chat-99", Type: "Direct"})
	})
	defer srv.Close()

	chatID, err := client.FindDirectChat(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chatID != "dm-chat-99" {
		t.Errorf("expected dm-chat-99, got %s", chatID)
	}
}

func TestFindDirectChat_Error(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	})
	defer srv.Close()

	_, err := client.FindDirectChat(context.Background(), "user-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveUserIDs_NumericOnly(t *testing.T) {
	// No server needed since numeric IDs don't require directory lookup
	client := &Client{}
	resolved := client.ResolveUserIDs(context.Background(), []string{"111", "222", "333"})
	if len(resolved) != 3 {
		t.Fatalf("expected 3 resolved IDs, got %d", len(resolved))
	}
	if resolved[0] != "111" || resolved[1] != "222" || resolved[2] != "333" {
		t.Errorf("unexpected resolved IDs: %v", resolved)
	}
}

func TestResolveUserIDs_EmailResolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DirectorySearchResult{
			Records: []DirectoryEntry{
				{ID: "999", FirstName: "Alice", LastName: "Smith", Email: "alice@example.com"},
			},
		})
	}))
	defer srv.Close()

	auth := &Auth{
		accessToken: "test-token",
		expiresAt:   time.Now().Add(1 * time.Hour),
		httpClient:  &http.Client{},
		serverURL:   srv.URL,
	}
	client := &Client{
		serverURL:  srv.URL,
		auth:       auth,
		httpClient: &http.Client{},
	}

	resolved := client.ResolveUserIDs(context.Background(), []string{"111", "alice@example.com"})
	if len(resolved) != 2 {
		t.Fatalf("expected 2 resolved IDs, got %d", len(resolved))
	}
	if resolved[0] != "111" {
		t.Errorf("expected numeric ID 111, got %s", resolved[0])
	}
	if resolved[1] != "999" {
		t.Errorf("expected resolved ID 999, got %s", resolved[1])
	}
}

func TestResolveUserIDs_EmailNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DirectorySearchResult{
			Records: []DirectoryEntry{
				{ID: "999", FirstName: "Bob", LastName: "Smith", Email: "bob@example.com"},
			},
		})
	}))
	defer srv.Close()

	auth := &Auth{
		accessToken: "test-token",
		expiresAt:   time.Now().Add(1 * time.Hour),
		httpClient:  &http.Client{},
		serverURL:   srv.URL,
	}
	client := &Client{
		serverURL:  srv.URL,
		auth:       auth,
		httpClient: &http.Client{},
	}

	// alice@example.com not in response, should be skipped
	resolved := client.ResolveUserIDs(context.Background(), []string{"alice@example.com"})
	if len(resolved) != 0 {
		t.Errorf("expected 0 resolved IDs for unfound email, got %d: %v", len(resolved), resolved)
	}
}

func TestResolveUserIDs_DirectoryError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	defer srv.Close()

	auth := &Auth{
		accessToken: "test-token",
		expiresAt:   time.Now().Add(1 * time.Hour),
		httpClient:  &http.Client{},
		serverURL:   srv.URL,
	}
	client := &Client{
		serverURL:  srv.URL,
		auth:       auth,
		httpClient: &http.Client{},
	}

	// Error should skip the email entry, keep numeric
	resolved := client.ResolveUserIDs(context.Background(), []string{"111", "alice@example.com"})
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved ID, got %d: %v", len(resolved), resolved)
	}
	if resolved[0] != "111" {
		t.Errorf("expected 111, got %s", resolved[0])
	}
}

func TestSearchDirectory_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.SearchDirectory(context.Background(), "alice")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestCreateConversation_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.CreateConversation(context.Background(), []string{"user-1"})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestGetExtensionInfo_InvalidJSON(t *testing.T) {
	client, srv := newTestClientWithServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`broken`))
	})
	defer srv.Close()

	_, err := client.GetExtensionInfo(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestResolveUserIDs_CaseInsensitiveEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DirectorySearchResult{
			Records: []DirectoryEntry{
				{ID: "777", FirstName: "Alice", LastName: "Smith", Email: "Alice@Example.COM"},
			},
		})
	}))
	defer srv.Close()

	auth := &Auth{
		accessToken: "test-token",
		expiresAt:   time.Now().Add(1 * time.Hour),
		httpClient:  &http.Client{},
		serverURL:   srv.URL,
	}
	client := &Client{
		serverURL:  srv.URL,
		auth:       auth,
		httpClient: &http.Client{},
	}

	resolved := client.ResolveUserIDs(context.Background(), []string{"alice@example.com"})
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved ID, got %d", len(resolved))
	}
	if resolved[0] != "777" {
		t.Errorf("expected 777, got %s", resolved[0])
	}
}
