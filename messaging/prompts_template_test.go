package messaging

import (
	"strings"
	"testing"
)

func TestIntentPromptTemplate_MatchesDefault(t *testing.T) {
	got := IntentPromptTemplate()
	if got != defaultIntentPrompt {
		t.Errorf("IntentPromptTemplate diverged from defaultIntentPrompt")
	}
	if !strings.Contains(got, "summarize") {
		t.Error("IntentPromptTemplate should mention summarize keyword")
	}
}

func TestNameExtractPromptTemplate_MatchesDefault(t *testing.T) {
	got := NameExtractPromptTemplate()
	if got != defaultNameExtractPrompt {
		t.Errorf("NameExtractPromptTemplate diverged from defaultNameExtractPrompt")
	}
	if !strings.Contains(got, "NONE") {
		t.Error("NameExtractPromptTemplate should mention NONE sentinel")
	}
}

func TestActionPromptTemplate_MatchesDefault(t *testing.T) {
	got := ActionPromptTemplate()
	if got != defaultActionPrompt {
		t.Errorf("ActionPromptTemplate diverged from defaultActionPrompt")
	}
	if !strings.Contains(got, "ACTION:MESSAGE") {
		t.Error("ActionPromptTemplate should describe ACTION:MESSAGE")
	}
	// Templates returned to eval scripts are RAW — no time substitution
	// must have been applied since the eval should see the same shape
	// the agent would see at request time.
	if !strings.Contains(got, "ACTION:CARD") {
		t.Error("ActionPromptTemplate should describe ACTION:CARD")
	}
}

func TestDateExtractPrompt_ReturnsNonEmpty(t *testing.T) {
	got := DateExtractPrompt()
	if got == "" {
		t.Error("DateExtractPrompt should be non-empty")
	}
	if !strings.Contains(got, "%s") {
		t.Error("DateExtractPrompt should contain a format placeholder")
	}
}
