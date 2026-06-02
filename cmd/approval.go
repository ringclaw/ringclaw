package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/paths"
	"github.com/spf13/cobra"
)

var approvalCmd = &cobra.Command{
	Use:   "approval",
	Short: "Manage OOB approval challenges",
	Long: `Approve or deny pending OOB challenges issued by the running ringclaw process.

Challenges are created when a non-owner user triggers a cross-chat ACTION,
or when /full-access is requested. Approval requires access to the host
machine running ringclaw.`,
}

var approvalApproveCmd = &cobra.Command{
	Use:   "approve <id>",
	Short: "Approve a pending challenge",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return approvalAction(args[0], "approve")
	},
}

var approvalDenyCmd = &cobra.Command{
	Use:   "deny <id>",
	Short: "Deny a pending challenge",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return approvalAction(args[0], "deny")
	},
}

var approvalListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pending challenges",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return approvalList()
	},
}

func init() {
	approvalCmd.AddCommand(approvalApproveCmd)
	approvalCmd.AddCommand(approvalDenyCmd)
	approvalCmd.AddCommand(approvalListCmd)
	// Also allow: ringclaw approval <id>  (shorthand for approve)
	approvalCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return approvalAction(args[0], "approve")
		}
		return cmd.Help()
	}
	approvalCmd.Args = cobra.MaximumNArgs(1)
	rootCmd.AddCommand(approvalCmd)
}

func approvalAPIBase() (string, string, error) {
	token, err := loadAPIToken()
	if err != nil {
		return "", "", fmt.Errorf("load API token: %w", err)
	}
	return "http://127.0.0.1:18011", token, nil
}

func loadAPIToken() (string, error) {
	path, err := paths.AppPath("api_token")
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read api token: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func approvalAction(id, action string) error {
	base, token, err := approvalAPIBase()
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/api/approvals/%s", base, id)
	if action == "deny" {
		url += "/deny"
	}
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-RingClaw-Token", token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to ringclaw: %w (is ringclaw running?)", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result map[string]string
	if err := json.Unmarshal(body, &result); err == nil {
		fmt.Printf("Challenge %s: %s\n", result["id"], result["status"])
	}
	return nil
}

func approvalList() error {
	base, token, err := approvalAPIBase()
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, base+"/api/approvals", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-RingClaw-Token", token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to ringclaw: %w (is ringclaw running?)", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var challenges []struct {
		ID          string `json:"id"`
		Intent      string `json:"intent"`
		RequesterID string `json:"requester_id"`
		ExpiresIn   string `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &challenges); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if len(challenges) == 0 {
		fmt.Println("No pending challenges.")
		return nil
	}
	fmt.Printf("%-10s  %-12s  %-10s  %s\n", "ID", "EXPIRES IN", "REQUESTER", "INTENT")
	for _, c := range challenges {
		fmt.Printf("%-10s  %-12s  %-10s  %s\n", c.ID, c.ExpiresIn, c.RequesterID, c.Intent)
	}
	return nil
}
