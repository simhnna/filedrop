package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Set at release build time via -ldflags "-X filedrop-cli/cmd.version=..."
var version = "dev"

var rootCmd = &cobra.Command{
	Use:     "filedrop",
	Short:   "Transfer files via filedrop.hannaweb.eu",
	Version: version,
	// Runtime errors (wrong code, disconnects) aren't usage mistakes
	SilenceUsage:  true,
	SilenceErrors: true, // Execute prints the error itself
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
