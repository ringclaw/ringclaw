package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(stopCmd)
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the background ringclaw process",
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, err := readPid()
		if err != nil {
			fmt.Println("ringclaw is not running")
			return nil
		}

		if !processExists(pid) {
			os.Remove(pidFile())
			fmt.Println("ringclaw is not running (stale pid file removed)")
			return nil
		}

		p, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("find process: %w", err)
		}

		if err := signalTerminate(p); err != nil {
			return fmt.Errorf("stop process: %w", err)
		}

		os.Remove(pidFile())
		fmt.Printf("ringclaw stopped (pid=%d)\n", pid)
		return nil
	},
}
