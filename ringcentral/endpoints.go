package ringcentral

import (
	"fmt"
	"net/url"
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

func ringOutsEndpoint() string {
	return "/restapi/v1.0/account/~/extension/~/ring-out"
}

func ringOutEndpoint(ringOutID string) string {
	return fmt.Sprintf("%s/%s", ringOutsEndpoint(), url.PathEscape(ringOutID))
}

func extensionCallLogEndpoint() string {
	return "/restapi/v1.0/account/~/extension/~/call-log"
}
