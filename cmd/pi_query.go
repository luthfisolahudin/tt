package cmd

import (
	"os"
	"strings"

	"github.com/luthfisolahudin/tt/internal/session"
	"github.com/luthfisolahudin/tt/internal/worker"
	"github.com/spf13/cobra"
)

// --- status -----------------------------------------------------------------

var piStatusCmd = &cobra.Command{
	Use:                "status [--json]",
	Short:              "One row per worker: state, elapsed, queue, last task, tier, gen",
	Example:            "  tt pi status --json",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		jsonFlag := false
		for _, a := range args {
			switch {
			case a == "--json":
				jsonFlag = true
			case strings.HasPrefix(a, "-"):
				die("unknown flag for status: " + a)
			default:
				die("status: unexpected arg: " + a)
			}
		}
		code := doDaemon("status", struct {
			JSON bool `json:"json"`
		}{jsonFlag})
		if code != 0 {
			osExit(code)
		}
	},
}

// --- collect ----------------------------------------------------------------

var piCollectCmd = &cobra.Command{
	Use:                "collect [--timeout SECONDS] [--json] [--digest] [all | <callsign>]",
	Short:              "Fan-out join that never drops a finished task (cursor-based)",
	Example:            "  tt pi collect --timeout 300 --digest all",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		jsonFlag, timeout, target := false, 0, ""
		digest := false
		i := 0
		for ; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--json":
				jsonFlag = true
			case a == "--digest":
				digest = true
			case a == "--timeout":
				i++
				if i >= len(args) {
					die("collect: --timeout requires seconds")
				}
				if !worker.ValidTimeoutArg(args[i]) {
					die("collect: --timeout must be a non-negative integer")
				}
				timeout = atoi(args[i])
			case strings.HasPrefix(a, "-"):
				die("unknown flag for collect: " + a)
			default:
				if target == "" {
					target = a
				} else {
					die("extra arg: " + a)
				}
			}
		}
		code := doDaemon("collect", struct {
			JSON    bool   `json:"json"`
			Timeout int    `json:"timeout"`
			Target  string `json:"target"`
			Digest  bool   `json:"digest"`
		}{jsonFlag, timeout, target, digest})
		if code != 0 {
			osExit(code)
		}
	},
}

// --- results ----------------------------------------------------------------

var piResultsCmd = &cobra.Command{
	Use:                "results [--json] [<callsign> | <task-id>]",
	Short:              "Re-read durable task outcomes from the per-id store",
	Example:            "  tt pi results --json alfa-3",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		jsonFlag, target := false, ""
		for _, a := range args {
			switch {
			case a == "--json":
				jsonFlag = true
			case strings.HasPrefix(a, "-"):
				die("unknown flag for results: " + a)
			default:
				if target == "" {
					target = a
				} else {
					die("extra arg: " + a)
				}
			}
		}
		code := doDaemon("results", struct {
			JSON   bool   `json:"json"`
			Target string `json:"target"`
		}{jsonFlag, target})
		if code != 0 {
			osExit(code)
		}
	},
}

// --- wait / wait-all --------------------------------------------------------

func newWaitCmd(use string, forceAll bool) *cobra.Command {
	short := "Block until a task's result is recorded (callsign | task-id | pool-id | all)"
	if forceAll {
		short = "Join every busy worker in one consolidated report"
	}
	return &cobra.Command{
		Use:                use + " [--timeout SECONDS] [--json] <callsign|task-id|pool-id|all> [task-id]",
		Short:              short,
		Example:            waitExample(use, forceAll),
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, args []string) {
			if helpRequested(args) {
				showHelp(cmd)
			}
			timeout, jsonFlag := 0, false
			name, taskID := "", ""
			i := 0
			for ; i < len(args); i++ {
				a := args[i]
				switch {
				case a == "--timeout":
					i++
					if i >= len(args) {
						die("wait: --timeout requires seconds")
					}
					if !worker.ValidTimeoutArg(args[i]) {
						die("wait: --timeout must be a non-negative integer")
					}
					timeout = atoi(args[i])
				case a == "--json":
					jsonFlag = true
				case strings.HasPrefix(a, "-"):
					die("unknown flag for wait: " + a)
				default:
					if name == "" {
						name = a
					} else if taskID == "" {
						taskID = a
					} else {
						die("extra arg: " + a)
					}
				}
			}
			if name == "" {
				die("wait: callsign (or 'all') required")
			}
			if forceAll {
				name = "all"
				taskID = ""
			}
			code := doDaemon("wait", struct {
				Target  string `json:"target"`
				TaskID  string `json:"task_id"`
				Timeout int    `json:"timeout"`
				JSON    bool   `json:"json"`
			}{name, taskID, timeout, jsonFlag})
			if code != 0 {
				osExit(code)
			}
		},
	}
}

func waitExample(use string, forceAll bool) string {
	if forceAll {
		return "  tt pi " + use + " --timeout 300"
	}
	return "  tt pi " + use + " --timeout 300 alfa alfa-3"
}

// --- logs (CLI-local, read-only tmux) ---------------------------------------

var piLogsCmd = &cobra.Command{
	Use:                "logs [--lines N] <callsign>",
	Short:              "Dump a worker's pi REPL scrollback (read-only)",
	Example:            "  tt pi logs --lines 100 alfa",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		lines, name := 200, ""
		i := 0
		for ; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--lines":
				i++
				if i >= len(args) {
					die("logs: --lines requires a count")
				}
				if !isPositiveInt(args[i]) {
					die("logs: --lines must be a positive integer")
				}
				lines = atoi(args[i])
			case strings.HasPrefix(a, "-"):
				die("unknown flag for logs: " + a)
			default:
				if name == "" {
					name = a
				} else {
					die("extra arg: " + a)
				}
			}
		}
		if name == "" {
			die("logs: callsign required")
		}
		if !worker.ValidCallsign(name) {
			die("invalid callsign: " + name)
		}
		sess := session.SessionName()
		if !hasSession(sess) {
			die("no session for " + cwd() + "; run `tt up` first")
		}
		if !windowExists(sess, "pi-"+name) {
			die("pi-" + name + " does not exist")
		}
		out, err := capturePane(sess, "pi-"+name, lines)
		if err != nil {
			die(err.Error())
		}
		os.Stdout.WriteString(out)
	},
}
