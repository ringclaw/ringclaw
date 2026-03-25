package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version and Commit are set at build time via -ldflags.
var (
	Version = "dev"
	Commit  = "unknown"
)

// FullVersion returns version with commit hash.
func FullVersion() string {
	if Commit == "unknown" || len(Commit) == 0 {
		return Version
	}
	short := Commit
	if len(short) > 7 {
		short = short[:7]
	}
	return fmt.Sprintf("%s (%s)", Version, short)
}

var rootCmd = &cobra.Command{
	Use:   "ringclaw",
	Short: "RingCentral AI agent bridge",
	Long:  "ringclaw bridges RingCentral Team Messaging to AI agents via the RingCentral API.",
	RunE:  runStart, // default command is start
}

func init() {
	rootCmd.Version = FullVersion()
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
