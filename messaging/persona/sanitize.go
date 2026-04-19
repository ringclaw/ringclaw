// Package persona implements the SOUL + layered MEMORY mechanism that
// lets RingClaw inject a stable context banner in front of every user
// message. The banner is assembled from files on disk so switching
// agents, resetting sessions, or restarting the bot never wipes the
// operator's carefully curated persona and memory.
//
// All file paths the package touches are built from sanitized IDs so
// hostile chat or user IDs cannot escape the configured memory
// directory via path traversal. See SanitizeID for the exact rules.
package persona

import "strings"

// maxSanitizedIDLen bounds the length of an ID used as a filename so
// that a single misbehaving client cannot produce an arbitrarily long
// on-disk path. RingCentral IDs are short numeric strings in practice;
// 64 leaves ample headroom without approaching any filesystem limit.
const maxSanitizedIDLen = 64

// SanitizeID turns an arbitrary chat or user ID into a safe filename
// component. Any character outside [A-Za-z0-9_-] is replaced with a
// single underscore, the result is truncated to maxSanitizedIDLen, and
// an empty input becomes "_" so callers always get a non-empty,
// filesystem-safe slug.
//
// The goal is path-traversal containment, not reversibility — two
// different raw IDs may map to the same sanitized form (extremely
// unlikely given the sanitizing charset vs. RingCentral's numeric
// IDs, but still possible). Callers that need strict uniqueness
// should combine the sanitized form with a hash, not rely on this
// function alone.
func SanitizeID(id string) string {
	if id == "" {
		return "_"
	}
	var b strings.Builder
	b.Grow(len(id))
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "_"
	}
	if len(out) > maxSanitizedIDLen {
		out = out[:maxSanitizedIDLen]
	}
	return out
}

// IsSafeID reports whether s would pass through SanitizeID unchanged.
// Useful for tests and for callers that want to reject (rather than
// silently rewrite) IDs containing hostile characters.
func IsSafeID(s string) bool {
	if s == "" || len(s) > maxSanitizedIDLen {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '-':
			// ok
		default:
			return false
		}
	}
	return true
}
