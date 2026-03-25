package messaging

import (
	"testing"
	"time"
)

func TestParseCommand_NoSlash(t *testing.T) {
	name, msg := parseCommand("hello world")
	if name != "" {
		t.Errorf("expected empty name, got %q", name)
	}
	if msg != "hello world" {
		t.Errorf("expected full text, got %q", msg)
	}
}

func TestParseCommand_WithAgent(t *testing.T) {
	name, msg := parseCommand("/claude explain this code")
	if name != "claude" {
		t.Errorf("expected claude, got %q", name)
	}
	if msg != "explain this code" {
		t.Errorf("expected 'explain this code', got %q", msg)
	}
}

func TestParseCommand_SwitchOnly(t *testing.T) {
	name, msg := parseCommand("/claude")
	if name != "claude" {
		t.Errorf("expected claude, got %q", name)
	}
	if msg != "" {
		t.Errorf("expected empty message, got %q", msg)
	}
}

func TestParseCommand_Alias(t *testing.T) {
	name, msg := parseCommand("/cc write a function")
	if name != "claude" {
		t.Errorf("expected claude from /cc alias, got %q", name)
	}
	if msg != "write a function" {
		t.Errorf("expected 'write a function', got %q", msg)
	}
}

func TestResolveAlias(t *testing.T) {
	tests := map[string]string{
		"cc":  "claude",
		"cx":  "codex",
		"oc":  "openclaw",
		"cs":  "cursor",
		"km":  "kimi",
		"gm":  "gemini",
		"ocd": "opencode",
		"pi":  "pi",
		"cp":  "copilot",
		"dr":  "droid",
		"if":  "iflow",
		"kr":  "kiro",
		"qw":  "qwen",
	}
	for alias, want := range tests {
		got := resolveAlias(alias)
		if got != want {
			t.Errorf("resolveAlias(%q) = %q, want %q", alias, got, want)
		}
	}
	// Unknown alias returns itself
	if got := resolveAlias("unknown"); got != "unknown" {
		t.Errorf("resolveAlias(unknown) = %q, want %q", got, "unknown")
	}
}

func TestWrapAnswer(t *testing.T) {
	got := wrapAnswer("hello")
	if got != "--------answer--------\nhello\n---------end----------" {
		t.Errorf("unexpected wrap: %q", got)
	}
}

func TestBuildHelpText(t *testing.T) {
	text := buildHelpText()
	if text == "" {
		t.Error("help text is empty")
	}
	if !containsStr(text, "/status") {
		t.Error("help text should mention /status")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{90 * time.Second, "1m 30s"},
		{2*time.Hour + 15*time.Minute, "2h 15m 0s"},
		{25*time.Hour + 30*time.Minute, "1d 1h 30m"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestInferMediaType(t *testing.T) {
	tests := []struct {
		name, want string
	}{
		{"photo.png", "image/png"},
		{"photo.PNG", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"photo.gif", "image/gif"},
		{"photo.webp", "image/webp"},
		{"document.pdf", ""},
		{"file.txt", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := inferMediaType(tt.name); got != tt.want {
			t.Errorf("inferMediaType(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestImageMediaTypes(t *testing.T) {
	supported := []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/jpg"}
	for _, mt := range supported {
		if !imageMediaTypes[mt] {
			t.Errorf("expected %q to be supported", mt)
		}
	}
	unsupported := []string{"image/bmp", "application/pdf", "text/plain", ""}
	for _, mt := range unsupported {
		if imageMediaTypes[mt] {
			t.Errorf("expected %q to NOT be supported", mt)
		}
	}
}
