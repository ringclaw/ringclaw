package messaging

import (
	"context"
	"fmt"
	"testing"

	"github.com/ringclaw/ringclaw/agent"
)

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

		// Should match — video
		{"创建一个视频会议讨论发布计划", true},
		{"帮我开一个明天的 RCV 会议", true},
		{"create a video meeting for release planning", true},
		{"schedule an RCV meeting tomorrow", true},

		// Should match — phone
		{"给 2123753080 打电话", true},
		{"帮我外呼 +12123753080", true},
		{"show my missed calls", true},
		{"list recent call logs", true},

		// Should NOT match — normal chat
		{"hello", false},
		{"what is the weather today", false},
		{"help me write a function", false},
		{"fix the bug in auth.go", false},
		{"review this video file", false},
		{"explain phone number formatting", false},
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
		{"video", IntentVideo},
		{"phone", IntentPhone},
		{"chat", IntentChat},
		{"Chat", IntentChat},
		// Agent may add extra text
		{"The intent is summarize.", IntentSummarize},
		{"I think the intent is task.", IntentTask},
		{"The primary intent is video.", IntentVideo},
		{"The primary intent is phone.", IntentPhone},
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

// classifyIntentAgent is a mock agent that returns a configurable reply for classifyIntent tests.
type classifyIntentAgent struct {
	reply string
	err   error
}

func (a *classifyIntentAgent) Chat(_ context.Context, _, _ string) (string, error) {
	if a.err != nil {
		return "", a.err
	}
	return a.reply, nil
}
func (a *classifyIntentAgent) ResetSession(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (a *classifyIntentAgent) SetCwd(_ string) {}
func (a *classifyIntentAgent) Info() agent.AgentInfo {
	return agent.AgentInfo{Name: "classify-test", Type: "test"}
}

func TestClassifyIntent_Summarize(t *testing.T) {
	ag := &classifyIntentAgent{reply: "summarize"}
	intent := classifyIntent(context.Background(), ag, "总结一下聊天")
	if intent != IntentSummarize {
		t.Errorf("expected IntentSummarize, got %v", intent)
	}
}

func TestClassifyIntent_Task(t *testing.T) {
	ag := &classifyIntentAgent{reply: "task"}
	intent := classifyIntent(context.Background(), ag, "create a task")
	if intent != IntentTask {
		t.Errorf("expected IntentTask, got %v", intent)
	}
}

func TestClassifyIntent_Note(t *testing.T) {
	ag := &classifyIntentAgent{reply: "note"}
	intent := classifyIntent(context.Background(), ag, "create a note")
	if intent != IntentNote {
		t.Errorf("expected IntentNote, got %v", intent)
	}
}

func TestClassifyIntent_Event(t *testing.T) {
	ag := &classifyIntentAgent{reply: "event"}
	intent := classifyIntent(context.Background(), ag, "schedule a meeting")
	if intent != IntentEvent {
		t.Errorf("expected IntentEvent, got %v", intent)
	}
}

func TestClassifyIntent_Video(t *testing.T) {
	ag := &classifyIntentAgent{reply: "video"}
	intent := classifyIntent(context.Background(), ag, "创建一个视频会议")
	if intent != IntentVideo {
		t.Errorf("expected IntentVideo, got %v", intent)
	}
}

func TestClassifyIntent_Phone(t *testing.T) {
	ag := &classifyIntentAgent{reply: "phone"}
	intent := classifyIntent(context.Background(), ag, "call +12123753080")
	if intent != IntentPhone {
		t.Errorf("expected IntentPhone, got %v", intent)
	}
}

func TestClassifyIntent_Chat(t *testing.T) {
	ag := &classifyIntentAgent{reply: "chat"}
	intent := classifyIntent(context.Background(), ag, "hello world")
	if intent != IntentChat {
		t.Errorf("expected IntentChat, got %v", intent)
	}
}

func TestClassifyIntent_UnknownFallsBackToChat(t *testing.T) {
	ag := &classifyIntentAgent{reply: "something random"}
	intent := classifyIntent(context.Background(), ag, "hello")
	if intent != IntentChat {
		t.Errorf("expected IntentChat for unknown reply, got %v", intent)
	}
}

func TestClassifyIntent_ErrorFallsBackToChat(t *testing.T) {
	ag := &classifyIntentAgent{err: fmt.Errorf("agent unavailable")}
	intent := classifyIntent(context.Background(), ag, "hello")
	if intent != IntentChat {
		t.Errorf("expected IntentChat on error, got %v", intent)
	}
}
