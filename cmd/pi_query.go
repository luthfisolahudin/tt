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
	Use:   "status [--json]",
	Short: "One row per worker: state, elapsed, queue, last task, tier, gen",
	Long: `Show the state of every worker in the pool.

Table columns: CALLSIGN  STATE  ELAPSED  QUEUE  LAST-TASK  TIER  GEN
A "pool: N task(s) queued (unclaimed)" footer appears when the shared
queue has pending tasks.

Worker states:
  idle          REPL ready, no task running.
  busy          Actively processing a task.
  blocked       Last task ended with BLOCKED (needs intervention).
  interrupted   Last task ended abnormally (Esc / crash).
  starting      REPL is booting (within the 45 s window).
  down          Window exists but REPL process is not running.
  missing       No pi-<callsign> window exists.

ELAPSED shows M:SS while busy, "-" otherwise. GEN is the context
generation (incremented on clear). A tier removed from the registry
shows as "stale:<name>"; the worker is blocked until cleared.

Flags:
  --json    Output a JSON array of worker objects.`,
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
	Use:   "collect [--timeout SECONDS] [--json] [--digest] [all | <callsign>]",
	Short: "Fan-out join that never drops a finished task (cursor-based)",
	Long: `Join results from a fan-out without dropping already-finished tasks.
Each call advances a per-worker cursor so repeated collects yield only
NEW results, never re-emitting previous ones.

Blocks for in-progress tasks (polls once per second). Pass a callsign
to scope to one worker, or "all" (or omit) for every worker.

Flags:
  --timeout SECONDS   Deadline for pending tasks (default: 0 = no limit).
  --json              Output a JSON array of result envelopes.
  --digest            One lean line per result:
                      <task-id>  <status>  <dur>  <summary>`,
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
	Use:   "results [--json] [<callsign> | <task-id>]",
	Short: "Re-read durable task outcomes from the per-id store",
	Long: `Read-only, non-blocking view of recorded results.

Three modes:
  (no argument)   List all results: TASK  STATUS  DUR  SUMMARY
  <callsign>      List that worker's results only.
  <task-id>       Show one result body (e.g. "alfa-3").

Result statuses: done, blocked, other, error.

Flags:
  --json    Single envelope for a task-id, JSON array for a listing.`,
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
	usage := use + " [--timeout SECONDS] [--json] <callsign|task-id|pool-id|all> [task-id]"
	long := `Block until the specified task completes and print its result body.

Target forms:
  <callsign>              Wait on the worker's latest task.
  <callsign> <task-id>    Wait on a specific task (e.g. "alfa alfa-3").
  <task-id>               Auto-extract callsign (e.g. "alfa-3").
  pool-N                  Wait on a pool-dispatched task.
  all                     Join every currently busy worker (same as wait-all).

Exit codes: 0 for done/blocked, 1 for error/other/timeout/down.
Stdout receives the result body; stderr receives any error messages.

Flags:
  --timeout SECONDS   Deadline (default: 0 = wait forever).
  --json              Output a JSON result envelope.`
	if forceAll {
		short = "Join every busy worker in one consolidated report"
		usage = use + " [--timeout SECONDS] [--json]"
		long = `Block until every busy worker finishes, then print all results.

Output: "== <callsign> (<task-id>) <status> ==" sections on stdout,
one per worker. A tally line appears on stderr. Finished workers
that were not busy at join time are noted with a hint to use collect.

Exit codes: 0 if all tasks are done/blocked, 1 if any failed.

Flags:
  --timeout SECONDS   Deadline (default: 0 = wait forever).
  --json              Output a JSON array of result envelopes.`
	}
	return &cobra.Command{
		Use:                usage,
		Short:              short,
		Long:               long,
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
	Use:   "logs [--lines N] <callsign>",
	Short: "Dump a worker's pi REPL scrollback (read-only)",
	Long: `Capture the worker's tmux pane scrollback and print it to stdout.
This is a direct tmux read, not a daemon operation. The worker's
pi-<callsign> window must exist.

Flags:
  --lines N   Number of scrollback lines to capture (default: 200).`,
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
