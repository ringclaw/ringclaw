package ringcentral

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ListVideoBridges lists RingCentral Video meeting bridges for the authenticated extension.
func (c *Client) ListVideoBridges(ctx context.Context) (*VideoBridgeList, error) {
	resp, err := c.doVideoRequest(ctx, http.MethodGet, videoBridgesEndpoint(), "", nil)
	if err != nil {
		return nil, err
	}
	var list VideoBridgeList
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse video bridge list: %w", err)
	}
	return &list, nil
}

// ListVideoMeetingHistory lists RingCentral Video meeting history for the
// authenticated owner extension. It is the API used for "records/history"
// queries; bridge APIs are only for creating and managing join bridges.
func (c *Client) ListVideoMeetingHistory(ctx context.Context, opts VideoMeetingHistoryOptions) (*VideoMeetingHistoryList, error) {
	if opts.Type == "" {
		opts.Type = "All"
	}
	if opts.PerPage == 0 {
		opts.PerPage = 20
	}
	resp, err := c.doVideoRequest(ctx, http.MethodGet, videoHistoryMeetingsEndpoint(opts), "", nil)
	if err != nil {
		return nil, err
	}
	var list VideoMeetingHistoryList
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse video meeting history: %w", err)
	}
	return &list, nil
}

// ListCloudCalendars lists connected calendars for the authenticated extension.
// This matches the FIJI Calendar module's upcoming-meeting source.
func (c *Client) ListCloudCalendars(ctx context.Context, sync bool) (*CloudCalendarList, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, cloudCalendarsEndpoint(sync), "", nil)
	if err != nil {
		return nil, err
	}
	var list CloudCalendarList
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse cloud calendar list: %w", err)
	}
	return &list, nil
}

// ListCloudCalendarEvents lists events from one connected cloud calendar.
func (c *Client) ListCloudCalendarEvents(ctx context.Context, providerID, calendarID string, opts CloudCalendarEventOptions) (*CloudCalendarEventList, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, cloudCalendarEventsEndpoint(providerID, calendarID, opts), "", nil)
	if err != nil {
		return nil, err
	}
	var list CloudCalendarEventList
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse cloud calendar events: %w", err)
	}
	return &list, nil
}

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
	resp, err := c.doVideoRequest(ctx, http.MethodPost, videoBridgesEndpoint(), "application/json", bytes.NewReader(data))
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
	path := videoBridgeEndpoint(bridgeID)
	resp, err := c.doVideoRequest(ctx, http.MethodGet, path, "", nil)
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
	path := videoBridgeEndpoint(bridgeID)
	resp, err := c.doVideoRequest(ctx, http.MethodPatch, path, "application/json", bytes.NewReader(data))
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
	path := videoBridgeEndpoint(bridgeID)
	_, err := c.doVideoRequest(ctx, http.MethodDelete, path, "", nil)
	return err
}
