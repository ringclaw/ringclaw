package agent

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestAppendMediaEntry_AppendsBase64Encoded(t *testing.T) {
	entries := []promptEntry{{Type: "text", Text: "hello"}}
	data := []byte("binary-content")
	got := appendMediaEntry(entries, "image", data, "image/png")

	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].Type != "text" || got[0].Text != "hello" {
		t.Errorf("first entry mutated: %+v", got[0])
	}
	media := got[1]
	if media.Type != "image" {
		t.Errorf("media type = %q, want image", media.Type)
	}
	if media.MimeType != "image/png" {
		t.Errorf("mime type = %q, want image/png", media.MimeType)
	}
	want := base64.StdEncoding.EncodeToString(data)
	if media.Data != want {
		t.Errorf("Data = %q, want %q", media.Data, want)
	}
	if media.Text != "" {
		t.Errorf("Text on media entry should be empty, got %q", media.Text)
	}
}

func TestAppendMediaEntry_EmptyDataStillEncodes(t *testing.T) {
	got := appendMediaEntry(nil, "audio", nil, "audio/m4a")
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0].Type != "audio" || got[0].MimeType != "audio/m4a" {
		t.Errorf("unexpected entry: %+v", got[0])
	}
	if got[0].Data != "" {
		t.Errorf("empty data should encode to empty string, got %q", got[0].Data)
	}
}

func TestAppendMediaEntry_PreservesUnrelatedFields(t *testing.T) {
	// The function must not touch fields like Text on the appended
	// entry — only Data / MimeType / Type are populated.
	got := appendMediaEntry(nil, "image", []byte("x"), "image/jpeg")
	if got[0].Text != "" {
		t.Errorf("Text on media entry should remain empty, got %q", got[0].Text)
	}
	// Round-trip the base64 to verify integrity.
	dec, err := base64.StdEncoding.DecodeString(got[0].Data)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if string(dec) != "x" {
		t.Errorf("decoded = %q, want %q", string(dec), "x")
	}
}

func TestAppendMediaEntry_LongDataKeepsRoundTrip(t *testing.T) {
	long := []byte(strings.Repeat("a", 4096))
	got := appendMediaEntry(nil, "image", long, "image/png")
	dec, err := base64.StdEncoding.DecodeString(got[0].Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(dec) != len(long) {
		t.Errorf("len mismatch: got %d, want %d", len(dec), len(long))
	}
}
