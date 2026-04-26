package messaging

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// audioExtensions lists the file extensions treated as voice/audio messages.
var audioExtensions = map[string]bool{
	".m4a":  true,
	".mp3":  true,
	".wav":  true,
	".ogg":  true,
	".aac":  true,
	".opus": true,
	".flac": true,
	".webm": true,
}

// audioMimeTypes lists MIME types treated as voice/audio content.
// Includes the common "audio/x-*" aliases and "video/webm" because some
// WebM containers carry Opus audio; we still emit the canonical form
// from inferAudioMimeType, this map is only used for recognition.
var audioMimeTypes = map[string]bool{
	"audio/m4a":    true,
	"audio/mp4":    true,
	"audio/mpeg":   true,
	"audio/wav":    true,
	"audio/ogg":    true,
	"audio/aac":    true,
	"audio/opus":   true,
	"audio/flac":   true,
	"audio/webm":   true,
	"audio/x-m4a":  true,
	"audio/x-wav":  true,
	"audio/x-flac": true,
	"audio/x-ogg":  true,
	"video/webm":   true,
}

// isAudioAttachment reports whether an attachment is an audio/voice file.
func isAudioAttachment(att ringcentral.Attachment) bool {
	if att.MediaType != "" && audioMimeTypes[att.MediaType] {
		return true
	}
	ext := strings.ToLower(filepath.Ext(att.Name))
	return audioExtensions[ext]
}

// inferAudioMimeType returns a MIME type based on the file extension.
func inferAudioMimeType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".m4a":
		return "audio/m4a"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	case ".aac":
		return "audio/aac"
	case ".opus":
		return "audio/opus"
	case ".flac":
		return "audio/flac"
	case ".webm":
		return "audio/webm"
	default:
		return "application/octet-stream"
	}
}

// maxAudioAttachments caps how many audio files we forward to the agent
// per post. Voice messages are normally a single clip per send, and a
// raw m4a is much larger than a typical screenshot, so we keep the cap
// tight. Bump deliberately if a use case demands it.
const maxAudioAttachments = 1

// extractAudioAttachments downloads audio attachments from a post.
// Mirrors extractImageAttachments in media.go: the response Content-Type
// is re-checked against audioMimeTypes after download so a mislabeled
// (or maliciously named) attachment whose actual bytes are not audio is
// dropped before being forwarded to the agent.
func extractAudioAttachments(ctx context.Context, client *ringcentral.Client, post ringcentral.Post) []agent.AudioAttachment {
	var audios []agent.AudioAttachment
	for _, att := range post.Attachments {
		if len(audios) >= maxAudioAttachments {
			break
		}
		if att.ContentURI == "" {
			continue
		}
		if !isAudioAttachment(att) {
			continue
		}
		data, detectedMT, err := client.DownloadAttachment(ctx, att.ContentURI)
		if err != nil {
			slog.Error("voice: failed to download audio attachment",
				"component", "voice", "id", att.ID, "name", att.Name, "error", err)
			continue
		}
		// Re-check the actual content type returned by the server. Skip
		// when the server reports a non-audio type, mirroring the
		// behavior of extractImageAttachments.
		if detectedMT != "" && !audioMimeTypes[detectedMT] {
			slog.Warn("voice: detected MIME does not match audio types, skipping",
				"component", "voice", "id", att.ID, "name", att.Name, "detected", detectedMT)
			continue
		}
		mt := att.MediaType
		if detectedMT != "" {
			mt = detectedMT
		} else if mt == "" {
			mt = inferAudioMimeType(att.Name)
		}
		audios = append(audios, agent.AudioAttachment{
			Data:      data,
			MediaType: mt,
			Name:      att.Name,
		})
		slog.Info("downloaded audio attachment",
			"component", "voice", "id", att.ID, "name", att.Name, "bytes", len(data))
	}
	return audios
}

// hasAudioAttachments reports whether a post contains any audio attachments
// without downloading them. Used for fast early-exit checks.
func hasAudioAttachments(post ringcentral.Post) bool {
	for _, att := range post.Attachments {
		if isAudioAttachment(att) {
			return true
		}
	}
	return false
}
