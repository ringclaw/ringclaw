package ringcentral

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ListMessages queries the authenticated extension's message store.
func (c *Client) ListMessages(ctx context.Context, opts MessageStoreListOptions) (*MessageStoreList, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, messageStoreListEndpoint(opts), "", nil)
	if err != nil {
		return nil, err
	}
	var list MessageStoreList
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse messages: %w", err)
	}
	return &list, nil
}

// GetMessage fetches one message-store record by ID.
func (c *Client) GetMessage(ctx context.Context, messageID string) (*MessageStoreItem, error) {
	if messageID == "" {
		return nil, fmt.Errorf("message id required")
	}
	resp, err := c.doRequest(ctx, http.MethodGet, messageStoreEndpoint(messageID), "", nil)
	if err != nil {
		return nil, err
	}
	var item MessageStoreItem
	if err := json.Unmarshal(resp, &item); err != nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}
	return &item, nil
}
