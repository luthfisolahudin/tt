package cmd

import (
	"strings"

	"github.com/luthfisolahudin/tt/internal/session"
	"github.com/spf13/cobra"
)

// upCmd is `tt up` — idempotent session create/heal. Inside tmux it does NOT
// switch away by default (that stole the caller's window); --attach switches.
// Outside tmux it attaches (the point of `up`). Off a tty attach is harmless.
var upCmd = &cobra.Command{
	Use:                "up [--attach]",
	Short:              "Create or heal the project's tmux session (attach outside tmux; --attach to switch in-tmux)",
	Example:            "  tt up\n  tt up --attach",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		attach := false
		for _, a := range args {
			if a == "--attach" {
				attach = true
				continue
			}
			if strings.HasPrefix(a, "-") {
				die("unknown flag for up: " + a)
			}
			die("up: unexpected argument: " + a)
		}
		if err := session.Up(attach); err != nil {
			die(err.Error())
		}
	},
}

// attachCmd is `tt attach` / `tt a` — enter without creating; errors if the
// session is missing.
var attachCmd = &cobra.Command{
	Use:                "attach",
	Short:              "Attach to the project's tmux session (without creating)",
	Example:            "  tt attach\n  tt a",
	Aliases:            []string{"a"},
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		for _, a := range args {
			if strings.HasPrefix(a, "-") {
				die("unknown flag for attach: " + a)
			}
			die("attach: unexpected argument: " + a)
		}
		if err := session.Attach(); err != nil {
			die(err.Error())
		}
	},
}

// downCmd is `tt down` — kill the project's session (with confirmation) and
// wipe its state dir.
var downCmd = &cobra.Command{
	Use:                "down",
	Short:              "Kill the project's tmux session (with confirmation)",
	Example:            "  tt down",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		for _, a := range args {
			if strings.HasPrefix(a, "-") {
				die("unknown flag for down: " + a)
			}
			die("down: unexpected argument: " + a)
		}
		if err := session.Down(); err != nil {
			die(err.Error())
		}
	},
}

func init() {
	rootCmd.AddCommand(upCmd, attachCmd, downCmd)
}
