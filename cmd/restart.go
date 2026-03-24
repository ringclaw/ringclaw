package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(restartCmd)
}

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the background ringclaw process",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Stop if running
		pid, err := readPid()
		if err == nil && processExists(pid) {
			fmt.Printf("Stopping ringclaw (pid=%d)...\n", pid)
			if p, err := os.FindProcess(pid); err == nil {
				signalTerminate(p)
			}
			for i := 0; i < 20; i++ {
				if !processExists(pid) {
					break
				}
				time.Sleep(500 * time.Millisecond)
			}
			os.Remove(pidFile())
		}

		// Start
		fmt.Println("Starting ringclaw...")
		return runDaemon()
	},
}
