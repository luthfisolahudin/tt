package cmd

import (
	"github.com/luthfisolahudin/tt/internal/version"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "tt",
	Short:         "tmux team: per-project tmux session with a pi worker pool",
	Version:       version.Version,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.SetVersionTemplate("{{.Name}} {{.Version}}\n")
	rootCmd.AddCommand(piCmd)
	rootCmd.AddCommand(daemonCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		osExit(1)
	}
}
