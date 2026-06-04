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
	if !strings.Contains(got, "To ask another bot/agent in the current group chat") {
		t.Error("ActionPromptTemplate should describe current-chat agent-to-agent mention messaging")
	}
	if !strings.Contains(got, "ready-to-send human message") ||
		!strings.Contains(got, "Do not include assistant framing") {
		t.Error("ActionPromptTemplate should require human-ready MESSAGE bodies")
	}
	// Templates returned to eval scripts are RAW — no time substitution
	// must have been applied since the eval should see the same shape
	// the agent would see at request time.
	if !strings.Contains(got, "ACTION:CARD") {
		t.Error("ActionPromptTemplate should describe ACTION:CARD")
	}
	if !strings.Contains(got, "ACTION:VIDEO_LIST [scope=today|upcoming|recent]") {
		t.Error("ActionPromptTemplate should advertise VIDEO_LIST upcoming scope")
	}
	if !strings.Contains(got, "ACTION:SMS to=<target phone> [from=<owned phone>]") {
		t.Error("ActionPromptTemplate should advertise ACTION:SMS")
	}
	if !strings.Contains(got, "ACTION:VIDEO title=<meeting title> [type=Instant|Scheduled|PMI] [start=<ISO8601> end=<ISO8601>]") {
		t.Error("ActionPromptTemplate should advertise scheduled VIDEO start/end params")
	}
	if !strings.Contains(got, "scope=upcoming for future/upcoming meeting requests") {
		t.Error("ActionPromptTemplate should direct future meeting requests to scope=upcoming")
	}
	if !strings.Contains(got, "type=Scheduled plus start=<ISO8601> and end=<ISO8601>") {
		t.Error("ActionPromptTemplate should direct scheduled video meetings to include start/end")
	}
	if !strings.Contains(got, "SMS/text-message requests") {
		t.Error("ActionPromptTemplate should direct explicit SMS requests to ACTION:SMS")
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
