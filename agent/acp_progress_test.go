package agent

import (
	"context"
	"testing"
)

func TestDescribeToolActivity(t *testing.T) {
	tests := []struct {
		name string
		u    *sessionUpdate
		want string
	}{
		{"nil", nil, ""},
		{
			"server and tool",
			&sessionUpdate{RawInput: []byte(`{"server":"gitlab","tool":"get_issue","arguments":{}}`)},
			"gitlab/get_issue",
		},
		{
			"command hint",
			&sessionUpdate{RawInput: []byte(`{"tool":"bash","arguments":{"command":"git status --short"}}`)},
			"bash · git status --short",
		},
		{
			"path hint",
			&sessionUpdate{RawInput: []byte(`{"tool":"read","arguments":{"path":"/tmp/a b.txt"}}`)},
			"read · /tmp/a b.txt",
		},
		{"title fallback", &sessionUpdate{Title: "Tool: apply_patch"}, "apply_patch"},
		{"empty", &sessionUpdate{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describeToolActivity(tt.u); got != tt.want {
				t.Errorf("describeToolActivity() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDescribeToolActivity_TruncatesLongHint(t *testing.T) {
	u := &sessionUpdate{RawInput: []byte(`{"tool":"bash","arguments":{"command":"echo ` + longString(200) + `"}}`)}
	got := describeToolActivity(u)
	if len([]rune(got)) > 80 {
		t.Errorf("expected hint to be truncated, got %d runes: %q", len([]rune(got)), got)
	}
}

func TestWithProgress_ReportsOnlyWhenSet(t *testing.T) {
	var got []string
	ctx := WithProgress(context.Background(), func(s string) { got = append(got, s) })

	reportProgress(ctx, "bash")
	reportProgress(ctx, "") // empty is ignored
	reportProgress(context.Background(), "ignored")

	if len(got) != 1 || got[0] != "bash" {
		t.Fatalf("unexpected reports: %#v", got)
	}
}

func TestWithProgress_NilCallbackIsSafe(t *testing.T) {
	ctx := WithProgress(context.Background(), nil)
	// Must not panic.
	reportProgress(ctx, "anything")
}

func longString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
