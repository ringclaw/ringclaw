package messaging

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ringclaw/ringclaw/ringcentral"
)

func TestIsAudioAttachment_ByExtension(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"voice.m4a", true},
		{"recording.mp3", true},
		{"audio.wav", true},
		{"clip.ogg", true},
		{"track.aac", true},
		{"msg.opus", true},
		{"music.flac", true},
		{"video.webm", true},
		{"photo.png", false},
		{"document.pdf", false},
		{"VOICE.M4A", true}, // case-insensitive
	}
	for _, tc := range cases {
		att := ringcentral.Attachment{Name: tc.name}
		got := isAudioAttachment(att)
		if got != tc.want {
			t.Errorf("isAudioAttachment(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsAudioAttachment_ByMimeType(t *testing.T) {
	cases := []struct {
		mediaType string
		want      bool
	}{
		{"audio/m4a", true},
		{"audio/mpeg", true},
		{"audio/wav", true},
		{"audio/ogg", true},
		{"audio/aac", true},
		{"audio/opus", true},
		{"audio/flac", true},
		{"audio/webm", true},
		{"audio/x-m4a", true},
		{"image/png", false},
		{"application/pdf", false},
		{"", false},
	}
	for _, tc := range cases {
		att := ringcentral.Attachment{Name: "file.bin", MediaType: tc.mediaType}
		got := isAudioAttachment(att)
		if got != tc.want {
			t.Errorf("isAudioAttachment(mediaType=%q) = %v, want %v", tc.mediaType, got, tc.want)
		}
	}
}

func TestInferAudioMimeType(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"voice.m4a", "audio/m4a"},
		{"track.mp3", "audio/mpeg"},
		{"clip.wav", "audio/wav"},
		{"msg.ogg", "audio/ogg"},
		{"file.aac", "audio/aac"},
		{"rec.opus", "audio/opus"},
		{"song.flac", "audio/flac"},
		{"vid.webm", "audio/webm"},
		{"unknown.xyz", "application/octet-stream"},
	}
	for _, tc := range cases {
		got := inferAudioMimeType(tc.name)
		if got != tc.want {
			t.Errorf("inferAudioMimeType(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestHasAudioAttachments(t *testing.T) {
	post := ringcentral.Post{
		Attachments: []ringcentral.Attachment{
			{Name: "photo.png", MediaType: "image/png"},
		},
	}
	if hasAudioAttachments(post) {
		t.Error("expected false for image-only post")
	}

	post.Attachments = append(post.Attachments, ringcentral.Attachment{
		Name: "voice.m4a",
	})
	if !hasAudioAttachments(post) {
		t.Error("expected true when m4a attachment present")
	}
}

func TestExtractAudioAttachments_Success(t *testing.T) {
	audioData := []byte("fake-audio-data")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/m4a")
		w.Write(audioData)
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "bot-token")
	post := ringcentral.Post{
		Attachments: []ringcentral.Attachment{
			{ID: "v1", ContentURI: srv.URL + "/voice.m4a", Name: "voice.m4a"},
		},
	}

	audios := extractAudioAttachments(context.Background(), client, post)
	if len(audios) != 1 {
		t.Fatalf("expected 1 audio, got %d", len(audios))
	}
	if audios[0].Name != "voice.m4a" {
		t.Errorf("expected name voice.m4a, got %q", audios[0].Name)
	}
	if string(audios[0].Data) != string(audioData) {
		t.Errorf("unexpected audio data")
	}
}

func TestExtractAudioAttachments_SkipsNonAudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("img"))
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "bot-token")
	post := ringcentral.Post{
		Attachments: []ringcentral.Attachment{
			{ID: "p1", ContentURI: srv.URL + "/photo.png", Name: "photo.png", MediaType: "image/png"},
			{ID: "d1", ContentURI: srv.URL + "/doc.pdf", Name: "doc.pdf"},
		},
	}

	audios := extractAudioAttachments(context.Background(), client, post)
	if len(audios) != 0 {
		t.Errorf("expected 0 audio attachments, got %d", len(audios))
	}
}

func TestExtractAudioAttachments_MaxLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/m4a")
		w.Write([]byte("data"))
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "bot-token")
	var atts []ringcentral.Attachment
	for i := 0; i < 5; i++ {
		atts = append(atts, ringcentral.Attachment{
			ID:         "a",
			ContentURI: srv.URL + "/voice.m4a",
			Name:       "voice.m4a",
		})
	}
	post := ringcentral.Post{Attachments: atts}

	audios := extractAudioAttachments(context.Background(), client, post)
	if len(audios) != maxAudioAttachments {
		t.Fatalf("expected %d audio (max), got %d", maxAudioAttachments, len(audios))
	}
}

func TestExtractAudioAttachments_SkipsMissingURI(t *testing.T) {
	post := ringcentral.Post{
		Attachments: []ringcentral.Attachment{
			{ID: "v1", ContentURI: "", Name: "voice.m4a"},
		},
	}
	audios := extractAudioAttachments(context.Background(), nil, post)
	if len(audios) != 0 {
		t.Errorf("expected 0 for missing URI, got %d", len(audios))
	}
}

// TestExtractAudioAttachments_DetectedMimeMismatch verifies that an
// attachment whose filename suggests audio but whose actual response
// body is served with a non-audio Content-Type is dropped, rather than
// blindly forwarded to the agent. Mirrors the behavior already in
// extractImageAttachments and protects against mislabeled or hostile
// uploads.
func TestExtractAudioAttachments_DetectedMimeMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>not audio</html>"))
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "bot-token")
	post := ringcentral.Post{
		Attachments: []ringcentral.Attachment{
			{ID: "v1", ContentURI: srv.URL + "/voice.m4a", Name: "voice.m4a"},
		},
	}

	audios := extractAudioAttachments(context.Background(), client, post)
	if len(audios) != 0 {
		t.Errorf("expected 0 audio (MIME mismatch), got %d", len(audios))
	}
}

// TestExtractAudioAttachments_DetectedMimeOverridesClaimed verifies that
// when the server returns an audio Content-Type, the resulting
// AudioAttachment.MediaType reflects the detected value rather than any
// caller-supplied or inferred guess.
func TestExtractAudioAttachments_DetectedMimeOverridesClaimed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/webm")
		w.Write([]byte("opus-bytes"))
	}))
	defer srv.Close()

	client := ringcentral.NewBotClient(srv.URL, "bot-token")
	post := ringcentral.Post{
		Attachments: []ringcentral.Attachment{
			// Caller claims m4a; server says webm. Detected MIME wins.
			{ID: "v1", ContentURI: srv.URL + "/voice.m4a", Name: "voice.m4a", MediaType: "audio/m4a"},
		},
	}

	audios := extractAudioAttachments(context.Background(), client, post)
	if len(audios) != 1 {
		t.Fatalf("expected 1 audio, got %d", len(audios))
	}
	if got := audios[0].MediaType; got != "audio/webm" {
		t.Errorf("expected MediaType=audio/webm (detected), got %q", got)
	}
}
