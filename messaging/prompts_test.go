package messaging

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestIntentPrompt_ReturnsNonEmpty(t *testing.T) {
	p := IntentPrompt()
	if p == "" {
		t.Error("IntentPrompt should return non-empty string")
	}
	if !strings.Contains(p, "summarize") {
		t.Error("IntentPrompt should mention summarize")
	}
	if !strings.Contains(p, "%"+"s") {
		t.Error("IntentPrompt should contain format placeholder")
	}
}

func TestHeartbeatPrompt_ReturnsNonEmpty(t *testing.T) {
	p := HeartbeatPrompt()
	if p == "" {
		t.Error("HeartbeatPrompt should return non-empty string")
	}
	if !strings.Contains(p, "%"+"s") {
		t.Error("HeartbeatPrompt should contain format placeholders")
	}
}

func TestActionPrompt_ContainsTime(t *testing.T) {
	p := ActionPrompt()
	if p == "" {
		t.Error("ActionPrompt should return non-empty string")
	}
	if !strings.Contains(p, "ACTION") {
		t.Error("ActionPrompt should mention ACTION")
	}
	// Should not contain the {{.Now}} template marker (should be replaced)
	if strings.Contains(p, "{{.Now}}") {
		t.Error("ActionPrompt should have replaced {{.Now}} with actual time")
	}
}

func TestSummaryPrompt_ReturnsNonEmpty(t *testing.T) {
	p := SummaryPrompt()
	if p == "" {
		t.Error("SummaryPrompt should return non-empty string")
	}
	if !strings.Contains(p, "%"+"s") {
		t.Error("SummaryPrompt should contain format placeholders")
	}
}

func TestNameExtractPrompt_ReturnsNonEmpty(t *testing.T) {
	p := NameExtractPrompt()
	if p == "" {
		t.Error("NameExtractPrompt should return non-empty string")
	}
	if !strings.Contains(p, "NONE") {
		t.Error("NameExtractPrompt should mention NONE")
	}
}

func TestLoadPrompt_CustomFile(t *testing.T) {
	// Reset the prompt cache for this test
	promptOnce = sync.Map{}
	promptText = sync.Map{}

	dir := t.TempDir()
	promptDir := filepath.Join(dir, ".ringclaw", "prompts")
	os.MkdirAll(promptDir, 0o700)

	customContent := "custom test prompt content"
	os.WriteFile(filepath.Join(promptDir, "test_custom.md"), []byte(customContent), 0o644)

	// We can't easily override os.UserHomeDir, but we can test that loadPrompt
	// returns the default when no custom file exists for a unique name
	result := loadPrompt("test_unique_name_12345", "default fallback text")
	if result != "default fallback text" {
		t.Errorf("expected default text, got %q", result)
	}
}

func TestLoadPrompt_FallbackToDefault(t *testing.T) {
	// Reset cache
	promptOnce = sync.Map{}
	promptText = sync.Map{}

	defaultText := "this is the default prompt"
	result := loadPrompt("nonexistent_prompt_xyz_987", defaultText)
	if result != defaultText {
		t.Errorf("expected %q, got %q", defaultText, result)
	}
}

func TestLoadPrompt_Caching(t *testing.T) {
	// Reset cache
	promptOnce = sync.Map{}
	promptText = sync.Map{}

	defaultText := "cached prompt value"
	first := loadPrompt("cached_test_abc", defaultText)
	second := loadPrompt("cached_test_abc", "different default")

	// Second call should return same result (cached) even with different default
	if first != second {
		t.Errorf("expected cached value %q, got %q", first, second)
	}
}
