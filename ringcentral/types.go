package ringcentral

// Post represents a Team Messaging post.
type Post struct {
	ID               string       `json:"id"`
	GroupID          string       `json:"groupId"`
	Type             string       `json:"type"`
	Text             string       `json:"text"`
	CreatorID        string       `json:"creatorId"`
	AddedPersonIDs   []string     `json:"addedPersonIds"`
	CreationTime     string       `json:"creationTime"`
	LastModifiedTime string       `json:"lastModifiedTime"`
	Attachments      []Attachment `json:"attachments"`
	Mentions         []Mention    `json:"mentions"`
	Activity         string       `json:"activity"`
	Title            string       `json:"title"`
	IconURI          string       `json:"iconUri"`
	IconEmoji        string       `json:"iconEmoji"`
	EventType        string       `json:"eventType"`
}

// Attachment represents a post attachment.
type Attachment struct {
	ID         string `json:"id"`
	ContentURI string `json:"contentUri"`
	Name       string `json:"name"`
	Type       string `json:"type"`
}

// Mention represents a mention in a post.
type Mention struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

// PostList is the response from listing posts in a chat.
type PostList struct {
	Records    []Post `json:"records"`
	Navigation struct {
		PrevPageToken string `json:"prevPageToken"`
		NextPageToken string `json:"nextPageToken"`
	} `json:"navigation"`
}

// ChatList is the response from listing chats.
type ChatList struct {
	Records []Chat `json:"records"`
}

// PersonInfo represents a user's profile.
type PersonInfo struct {
	ID        string `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
}

// ListPostsOpts holds query parameters for ListPosts.
type ListPostsOpts struct {
	RecordCount      int
	CreationTimeFrom string
	CreationTimeTo   string
}

// CreatePostRequest is the body for creating a post.
type CreatePostRequest struct {
	Text string `json:"text"`
}

// UpdatePostRequest is the body for updating a post.
type UpdatePostRequest struct {
	Text string `json:"text"`
}

// FileUploadResponse is the response from uploading a file.
type FileUploadResponse struct {
	ID         string `json:"id"`
	ContentURI string `json:"contentUri"`
	Name       string `json:"name"`
}

// TokenResponse is the response from OAuth token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	OwnerID      string `json:"owner_id"`
	EndpointID   string `json:"endpoint_id"`
}

// WSTokenResponse is the response from WebSocket token endpoint.
type WSTokenResponse struct {
	WSAccessToken string `json:"ws_access_token"`
	URI           string `json:"uri"`
	ExpiresIn     int    `json:"expires_in"`
}

// WSConnectionDetails is the initial message from WebSocket server.
type WSConnectionDetails struct {
	Type      string   `json:"type"`
	MessageID string   `json:"messageId"`
	Status    int      `json:"status"`
	WSC       WSCInfo  `json:"wsc"`
	Headers   WSHeaders `json:"headers"`
}

// WSCInfo holds WebSocket connection info for session recovery.
type WSCInfo struct {
	Token    string `json:"token"`
	Sequence int    `json:"sequence"`
}

// WSHeaders holds response headers.
type WSHeaders struct {
	RCRequestID string `json:"RCRequestId"`
}

// WSClientRequest is the subscription request sent over WebSocket.
type WSClientRequest struct {
	Type      string             `json:"type"`
	MessageID string             `json:"messageId"`
	Method    string             `json:"method"`
	Path      string             `json:"path"`
}

// WSSubscriptionBody is the body of a subscription request.
type WSSubscriptionBody struct {
	EventFilters []string           `json:"eventFilters"`
	DeliveryMode WSDeliveryMode     `json:"deliveryMode"`
}

// WSDeliveryMode specifies the transport type.
type WSDeliveryMode struct {
	TransportType string `json:"transportType"`
}

// WSEvent is the event received over WebSocket.
type WSEvent struct {
	UUID           string `json:"uuid"`
	Event          string `json:"event"`
	Timestamp      string `json:"timestamp"`
	SubscriptionID string `json:"subscriptionId"`
	OwnerID        string `json:"ownerId"`
	Body           Post   `json:"body"`
}

// Chat represents a Team Messaging chat.
type Chat struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Type             string   `json:"type"`
	Status           string   `json:"status"`
	Members          []string `json:"members"`
	IsPublic         bool     `json:"isPublic"`
	CreationTime     string   `json:"creationTime"`
	LastModifiedTime string   `json:"lastModifiedTime"`
}

// Credentials stores RC session data.
type Credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	JWTToken     string `json:"jwt_token"`
	ServerURL    string `json:"server_url"`
	ChatID       string `json:"chat_id"`
}
