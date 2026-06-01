package ringcentral

import "testing"

func TestTeamMessagingEndpoints(t *testing.T) {
	if got := teamMessagingChatPostsEndpoint("chat-1"); got != "/team-messaging/v1/chats/chat-1/posts" {
		t.Fatalf("teamMessagingChatPostsEndpoint = %q", got)
	}
	if got := teamMessagingChatPostEndpoint("chat-1", "post-1"); got != "/team-messaging/v1/chats/chat-1/posts/post-1" {
		t.Fatalf("teamMessagingChatPostEndpoint = %q", got)
	}
}

func TestVideoEndpoints(t *testing.T) {
	if got := videoBridgesEndpoint(); got != "/rcvideo/v2/account/~/extension/~/bridges" {
		t.Fatalf("videoBridgesEndpoint = %q", got)
	}
	if got := videoBridgeEndpoint("bridge-1"); got != "/rcvideo/v2/bridges/bridge-1" {
		t.Fatalf("videoBridgeEndpoint = %q", got)
	}
	if got := videoHistoryMeetingsEndpoint(VideoMeetingHistoryOptions{Type: "All", PerPage: 20, PageToken: "p2"}); got != "/rcvideo/v1/history/meetings?pageToken=p2&perPage=20&type=All" {
		t.Fatalf("videoHistoryMeetingsEndpoint = %q", got)
	}
}

func TestPhoneEndpoints(t *testing.T) {
	if got := ringOutsEndpoint(); got != "/restapi/v1.0/account/~/extension/~/ring-out" {
		t.Fatalf("ringOutsEndpoint = %q", got)
	}
	if got := smsEndpoint(); got != "/restapi/v1.0/account/~/extension/~/sms" {
		t.Fatalf("smsEndpoint = %q", got)
	}
	if got := ringOutEndpoint("ringout-1"); got != "/restapi/v1.0/account/~/extension/~/ring-out/ringout-1" {
		t.Fatalf("ringOutEndpoint = %q", got)
	}
	if got := extensionCallLogEndpoint(""); got != "/restapi/v1.0/account/~/extension/~/call-log" {
		t.Fatalf("extensionCallLogEndpoint = %q", got)
	}
	if got := extensionCallLogEndpoint("user-1"); got != "/restapi/v1.0/account/~/extension/user-1/call-log" {
		t.Fatalf("extensionCallLogEndpoint with extension = %q", got)
	}
}
