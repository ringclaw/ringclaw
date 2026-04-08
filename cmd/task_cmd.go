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
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskCreateCmd)
	taskCmd.AddCommand(taskGetCmd)
	taskCmd.AddCommand(taskUpdateCmd)
	taskCmd.AddCommand(taskCompleteCmd)
	taskCmd.AddCommand(taskDeleteCmd)
	rootCmd.AddCommand(taskCmd)
}

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Task operations",
}

var taskListCmd = &cobra.Command{
	Use:   "list <chatId>",
	Short: "List tasks in a chat",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		list, err := client.ListTasks(ctx, args[0])
		if err != nil {
			return fmt.Errorf("list tasks failed: %w", err)
		}
		sort.Slice(list.Records, func(i, j int) bool {
			return list.Records[i].CreationTime > list.Records[j].CreationTime
		})
		if jsonOutput {
			printJSON(list)
		} else {
			fmt.Printf("Tasks (%d)\n", len(list.Records))
			for _, t := range list.Records {
				fmt.Printf("  [%s] %s  %s\n", t.Status, t.ID, t.Subject)
			}
		}
		return nil
	},
}

var taskCreateCmd = &cobra.Command{
	Use:   "create <chatId> <subject>",
	Short: "Create a task",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		subject := strings.Join(args[1:], " ")
		task, err := client.CreateTask(ctx, args[0], &ringcentral.CreateTaskRequest{Subject: subject})
		if err != nil {
			return fmt.Errorf("create task failed: %w", err)
		}
		if jsonOutput {
			printJSON(task)
		} else {
			fmt.Printf("Task created: %s — %s\n", task.ID, task.Subject)
		}
		return nil
	},
}

var taskGetCmd = &cobra.Command{
	Use:   "get <taskId>",
	Short: "Get task details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		task, err := client.GetTask(ctx, args[0])
		if err != nil {
			return fmt.Errorf("get task failed: %w", err)
		}
		if jsonOutput {
			printJSON(task)
		} else {
			fmt.Printf("Task: %s\n", task.ID)
			fmt.Printf("  Subject: %s\n", task.Subject)
			fmt.Printf("  Status:  %s\n", task.Status)
			if task.DueDate != "" {
				fmt.Printf("  Due:     %s\n", task.DueDate)
			}
			if task.Description != "" {
				fmt.Printf("  Desc:    %s\n", task.Description)
			}
		}
		return nil
	},
}

var taskUpdateCmd = &cobra.Command{
	Use:   "update <taskId> <key=value...>",
	Short: "Update a task (subject=X description=Y status=Z)",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		req := &ringcentral.UpdateTaskRequest{}
		for _, arg := range args[1:] {
			k, v, ok := strings.Cut(arg, "=")
			if !ok {
				continue
			}
			switch strings.ToLower(k) {
			case "subject":
				req.Subject = v
			case "description":
				req.Description = v
			case "status":
				req.Status = v
			case "duedate", "due_date":
				req.DueDate = v
			case "color":
				req.Color = v
			}
		}
		task, err := client.UpdateTask(ctx, args[0], req)
		if err != nil {
			return fmt.Errorf("update task failed: %w", err)
		}
		if jsonOutput {
			printJSON(task)
		} else {
			fmt.Printf("Task updated: %s — %s\n", task.ID, task.Subject)
		}
		return nil
	},
}

var taskCompleteCmd = &cobra.Command{
	Use:   "complete <taskId>",
	Short: "Mark a task as completed",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		if err := client.CompleteTask(ctx, args[0]); err != nil {
			return fmt.Errorf("complete task failed: %w", err)
		}
		if jsonOutput {
			printJSON(map[string]string{"status": "completed", "taskId": args[0]})
		} else {
			fmt.Printf("Task %s marked as completed\n", args[0])
		}
		return nil
	},
}

var taskDeleteCmd = &cobra.Command{
	Use:   "delete <taskId>",
	Short: "Delete a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newCLIClient()
		if err != nil {
			return err
		}
		ctx, cancel := notifyContext(context.Background())
		defer cancel()

		if err := client.DeleteTask(ctx, args[0]); err != nil {
			return fmt.Errorf("delete task failed: %w", err)
		}
		if jsonOutput {
			printJSON(map[string]string{"status": "deleted", "taskId": args[0]})
		} else {
			fmt.Printf("Task %s deleted\n", args[0])
		}
		return nil
	},
}
