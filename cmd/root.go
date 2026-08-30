package cmd

import (
	"github.com/luthfisolahudin/tt/internal/session"
	"github.com/luthfisolahudin/tt/internal/version"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tt [--continue]",
	Short: "tmux team: per-project tmux session with a pi worker pool",
	Long: `Bare "tt" is an alias for "tt up": create or heal the project's
tmux session. Outside tmux it attaches; inside tmux it builds/heals
only (use "tt up --attach" or "tt attach" to switch).

Pass -c / --continue to resume the latest Claude Code conversation
instead of starting fresh.`,
	Example:       "  tt\n  tt --continue\n  tt pi send alfa - <<<'TASK: inspect the authentication flow; SUCCESS: report findings.'",
	Version:       version.Version,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.SetVersionTemplate("{{.Name}} {{.Version}}\n")
	rootCmd.Flags().BoolP("continue", "c", false, "resume the default Claude Code conversation when launching a bare claude pane")
	rootCmd.AddCommand(piCmd)
	rootCmd.AddCommand(daemonCmd)
	// bare `tt` aliases `tt up` (bash dispatch: ""|up -> up_cmd)
	rootCmd.Run = func(cmd *cobra.Command, args []string) {
		continued, err := cmd.Flags().GetBool("continue")
		if err != nil {
			die(err.Error())
		}
		if err := session.Up(false, continued); err != nil {
			die(err.Error())
		}
	}
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		osExit(1)
	}
}
