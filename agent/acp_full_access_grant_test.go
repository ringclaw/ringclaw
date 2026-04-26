package agent

import "testing"

// TestFullAccessGrantSource_DynamicToggle confirms isFullAccessGranted
// reflects the installed callback. This is the hook the OOB Phase 2
// /full-access flow uses to flip already-running ACP agents into
// full-access mode for the duration of the grant.
func TestFullAccessGrantSource_DynamicToggle(t *testing.T) {
	t.Cleanup(func() { SetFullAccessGrantSource(nil) })

	if isFullAccessGranted() {
		t.Fatalf("default state should be ungranted")
	}

	var on bool
	SetFullAccessGrantSource(func() bool { return on })

	if isFullAccessGranted() {
		t.Fatalf("source returns false → granted should be false")
	}
	on = true
	if !isFullAccessGranted() {
		t.Fatalf("source returns true → granted should be true")
	}
	on = false
	if isFullAccessGranted() {
		t.Fatalf("source returns false again → granted should be false")
	}

	SetFullAccessGrantSource(nil)
	if isFullAccessGranted() {
		t.Fatalf("after clearing source, granted must be false")
	}
}
