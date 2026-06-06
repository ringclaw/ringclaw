package cmd

import (
	"encoding/json"
	"strings"
	"testing"
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
