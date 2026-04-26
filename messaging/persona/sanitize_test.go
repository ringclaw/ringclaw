package persona

import (
	"strings"
	"testing"
)

func TestSanitizeID_BasicNumeric(t *testing.T) {
	// RingCentral IDs are numeric in practice — they must pass through
	// untouched.
	got := SanitizeID("1234567890")
	if got != "1234567890" {
		t.Errorf("SanitizeID(\"1234567890\") = %q, want %q", got, "1234567890")
	}
}

func TestSanitizeID_Empty(t *testing.T) {
	// Empty input is rewritten to "_" so callers always get a
	// non-empty filesystem slug. Multiple empty IDs collide on
	// purpose — callers are expected not to feed empty IDs in the
	// first place (Monitor already filters empty CreatorID).
	if got := SanitizeID(""); got != "_" {
		t.Errorf("SanitizeID(\"\") = %q, want \"_\"", got)
	}
}

func TestSanitizeID_PathTraversalAttempts(t *testing.T) {
	// Each of these, if passed to filepath.Join unsanitized, would
	// escape the memory directory. After sanitization they must
	// stay within the allowed charset.
	attacks := []string{
		"../etc/passwd",
		"..",
		"../../root",
		"/absolute/path",
		"chat/../../other",
		"chat\x00../etc",
		"foo/bar",
		"foo\\bar",
	}
	for _, raw := range attacks {
		got := SanitizeID(raw)
		if strings.ContainsAny(got, "/\\.\x00") {
			t.Errorf("SanitizeID(%q) = %q still contains path chars", raw, got)
		}
		if got == "" {
			t.Errorf("SanitizeID(%q) returned empty", raw)
		}
	}
}

func TestSanitizeID_UnicodeReplacement(t *testing.T) {
	// Unicode is not in the allowed charset; every non-ASCII rune
	// becomes "_". The result must still be non-empty and ASCII-only.
	got := SanitizeID("ä用户名123")
	if got == "" {
		t.Fatal("unicode input produced empty slug")
	}
	for _, r := range got {
		if r > 127 {
			t.Errorf("SanitizeID(unicode) = %q contains non-ASCII rune %q", got, r)
		}
	}
}

func TestSanitizeID_Truncation(t *testing.T) {
	// Inputs longer than the cap are truncated, not rejected.
	long := strings.Repeat("a", maxSanitizedIDLen*2)
	got := SanitizeID(long)
	if len(got) != maxSanitizedIDLen {
		t.Errorf("SanitizeID(long) len = %d, want %d", len(got), maxSanitizedIDLen)
	}
}

func TestSanitizeID_AllReplacedCollapsesToUnderscores(t *testing.T) {
	// An all-replaced input is still filesystem-safe — it becomes a
	// string of underscores, which is a legal filename.
	got := SanitizeID("///")
	if got != "___" {
		t.Errorf("SanitizeID(\"///\") = %q, want \"___\"", got)
	}
}

func TestIsSafeID(t *testing.T) {
	cases := map[string]bool{
		"1234567890":      true,
		"alice-bob_42":    true,
		"":                false,
		"../x":            false,
		"a/b":             false,
		"with space":      false,
		"ä":               false,
		strings.Repeat("a", maxSanitizedIDLen):   true,
		strings.Repeat("a", maxSanitizedIDLen+1): false,
	}
	for s, want := range cases {
		if got := IsSafeID(s); got != want {
			t.Errorf("IsSafeID(%q) = %v, want %v", s, got, want)
		}
	}
}
