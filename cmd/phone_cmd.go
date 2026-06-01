package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/ringclaw/ringclaw/ringcentral"
	"github.com/spf13/cobra"
)

var (
	phoneCallerID    string
	phoneFrom        string
	phonePlayPrompt  bool
	callLogView      string
	callLogDirection string
	callLogType      string
	callLogResult    string
	callLogDateFrom  string
	callLogDateTo    string
	callLogLimit     int
	callLogExtension string
)

func init() {
	phoneRingOutCmd.Flags().StringVar(&phoneFrom, "from", "", "Optional owner forwarding/callback phone number. Omit to use a RingOut callback number configured for the current JWT user")
	phoneRingOutCmd.Flags().StringVar(&phoneCallerID, "caller-id", "", "Caller ID phone number")
	phoneRingOutCmd.Flags().BoolVar(&phonePlayPrompt, "play-prompt", false, "Play a prompt before connecting the RingOut call")

	phoneCallLogCmd.Flags().StringVar(&callLogView, "view", "Simple", "Call log view: Simple or Detailed")
	phoneCallLogCmd.Flags().StringVar(&callLogDirection, "direction", "", "Call direction: Inbound or Outbound")
	phoneCallLogCmd.Flags().StringVar(&callLogType, "type", "", "Call log type, for example Voice")
	phoneCallLogCmd.Flags().StringVar(&callLogResult, "result", "", "Client-side result filter, for example Missed")
	phoneCallLogCmd.Flags().StringVar(&callLogDateFrom, "date-from", "", "Start time filter in ISO8601")
	phoneCallLogCmd.Flags().StringVar(&callLogDateTo, "date-to", "", "End time filter in ISO8601")
	phoneCallLogCmd.Flags().IntVar(&callLogLimit, "limit", 10, "Number of call log records")
	phoneCallLogCmd.Flags().StringVar(&callLogExtension, "extension-id", "", "FIJI requester extension ID for user-scoped call logs")

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
	Use:     "ringout <toPhone>",
	Short:   "Start a two-legged RingOut call",
	Example: "  ringclaw phone ringout +14155550199 --caller-id +14155550100",
	Args:    cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		from := phoneFrom
		to := args[0]
		if len(args) == 2 {
			from = args[0]
			to = args[1]
		}
		if from == "" {
			from, err = defaultCLIRingOutFromNumber(ctx, client)
			if err != nil {
				return err
			}
		}
		req := &ringcentral.CreateRingOutRequest{
			To:         ringcentral.PhoneNumberRef{PhoneNumber: to},
			PlayPrompt: phonePlayPrompt,
		}
		if from != "" {
			req.From = &ringcentral.PhoneNumberRef{PhoneNumber: from}
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

func defaultCLIRingOutFromNumber(ctx context.Context, client *ringcentral.Client) (string, error) {
	list, err := client.ListForwardingNumbers(ctx)
	if err != nil {
		return "", fmt.Errorf("list current extension forwarding numbers: %w", err)
	}
	for _, record := range list.Records {
		if phone := strings.TrimSpace(record.PhoneNumber); phone != "" && forwardingNumberAvailable(record) && forwardingNumberHasFeature(record, "RingOut") {
			return phone, nil
		}
	}
	for _, record := range list.Records {
		if phone := strings.TrimSpace(record.PhoneNumber); phone != "" && forwardingNumberAvailable(record) {
			return phone, nil
		}
	}
	return "", fmt.Errorf("current extension has no RingOut forwarding/callback number; configure a RingOut callback number in RingCentral call handling or pass --from <callback phone>")
}

func forwardingNumberAvailable(record ringcentral.ForwardingNumber) bool {
	if record.Hidden {
		return false
	}
	status := strings.TrimSpace(record.Status)
	return status == "" || strings.EqualFold(status, "Normal")
}

func forwardingNumberHasFeature(record ringcentral.ForwardingNumber, want string) bool {
	for _, feature := range record.Features {
		if strings.EqualFold(strings.TrimSpace(feature), want) {
			return true
		}
	}
	return false
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

		opts := ringcentral.CallLogOptions{
			RecordCount: callLogLimit,
			ExtensionID: strings.TrimSpace(callLogExtension),
			View:        callLogView,
			Direction:   callLogDirection,
			Type:        callLogType,
			Result:      callLogResult,
			DateFrom:    callLogDateFrom,
			DateTo:      callLogDateTo,
		}
		list, err := client.ListExtensionCallLog(ctx, opts)
		if err != nil {
			return fmt.Errorf("list call log failed: %w", err)
		}
		list.Records = filterCLIPhoneCallLogRecords(list.Records, callLogResult)
		if jsonOutput {
			printJSON(list)
		} else {
			records := list.Records
			fmt.Printf("Call logs (%d)\n", len(records))
			for _, rec := range records {
				fmt.Printf("  %s  %s  %s  %s  %s -> %s  %ds\n",
					rec.ID, rec.StartTime, rec.Direction, rec.Result, rec.From.PhoneNumber, rec.To.PhoneNumber, rec.Duration)
			}
		}
		return nil
	},
}

func filterCLIPhoneCallLogRecords(records []ringcentral.CallLogRecord, result string) []ringcentral.CallLogRecord {
	result = strings.TrimSpace(result)
	if result == "" {
		return records
	}
	filtered := make([]ringcentral.CallLogRecord, 0, len(records))
	for _, rec := range records {
		if strings.EqualFold(strings.TrimSpace(rec.Result), result) {
			filtered = append(filtered, rec)
		}
	}
	return filtered
}

func printRingOut(ringOut *ringcentral.RingOut) {
	fmt.Printf("RingOut: %s\n", ringOut.ID)
	fmt.Printf("  Call:   %s\n", ringOut.Status.CallStatus)
	fmt.Printf("  Caller: %s\n", ringOut.Status.CallerStatus)
	fmt.Printf("  Callee: %s\n", ringOut.Status.CalleeStatus)
}
