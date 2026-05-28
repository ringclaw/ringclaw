package ringcentral

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// CreateVideoBridge creates a RingCentral Video meeting bridge.
func (c *Client) CreateVideoBridge(ctx context.Context, req *CreateVideoBridgeRequest) (*VideoBridge, error) {
	if req == nil {
		req = &CreateVideoBridgeRequest{}
	}
	if req.Type == "" {
		req.Type = "Instant"
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal video bridge: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, "/rcvideo/v2/account/~/extension/~/bridges", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var bridge VideoBridge
	if err := json.Unmarshal(resp, &bridge); err != nil {
		return nil, fmt.Errorf("parse video bridge: %w", err)
	}
	return &bridge, nil
}

// GetVideoBridge retrieves a RingCentral Video bridge by ID.
func (c *Client) GetVideoBridge(ctx context.Context, bridgeID string) (*VideoBridge, error) {
	path := fmt.Sprintf("/rcvideo/v2/bridges/%s", url.PathEscape(bridgeID))
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	var bridge VideoBridge
	if err := json.Unmarshal(resp, &bridge); err != nil {
		return nil, fmt.Errorf("parse video bridge: %w", err)
	}
	return &bridge, nil
}

// UpdateVideoBridge updates a RingCentral Video bridge by ID.
func (c *Client) UpdateVideoBridge(ctx context.Context, bridgeID string, req *UpdateVideoBridgeRequest) (*VideoBridge, error) {
	if req == nil {
		req = &UpdateVideoBridgeRequest{}
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal video bridge update: %w", err)
	}
	path := fmt.Sprintf("/rcvideo/v2/bridges/%s", url.PathEscape(bridgeID))
	resp, err := c.doRequest(ctx, http.MethodPatch, path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var bridge VideoBridge
	if err := json.Unmarshal(resp, &bridge); err != nil {
		return nil, fmt.Errorf("parse video bridge: %w", err)
	}
	return &bridge, nil
}

// DeleteVideoBridge deletes a RingCentral Video bridge by ID.
func (c *Client) DeleteVideoBridge(ctx context.Context, bridgeID string) error {
	path := fmt.Sprintf("/rcvideo/v2/bridges/%s", url.PathEscape(bridgeID))
	_, err := c.doRequest(ctx, http.MethodDelete, path, "", nil)
	return err
}
