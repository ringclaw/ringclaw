package cmd

import (
	"context"
	"fmt"

	"github.com/ringclaw/ringclaw/ringcentral"
	"github.com/spf13/cobra"
)

var (
	phoneCallerID    string
	phonePlayPrompt  bool
	callLogView      string
	callLogDirection string
	callLogType      string
	callLogDateFrom  string
	callLogDateTo    string
	callLogLimit     int
)

func init() {
	phoneRingOutCmd.Flags().StringVar(&phoneCallerID, "caller-id", "", "Caller ID phone number")
	phoneRingOutCmd.Flags().BoolVar(&phonePlayPrompt, "play-prompt", false, "Play a prompt before connecting the RingOut call")

	phoneCallLogCmd.Flags().StringVar(&callLogView, "view", "Simple", "Call log view: Simple or Detailed")
	phoneCallLogCmd.Flags().StringVar(&callLogDirection, "direction", "", "Call direction: Inbound or Outbound")
	phoneCallLogCmd.Flags().StringVar(&callLogType, "type", "", "Call log type, for example Voice")
	phoneCallLogCmd.Flags().StringVar(&callLogDateFrom, "date-from", "", "Start time filter in ISO8601")
	phoneCallLogCmd.Flags().StringVar(&callLogDateTo, "date-to", "", "End time filter in ISO8601")
	phoneCallLogCmd.Flags().IntVar(&callLogLimit, "limit", 10, "Number of call log records")

	phoneCmd.AddCommand(phoneRingOutCmd)
	phoneCmd.AddCommand(phoneStatusCmd)
	phoneCmd.AddCommand(phoneCancelCmd)
	phoneCmd.AddCommand(phoneCallLogCmd)
	rootCmd.AddCommand(phoneCmd)
}

var phoneCmd = &cobra.Command{
	Use:   "phone",
	Short: "RingCentral Phone operations",
}

var phoneRingOutCmd = &cobra.Command{
	Use:     "ringout <fromPhone> <toPhone>",
	Short:   "Start a two-legged RingOut call",
	Example: "  ringclaw phone ringout +14155550100 +14155550199 --caller-id +14155550100",
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		req := &ringcentral.CreateRingOutRequest{
			From:       ringcentral.PhoneNumberRef{PhoneNumber: args[0]},
			To:         ringcentral.PhoneNumberRef{PhoneNumber: args[1]},
			PlayPrompt: phonePlayPrompt,
		}
		if phoneCallerID != "" {
			req.CallerID = &ringcentral.PhoneNumberRef{PhoneNumber: phoneCallerID}
		}
		ringOut, err := client.CreateRingOut(ctx, req)
		if err != nil {
			return fmt.Errorf("start ringout failed: %w", err)
		}
		if jsonOutput {
			printJSON(ringOut)
		} else {
			printRingOut(ringOut)
		}
		return nil
	},
}

var phoneStatusCmd = &cobra.Command{
	Use:   "status <ringOutId>",
	Short: "Get RingOut status",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		ringOut, err := client.GetRingOut(ctx, args[0])
		if err != nil {
			return fmt.Errorf("get ringout failed: %w", err)
		}
		if jsonOutput {
			printJSON(ringOut)
		} else {
			printRingOut(ringOut)
		}
		return nil
	},
}

var phoneCancelCmd = &cobra.Command{
	Use:   "cancel <ringOutId>",
	Short: "Cancel a RingOut call while it is still connecting",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		if err := client.DeleteRingOut(ctx, args[0]); err != nil {
			return fmt.Errorf("cancel ringout failed: %w", err)
		}
		if jsonOutput {
			printJSON(map[string]string{"status": "cancelled", "ringOutId": args[0]})
		} else {
			fmt.Printf("RingOut %s cancelled\n", args[0])
		}
		return nil
	},
}

var phoneCallLogCmd = &cobra.Command{
	Use:   "calllog",
	Short: "List extension-level call logs",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		list, err := client.ListExtensionCallLog(ctx, ringcentral.CallLogOptions{
			RecordCount: callLogLimit,
			View:        callLogView,
			Direction:   callLogDirection,
			Type:        callLogType,
			DateFrom:    callLogDateFrom,
			DateTo:      callLogDateTo,
		})
		if err != nil {
			return fmt.Errorf("list call log failed: %w", err)
		}
		if jsonOutput {
			printJSON(list)
		} else {
			fmt.Printf("Call logs (%d)\n", len(list.Records))
			for _, rec := range list.Records {
				fmt.Printf("  %s  %s  %s  %s -> %s  %ds\n",
					rec.ID, rec.StartTime, rec.Direction, rec.From.PhoneNumber, rec.To.PhoneNumber, rec.Duration)
			}
		}
		return nil
	},
}

func printRingOut(ringOut *ringcentral.RingOut) {
	fmt.Printf("RingOut: %s\n", ringOut.ID)
	fmt.Printf("  Call:   %s\n", ringOut.Status.CallStatus)
	fmt.Printf("  Caller: %s\n", ringOut.Status.CallerStatus)
	fmt.Printf("  Callee: %s\n", ringOut.Status.CalleeStatus)
}
