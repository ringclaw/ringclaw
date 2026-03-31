package cmd

import (
	"context"
	"fmt"

	"github.com/ringclaw/ringclaw/config"
	"github.com/ringclaw/ringclaw/messaging"
	"github.com/ringclaw/ringclaw/ringcentral"
	"github.com/spf13/cobra"
)

var (
	sendTo       string
	sendText     string
	sendMediaURL string
)

func init() {
	sendCmd.Flags().StringVar(&sendTo, "to", "", "Target chat ID (overrides config)")
	sendCmd.Flags().StringVar(&sendText, "text", "", "Message text to send")
	sendCmd.Flags().StringVar(&sendMediaURL, "media", "", "Media URL to send (image/video/file)")
	rootCmd.AddCommand(sendCmd)
}

var sendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send a message to a RingCentral chat",
	Example: `  ringclaw send --text "Hello"
  ringclaw send --to "chatId" --text "Hello"
  ringclaw send --media "https://example.com/image.png"
  ringclaw send --text "See this" --media "https://example.com/image.png"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if sendText == "" && sendMediaURL == "" {
			return fmt.Errorf("at least one of --text or --media is required")
		}

		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		chatID := sendTo
		if chatID == "" && len(cfg.RC.ChatIDs) > 0 {
			chatID = cfg.RC.ChatIDs[0]
		}
		if chatID == "" {
			return fmt.Errorf("no chat ID specified. Use --to or add chat_ids to config file")
		}

		var client *ringcentral.Client
		if cfg.RC.BotToken != "" {
			client = ringcentral.NewBotClient(cfg.RC.ServerURL, cfg.RC.BotToken)
		} else if cfg.RC.HasPrivateApp() {
			creds := &ringcentral.Credentials{
				ClientID:     cfg.RC.ClientID,
				ClientSecret: cfg.RC.ClientSecret,
				JWTToken:     cfg.RC.JWTToken,
				ServerURL:    cfg.RC.ServerURL,
			}
			client = ringcentral.NewClient(creds)
			if err := client.Authenticate(); err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}
		} else {
			return fmt.Errorf("no credentials configured. Set RC_BOT_TOKEN or run 'ringclaw setup'")
		}

		if sendText != "" {
			if err := messaging.SendTextReply(ctx, client, chatID, sendText); err != nil {
				return fmt.Errorf("send text failed: %w", err)
			}
			fmt.Println("Text sent")
		}

		if sendMediaURL != "" {
			if err := messaging.SendMediaFromURL(ctx, client, chatID, sendMediaURL); err != nil {
				return fmt.Errorf("send media failed: %w", err)
			}
			fmt.Println("Media sent")
		}

		return nil
	},
}
