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
	RecordCount int
	PageToken   string
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

// ChatMember represents a member in a chat.
type ChatMember struct {
	ID string `json:"id"`
}

// Chat represents a Team Messaging chat.
type Chat struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	Description      string       `json:"description"`
	Type             string       `json:"type"`
	Status           string       `json:"status"`
	Members          []ChatMember `json:"members"`
	IsPublic         bool         `json:"isPublic"`
	CreationTime     string       `json:"creationTime"`
	LastModifiedTime string       `json:"lastModifiedTime"`
}

// MemberIDs returns a string slice of member IDs.
func (c Chat) MemberIDs() []string {
	ids := make([]string, len(c.Members))
	for i, m := range c.Members {
		ids[i] = m.ID
	}
	return ids
}

// DirectoryEntry represents a user in the company directory.
type DirectoryEntry struct {
	ID        string `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
}

// DirectorySearchResult is the response from searching directory entries.
type DirectorySearchResult struct {
	Records []DirectoryEntry `json:"records"`
}

// CreateChatRequest is the body for creating/finding a conversation.
type CreateChatRequest struct {
	Members []ChatMember `json:"members"`
}

// --- Task types ---

// TaskAssignee represents a task assignee.
type TaskAssignee struct {
	ID     string `json:"id"`
	Status string `json:"status,omitempty"` // Pending, Completed
}

// TaskRecurrence holds recurrence settings for a task.
type TaskRecurrence struct {
	Schedule        string `json:"schedule"`                  // None, Daily, Weekdays, Weekly, Monthly, Yearly
	EndingCondition string `json:"endingCondition"`           // None, Count, Date
	EndingAfter     int    `json:"endingAfter,omitempty"`
	EndingOn        string `json:"endingOn,omitempty"`
}

// CreateTaskRequest is the body for creating a task.
type CreateTaskRequest struct {
	Subject              string         `json:"subject"`
	Assignees            []TaskAssignee `json:"assignees,omitempty"`
	CompletenessCondition string        `json:"completenessCondition,omitempty"` // Simple, AllAssignees, Percentage
	StartDate            string         `json:"startDate,omitempty"`
	DueDate              string         `json:"dueDate,omitempty"`
	Color                string         `json:"color,omitempty"`
	Section              string         `json:"section,omitempty"`
	Description          string         `json:"description,omitempty"`
}

// UpdateTaskRequest is the body for updating a task.
type UpdateTaskRequest struct {
	Subject     string `json:"subject,omitempty"`
	Description string `json:"description,omitempty"`
	DueDate     string `json:"dueDate,omitempty"`
	Color       string `json:"color,omitempty"`
	Status      string `json:"status,omitempty"`
}

// Task represents a Team Messaging task.
type Task struct {
	ID                   string         `json:"id"`
	CreationTime         string         `json:"creationTime"`
	LastModifiedTime     string         `json:"lastModifiedTime"`
	Type                 string         `json:"type"`
	Creator              TaskAssignee   `json:"creator"`
	ChatIDs              []string       `json:"chatIds"`
	Status               string         `json:"status"` // Pending, InProgress, Completed
	Subject              string         `json:"subject"`
	Assignees            []TaskAssignee `json:"assignees"`
	CompletenessCondition string        `json:"completenessCondition"`
	StartDate            string         `json:"startDate"`
	DueDate              string         `json:"dueDate"`
	Color                string         `json:"color"`
	Section              string         `json:"section"`
	Description          string         `json:"description"`
}

// TaskList is the response from listing tasks.
type TaskList struct {
	Records    []Task `json:"records"`
	Navigation struct {
		NextPageToken string `json:"nextPageToken"`
	} `json:"navigation"`
}

// --- Note types ---

// CreateNoteRequest is the body for creating a note.
type CreateNoteRequest struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
}

// UpdateNoteRequest is the body for updating a note.
type UpdateNoteRequest struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

// Note represents a Team Messaging note.
type Note struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	ChatIDs          []string `json:"chatIds"`
	Preview          string `json:"preview"`
	Status           string `json:"status"` // Active, Draft
	CreationTime     string `json:"creationTime"`
	LastModifiedTime string `json:"lastModifiedTime"`
	Type             string `json:"type"`
	Creator          struct {
		ID string `json:"id"`
	} `json:"creator"`
}

// NoteList is the response from listing notes.
type NoteList struct {
	Records    []Note `json:"records"`
	Navigation struct {
		NextPageToken string `json:"nextPageToken"`
	} `json:"navigation"`
}

// --- Event types ---

// EventRecurrence holds recurrence settings for an event.
type EventRecurrence struct {
	Schedule        string `json:"schedule"`                  // None, Day, Weekday, Week, Month, Year
	EndingCondition string `json:"endingCondition"`           // None, Count, Date
	EndingAfter     int    `json:"endingAfter,omitempty"`
	EndingOn        string `json:"endingOn,omitempty"`
}

// CreateEventRequest is the body for creating an event.
type CreateEventRequest struct {
	Title       string           `json:"title"`
	StartTime   string           `json:"startTime"`
	EndTime     string           `json:"endTime"`
	AllDay      bool             `json:"allDay,omitempty"`
	Color       string           `json:"color,omitempty"`
	Location    string           `json:"location,omitempty"`
	Description string           `json:"description,omitempty"`
	Recurrence  *EventRecurrence `json:"recurrence,omitempty"`
}

// UpdateEventRequest is the body for updating an event.
type UpdateEventRequest struct {
	Title       string `json:"title,omitempty"`
	StartTime   string `json:"startTime,omitempty"`
	EndTime     string `json:"endTime,omitempty"`
	Color       string `json:"color,omitempty"`
	Location    string `json:"location,omitempty"`
	Description string `json:"description,omitempty"`
}

// Event represents a Team Messaging calendar event.
type Event struct {
	ID          string `json:"id"`
	CreatorID   string `json:"creatorId"`
	Title       string `json:"title"`
	StartTime   string `json:"startTime"`
	EndTime     string `json:"endTime"`
	AllDay      bool   `json:"allDay"`
	Color       string `json:"color"`
	Location    string `json:"location"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

// EventList is the response from listing events.
type EventList struct {
	Records    []Event `json:"records"`
	Navigation struct {
		NextPageToken string `json:"nextPageToken"`
	} `json:"navigation"`
}

// --- Adaptive Card types ---

// AdaptiveCard represents a Team Messaging adaptive card response.
type AdaptiveCard struct {
	ID               string   `json:"id"`
	CreationTime     string   `json:"creationTime"`
	LastModifiedTime string   `json:"lastModifiedTime"`
	Type             string   `json:"type"`
	Version          string   `json:"version"`
	ChatIDs          []string `json:"chatIds"`
}

// PresenceInfo represents a user's presence/availability status.
type PresenceInfo struct {
	UserStatus      string `json:"userStatus"`      // Available, Busy, DoNotDisturb, Offline
	DndStatus       string `json:"dndStatus"`       // TakeAllCalls, DoNotAcceptDepartmentCalls, TakeDepartmentCallsOnly, DoNotAcceptAnyCalls
	TelephonyStatus string `json:"telephonyStatus"` // NoCall, CallConnected, Ringing, OnHold, ParkedCall
	Message         string `json:"message"`
}

// Credentials stores RC session data.
type Credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	JWTToken     string `json:"jwt_token"`
	ServerURL    string `json:"server_url"`
}
