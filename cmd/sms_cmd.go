package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/ringclaw/ringclaw/ringcentral"
	"github.com/spf13/cobra"
)

var smsFrom string

func init() {
	smsSendCmd.Flags().StringVar(&smsFrom, "from", "", "Optional sender phone number. Omit to use the first phone number on the current extension")
	smsCmd.AddCommand(smsSendCmd)
	rootCmd.AddCommand(smsCmd)
}

var smsCmd = &cobra.Command{
	Use:   "sms",
	Short: "RingCentral SMS operations",
}

var smsSendCmd = &cobra.Command{
	Use:     "send <toPhone> <message>",
	Short:   "Send an SMS message",
	Example: "  ringclaw sms send +14155550199 \"Hello from RingClaw\"",
	Args:    cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		from := strings.TrimSpace(smsFrom)
		if from == "" {
			from, err = defaultCLISMSFromNumber(ctx, client)
			if err != nil {
				return err
			}
		}
		req := &ringcentral.CreateSMSRequest{
			From: ringcentral.PhoneNumberRef{PhoneNumber: from},
			To:   []ringcentral.PhoneNumberRef{{PhoneNumber: args[0]}},
			Text: strings.TrimSpace(strings.Join(args[1:], " ")),
		}
		msg, err := client.SendSMS(ctx, req)
		if err != nil {
			return fmt.Errorf("send sms failed: %w", err)
		}
		if jsonOutput {
			printJSON(msg)
		} else {
			printSMSMessage(msg)
		}
		return nil
	},
}

func defaultCLISMSFromNumber(ctx context.Context, client *ringcentral.Client) (string, error) {
	list, err := client.ListExtensionPhoneNumbers(ctx)
	if err != nil {
		return "", fmt.Errorf("list current extension phone numbers: %w", err)
	}
	for _, record := range list.Records {
		if phone := strings.TrimSpace(record.PhoneNumber); phone != "" && extensionPhoneNumberAvailable(record) {
			return phone, nil
		}
	}
	return "", fmt.Errorf("current extension has no active phone number for SMS; pass --from <owned phone number>")
}

func extensionPhoneNumberAvailable(record ringcentral.ExtensionPhoneNumber) bool {
	status := strings.TrimSpace(record.Status)
	return status == "" || strings.EqualFold(status, "Normal")
}

func printSMSMessage(msg *ringcentral.SMSMessage) {
	fmt.Printf("SMS: %s\n", ringcentral.FormatResourceID(msg.ID))
	fmt.Printf("  Status: %s\n", firstNonEmptyString(msg.MessageStatus, "Unknown"))
	fmt.Printf("  From:   %s\n", msg.From.PhoneNumber)
	if len(msg.To) > 0 {
		fmt.Printf("  To:     %s\n", msg.To[0].PhoneNumber)
	}
	if msg.Subject != "" {
		fmt.Printf("  Text:   %s\n", msg.Subject)
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
