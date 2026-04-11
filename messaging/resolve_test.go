package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// --- extractNameFromText tests ---

func TestExtractNameFromText_Simple(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"总结 john 的消息", "john"},
		{"summarize messages with alice", "alice"},
		{"总结 bob", "bob"},
	}
	for _, tt := range tests {
		got := extractNameFromText(tt.input)
		got = strings.TrimSpace(got)
		if got != tt.want {
			t.Errorf("extractNameFromText(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractNameFromText_Empty(t *testing.T) {
	got := extractNameFromText("")
	if got != "" {
		t.Errorf("extractNameFromText(\"\") = %q, want empty", got)
	}
}

// --- extractNameViaAgent tests ---

func TestExtractNameViaAgent_Success(t *testing.T) {
	ag := &nameExtractAgent{reply: "john lin"}
	name := extractNameViaAgent(context.Background(), ag, "总结一下 john lin 的消息")
	if name != "john lin" {
		t.Errorf("expected 'john lin', got %q", name)
	}
}

func TestExtractNameViaAgent_None(t *testing.T) {
	ag := &nameExtractAgent{reply: "none"}
	name := extractNameViaAgent(context.Background(), ag, "总结一下消息")
	if name != "" {
		t.Errorf("expected empty string for 'none', got %q", name)
	}
}

func TestExtractNameViaAgent_Empty(t *testing.T) {
	ag := &nameExtractAgent{reply: ""}
	name := extractNameViaAgent(context.Background(), ag, "总结")
	if name != "" {
		t.Errorf("expected empty string, got %q", name)
	}
}

func TestExtractNameViaAgent_Error(t *testing.T) {
	ag := &nameExtractAgent{err: fmt.Errorf("agent error")}
	name := extractNameViaAgent(context.Background(), ag, "总结 john")
	if name != "" {
		t.Errorf("expected empty string on error, got %q", name)
	}
}

func TestExtractNameViaAgent_QuotedName(t *testing.T) {
	ag := &nameExtractAgent{reply: `"John"`}
	name := extractNameViaAgent(context.Background(), ag, "总结 John 的消息")
	if name != "john" {
		t.Errorf("expected 'john' (trimmed quotes, lowered), got %q", name)
	}
}

type nameExtractAgent struct {
	reply string
	err   error
}

func (a *nameExtractAgent) Chat(_ context.Context, _, _ string) (string, error) {
	if a.err != nil {
		return "", a.err
	}
	return a.reply, nil
}
func (a *nameExtractAgent) ResetSession(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (a *nameExtractAgent) SetCwd(_ string)        {}
func (a *nameExtractAgent) Info() agent.AgentInfo {
	return agent.AgentInfo{Name: "name-extractor", Type: "test"}
}

// --- exactMatch / fuzzyMatch tests ---

func TestExactMatch_Resolve(t *testing.T) {
	tests := []struct {
		haystack, needle string
		want             bool
	}{
		{"John Lin", "john lin", true},
		{"JOHN LIN", "john lin", true},
		{" John Lin ", "john lin", true},
		{"John", "John Lin", false},
		{"", "", true},
		{"John", "", false},
		{"", "John", false},
	}
	for _, tt := range tests {
		if got := exactMatch(tt.haystack, tt.needle); got != tt.want {
			t.Errorf("exactMatch(%q, %q) = %v, want %v", tt.haystack, tt.needle, got, tt.want)
		}
	}
}

func TestFuzzyMatch_Resolve(t *testing.T) {
	tests := []struct {
		haystack, needle string
		want             bool
	}{
		{"John Lin", "john", true},
		{"John Lin", "lin", true},
		{"John Lin", "johnlin", true},
		{"John Lin", "alice", false},
		{"", "john", false},
		{"John", "", false},
	}
	for _, tt := range tests {
		if got := fuzzyMatch(tt.haystack, tt.needle); got != tt.want {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", tt.haystack, tt.needle, got, tt.want)
		}
	}
}

// --- ResolveChatTarget tests ---

func TestResolveChatTarget_TeamMention(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	req, err := ResolveChatTarget(context.Background(), client, nil, "总结一下",
		[]ringcentral.Mention{{ID: "team-123", Type: "Team", Name: "Backend Team"}})
	if err != nil {
		t.Fatal(err)
	}
	if req.ChatID != "team-123" {
		t.Errorf("expected chatID 'team-123', got %q", req.ChatID)
	}
	if req.ChatName != "Backend Team" {
		t.Errorf("expected chatName 'Backend Team', got %q", req.ChatName)
	}
}

func TestResolveChatTarget_PersonMention(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// ListChats for Direct type
		json.NewEncoder(w).Encode(map[string]interface{}{
			"records": []map[string]interface{}{
				{
					"id":      "dm-chat-1",
					"type":    "Direct",
					"members": []map[string]string{{"id": "person-1"}, {"id": "bot-1"}},
				},
			},
		})
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	req, err := ResolveChatTarget(context.Background(), client, nil, "总结 alice 的消息",
		[]ringcentral.Mention{{ID: "person-1", Type: "Person", Name: "Alice"}})
	if err != nil {
		t.Fatal(err)
	}
	if req.ChatID != "dm-chat-1" {
		t.Errorf("expected chatID 'dm-chat-1', got %q", req.ChatID)
	}
}

func TestResolveChatTarget_NoMention_NoName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	_, err := ResolveChatTarget(context.Background(), client, nil, "summarize", nil)
	if err == nil {
		t.Fatal("expected error when no mention and no name")
	}
	if !strings.Contains(err.Error(), "cannot determine") {
		t.Errorf("expected 'cannot determine' error, got %q", err.Error())
	}
}

// --- chatCache tests ---

func TestChatCache_Lookup_ExactMatch(t *testing.T) {
	c := &chatCache{persons: make(map[string]*ringcentral.PersonInfo)}
	c.entries = []chatCacheEntry{
		{ChatID: "c1", ChatName: "John Lin", ChatType: "Direct"},
		{ChatID: "c2", ChatName: "Johnny Lin", ChatType: "Direct"},
	}
	c.loaded = true

	result := c.lookup("John Lin")
	if result == nil {
		t.Fatal("expected a match")
	}
	if result.ChatID != "c1" {
		t.Errorf("expected exact match c1, got %q", result.ChatID)
	}
}

func TestChatCache_Lookup_FuzzyMatch(t *testing.T) {
	c := &chatCache{persons: make(map[string]*ringcentral.PersonInfo)}
	c.entries = []chatCacheEntry{
		{ChatID: "c1", ChatName: "John Lin", ChatType: "Direct"},
	}
	c.loaded = true

	result := c.lookup("john")
	if result == nil {
		t.Fatal("expected a fuzzy match")
	}
	if result.ChatID != "c1" {
		t.Errorf("expected c1, got %q", result.ChatID)
	}
}

func TestChatCache_Lookup_NoMatch(t *testing.T) {
	c := &chatCache{persons: make(map[string]*ringcentral.PersonInfo)}
	c.entries = []chatCacheEntry{
		{ChatID: "c1", ChatName: "Alice", ChatType: "Direct"},
	}
	c.loaded = true

	result := c.lookup("Bob")
	if result != nil {
		t.Errorf("expected no match, got %v", result)
	}
}

func TestChatCache_GetPerson_Cached(t *testing.T) {
	c := &chatCache{persons: make(map[string]*ringcentral.PersonInfo)}
	c.persons["p1"] = &ringcentral.PersonInfo{ID: "p1", FirstName: "John", LastName: "Lin"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not call API for cached person")
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	person := c.getPerson(context.Background(), client, "p1")
	if person == nil || person.ID != "p1" {
		t.Errorf("expected cached person p1")
	}
}

func TestChatCache_GetPerson_GlipPrefix(t *testing.T) {
	c := &chatCache{persons: make(map[string]*ringcentral.PersonInfo)}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not call API for glip- prefixed person")
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	person := c.getPerson(context.Background(), client, "glip-user-1")
	if person != nil {
		t.Errorf("expected nil for glip- prefix, got %v", person)
	}
}

func TestChatCache_GetPerson_FetchFromAPI(t *testing.T) {
	c := &chatCache{persons: make(map[string]*ringcentral.PersonInfo)}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ringcentral.PersonInfo{
			ID: "p2", FirstName: "Alice", LastName: "Smith",
		})
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	person := c.getPerson(context.Background(), client, "p2")
	if person == nil || person.FirstName != "Alice" {
		t.Errorf("expected fetched person Alice, got %v", person)
	}

	// Second call should use cache
	person2 := c.getPerson(context.Background(), client, "p2")
	if person2 == nil || person2.ID != "p2" {
		t.Errorf("expected cached person on second call")
	}
}

// --- findDirectChat tests ---

func TestFindDirectChat_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"records": []map[string]interface{}{
				{"id": "c1", "type": "Direct", "members": []map[string]string{{"id": "p1"}, {"id": "bot"}}},
				{"id": "c2", "type": "Direct", "members": []map[string]string{{"id": "p2"}, {"id": "bot"}}},
			},
		})
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	chatID, err := findDirectChat(context.Background(), client, "p2")
	if err != nil {
		t.Fatal(err)
	}
	if chatID != "c2" {
		t.Errorf("expected 'c2', got %q", chatID)
	}
}

func TestFindDirectChat_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"records": []map[string]interface{}{
				{"id": "c1", "type": "Direct", "members": []map[string]string{{"id": "p1"}}},
			},
		})
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "token")
	_, err := findDirectChat(context.Background(), client, "p999")
	if err == nil {
		t.Fatal("expected error for not found")
	}
	if !strings.Contains(err.Error(), "no direct chat") {
		t.Errorf("expected 'no direct chat' error, got %q", err.Error())
	}
}

// --- removeWholeWords tests ---

func TestRemoveWholeWords(t *testing.T) {
	tests := []struct {
		text  string
		words []string
		want  string
	}{
		{"summarize messages with alice", []string{"summarize", "messages", "with"}, "alice"},
		{"hello world", []string{"foo"}, "hello world"},
		{"the cat", []string{"the"}, "cat"},
		{"", []string{"word"}, ""},
	}
	for _, tt := range tests {
		got := removeWholeWords(tt.text, tt.words)
		if got != tt.want {
			t.Errorf("removeWholeWords(%q, %v) = %q, want %q", tt.text, tt.words, got, tt.want)
		}
	}
}

// --- chatCache addEntry test ---

func TestChatCache_AddEntry(t *testing.T) {
	c := &chatCache{persons: make(map[string]*ringcentral.PersonInfo)}
	entry := chatCacheEntry{ChatID: "c1", ChatName: "Test", ChatType: "Direct"}
	c.addEntry(entry)

	if len(c.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(c.entries))
	}
	if c.entries[0].ChatID != "c1" {
		t.Errorf("expected c1, got %q", c.entries[0].ChatID)
	}
	if !c.loaded {
		t.Error("expected loaded to be true after addEntry")
	}
}

// --- chatCache lookupViaDirectory tests ---

func TestChatCache_LookupViaDirectory_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "directory/entries/search") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"records": []map[string]string{
					{"id": "p1", "firstName": "John", "lastName": "Lin", "email": "john@example.com"},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "conversations") {
			json.NewEncoder(w).Encode(map[string]string{"id": "dm-chat-1", "type": "Direct"})
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	c := &chatCache{persons: make(map[string]*ringcentral.PersonInfo)}
	entry := c.lookupViaDirectory(context.Background(), client, "John Lin")
	if entry == nil {
		t.Fatal("expected a match from directory")
	}
	if entry.ChatID != "dm-chat-1" {
		t.Errorf("expected dm-chat-1, got %q", entry.ChatID)
	}
}

func TestChatCache_LookupViaDirectory_NoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"records": []interface{}{}})
	}))
	defer srv.Close()

	client := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID: "id", ClientSecret: "secret", JWTToken: "jwt", ServerURL: srv.URL,
	})
	client.Auth().SetTokenForTest("test-token", time.Now().Add(1*time.Hour))

	c := &chatCache{persons: make(map[string]*ringcentral.PersonInfo)}
	entry := c.lookupViaDirectory(context.Background(), client, "Unknown Person")
	if entry != nil {
		t.Errorf("expected nil for no directory results, got %v", entry)
	}
}
