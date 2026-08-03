package cmd

import (
	"github.com/luthfisolahudin/tt/internal/session"
	"github.com/luthfisolahudin/tt/internal/version"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "tt",
	Short:         "tmux team: per-project tmux session with a pi worker pool",
	Example:       "  tt up\n  tt pi send alfa - <<<'TASK: inspect the authentication flow; SUCCESS: report findings.'",
	Version:       version.Version,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.SetVersionTemplate("{{.Name}} {{.Version}}\n")
	rootCmd.AddCommand(piCmd)
	rootCmd.AddCommand(daemonCmd)
	// bare `tt` aliases `tt up` (bash dispatch: ""|up -> up_cmd)
	rootCmd.Run = func(cmd *cobra.Command, args []string) {
		if err := session.Up(false); err != nil {
			die(err.Error())
		}
	}
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		osExit(1)
	}
}
