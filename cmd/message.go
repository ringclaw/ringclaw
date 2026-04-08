package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ringclaw/ringclaw/ringcentral"
	"github.com/spf13/cobra"
)

func init() {
	msgListCmd.Flags().IntVar(&msgCount, "count", 30, "Number of messages to fetch")

	msgCmd.AddCommand(msgSendCmd)
	msgCmd.AddCommand(msgGetCmd)
	msgCmd.AddCommand(msgListCmd)
	msgCmd.AddCommand(msgEditCmd)
	msgCmd.AddCommand(msgDeleteCmd)
	rootCmd.AddCommand(msgCmd)
}

var msgCount int

var msgCmd = &cobra.Command{
	Use:   "message",
	Short: "Message operations",
}

var msgSendCmd = &cobra.Command{
	Use:   "send <chatId> <text>",
	Short: "Send a message to a chat",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		chatID := args[0]
		text := strings.Join(args[1:], " ")
		post, err := client.SendPost(ctx, chatID, text)
		if err != nil {
			return fmt.Errorf("send failed: %w", err)
		}
		if jsonOutput {
			printJSON(post)
		} else {
			fmt.Printf("Message sent (id=%s)\n", post.ID)
		}
		return nil
	},
}

var msgGetCmd = &cobra.Command{
	Use:   "get <chatId> <postId>",
	Short: "Fetch a single message",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		post, err := client.GetPost(ctx, args[0], args[1])
		if err != nil {
			return fmt.Errorf("get failed: %w", err)
		}
		if jsonOutput {
			printJSON(post)
		} else {
			printPost(post)
		}
		return nil
	},
}

var msgListCmd = &cobra.Command{
	Use:   "list <chatId>",
	Short: "List recent messages in a chat",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		list, err := client.ListPosts(ctx, args[0], ringcentral.ListPostsOpts{RecordCount: msgCount})
		if err != nil {
			return fmt.Errorf("list failed: %w", err)
		}
		sort.Slice(list.Records, func(i, j int) bool {
			return list.Records[i].CreationTime > list.Records[j].CreationTime
		})
		if jsonOutput {
			printJSON(list)
		} else {
			fmt.Printf("Messages (%d)\n", len(list.Records))
			for _, p := range list.Records {
				printPost(&p)
				fmt.Println()
			}
		}
		return nil
	},
}

var msgEditCmd = &cobra.Command{
	Use:   "edit <chatId> <postId> <text>",
	Short: "Edit a message",
	Args:  cobra.MinimumNArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		text := strings.Join(args[2:], " ")
		post, err := client.UpdatePost(ctx, args[0], args[1], text)
		if err != nil {
			return fmt.Errorf("edit failed: %w", err)
		}
		if jsonOutput {
			printJSON(post)
		} else {
			fmt.Printf("Message updated (id=%s)\n", post.ID)
		}
		return nil
	},
}

var msgDeleteCmd = &cobra.Command{
	Use:   "delete <chatId> <postId>",
	Short: "Delete a message",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		if err := client.DeletePost(ctx, args[0], args[1]); err != nil {
			return fmt.Errorf("delete failed: %w", err)
		}
		if jsonOutput {
			printJSON(map[string]string{"status": "deleted", "postId": args[1]})
		} else {
			fmt.Printf("Message %s deleted\n", args[1])
		}
		return nil
	},
}

func printPost(p *ringcentral.Post) {
	ts := p.CreationTime
	if len(ts) > 16 {
		ts = ts[:16]
	}
	text := p.Text
	if len(text) > 120 {
		text = text[:120] + "..."
	}
	fmt.Printf("[%s] %s (by %s): %s\n", ts, p.ID, p.CreatorID, text)
}
