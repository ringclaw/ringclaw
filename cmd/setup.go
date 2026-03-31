package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/config"
	"github.com/ringclaw/ringclaw/ringcentral"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(setupCmd)
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive setup wizard for RingCentral credentials",
	Long: `Guides you through configuring RingCentral credentials step by step.

Before running this command, you need to create apps at
https://developers.ringcentral.com/console:

  1. Create a "Bot Add-in (No UI)" app (required)
  2. (Optional) Create a "REST API" app (Private App) for summarize & cross-chat

This wizard will collect your credentials, validate them against the
RingCentral API, and save the configuration to ~/.ringclaw/config.json.`,
	RunE: runSetup,
}

func runSetup(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(os.Stdin)

	cfg, err := config.Load()
	if err != nil {
		cfg = config.DefaultConfig()
	}

	fmt.Println("=== RingClaw Setup Wizard ===")
	fmt.Println()
	fmt.Println("This wizard will guide you through RingCentral configuration.")
	fmt.Println("You need to create apps at: https://developers.ringcentral.com/console")
	fmt.Println()

	// Step 1: Bot App (required)
	fmt.Println("--- Step 1: Bot App (Required) ---")
	fmt.Println()
	fmt.Println("Create a Bot Add-in (No UI) app with:")
	fmt.Println("  - Scopes: ReadAccounts, TeamMessaging, WebSocketsSubscription")
	fmt.Println("  - Install it to your account, then copy the token from the Bot tab")
	fmt.Println()

	cfg.RC.BotToken = promptWithDefault(reader, "Bot Token", cfg.RC.BotToken)
	cfg.RC.ServerURL = promptWithDefault(reader, "Server URL", withDefault(cfg.RC.ServerURL, "https://platform.ringcentral.com"))

	if cfg.RC.BotToken != "" {
		fmt.Println()
		fmt.Print("Validating Bot Token... ")
		if err := validateBotToken(cfg); err != nil {
			fmt.Println("FAILED")
			fmt.Printf("  Error: %v\n", err)
			fmt.Println("  Please check that:")
			fmt.Println("    - The bot is installed to your account")
			fmt.Println("    - The token is copied from the Bot tab, not Credentials")
			fmt.Println()
			if !promptYesNo(reader, "Save anyway and fix later?") {
				fmt.Println("Setup aborted.")
				return nil
			}
		} else {
			fmt.Println("OK")
		}
	}

	mentionOnly := cfg.RC.IsBotMentionOnly()
	if promptYesNo(reader, fmt.Sprintf("Require @mention in group chats? (current: %v)", mentionOnly)) {
		t := true
		cfg.RC.BotMentionOnly = &t
	} else {
		f := false
		cfg.RC.BotMentionOnly = &f
	}

	// Step 2: Chat IDs
	fmt.Println()
	fmt.Println("--- Step 2: Chat IDs ---")
	fmt.Println()
	fmt.Println("Enter the chat IDs to monitor (comma-separated).")
	fmt.Println("Find them via: https://developers.ringcentral.com/api-reference/Chats/listGlipChatsNew")
	fmt.Println()

	existingIDs := strings.Join(cfg.RC.ChatIDs, ",")
	chatIDsStr := promptWithDefault(reader, "Chat IDs", existingIDs)
	if chatIDsStr != "" {
		ids := strings.Split(chatIDsStr, ",")
		cfg.RC.ChatIDs = nil
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id != "" {
				cfg.RC.ChatIDs = append(cfg.RC.ChatIDs, id)
			}
		}
	}

	// Step 3: Private App (optional)
	fmt.Println()
	fmt.Println("--- Step 3: Private App (Optional) ---")
	fmt.Println()
	fmt.Println("A Private App (REST API with JWT) enables:")
	fmt.Println("  - Summarize conversations from other chats")
	fmt.Println("  - Cross-chat actions and broader API access")
	fmt.Println()

	if promptYesNo(reader, "Configure a Private App?") {
		cfg.RC.ClientID = promptWithDefault(reader, "Client ID", cfg.RC.ClientID)
		cfg.RC.ClientSecret = promptWithDefault(reader, "Client Secret", cfg.RC.ClientSecret)
		cfg.RC.JWTToken = promptWithDefault(reader, "JWT Token", cfg.RC.JWTToken)

		if cfg.RC.HasPrivateApp() {
			fmt.Println()
			fmt.Print("Validating Private App credentials... ")
			if err := validatePrivateApp(cfg); err != nil {
				fmt.Println("FAILED")
				fmt.Printf("  Error: %v\n", err)
				fmt.Println("  Please check your Client ID, Client Secret, and JWT Token.")
				fmt.Println()
			} else {
				fmt.Println("OK")
			}
		}
	}

	// Save
	fmt.Println()
	fmt.Println("--- Configuration Summary ---")
	fmt.Println()
	fmt.Printf("  Server URL:    %s\n", cfg.RC.ServerURL)
	fmt.Printf("  Bot Token:     %s\n", maskSecret(cfg.RC.BotToken))
	fmt.Printf("  Mention Only:  %v\n", cfg.RC.IsBotMentionOnly())
	fmt.Printf("  Chat IDs:      %v\n", cfg.RC.ChatIDs)
	if cfg.RC.HasPrivateApp() {
		fmt.Printf("  Client ID:     %s\n", maskSecret(cfg.RC.ClientID))
		fmt.Printf("  Client Secret: %s\n", maskSecret(cfg.RC.ClientSecret))
		fmt.Printf("  JWT Token:     %s\n", maskSecret(cfg.RC.JWTToken))
	} else {
		fmt.Printf("  Private App:   not configured (summarize disabled)\n")
	}
	fmt.Println()

	if !promptYesNo(reader, "Save configuration?") {
		fmt.Println("Setup cancelled.")
		return nil
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	path, _ := config.ConfigPath()
	fmt.Printf("\nConfiguration saved to %s\n", path)
	fmt.Println("\nNext steps:")
	fmt.Println("  ringclaw start     # start the message bridge")
	fmt.Println("  ringclaw status    # check if running")
	return nil
}

func validatePrivateApp(cfg *config.Config) error {
	creds := &ringcentral.Credentials{
		ClientID:     cfg.RC.ClientID,
		ClientSecret: cfg.RC.ClientSecret,
		JWTToken:     cfg.RC.JWTToken,
		ServerURL:    cfg.RC.ServerURL,
	}
	client := ringcentral.NewClient(creds)
	if err := client.Authenticate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id, err := client.GetExtensionInfo(ctx)
	if err != nil {
		return fmt.Errorf("authenticated but failed to read account: %w", err)
	}
	fmt.Printf("(extension ID: %s) ", id)
	return nil
}

func validateBotToken(cfg *config.Config) error {
	serverURL := cfg.RC.ServerURL
	if serverURL == "" {
		serverURL = "https://platform.ringcentral.com"
	}
	client := ringcentral.NewBotClient(serverURL, cfg.RC.BotToken)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id, err := client.GetExtensionInfo(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("(bot extension ID: %s) ", id)
	return nil
}

func promptWithDefault(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("  %s [%s]: ", label, maskSecret(defaultVal))
	} else {
		fmt.Printf("  %s: ", label)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}

func promptYesNo(reader *bufio.Reader, question string) bool {
	fmt.Printf("  %s [Y/n]: ", question)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "" || line == "y" || line == "yes"
}

func maskSecret(s string) string {
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:]
}

func withDefault(val, def string) string {
	if val != "" {
		return val
	}
	return def
}
