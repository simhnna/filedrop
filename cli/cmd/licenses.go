package cmd

import (
	"fmt"

	"filedrop-cli/internal/licenses"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(licensesCmd)
}

var licensesCmd = &cobra.Command{
	Use:   "licenses",
	Short: "Print the licenses of filedrop and the software it includes",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(licenses.Text)
	},
}
