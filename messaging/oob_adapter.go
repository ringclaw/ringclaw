package messaging

import (
	"context"
	"encoding/json"

	"github.com/ringclaw/ringclaw/messaging/oob"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// rcOOBClient adapts *ringcentral.Client to the narrow oob.Client
// interface that messaging/oob expects. The adapter exists so the oob
// package does not import ringcentral (and so unit tests in oob can
// substitute a fake without dragging in HTTP plumbing).
type rcOOBClient struct {
	c *ringcentral.Client
}

func newOOBClient(c *ringcentral.Client) oob.Client {
	if c == nil {
		return nil
	}
	return &rcOOBClient{c: c}
}

func (r *rcOOBClient) CreateAdaptiveCard(ctx context.Context, chatID string, card json.RawMessage) (oob.Card, error) {
	out, err := r.c.CreateAdaptiveCard(ctx, chatID, card)
	if err != nil {
		return nil, err
	}
	return rcOOBCard{ID: out.ID}, nil
}

func (r *rcOOBClient) SendText(ctx context.Context, chatID, text string) error {
	return SendTextReply(ctx, r.c, chatID, text)
}

type rcOOBCard struct {
	ID string
}

func (c rcOOBCard) GetID() string { return c.ID }
