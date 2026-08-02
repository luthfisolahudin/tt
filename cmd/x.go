package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/luthfisolahudin/tt/internal/worker"
	"github.com/spf13/cobra"
)

// xCmd is `tt x` — cross-session messaging, served by the daemon (the single
// cross-session owner per the plan).
var xCmd = &cobra.Command{
	Use:   "x",
	Short: "Cross-session messaging",
	Run: func(cmd *cobra.Command, args []string) {
		die("x: subcommand required (try `tt --help`)")
	},
}

// xSendCmd is `tt x send [--timeout N] <session-id> (FILE | -)`: push a
// message into another tt session's orchestrator and submit it once the
// Claude Code TUI can safely accept input (see the daemon's classifier).
var xSendCmd = &cobra.Command{
	Use:                "send [--timeout SECONDS] <session-id> (FILE | -)",
	Short:              "Send a message to another session's orchestrator",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		timeout, target, src := 0, "", ""
		i := 0
		for ; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--timeout":
				i++
				if i >= len(args) {
					die("x send: --timeout requires seconds")
				}
				if !worker.ValidTimeoutArg(args[i]) {
					die("x send: --timeout must be a non-negative integer")
				}
				timeout = atoi(args[i])
			case a == "-":
				src = "-"
			case strings.HasPrefix(a, "-"):
				die("unknown flag for x send: " + a)
			default:
				switch {
				case target == "":
					target = a
				case src == "":
					src = a
				default:
					die("extra arg: " + a)
				}
			}
		}
		if target == "" {
			die("x send: session id required")
		}
		if src == "" {
			die("x send: message source required (file path or -)")
		}
		if src != "-" {
			if _, err := os.Open(src); err != nil {
				die(fmt.Sprintf("x send: '%s' is not a readable file. Source must be a FILE path or '-' for stdin.\n  echo text:   printf 'hello\\n' | tt x send <id> -\n  from a file: tt x send <id> ./msg.txt", src))
			}
		}
		// bash: body=$(cat) strips trailing newlines; the daemon checks the
		// trimmed body for emptiness.
		msg := trimTrailingNewlines(string(readSource(src, "x send")))
		code := doDaemon("x-send", struct {
			Target     string `json:"target"`
			Timeout    int    `json:"timeout"`
			MessageB64 string `json:"message_b64"`
		}{target, timeout, b64([]byte(msg))})
		if code != 0 {
			osExit(code)
		}
	},
}

// newXListCmd is `tt x ls` / `tt x list [--all]`: list tt sessions available
// to message. Default shows only sessions whose orchestrator is running.
func newXListCmd(use string) *cobra.Command {
	return &cobra.Command{
		Use:                use + " [--all]",
		Short:              "List tt sessions available to message",
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, args []string) {
			if helpRequested(args) {
				showHelp(cmd)
			}
			all := false
			for _, a := range args {
				switch {
				case a == "--all":
					all = true
				case strings.HasPrefix(a, "-"):
					die("unknown flag for x list: " + a)
				default:
					die("x list: unexpected argument: " + a)
				}
			}
			code := doDaemon("x-list", struct {
				All bool `json:"all"`
			}{all})
			if code != 0 {
				osExit(code)
			}
		},
	}
}

// xObserveCmd is `tt x observe [run] [--interval N] [--duration N] [--all]`:
// a passive read-only sampler writing to x-observe.sqlite. Bare `tt x observe`
// aliases `run`; flags may also sit in the first position (bash x_observe_cmd).
var xObserveCmd = &cobra.Command{
	Use:                "observe [run] [--interval SECONDS] [--duration SECONDS] [--all]",
	Short:              "Sample Claude panes to tune the x send classifier (writes sqlite)",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		if len(args) > 0 && args[0] == "run" {
			args = args[1:]
		} else if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			die("unknown x observe subcommand '" + args[0] + "' (try `tt --help`)")
		}
		interval, duration := 1, 0
		all := false
		i := 0
		for ; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--interval":
				i++
				if i >= len(args) {
					die("x observe: --interval requires seconds")
				}
				if !worker.ValidTimeoutArg(args[i]) {
					die("x observe: --timeout must be a non-negative integer")
				}
				if atoi(args[i]) <= 0 {
					die("x observe: --interval must be greater than 0")
				}
				interval = atoi(args[i])
			case a == "--duration":
				i++
				if i >= len(args) {
					die("x observe: --duration requires seconds")
				}
				if !worker.ValidTimeoutArg(args[i]) {
					die("x observe: --timeout must be a non-negative integer")
				}
				duration = atoi(args[i])
			case a == "--all":
				all = true
			case strings.HasPrefix(a, "-"):
				die("unknown flag for x observe: " + a)
			default:
				die("x observe: unexpected argument: " + a)
			}
		}
		code := doDaemon("x-observe", struct {
			Interval int  `json:"interval"`
			Duration int  `json:"duration"`
			All      bool `json:"all"`
		}{interval, duration, all})
		if code != 0 {
			osExit(code)
		}
	},
}

func init() {
	xCmd.AddCommand(xSendCmd, newXListCmd("ls"), newXListCmd("list"), xObserveCmd)
	rootCmd.AddCommand(xCmd)
}
