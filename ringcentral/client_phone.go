package ringcentral

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// SendSMS sends a plain-text SMS from the authenticated extension.
func (c *Client) SendSMS(ctx context.Context, req *CreateSMSRequest) (*SMSMessage, error) {
	if req == nil {
		req = &CreateSMSRequest{}
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal sms: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, smsEndpoint(), "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var message SMSMessage
	if err := json.Unmarshal(resp, &message); err != nil {
		return nil, fmt.Errorf("parse sms: %w", err)
	}
	return &message, nil
}

// CreateRingOut starts a two-legged RingOut call.
func (c *Client) CreateRingOut(ctx context.Context, req *CreateRingOutRequest) (*RingOut, error) {
	if req == nil {
		req = &CreateRingOutRequest{}
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal ringout: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, ringOutsEndpoint(), "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var ringOut RingOut
	if err := json.Unmarshal(resp, &ringOut); err != nil {
		return nil, fmt.Errorf("parse ringout: %w", err)
	}
	return &ringOut, nil
}

// ListExtensionPhoneNumbers returns phone numbers assigned to the
// authenticated extension. The action layer uses this to choose a default
// RingOut callback number when the user does not provide from= explicitly.
func (c *Client) ListExtensionPhoneNumbers(ctx context.Context) (*ExtensionPhoneNumberList, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/restapi/v1.0/account/~/extension/~/phone-number", "", nil)
	if err != nil {
		return nil, err
	}
	var list ExtensionPhoneNumberList
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse extension phone numbers: %w", err)
	}
	return &list, nil
}

// ListForwardingNumbers returns callback-capable numbers configured for the
// authenticated extension. RingOut requires this kind of number in `from`.
func (c *Client) ListForwardingNumbers(ctx context.Context) (*ForwardingNumberList, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/restapi/v1.0/account/~/extension/~/forwarding-number", "", nil)
	if err != nil {
		return nil, err
	}
	var list ForwardingNumberList
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse forwarding numbers: %w", err)
	}
	return &list, nil
}

// GetRingOut retrieves the status of an in-progress RingOut call.
func (c *Client) GetRingOut(ctx context.Context, ringOutID string) (*RingOut, error) {
	path := ringOutEndpoint(ringOutID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	var ringOut RingOut
	if err := json.Unmarshal(resp, &ringOut); err != nil {
		return nil, fmt.Errorf("parse ringout: %w", err)
	}
	return &ringOut, nil
}

// DeleteRingOut cancels a RingOut call while it is still in progress.
func (c *Client) DeleteRingOut(ctx context.Context, ringOutID string) error {
	path := ringOutEndpoint(ringOutID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, "", nil)
	return err
}

// ListExtensionCallLog returns call logs for the authenticated extension.
func (c *Client) ListExtensionCallLog(ctx context.Context, opts CallLogOptions) (*CallLogList, error) {
	return c.listCallLog(ctx, extensionCallLogEndpoint(opts.ExtensionID), opts)
}

func (c *Client) listCallLog(ctx context.Context, endpoint string, opts CallLogOptions) (*CallLogList, error) {
	params := url.Values{}
	if opts.RecordCount > 0 {
		params.Set("recordCount", strconv.Itoa(opts.RecordCount))
	}
	if opts.Page > 0 {
		params.Set("page", strconv.Itoa(opts.Page))
	}
	if opts.PageToken != "" {
		params.Set("pageToken", opts.PageToken)
	}
	if opts.View != "" {
		params.Set("view", opts.View)
	}
	if opts.Direction != "" {
		params.Set("direction", opts.Direction)
	}
	if opts.Type != "" {
		params.Set("type", opts.Type)
	}
	if opts.DateFrom != "" {
		params.Set("dateFrom", opts.DateFrom)
	}
	if opts.DateTo != "" {
		params.Set("dateTo", opts.DateTo)
	}
	path := endpoint
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	resp, err := c.doRequest(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	var list CallLogList
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse call log: %w", err)
	}
	return &list, nil
}
