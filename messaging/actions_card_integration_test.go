//go:build integration

// Integration test for the assumption baked into selectCardClient:
// the Private App must be allowed to POST adaptive cards into the
// bot's own DM. If the underlying RingCentral API ever stops
// honouring this — for example because the Private App role is
// changed or the platform tightens permissions — this test fails
// loudly so we can re-introduce the Bot fallback for that one path.
//
// Skipped when ~/.ringclaw/config.json is missing or does not have
// both a Bot token and a Private App configured. Runs only when the
// `integration` build tag is set:
//
//   go test -tags integration ./messaging/ -run CardClientIntegration -v
package messaging

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ringclaw/ringclaw/config"
	"github.com/ringclaw/ringclaw/ringcentral"
)

const integrationCardJSON = `{
  "$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
  "type": "AdaptiveCard",
  "version": "1.3",
  "body": [
    {"type": "TextBlock", "text": "[integration test] safe to ignore", "weight": "Bolder"}
  ]
}`

// TestCardClientIntegration_PrivateAppCanPostIntoBotDM verifies that
// selectCardClient's preference for the Private App in a Bot DM is
// actually honoured by the RingCentral API. It posts a minimal
// adaptive card with the Private App credentials into the bot's own
// DM chat and immediately deletes it. The assertion is binary: if
// the POST succeeds, our routing assumption holds; if it fails, the
// fallback in selectCardClient must be re-introduced.
func TestCardClientIntegration_PrivateAppCanPostIntoBotDM(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		t.Skipf("skip: load config: %v", err)
	}
	if !cfg.RC.HasPrivateApp() || cfg.RC.BotToken == "" {
		t.Skip("skip: integration test requires both Bot token and Private App credentials in ~/.ringclaw/config.json")
	}

	pa := ringcentral.NewClient(&ringcentral.Credentials{
		ClientID:     cfg.RC.ClientID,
		ClientSecret: cfg.RC.ClientSecret,
		JWTToken:     cfg.RC.JWTToken,
		ServerURL:    cfg.RC.ServerURL,
	})
	if err := pa.Authenticate(); err != nil {
		t.Fatalf("Private App auth: %v", err)
	}
	ownerID, err := pa.GetExtensionInfo(ctx)
	if err != nil {
		t.Fatalf("fetch PA owner ID: %v", err)
	}
	pa.SetOwnerID(ownerID)

	bot := ringcentral.NewBotClient(cfg.RC.ServerURL, cfg.RC.BotToken)
	dmChatID, err := bot.FindDirectChat(ctx, ownerID)
	if err != nil {
		t.Fatalf("discover bot DM chat: %v", err)
	}

	t.Logf("Private App owner=%s, bot DM chat=%s", ownerID, dmChatID)

	// The control: Bot can always post into its own DM. If this fails
	// the test environment is broken — surface that distinctly.
	controlCard, err := bot.CreateAdaptiveCard(ctx, dmChatID, json.RawMessage(integrationCardJSON))
	if err != nil {
		t.Fatalf("control: bot client cannot POST into its own DM (test env broken?): %v", err)
	}
	if delErr := bot.DeleteAdaptiveCard(ctx, controlCard.ID); delErr != nil {
		t.Logf("(cleanup) failed to delete control card %s: %v", controlCard.ID, delErr)
	}

	// The actual assertion: Private App can POST into the bot DM.
	// selectCardClient relies on this being true.
	paCard, err := pa.CreateAdaptiveCard(ctx, dmChatID, json.RawMessage(integrationCardJSON))
	if err != nil {
		t.Fatalf("Private App POST /chats/%s/adaptive-cards failed: %v\n\nselectCardClient assumes the Private App can post cards into the bot DM. If this assertion fails, restore the bot fallback for IsBotDM in messaging/actions_resolve.go.", dmChatID, err)
	}
	t.Logf("Private App posted card OK: cardID=%s", paCard.ID)
	if delErr := pa.DeleteAdaptiveCard(ctx, paCard.ID); delErr != nil {
		t.Logf("(cleanup) failed to delete PA-created card %s: %v", paCard.ID, delErr)
	}
}
