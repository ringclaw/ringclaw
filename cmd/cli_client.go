package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ringclaw/ringclaw/config"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// newCLIClient creates a RingCentral API client from config for CLI commands.
func newCLIClient() (*ringcentral.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if cfg.RC.HasPrivateApp() {
		return newPrivateClientFromConfig(cfg)
	}

	if cfg.RC.BotToken != "" {
		return ringcentral.NewBotClient(cfg.RC.ServerURL, cfg.RC.BotToken), nil
	}

	return nil, fmt.Errorf("no credentials configured. Run 'ringclaw setup' or edit ~/.ringclaw/config.json")
}

func newPrivateClientFromConfig(cfg *config.Config) (*ringcentral.Client, error) {
	creds := &ringcentral.Credentials{
		ClientID:       cfg.RC.ClientID,
		ClientSecret:   cfg.RC.ClientSecret,
		JWTToken:       cfg.RC.JWTToken,
		ServerURL:      cfg.RC.ServerURL,
		VideoServerURL: cfg.RC.VideoServerURL,
	}
	client := ringcentral.NewClient(creds)
	if err := client.Authenticate(); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}
	return client, nil
}

// defaultChatID returns the first chat ID from config, or empty string.
func defaultChatID() string {
	cfg, _ := config.Load()
	if cfg != nil && len(cfg.RC.ChatIDs) > 0 {
		return cfg.RC.ChatIDs[0]
	}
	return ""
}

// printJSON marshals v to JSON and prints it.
func printJSON(v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}
