package messaging

import (
	"testing"
)

func TestHandler_AddTrustedSender_Idempotent(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	h.AddTrustedSender("user-1")
	h.AddTrustedSender("user-1")
	h.AddTrustedSender("user-2")

	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.trustedSenders) != 2 {
		t.Errorf("trustedSenders len=%d, want 2", len(h.trustedSenders))
	}
	if !h.trustedSenders["user-1"] || !h.trustedSenders["user-2"] {
		t.Errorf("missing expected entries: %+v", h.trustedSenders)
	}
}

func TestHandler_AddTrustedSender_RejectsEmpty(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	h.AddTrustedSender("")
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.trustedSenders) != 0 {
		t.Errorf("empty sender should not be added, got %+v", h.trustedSenders)
	}
}

func TestHandler_AddTrustedSender_LazyInit(t *testing.T) {
	// Defense-in-depth: the field is initialized in NewHandler, but
	// AddTrustedSender also lazy-initialises so callers that obtain a
	// Handler via a different path (e.g. tests setting fields by
	// reflection) do not panic.
	h := &Handler{}
	h.AddTrustedSender("u1")
	if !h.trustedSenders["u1"] {
		t.Errorf("expected trustedSenders to be initialised lazily")
	}
}

func TestHandler_EnforceSenderAllowlist_FlipsFlag(t *testing.T) {
	h := NewHandler(nil, nil, "test")
	if !h.allowAllSenders {
		t.Fatal("allowAllSenders should default to true")
	}
	h.EnforceSenderAllowlist()
	if h.allowAllSenders {
		t.Error("EnforceSenderAllowlist should flip allowAllSenders to false")
	}
}

func TestExtractChatID_Variants(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"12345", "12345"},
		{"![:Team](12345)", "12345"},
		{"![:Person](67890)", "67890"},
		{"  ![:Team](42)  ", "42"},
		{"alice", "alice"},
		{"unbalanced(", "unbalanced("}, // no closing paren -> returned as-is
	}
	for _, tc := range cases {
		got := extractChatID(tc.in)
		if got != tc.want {
			t.Errorf("extractChatID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsNumericID_AcceptsDigitsOnly(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"123", true},
		{"0", true},
		{"12a", false},
		{"12 3", false},
		{"-1", false},
	}
	for _, tc := range cases {
		got := isNumericID(tc.in)
		if got != tc.want {
			t.Errorf("isNumericID(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestIsSelfPronoun_RecognisesCommonForms(t *testing.T) {
	want := []string{"我", "me", "MYSELF", "  Me  ", "私", "yo"}
	for _, s := range want {
		if !isSelfPronoun(s) {
			t.Errorf("isSelfPronoun(%q) = false, want true", s)
		}
	}
	notWant := []string{"", "alice", "team", "12345"}
	for _, s := range notWant {
		if isSelfPronoun(s) {
			t.Errorf("isSelfPronoun(%q) = true, want false", s)
		}
	}
}

func TestExactMatch_CaseAndWhitespaceInsensitive(t *testing.T) {
	if !exactMatch("Alice Smith", "alice smith") {
		t.Error("case-insensitive exact should match")
	}
	if !exactMatch(" Bob ", "bob") {
		t.Error("trim should be applied to both sides")
	}
	if exactMatch("alice", "bob") {
		t.Error("different strings should not match")
	}
}

func TestFuzzyMatch_SubstringEitherWay(t *testing.T) {
	if !fuzzyMatch("Alice Smith", "alice") {
		t.Error("haystack should contain needle")
	}
	if !fuzzyMatch("alice", "Alice Smith") {
		t.Error("needle longer than haystack should still match if reversed substring")
	}
	if fuzzyMatch("Alice", "Bob") {
		t.Error("non-matching strings should not match")
	}
	if fuzzyMatch("", "alice") || fuzzyMatch("alice", "") {
		t.Error("empty string inputs should not match")
	}
}
