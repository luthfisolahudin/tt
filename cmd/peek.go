package cmd

import (
	"github.com/spf13/cobra"
)

// peekCmd is `tt peek <window|worker> [--lines N]` — a read-only daemon state
// query returning any window's current pane content, so an agent can read the
// dev server, a worker REPL, or the orchestrator pane without scraping tmux.
var peekCmd = &cobra.Command{
	Use:                "peek [--lines N] <window|callsign>",
	Short:              "Read a window's current pane content (any window, read-only)",
	Example:            "  tt peek --lines 50 dev\n  tt peek --lines 100 alfa",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		lines, target := 200, ""
		i := 0
		for ; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--lines":
				i++
				if i >= len(args) {
					die("peek: --lines requires a count")
				}
				if !isPositiveInt(args[i]) {
					die("peek: --lines must be a positive integer")
				}
				lines = atoi(args[i])
			case len(a) > 0 && a[0] == '-' && a != "-":
				die("unknown flag for peek: " + a)
			default:
				if target == "" {
					target = a
				} else {
					die("extra arg: " + a)
				}
			}
		}
		if target == "" {
			die("peek: target window or callsign required")
		}
		code := doDaemon("peek", struct {
			Target string `json:"target"`
			Lines  int    `json:"lines"`
		}{target, lines})
		if code != 0 {
			osExit(code)
		}
	},
}

func init() {
	rootCmd.AddCommand(peekCmd)
}
