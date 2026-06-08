package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ringclaw/ringclaw/ringcentral"
)

func TestRuntimeInteractiveDecisionCard_ApproveRemovesActions(t *testing.T) {
	card := runtimeInteractiveDecisionCard(runtimeInteractiveEvent{
		ID:             "evt-1",
		UserID:         "20762292004",
		UserFirstName:  "Andrew",
		UserLastName:   "Wenner",
		EventTimestamp: "2026-06-06T08:51:05Z",
		Data: map[string]any{
			"action":        "approve",
			"rx_id":         "RX-20260606-AX2847",
			"patient_id":    "AX-2847",
			"medication":    "Sertraline 100mg",
			"provider_name": "Andrew Wenner",
		},
	})
	if len(card) == 0 {
		t.Fatal("expected decision card")
	}
	var decoded map[string]any
	if err := json.Unmarshal(card, &decoded); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	if _, ok := decoded["actions"]; ok {
		t.Fatalf("decision card must not keep submit actions: %s", string(card))
	}
	body, _ := json.Marshal(decoded["body"])
	text := string(body)
	for _, want := range []string{"Refill approved", "Approved by Andrew Wenner", "RX-20260606-AX2847", "AX-2847", "Sertraline 100mg"} {
		if !strings.Contains(text, want) {
			t.Fatalf("decision card missing %q: %s", want, string(card))
		}
	}
}

func TestRuntimeInteractiveDecisionCard_UnknownActionSkipped(t *testing.T) {
	card := runtimeInteractiveDecisionCard(runtimeInteractiveEvent{
		Data: map[string]any{"action": ""},
	})
	if len(card) != 0 {
		t.Fatalf("expected no card for empty action, got %s", string(card))
	}
}

func TestRuntimeInteractiveDecisionCard_CoverageConfirmUsesCoverageTemplate(t *testing.T) {
	card := runtimeInteractiveDecisionCard(runtimeInteractiveEvent{
		ID:             "evt-coverage-1",
		UserID:         "86468608003",
		UserFirstName:  "Jennifer",
		UserLastName:   "S.",
		EventTimestamp: "2026-06-08T08:11:38Z",
		Data: map[string]any{
			"action":        "coverage_confirm",
			"coverage_id":   "cov-20260608-001",
			"candidate_name": "Jennifer S.",
			"date":          "2026-06-08",
			"shift":         "Day shift",
			"workload":      "Refill queue, provider follow-up, SMS follow-up",
		},
	})
	if len(card) == 0 {
		t.Fatal("expected coverage decision card")
	}
	body := string(card)
	for _, want := range []string{
		"Coverage confirmed",
		"cov-20260608-001",
		"Jennifer S.",
		"Day shift",
		"Transferred workload",
		"Runtime will continue the coverage workflow",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("coverage card missing %q: %s", want, body)
		}
	}
	for _, unwanted := range []string{"Refill decision recorded", "Patient", "Medication"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("coverage card should not contain %q: %s", unwanted, body)
		}
	}
}

func TestRuntimeInteractiveDecisionCard_CoverageFollowupUsesCoverageTemplate(t *testing.T) {
	card := runtimeInteractiveDecisionCard(runtimeInteractiveEvent{
		UserFirstName: "Jennifer",
		UserLastName:  "S.",
		Data: map[string]any{
			"action":         "coverage_followup",
			"coverage_id":    "cov-20260608-002",
			"candidate_name": "Jennifer S.",
		},
	})
	if len(card) == 0 {
		t.Fatal("expected coverage follow-up card")
	}
	body := string(card)
	if !strings.Contains(body, "Coverage follow-up requested") {
		t.Fatalf("expected coverage follow-up header, got %s", body)
	}
	if strings.Contains(body, "Refill") {
		t.Fatalf("coverage follow-up card should not use refill copy: %s", body)
	}
}

func TestRuntimeInteractiveEventText_CoverageAliases(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		want string
	}{
		{
			name: "explicit action",
			data: map[string]any{"action": "coverage_confirm"},
			want: "coverage_confirm",
		},
		{
			name: "response accept",
			data: map[string]any{"response": "accept"},
			want: "coverage_confirm",
		},
		{
			name: "response coverage confirm",
			data: map[string]any{"response": "coverage_confirm"},
			want: "coverage_confirm",
		},
		{
			name: "coverage response accept full day",
			data: map[string]any{"coverage_response": "accept_full_day"},
			want: "coverage_confirm",
		},
		{
			name: "intent coverage dot confirm",
			data: map[string]any{"intent": "coverage.confirm"},
			want: "coverage_confirm",
		},
		{
			name: "decline alias",
			data: map[string]any{"coverage_response": "decline_full_day"},
			want: "coverage_decline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runtimeInteractiveEventText(runtimeInteractiveEvent{Data: tt.data})
			if got != tt.want {
				t.Fatalf("runtimeInteractiveEventText() = %q, want %q", got, tt.want)
			}
			if action := runtimeInteractiveAction(runtimeInteractiveEvent{Data: tt.data}); action != tt.want {
				t.Fatalf("runtimeInteractiveAction() = %q, want %q", action, tt.want)
			}
		})
	}
}
func TestRuntimeInteractiveExecutorID_PrefersPrivateOwner(t *testing.T) {
	bot := ringcentral.NewBotClient("", "bot-token")
	bot.SetOwnerID("bot-owner")
	private := ringcentral.NewClient(&ringcentral.Credentials{})
	private.SetOwnerID("private-owner")

	got := runtimeInteractiveExecutorID(&clients{bot: bot, private: private}, runtimeInteractiveEvent{UserID: "submitter"})
	if got != "private-owner" {
		t.Fatalf("executor ID = %q, want private owner", got)
	}
}

func TestRuntimeInteractiveExecutorID_FallsBackToBotOwner(t *testing.T) {
	bot := ringcentral.NewBotClient("", "bot-token")
	bot.SetOwnerID("bot-owner")

	got := runtimeInteractiveExecutorID(&clients{bot: bot}, runtimeInteractiveEvent{UserID: "submitter"})
	if got != "bot-owner" {
		t.Fatalf("executor ID = %q, want bot owner", got)
	}
}

func TestRuntimeInteractiveExecutorID_FallsBackToSubmitter(t *testing.T) {
	got := runtimeInteractiveExecutorID(nil, runtimeInteractiveEvent{UserID: "submitter"})
	if got != "submitter" {
		t.Fatalf("executor ID = %q, want submitter", got)
	}
}
