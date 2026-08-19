package cmd

import (
	"fmt"
	"strings"

	"github.com/luthfisolahudin/tt/internal/session"
	"github.com/spf13/cobra"
)

// upCmd is `tt up` — idempotent session create/heal. Inside tmux it does NOT
// switch away by default (that stole the caller's window); --attach switches.
// Outside tmux it attaches (the point of `up`). Off a tty attach is harmless.
var upCmd = &cobra.Command{
	Use:                "up [--attach] [--continue]",
	Short:              "Create or heal the project's tmux session (fresh Claude by default; --continue/-c to resume)",
	Example:            "  tt up\n  tt up --continue\n  tt up --attach --continue",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		attach, continued, err := parseUpArgs(args)
		if err != nil {
			die(err.Error())
		}
		if err := session.Up(attach, continued); err != nil {
			die(err.Error())
		}
	},
}

func parseUpArgs(args []string) (attach, continued bool, err error) {
	for _, a := range args {
		switch a {
		case "--attach":
			attach = true
		case "-c", "--continue":
			continued = true
		default:
			if strings.HasPrefix(a, "-") {
				return false, false, fmt.Errorf("unknown flag for up: %s", a)
			}
			return false, false, fmt.Errorf("up: unexpected argument: %s", a)
		}
	}
	return attach, continued, nil
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
	upCmd.Flags().Bool("attach", false, "switch to the project session when already inside tmux")
	upCmd.Flags().BoolP("continue", "c", false, "resume the default Claude Code conversation when launching a bare claude pane")
	rootCmd.AddCommand(upCmd, attachCmd, downCmd)
}
