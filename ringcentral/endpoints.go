package ringcentral

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func teamMessagingChatPostsEndpoint(chatID string) string {
	return fmt.Sprintf("/team-messaging/v1/chats/%s/posts", chatID)
}

func teamMessagingChatPostEndpoint(chatID, postID string) string {
	return fmt.Sprintf("%s/%s", teamMessagingChatPostsEndpoint(chatID), postID)
}

func videoBridgesEndpoint() string {
	return "/rcvideo/v2/account/~/extension/~/bridges"
}

func videoBridgeEndpoint(bridgeID string) string {
	return fmt.Sprintf("/rcvideo/v2/bridges/%s", url.PathEscape(bridgeID))
}

func videoHistoryMeetingsEndpoint(opts VideoMeetingHistoryOptions) string {
	params := url.Values{}
	if opts.Type != "" {
		params.Set("type", opts.Type)
	}
	if opts.PerPage > 0 {
		params.Set("perPage", strconv.Itoa(opts.PerPage))
	}
	if opts.PageToken != "" {
		params.Set("pageToken", opts.PageToken)
	}
	path := "/rcvideo/v1/history/meetings"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	return path
}

func cloudCalendarsEndpoint(sync bool) string {
	params := url.Values{}
	params.Set("sync", strconv.FormatBool(sync))
	return "/restapi/v1.0/account/~/extension/~/cloud-calendars/ucc?" + params.Encode()
}

func cloudCalendarEventsEndpoint(providerID, calendarID string, opts CloudCalendarEventOptions) string {
	params := url.Values{}
	if opts.StartTimeFrom != "" {
		params.Set("startTimeFrom", opts.StartTimeFrom)
	}
	if opts.StartTimeTo != "" {
		params.Set("startTimeTo", opts.StartTimeTo)
	}
	if opts.IncludeNonRCEvents {
		params.Set("includeNonRcEvents", "true")
	}
	if opts.PerPage > 0 {
		params.Set("perPage", strconv.Itoa(opts.PerPage))
	}
	if opts.PageToken != "" {
		params.Set("pageToken", opts.PageToken)
	}
	path := fmt.Sprintf("/restapi/v1.0/account/~/extension/~/cloud-calendars/ucc/%s/~/%s/events",
		url.PathEscape(providerID),
		url.PathEscape(calendarID),
	)
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	return path
}

func ringOutsEndpoint() string {
	return "/restapi/v1.0/account/~/extension/~/ring-out"
}

func smsEndpoint() string {
	return "/restapi/v1.0/account/~/extension/~/sms"
}

func ringOutEndpoint(ringOutID string) string {
	return fmt.Sprintf("%s/%s", ringOutsEndpoint(), url.PathEscape(ringOutID))
}

func extensionCallLogEndpoint(extensionID string) string {
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" {
		extensionID = "~"
	}
	return fmt.Sprintf("/restapi/v1.0/account/~/extension/%s/call-log", url.PathEscape(extensionID))
}
