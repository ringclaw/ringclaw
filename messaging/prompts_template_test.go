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
	if !strings.Contains(got, "ACTION:MESSAGE [chatid=<name or chat ID>] [to_role_id=<AgentsMesh role id>]") ||
		!strings.Contains(got, "RingClaw will choose the configured shared chat and prepend the target bot mention") {
		t.Error("ActionPromptTemplate should describe visible role-targeted bot-to-bot messages")
	}
	if !strings.Contains(got, "ACTION:MESH_TASK") ||
		!strings.Contains(got, "to_role_id is optional when exactly one delegated role can handle the intent") ||
		!strings.Contains(got, "Do not send an ACTION:MESSAGE to #admin") ||
		!strings.Contains(got, "intent=coverage.transfer") ||
		!strings.Contains(got, "absence_coverage_request") {
		t.Error("ActionPromptTemplate should describe Agent Mesh delegation")
	}
	if strings.Contains(got, "role-nursecoord-bot") {
		t.Error("ActionPromptTemplate should not hardcode a specific AgentsMesh role")
	}
	if !strings.Contains(got, "To ask another bot/agent in the current group chat") {
		t.Error("ActionPromptTemplate should describe current-chat agent-to-agent mention messaging")
	}
	if !strings.Contains(got, "current-group bot-to-bot collaboration") ||
		!strings.Contains(got, "keep that collaborator mention at the start of the body") {
		t.Error("ActionPromptTemplate should describe bot-to-bot relay mention behavior")
	}
	if !strings.Contains(got, "ready-to-send human message") ||
		!strings.Contains(got, "Do not include assistant framing") {
		t.Error("ActionPromptTemplate should require human-ready MESSAGE bodies")
	}
	if !strings.Contains(got, "For bot-to-bot collaboration requests in a group chat, do not stop at a draft") {
		t.Error("ActionPromptTemplate should forbid draft-only bot-to-bot replies")
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
	if !strings.Contains(got, "ACTION:SMS to=<target phone or person/contact name> [from=<owned phone>]") {
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
	if !strings.Contains(got, "there is no separate CONTACT action") {
		t.Error("ActionPromptTemplate should explain contact lookup through SMS/phone actions")
	}
	if !strings.Contains(got, "card-like notifications") || !strings.Contains(got, "not ACTION:TASK") {
		t.Error("ActionPromptTemplate should prevent card-like notifications from becoming tasks")
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
