package cmd

import (
	"fmt"

	"github.com/luthfisolahudin/tt/internal/session"

	"github.com/spf13/cobra"
)

var nameCmd = &cobra.Command{
	Use:   "name",
	Short: "Print computed session name",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(session.SessionName())
	},
}

func init() {
	rootCmd.AddCommand(nameCmd)
}
