package ringcentral

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

func (c *Client) SearchDirectory(ctx context.Context, searchString string) (*DirectorySearchResult, error) {
	body := map[string]string{"searchString": searchString}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal search body: %w", err)
	}

	path := "/restapi/v1.0/account/~/directory/entries/search"
	respBody, err := c.doRequest(ctx, http.MethodPost, path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	var result DirectorySearchResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse directory search: %w", err)
	}
	return &result, nil
}

// ResolveUserIDs resolves a mixed list of numeric IDs and email addresses to numeric IDs.
// Entries that already look like numeric IDs are kept as-is.
// Entries containing "@" are looked up via SearchDirectory; unresolved entries are logged
// as warnings and skipped so a typo doesn't silently block all users.
func (c *Client) ResolveUserIDs(ctx context.Context, ids []string) []string {
	resolved := make([]string, 0, len(ids))
	for _, id := range ids {
		if !strings.Contains(id, "@") {
			resolved = append(resolved, id)
			continue
		}
		result, err := c.SearchDirectory(ctx, id)
		if err != nil {
			slog.Warn("user_ids: failed to search directory for email", "email", id, "error", err)
			continue
		}
		found := false
		for _, entry := range result.Records {
			if strings.EqualFold(entry.Email, id) {
				slog.Info("user_ids: resolved email to ID", "email", id, "id", entry.ID)
				resolved = append(resolved, entry.ID)
				found = true
				break
			}
		}
		if !found {
			slog.Warn("user_ids: email not found in directory, entry will be skipped", "email", id)
		}
	}
	return resolved
}

// CreateConversation creates or finds an existing Direct chat with the given members.
// If a conversation already exists with those members, it is returned (idempotent).
func (c *Client) CreateConversation(ctx context.Context, memberIDs []string) (*Chat, error) {
	members := make([]ChatMember, len(memberIDs))
	for i, id := range memberIDs {
		members[i] = ChatMember{ID: id}
	}
	body := CreateChatRequest{Members: members}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal create chat: %w", err)
	}

	respBody, err := c.doRequest(ctx, http.MethodPost, "/team-messaging/v1/conversations", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	var chat Chat
	if err := json.Unmarshal(respBody, &chat); err != nil {
		return nil, fmt.Errorf("parse create chat: %w", err)
	}
	return &chat, nil
}

// CurrentExtensionProfile contains the current JWT owner's basic extension info.
type CurrentExtensionProfile struct {
	ID                 string
	ExtensionNumber    string
	PrimaryPhoneNumber string
}

type currentExtensionResponse struct {
	ID              int64  `json:"id"`
	ExtensionNumber string `json:"extensionNumber"`
	Contact         struct {
		BusinessPhone string `json:"businessPhone"`
		PhoneNumber   string `json:"phoneNumber"`
		MobilePhone   string `json:"mobilePhone"`
	} `json:"contact"`
	PhoneNumbers []ExtensionPhoneNumber `json:"phoneNumbers"`
}

// ExtensionPhoneNumber describes a phone number assigned to an extension.
type ExtensionPhoneNumber struct {
	PhoneNumber string `json:"phoneNumber"`
	Type        string `json:"type"`
	UsageType   string `json:"usageType"`
	Primary     bool   `json:"primary"`
}

type extensionPhoneNumberList struct {
	Records []ExtensionPhoneNumber `json:"records"`
}

// GetCurrentExtensionProfile fetches the current JWT owner's extension profile.
func (c *Client) GetCurrentExtensionProfile(ctx context.Context) (*CurrentExtensionProfile, error) {
	respBody, err := c.doRequest(ctx, http.MethodGet, "/restapi/v1.0/account/~/extension/~", "", nil)
	if err != nil {
		return nil, err
	}

	var info currentExtensionResponse
	if err := json.Unmarshal(respBody, &info); err != nil {
		return nil, fmt.Errorf("parse extension info: %w", err)
	}
	return &CurrentExtensionProfile{
		ID:                 fmt.Sprintf("%d", info.ID),
		ExtensionNumber:    strings.TrimSpace(info.ExtensionNumber),
		PrimaryPhoneNumber: firstNonEmptyPhone(info.Contact.BusinessPhone, info.Contact.PhoneNumber, info.Contact.MobilePhone, selectRingOutPhoneNumber(info.PhoneNumbers)),
	}, nil
}

// GetExtensionInfo fetches current user's extension info to get the owner ID.
func (c *Client) GetExtensionInfo(ctx context.Context) (string, error) {
	profile, err := c.GetCurrentExtensionProfile(ctx)
	if err != nil {
		return "", err
	}
	return profile.ID, nil
}

// ResolveDefaultRingOutFromNumber returns the current JWT owner's default RingOut callback number.
func (c *Client) ResolveDefaultRingOutFromNumber(ctx context.Context) (string, error) {
	profile, err := c.GetCurrentExtensionProfile(ctx)
	if err != nil {
		return "", err
	}
	if profile.PrimaryPhoneNumber != "" {
		return profile.PrimaryPhoneNumber, nil
	}

	respBody, err := c.doRequest(ctx, http.MethodGet, "/restapi/v1.0/account/~/extension/~/phone-number", "", nil)
	if err != nil {
		return "", err
	}
	var list extensionPhoneNumberList
	if err := json.Unmarshal(respBody, &list); err != nil {
		return "", fmt.Errorf("parse extension phone numbers: %w", err)
	}
	if phone := selectRingOutPhoneNumber(list.Records); phone != "" {
		return phone, nil
	}
	return "", fmt.Errorf("current extension has no phone number available for RingOut; configure a direct number or callback number for the owner account")
}

func firstNonEmptyPhone(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func selectRingOutPhoneNumber(numbers []ExtensionPhoneNumber) string {
	for _, number := range numbers {
		if number.Primary && strings.TrimSpace(number.PhoneNumber) != "" {
			return strings.TrimSpace(number.PhoneNumber)
		}
	}
	for _, number := range numbers {
		if strings.EqualFold(number.UsageType, "DirectNumber") && strings.TrimSpace(number.PhoneNumber) != "" {
			return strings.TrimSpace(number.PhoneNumber)
		}
	}
	for _, number := range numbers {
		if strings.TrimSpace(number.PhoneNumber) != "" {
			return strings.TrimSpace(number.PhoneNumber)
		}
	}
	return ""
}

// FindDirectChat finds or creates a Direct (1:1) chat between the current
// user and the given person. Returns the chat ID.
func (c *Client) FindDirectChat(ctx context.Context, personID string) (string, error) {
	chat, err := c.CreateConversation(ctx, []string{personID})
	if err != nil {
		return "", fmt.Errorf("find direct chat: %w", err)
	}
	return chat.ID, nil
}

// --- Task CRUD ---
