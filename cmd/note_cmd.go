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
	noteCmd.AddCommand(noteListCmd)
	noteCmd.AddCommand(noteCreateCmd)
	noteCmd.AddCommand(noteGetCmd)
	noteCmd.AddCommand(noteUpdateCmd)
	noteCmd.AddCommand(noteLockCmd)
	noteCmd.AddCommand(noteUnlockCmd)
	noteCmd.AddCommand(noteDeleteCmd)
	rootCmd.AddCommand(noteCmd)
}

var noteCmd = &cobra.Command{
	Use:   "note",
	Short: "Note operations",
}

var noteListCmd = &cobra.Command{
	Use:   "list <chatId>",
	Short: "List notes in a chat",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		list, err := client.ListNotes(ctx, args[0])
		if err != nil {
			return fmt.Errorf("list notes failed: %w", err)
		}
		sort.Slice(list.Records, func(i, j int) bool {
			return list.Records[i].CreationTime > list.Records[j].CreationTime
		})
		if jsonOutput {
			printJSON(list)
		} else {
			fmt.Printf("Notes (%d)\n", len(list.Records))
			for _, n := range list.Records {
				fmt.Printf("  %s  [%s]  %s\n", n.ID, n.Status, n.Title)
			}
		}
		return nil
	},
}

var noteCreateCmd = &cobra.Command{
	Use:   "create <chatId> <title> [body]",
	Short: "Create a note (auto-published)",
	Long:  "Create a note. Use '|' to separate title from body: ringclaw note create <chatId> \"title | body text\"",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		content := strings.Join(args[1:], " ")
		title, body, _ := strings.Cut(content, "|")
		title = strings.TrimSpace(title)
		body = strings.TrimSpace(body)

		note, err := client.CreateNote(ctx, args[0], &ringcentral.CreateNoteRequest{Title: title, Body: body})
		if err != nil {
			return fmt.Errorf("create note failed: %w", err)
		}
		_ = client.PublishNote(ctx, note.ID)
		if jsonOutput {
			printJSON(note)
		} else {
			fmt.Printf("Note created: %s — %s\n", note.ID, note.Title)
		}
		return nil
	},
}

var noteGetCmd = &cobra.Command{
	Use:   "get <noteId>",
	Short: "Get note details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		note, err := client.GetNote(ctx, args[0])
		if err != nil {
			return fmt.Errorf("get note failed: %w", err)
		}
		if jsonOutput {
			printJSON(note)
		} else {
			fmt.Printf("Note: %s\n", note.ID)
			fmt.Printf("  Title:   %s\n", note.Title)
			fmt.Printf("  Status:  %s\n", note.Status)
			if note.Preview != "" {
				fmt.Printf("  Preview: %s\n", note.Preview)
			}
			fmt.Printf("  Created: %s\n", note.CreationTime)
		}
		return nil
	},
}

var noteUpdateCmd = &cobra.Command{
	Use:   "update <noteId> <key=value...>",
	Short: "Update a note (title=X body=Y)",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		req := &ringcentral.UpdateNoteRequest{}
		for _, arg := range args[1:] {
			k, v, ok := strings.Cut(arg, "=")
			if !ok {
				continue
			}
			switch strings.ToLower(k) {
			case "title":
				req.Title = v
			case "body":
				req.Body = v
			}
		}
		note, err := client.UpdateNote(ctx, args[0], req)
		if err != nil {
			return fmt.Errorf("update note failed: %w", err)
		}
		if jsonOutput {
			printJSON(note)
		} else {
			fmt.Printf("Note updated: %s — %s\n", note.ID, note.Title)
		}
		return nil
	},
}

var noteLockCmd = &cobra.Command{
	Use:   "lock <noteId>",
	Short: "Lock a note for exclusive editing",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		if err := client.LockNote(ctx, args[0]); err != nil {
			return fmt.Errorf("lock note failed: %w", err)
		}
		if jsonOutput {
			printJSON(map[string]string{"status": "locked", "noteId": args[0]})
		} else {
			fmt.Printf("Note %s locked\n", args[0])
		}
		return nil
	},
}

var noteUnlockCmd = &cobra.Command{
	Use:   "unlock <noteId>",
	Short: "Unlock a note",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		if err := client.UnlockNote(ctx, args[0]); err != nil {
			return fmt.Errorf("unlock note failed: %w", err)
		}
		if jsonOutput {
			printJSON(map[string]string{"status": "unlocked", "noteId": args[0]})
		} else {
			fmt.Printf("Note %s unlocked\n", args[0])
		}
		return nil
	},
}

var noteDeleteCmd = &cobra.Command{
	Use:   "delete <noteId>",
	Short: "Delete a note",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		if err := client.DeleteNote(ctx, args[0]); err != nil {
			return fmt.Errorf("delete note failed: %w", err)
		}
		if jsonOutput {
			printJSON(map[string]string{"status": "deleted", "noteId": args[0]})
		} else {
			fmt.Printf("Note %s deleted\n", args[0])
		}
		return nil
	},
}
