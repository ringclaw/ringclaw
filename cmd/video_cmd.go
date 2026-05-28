package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/ringclaw/ringclaw/ringcentral"
	"github.com/spf13/cobra"
)

var videoBridgeType string

func init() {
	videoCreateCmd.Flags().StringVar(&videoBridgeType, "type", "Instant", "Bridge type: Instant, Scheduled, or PMI")
	videoCmd.AddCommand(videoCreateCmd)
	videoCmd.AddCommand(videoGetCmd)
	videoCmd.AddCommand(videoDeleteCmd)
	rootCmd.AddCommand(videoCmd)
}

var videoCmd = &cobra.Command{
	Use:   "video",
	Short: "RingCentral Video bridge operations",
}

var videoCreateCmd = &cobra.Command{
	Use:     "create <title>",
	Short:   "Create a RingCentral Video meeting bridge",
	Example: "  ringclaw video create \"Design Review\" --type Scheduled",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		bridge, err := client.CreateVideoBridge(ctx, &ringcentral.CreateVideoBridgeRequest{
			Name: strings.Join(args, " "),
			Type: videoBridgeType,
		})
		if err != nil {
			return fmt.Errorf("create video bridge failed: %w", err)
		}
		if jsonOutput {
			printJSON(bridge)
		} else {
			printVideoBridge(bridge)
		}
		return nil
	},
}

var videoGetCmd = &cobra.Command{
	Use:   "get <bridgeId>",
	Short: "Get a RingCentral Video bridge",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		bridge, err := client.GetVideoBridge(ctx, args[0])
		if err != nil {
			return fmt.Errorf("get video bridge failed: %w", err)
		}
		if jsonOutput {
			printJSON(bridge)
		} else {
			printVideoBridge(bridge)
		}
		return nil
	},
}

var videoDeleteCmd = &cobra.Command{
	Use:   "delete <bridgeId>",
	Short: "Delete a RingCentral Video bridge",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		if err := client.DeleteVideoBridge(ctx, args[0]); err != nil {
			return fmt.Errorf("delete video bridge failed: %w", err)
		}
		if jsonOutput {
			printJSON(map[string]string{"status": "deleted", "bridgeId": args[0]})
		} else {
			fmt.Printf("Video bridge %s deleted\n", args[0])
		}
		return nil
	},
}

func printVideoBridge(bridge *ringcentral.VideoBridge) {
	fmt.Printf("Video bridge: %s\n", bridge.ID)
	fmt.Printf("  Name: %s\n", bridge.Name)
	if bridge.Type != "" {
		fmt.Printf("  Type: %s\n", bridge.Type)
	}
	if bridge.Discovery.Web != "" {
		fmt.Printf("  Join: %s\n", bridge.Discovery.Web)
	}
}
