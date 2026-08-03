package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

// --- resume -----------------------------------------------------------------

var piResumeCmd = &cobra.Command{
	Use:                "resume <callsign>",
	Short:              "Recover an interrupted worker without a context wipe",
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
	Use:                "clear [--force] <callsign>",
	Short:              "Wipe the worker's context and respawn its REPL",
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
		Use:                use + " [--force] <callsign>",
		Short:              "Remove a worker (kill its REPL + window, wipe its state)",
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
	Use:                "popidle",
	Short:              "Remove the highest-NATO idle worker",
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
