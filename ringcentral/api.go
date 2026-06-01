package ringcentral

import (
	"context"
	"encoding/json"
)

// MessageSender is implemented by Client for sending messages and managing posts.
// This interface enables test mocking and decouples callers from the concrete Client.
type MessageSender interface {
	SendPost(ctx context.Context, chatID, text string) (*Post, error)
	UpdatePost(ctx context.Context, chatID, postID, text string) (*Post, error)
	DeletePost(ctx context.Context, chatID, postID string) error
	UploadFile(ctx context.Context, chatID, fileName string, fileData []byte) (*FileUploadResponse, error)
	DownloadAttachment(ctx context.Context, contentURI string) ([]byte, string, error)
	CreateAdaptiveCard(ctx context.Context, chatID string, card json.RawMessage) (*AdaptiveCard, error)
}

// ResourceAccessor is implemented by Client for CRUD operations on RingCentral resources.
// Requires private app credentials for full API access.
type ResourceAccessor interface {
	// Posts
	ListPosts(ctx context.Context, chatID string, opts ListPostsOpts) (*PostList, error)

	// Tasks
	ListTasks(ctx context.Context, chatID string) (*TaskList, error)
	CreateTask(ctx context.Context, chatID string, req *CreateTaskRequest) (*Task, error)
	GetTask(ctx context.Context, taskID string) (*Task, error)
	UpdateTask(ctx context.Context, taskID string, req *UpdateTaskRequest) (*Task, error)
	DeleteTask(ctx context.Context, taskID string) error
	CompleteTask(ctx context.Context, taskID string) error

	// Notes
	ListNotes(ctx context.Context, chatID string) (*NoteList, error)
	CreateNote(ctx context.Context, chatID string, req *CreateNoteRequest) (*Note, error)
	GetNote(ctx context.Context, noteID string) (*Note, error)
	UpdateNote(ctx context.Context, noteID string, req *UpdateNoteRequest) (*Note, error)
	DeleteNote(ctx context.Context, noteID string) error
	PublishNote(ctx context.Context, noteID string) error

	// Events
	ListEvents(ctx context.Context) (*EventList, error)
	CreateEvent(ctx context.Context, req *CreateEventRequest) (*Event, error)
	GetEvent(ctx context.Context, eventID string) (*Event, error)
	UpdateEvent(ctx context.Context, eventID string, req *UpdateEventRequest) (*Event, error)
	DeleteEvent(ctx context.Context, eventID string) error

	// Cards
	GetAdaptiveCard(ctx context.Context, cardID string) (*AdaptiveCard, error)
	UpdateAdaptiveCard(ctx context.Context, cardID string, card json.RawMessage) (*AdaptiveCard, error)
	DeleteAdaptiveCard(ctx context.Context, cardID string) error
}

// DirectoryAccessor is implemented by Client for chat and user lookup operations.
type DirectoryAccessor interface {
	GetChat(ctx context.Context, chatID string) (*Chat, error)
	ListChats(ctx context.Context, chatType string) (*ChatList, error)
	SearchDirectory(ctx context.Context, query string) (*DirectorySearchResult, error)
	SearchContacts(ctx context.Context, query string) (*ContactList, error)
	CreateConversation(ctx context.Context, memberIDs []string) (*Chat, error)
	FindDirectChat(ctx context.Context, personID string) (string, error)
	GetPersonInfo(ctx context.Context, personID string) (*PersonInfo, error)
}

// BotInfo is implemented by Client for bot identity queries.
type BotInfo interface {
	IsBot() bool
	IsBotDM(chatID string) bool
	OwnerID() string
}

// Compile-time interface compliance checks.
var (
	_ MessageSender     = (*Client)(nil)
	_ ResourceAccessor  = (*Client)(nil)
	_ DirectoryAccessor = (*Client)(nil)
	_ BotInfo           = (*Client)(nil)
)
