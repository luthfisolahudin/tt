package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

// --- resume -----------------------------------------------------------------

var piResumeCmd = &cobra.Command{
	Use:   "resume <callsign>",
	Short: "Recover an interrupted worker without a context wipe",
	Long: `Re-drive an interrupted task to completion on the same REPL, keeping
the worker's full conversation context intact.

Use resume when a worker was interrupted (Esc, crash) and you want it
to finish the same task. Use clear instead to wipe context and start
fresh on a new task.

The worker must be in the "interrupted" state (its last task ended with
status "other" or "error").`,
	Example:            "  tt pi resume alfa",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		name := ""
		for _, a := range args {
			if strings.HasPrefix(a, "-") {
				die("unknown flag for resume: " + a)
			}
			if name == "" {
				name = a
			} else {
				die("extra arg: " + a)
			}
		}
		if name == "" {
			die("resume: callsign required")
		}
		code := doDaemon("resume", struct {
			Callsign string `json:"callsign"`
		}{name})
		if code != 0 {
			osExit(code)
		}
	},
}

// --- clear ------------------------------------------------------------------

var piClearCmd = &cobra.Command{
	Use:   "clear [--force] <callsign>",
	Short: "Wipe the worker's context and respawn its REPL",
	Long: `Kill the worker's REPL, wipe its conversation context, bump the
generation counter, reset the tier to "default", and spawn a fresh
REPL. Durable results in results/ are preserved.

Required before reusing a worker for a different task domain, or
before changing a worker's tier.

Flags:
  --force   Clear even if the worker is busy (default: refused).`,
	Example:            "  tt pi clear --force alfa",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		name, force := "", false
		for _, a := range args {
			switch {
			case a == "--force":
				force = true
			case strings.HasPrefix(a, "-"):
				die("unknown flag for clear: " + a)
			default:
				if name == "" {
					name = a
				} else {
					die("extra arg: " + a)
				}
			}
		}
		if name == "" {
			die("clear: callsign required")
		}
		code := doDaemon("clear", struct {
			Callsign string `json:"callsign"`
			Force    bool   `json:"force"`
		}{name, force})
		if code != 0 {
			osExit(code)
		}
	},
}

// --- rm / remove ------------------------------------------------------------

func newRmCmd(use string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " [--force] <callsign>",
		Short: "Remove a worker (kill its REPL + window, wipe its state)",
		Long: `Kill the worker's REPL, destroy its tmux window, and wipe its state
files. Durable results in results/ are preserved. The callsign
becomes available for a future worker.

"rm" and "remove" are identical aliases.

Flags:
  --force   Remove even if the worker is busy (default: refused).`,
		Example:            "  tt pi " + use + " --force alfa",
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, args []string) {
			if helpRequested(args) {
				showHelp(cmd)
			}
			name, force := "", false
			for _, a := range args {
				switch {
				case a == "--force":
					force = true
				case strings.HasPrefix(a, "-"):
					die("unknown flag for rm: " + a)
				default:
					if name == "" {
						name = a
					} else {
						die("extra arg: " + a)
					}
				}
			}
			if name == "" {
				die("rm: callsign required")
			}
			code := doDaemon("rm", struct {
				Callsign string `json:"callsign"`
				Force    bool   `json:"force"`
			}{name, force})
			if code != 0 {
				osExit(code)
			}
		},
	}
}

// --- popidle ----------------------------------------------------------------

var piPopidleCmd = &cobra.Command{
	Use:   "popidle",
	Short: "Remove the highest-NATO idle worker",
	Long: `Remove the idle worker whose callsign is last in NATO alphabetical
order (e.g. zulu before yankee). Used for pool shrinking / resource
reclamation. No-op if no idle workers exist.`,
	Example:            "  tt pi popidle",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		for _, a := range args {
			if strings.HasPrefix(a, "-") {
				die("unknown flag for popidle: " + a)
			}
			die("popidle: unexpected arg: " + a)
		}
		code := doDaemon("popidle", struct{}{})
		if code != 0 {
			osExit(code)
		}
	},
}
