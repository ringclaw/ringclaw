package messaging

import "testing"

func TestMatchesIntentTrigger(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		// Should match — summarize
		{"总结一下今天的聊天", true},
		{"帮我总结下昨天讨论的内容", true},
		{"summarize the chat from today", true},
		{"can you give me a summary?", true},
		{"recap of yesterday's meeting", true},
		{"给个摘要", true},
		{"まとめてください", true},
		{"дайте итог", true},
		{"resumen de hoy", true},

		// Should match — task
		{"创建任务 修复bug", true},
		{"add task fix the login issue", true},
		{"帮我加个任务", true},
		{"create task for John", true},

		// Should match — note
		{"创建笔记 会议纪要", true},
		{"add note about the design", true},
		{"记一下这个方案", true},

		// Should match — event
		{"创建日程 下周一开会", true},
		{"schedule a meeting", true},
		{"add event tomorrow 3pm", true},

		// Should NOT match — normal chat
		{"hello", false},
		{"what is the weather today", false},
		{"help me write a function", false},
		{"fix the bug in auth.go", false},
		{"/cron list", false},
		{"/task create something", false},
		{"how are you", false},
		{"explain this code", false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := matchesIntentTrigger(tt.text); got != tt.want {
				t.Errorf("matchesIntentTrigger(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestIsSummarizeKeyword_FastPath(t *testing.T) {
	// These should be caught by the fast-path (prefix match) and skip AI classification
	fastPathCases := []struct {
		text string
		want bool
	}{
		{"总结 maxwell 并用 note 发给他", true},
		{"总结 昨天跟 maxwell 的聊天并用 note 发给他", true},
		{"summarize john and then create a note", true},
		{"summary of alice and also send to her", true},
		{"总结一下跟张三的聊天", true},
		// Should NOT match — not summarize prefix
		{"帮我创建笔记 关于会议", false},
		{"create task for John", false},
		{"hello summarize", false}, // "summarize" is not at the start
	}
	for _, tt := range fastPathCases {
		t.Run(tt.text, func(t *testing.T) {
			if got := isSummarizeKeyword(tt.text); got != tt.want {
				t.Errorf("isSummarizeKeyword(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestParseIntentReply(t *testing.T) {
	tests := []struct {
		reply string
		want  Intent
	}{
		{"summarize", IntentSummarize},
		{"summary", IntentSummarize},
		{"Summarize", IntentSummarize},
		{"task", IntentTask},
		{"Task", IntentTask},
		{"note", IntentNote},
		{"event", IntentEvent},
		{"chat", IntentChat},
		{"Chat", IntentChat},
		// Agent may add extra text
		{"The intent is summarize.", IntentSummarize},
		{"I think the intent is task.", IntentTask},
		{"This is a normal chat message.", IntentChat},
		// Unrecognized -> chat
		{"", IntentChat},
		{"unknown", IntentChat},
		{"hello world", IntentChat},
	}

	for _, tt := range tests {
		t.Run(tt.reply, func(t *testing.T) {
			if got := parseIntentReply(tt.reply); got != tt.want {
				t.Errorf("parseIntentReply(%q) = %v, want %v", tt.reply, got, tt.want)
			}
		})
	}
}
