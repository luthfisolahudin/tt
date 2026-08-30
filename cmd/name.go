package cmd

import (
	"fmt"

	"github.com/luthfisolahudin/tt/internal/session"

	"github.com/spf13/cobra"
)

var nameCmd = &cobra.Command{
	Use:   "name",
	Short: "Print computed session name",
	Long: `Print the tmux session name derived from the current project
directory. Useful as a target for "tt x send".`,
	Example: "  tt name",
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(session.SessionName())
	},
}

func init() {
	rootCmd.AddCommand(nameCmd)
}
