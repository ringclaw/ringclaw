package agent

import (
	"net/http"
	"testing"
)

func TestCloneHeaders_NilAndEmpty(t *testing.T) {
	if got := cloneHeaders(nil); got != nil {
		t.Errorf("cloneHeaders(nil) = %v, want nil", got)
	}
	if got := cloneHeaders(map[string]string{}); got != nil {
		t.Errorf("cloneHeaders(empty) = %v, want nil", got)
	}
}

func TestCloneHeaders_DeepCopy(t *testing.T) {
	in := map[string]string{"X-Custom": "v1", "X-Other": "v2"}
	got := cloneHeaders(in)
	if len(got) != 2 || got["X-Custom"] != "v1" || got["X-Other"] != "v2" {
		t.Errorf("cloneHeaders result missing entries: %+v", got)
	}
	// Mutating the original must not bleed into the clone.
	in["X-Custom"] = "mutated"
	if got["X-Custom"] != "v1" {
		t.Errorf("clone aliased original map: %+v", got)
	}
	// And vice-versa.
	got["X-New"] = "added"
	if _, ok := in["X-New"]; ok {
		t.Errorf("mutating clone bled into original: %+v", in)
	}
}

func TestOpenAIFormat_Capabilities(t *testing.T) {
	f := &openaiFormat{}
	if f.managesHistory() {
		t.Error("openai format should NOT manage history server-side")
	}
	if f.supportsCwd() {
		t.Error("openai format should NOT support cwd")
	}
}

func TestNanoClawFormat_Capabilities(t *testing.T) {
	f := &nanoclawFormat{}
	if !f.managesHistory() {
		t.Error("nanoclaw format SHOULD manage history server-side")
	}
	if !f.supportsCwd() {
		t.Error("nanoclaw format SHOULD support cwd")
	}
}

func TestDifyFormat_Capabilities(t *testing.T) {
	f := newDifyFormat("https://api.dify.ai/v1/chat-messages", "k", &http.Client{})
	if !f.managesHistory() {
		t.Error("dify format SHOULD manage history server-side")
	}
	if f.supportsCwd() {
		t.Error("dify format should NOT support cwd")
	}
}

func TestDifyFormat_BaseURL_StripsV1Suffix(t *testing.T) {
	f := newDifyFormat("https://api.dify.ai/v1/chat-messages", "", &http.Client{})
	if f.baseURL != "https://api.dify.ai" {
		t.Errorf("baseURL = %q, want https://api.dify.ai", f.baseURL)
	}
}

func TestDifyFormat_BaseURL_NoV1Path(t *testing.T) {
	// When the configured endpoint is a bare host with no /v1/ segment,
	// baseURL drops the path entirely so DELETE paths still resolve.
	f := newDifyFormat("https://api.dify.ai/some/other", "", &http.Client{})
	if f.baseURL != "https://api.dify.ai" {
		t.Errorf("baseURL = %q, want https://api.dify.ai", f.baseURL)
	}
}

func TestDifyFormat_ParseResponse_Errors(t *testing.T) {
	f := newDifyFormat("https://api.dify.ai/v1/chat-messages", "", &http.Client{})

	// Invalid JSON -> error.
	if _, err := f.parseResponse([]byte("not json")); err == nil {
		t.Error("invalid JSON should produce error")
	}

	// Empty answer -> error.
	if _, err := f.parseResponse([]byte(`{"answer":"","conversation_id":"x"}`)); err == nil {
		t.Error("empty answer should produce error")
	}

	// Whitespace-only answer -> error.
	if _, err := f.parseResponse([]byte(`{"answer":"   ","conversation_id":"x"}`)); err == nil {
		t.Error("whitespace-only answer should produce error")
	}
}

func TestDifyFormat_ParseResponse_TrimsAnswer(t *testing.T) {
	// We need pendingConvID set up for parseResponse to record the
	// returned conversation_id; do that by calling buildRequest first.
	f := newDifyFormat("https://api.dify.ai/v1/chat-messages", "", &http.Client{})
	if _, err := f.buildRequest("rc-1", "hi", formatOpts{}); err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	got, err := f.parseResponse([]byte(`{"answer":"  trimmed  ","conversation_id":"dify-1"}`))
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if got != "trimmed" {
		t.Errorf("answer should be trimmed, got %q", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if s := f.sessions["rc-1"]; s.convID != "dify-1" {
		t.Errorf("conversation_id not stored, got %+v", s)
	}
}

func TestNanoClawFormat_ParseResponse_FallsBackToRawText(t *testing.T) {
	f := &nanoclawFormat{}

	got, err := f.parseResponse([]byte("just plain text"))
	if err != nil || got != "just plain text" {
		t.Errorf("plain-text fallback: got=%q err=%v", got, err)
	}

	if _, err := f.parseResponse([]byte("   \n  ")); err == nil {
		t.Error("whitespace-only body should fail")
	}

	got, err = f.parseResponse([]byte(`{"reply":"json reply"}`))
	if err != nil || got != "json reply" {
		t.Errorf("json reply: got=%q err=%v", got, err)
	}
}

func TestOpenAIFormat_BuildRequest_SystemAndHistory(t *testing.T) {
	f := &openaiFormat{}
	body, err := f.buildRequest("conv", "hello", formatOpts{
		Model:        "gpt-x",
		SystemPrompt: "be helpful",
		History: []ChatMessage{
			{Role: "user", Content: "older"},
			{Role: "assistant", Content: "older reply"},
		},
	})
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("empty body")
	}
	got, err := f.parseResponse([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
	if err != nil || got != "hi" {
		t.Errorf("parseResponse: got=%q err=%v", got, err)
	}
	if _, err := f.parseResponse([]byte("not json")); err == nil {
		t.Error("invalid JSON should fail")
	}
	if _, err := f.parseResponse([]byte(`{"choices":[]}`)); err == nil {
		t.Error("empty choices should fail")
	}
}
