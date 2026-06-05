//go:build integration

// Integration diagnostic for Private App adaptive-card permissions. Current
// chat cards are posted by the bot identity, but cross-chat/OOB paths may
// still rely on Private App capabilities.
//
// Skipped when ~/.ringclaw/config.json is missing or does not have
// both a Bot token and a Private App configured. Runs only when the
// `integration` build tag is set:
//
//	go test -tags integration ./messaging/ -run CardClientIntegration -v
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

// TestCardClientIntegration_PrivateAppCanPostIntoBotDM posts a minimal
// adaptive card with Private App credentials into the bot's own DM chat and
// immediately deletes it. This remains useful for diagnosing platform
// permission changes even though current-chat cards prefer the bot client.
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
	paCard, err := pa.CreateAdaptiveCard(ctx, dmChatID, json.RawMessage(integrationCardJSON))
	if err != nil {
		t.Fatalf("Private App POST /chats/%s/adaptive-cards failed: %v", dmChatID, err)
	}
	t.Logf("Private App posted card OK: cardID=%s", paCard.ID)
	if delErr := pa.DeleteAdaptiveCard(ctx, paCard.ID); delErr != nil {
		t.Logf("(cleanup) failed to delete PA-created card %s: %v", paCard.ID, delErr)
	}
}
