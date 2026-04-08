package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
	"github.com/spf13/cobra"
)

func init() {
	eventCmd.AddCommand(eventListCmd)
	eventCmd.AddCommand(eventCreateCmd)
	eventCmd.AddCommand(eventGetCmd)
	eventCmd.AddCommand(eventUpdateCmd)
	eventCmd.AddCommand(eventDeleteCmd)
	rootCmd.AddCommand(eventCmd)
}

var eventCmd = &cobra.Command{
	Use:   "event",
	Short: "Calendar event operations",
}

var eventListCmd = &cobra.Command{
	Use:   "list [chatId]",
	Short: "List events (global or per-chat if chatId given)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		cutoff := time.Now().AddDate(0, -3, 0).Format(time.RFC3339)
		if len(args) == 1 {
			list, err := client.ListGroupEvents(ctx, args[0])
			if err != nil {
				return fmt.Errorf("list group events failed: %w", err)
			}
			filtered := list.Records[:0]
			for _, e := range list.Records {
				if e.StartTime >= cutoff {
					filtered = append(filtered, e)
				}
			}
			sort.Slice(filtered, func(i, j int) bool {
				return filtered[i].StartTime > filtered[j].StartTime
			})
			list.Records = filtered
			if jsonOutput {
				printJSON(list)
			} else {
				fmt.Printf("Events in %s (%d)\n", args[0], len(filtered))
				for _, e := range filtered {
					printEvent(&e)
				}
			}
			return nil
		}

		list, err := client.ListEvents(ctx)
		if err != nil {
			return fmt.Errorf("list events failed: %w", err)
		}
		filtered := list.Records[:0]
		for _, e := range list.Records {
			if e.StartTime >= cutoff {
				filtered = append(filtered, e)
			}
		}
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].StartTime > filtered[j].StartTime
		})
		list.Records = filtered
		if jsonOutput {
			printJSON(list)
		} else {
			fmt.Printf("Events (%d)\n", len(filtered))
			for _, e := range filtered {
				printEvent(&e)
			}
		}
		return nil
	},
}

var eventCreateCmd = &cobra.Command{
	Use:     "create <title> <startTime> <endTime>",
	Short:   "Create an event",
	Example: "  ringclaw event create \"Team Meeting\" 2026-04-01T14:00:00Z 2026-04-01T15:00:00Z",
	Args:    cobra.MinimumNArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		endTime := args[len(args)-1]
		startTime := args[len(args)-2]
		title := strings.Join(args[:len(args)-2], " ")

		event, err := client.CreateEvent(ctx, &ringcentral.CreateEventRequest{
			Title: title, StartTime: startTime, EndTime: endTime,
		})
		if err != nil {
			return fmt.Errorf("create event failed: %w", err)
		}
		if jsonOutput {
			printJSON(event)
		} else {
			fmt.Printf("Event created: %s — %s\n", event.ID, event.Title)
		}
		return nil
	},
}

var eventGetCmd = &cobra.Command{
	Use:   "get <eventId>",
	Short: "Get event details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		event, err := client.GetEvent(ctx, args[0])
		if err != nil {
			return fmt.Errorf("get event failed: %w", err)
		}
		if jsonOutput {
			printJSON(event)
		} else {
			fmt.Printf("Event: %s\n", event.ID)
			fmt.Printf("  Title: %s\n", event.Title)
			fmt.Printf("  Start: %s\n", event.StartTime)
			fmt.Printf("  End:   %s\n", event.EndTime)
			if event.Location != "" {
				fmt.Printf("  Loc:   %s\n", event.Location)
			}
		}
		return nil
	},
}

var eventUpdateCmd = &cobra.Command{
	Use:   "update <eventId> <key=value...>",
	Short: "Update an event (title=X start=T end=T location=L)",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		req := &ringcentral.UpdateEventRequest{}
		for _, arg := range args[1:] {
			k, v, ok := strings.Cut(arg, "=")
			if !ok {
				continue
			}
			switch strings.ToLower(k) {
			case "title":
				req.Title = v
			case "start", "starttime":
				req.StartTime = v
			case "end", "endtime":
				req.EndTime = v
			case "location":
				req.Location = v
			case "description":
				req.Description = v
			case "color":
				req.Color = v
			}
		}
		event, err := client.UpdateEvent(ctx, args[0], req)
		if err != nil {
			return fmt.Errorf("update event failed: %w", err)
		}
		if jsonOutput {
			printJSON(event)
		} else {
			fmt.Printf("Event updated: %s — %s\n", event.ID, event.Title)
		}
		return nil
	},
}

var eventDeleteCmd = &cobra.Command{
	Use:   "delete <eventId>",
	Short: "Delete an event",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		if err := client.DeleteEvent(ctx, args[0]); err != nil {
			return fmt.Errorf("delete event failed: %w", err)
		}
		if jsonOutput {
			printJSON(map[string]string{"status": "deleted", "eventId": args[0]})
		} else {
			fmt.Printf("Event %s deleted\n", args[0])
		}
		return nil
	},
}

func printEvent(e *ringcentral.Event) {
	start := e.StartTime
	if len(start) > 16 {
		start = start[:16]
	}
	fmt.Printf("  %s  %s  %s\n", e.ID, start, e.Title)
}
