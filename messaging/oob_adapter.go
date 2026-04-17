package messaging

import (
	"context"

	"github.com/ringclaw/ringclaw/messaging/oob"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// rcOOBClient adapts *ringcentral.Client to the narrow oob.Client
// interface that messaging/oob expects. The adapter exists so the oob
// package does not import ringcentral (and so unit tests in oob can
// substitute a fake without dragging in HTTP plumbing).
//
// Phase 2b only needs SendText: Adaptive-Card-based approval flows
// were dropped along with the PIN layer, since RingCentral's WebSocket
// subscription cannot deliver Action.Submit events anyway.
type rcOOBClient struct {
	c *ringcentral.Client
}

func newOOBClient(c *ringcentral.Client) oob.Client {
	if c == nil {
		return nil
	}
	return &rcOOBClient{c: c}
}

func (r *rcOOBClient) SendText(ctx context.Context, chatID, text string) error {
	return SendTextReply(ctx, r.c, chatID, text)
}
