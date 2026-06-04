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

var (
	jsonOutput    bool
	logLevelFlag  string
	logFormatFlag string
	dirFlag       string
)

var rootCmd = &cobra.Command{
	Use:   "ringclaw",
	Short: "RingCentral AI agent bridge",
	Long:  "ringclaw bridges RingCentral Team Messaging to AI agents via the RingCentral API.",
	RunE:  runStart, // default command is start
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	rootCmd.PersistentFlags().StringVar(&logLevelFlag, "log-level", "", "Log level: debug, info, warn, error")
	rootCmd.PersistentFlags().StringVar(&logFormatFlag, "log-format", "", "Log format: text, json, color")
	rootCmd.PersistentFlags().StringVar(&dirFlag, "dir", "", "RingClaw home directory (default: ~/.ringclaw)")
	rootCmd.Version = FullVersion()
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
