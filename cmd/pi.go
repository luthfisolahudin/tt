package cmd

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/session"
	"github.com/luthfisolahudin/tt/internal/worker"
	"github.com/spf13/cobra"
)

var piCmd = &cobra.Command{
	Use:   "pi",
	Short: "pi worker pool",
	Run: func(cmd *cobra.Command, args []string) {
		die("pi: subcommand required (try `tt --help`)")
	},
}

// doDaemon sends one op to ttd and relays its stdout/stderr; returns the exit
// code (the CLI exits with it, mirroring bash's exit semantics).
func doDaemon(op string, args any) int {
	c := client.New()
	resp, err := c.Do(op, session.SessionName(), cwd(), args)
	if err != nil {
		die(err.Error())
	}
	if !resp.OK {
		die(resp.Error)
	}
	os.Stdout.WriteString(resp.Stdout)
	os.Stderr.WriteString(resp.Stderr)
	return resp.ExitCode
}

func cwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}

func readSource(src string, context string) []byte {
	if src == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			die(context + ": cannot read stdin: " + err.Error())
		}
		return data
	}
	data, err := os.ReadFile(src)
	if err != nil {
		die(context + ": cannot read '" + src + "'")
	}
	return data
}

// trimTrailingNewlines mirrors bash `$(cat)` command substitution, which
// strips trailing newlines from the captured text.
func trimTrailingNewlines(s string) string {
	return strings.TrimRight(s, "\n")
}

func b64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// helpRequested reports whether the arg list asks for help. The pi verbs
// hand-parse flags (DisableFlagParsing) so flags may sit in any position like
// the bash tool; cobra therefore never intercepts --help, so each Run does.
func helpRequested(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			return true
		}
	}
	return false
}

// showHelp prints the cobra-generated help for a hand-parsed verb and exits 0.
func showHelp(cmd *cobra.Command) {
	cmd.Help()
	osExit(0)
}

// --- send -------------------------------------------------------------------

var piSendCmd = &cobra.Command{
	Use:                "send [--tier NAME] [--notify] <callsign> (FILE | -)",
	Short:              "Send a prompt to a worker's live pi REPL (run-next)",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		name, reqTier, src := "", "", ""
		notify := false
		i := 0
		for ; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--low" || a == "--medium" || a == "--high" || a == "--xhigh" || a == "--max":
				die(fmt.Sprintf("thinking effort is fixed per tier — use --tier=<name> (one of: %s) instead of the %s flag", strings.Join(worker.TierNames(), " "), a))
			case a == "--tier":
				i++
				if i >= len(args) {
					die("send: --tier requires a value")
				}
				reqTier = args[i]
			case strings.HasPrefix(a, "--tier="):
				reqTier = strings.TrimPrefix(a, "--tier=")
			case a == "--notify":
				notify = true
			case a == "-":
				src = "-"
			case strings.HasPrefix(a, "-"):
				die("unknown flag for send: " + a)
			default:
				switch {
				case name == "":
					name = a
				case src == "":
					src = a
				default:
					die("extra arg: " + a)
				}
			}
		}
		if reqTier != "" && !worker.IsKnownTier(reqTier) {
			die(fmt.Sprintf("unknown --tier '%s' (valid: %s)", reqTier, strings.Join(worker.TierNames(), " ")))
		}
		if name == "" {
			die("send: callsign required")
		}
		if src == "" {
			die("send: prompt source required (file path or -)")
		}
		if src != "-" {
			if _, err := os.Open(src); err != nil {
				die(fmt.Sprintf("send: '%s' is not a readable file. Source must be a FILE path or '-' for stdin.\n  echo text:   printf 'TASK: ...\\n' | tt pi send <callsign> -\n  from a file: tt pi send <callsign> ./task.txt", src))
			}
		}
		if !worker.ValidCallsign(name) {
			die("invalid callsign: " + name)
		}
		prompt := readSource(src, "send")
		code := doDaemon("send", struct {
			Callsign  string `json:"callsign"`
			Tier      string `json:"tier"`
			Notify    bool   `json:"notify"`
			PromptB64 string `json:"prompt_b64"`
		}{name, reqTier, notify, b64(prompt)})
		if code != 0 {
			osExit(code)
		}
	},
}

// --- auto -------------------------------------------------------------------

var piAutoCmd = &cobra.Command{
	Use:                "auto [--tier NAME] [--prefer-fresh] [--rm] [--notify] [--json] (FILE | -)",
	Short:              "Dispatch without naming a worker (idle → spawn → pool)",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		reqTier, src := "", ""
		rmFlag, notify, jsonFlag, preferFresh := false, false, false, false
		i := 0
		for ; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--low" || a == "--medium" || a == "--high" || a == "--xhigh" || a == "--max":
				die(fmt.Sprintf("thinking effort is fixed per tier — use --tier=<name> (one of: %s) instead of the %s flag", strings.Join(worker.TierNames(), " "), a))
			case a == "--tier":
				i++
				if i >= len(args) {
					die("auto: --tier requires a value")
				}
				reqTier = args[i]
			case strings.HasPrefix(a, "--tier="):
				reqTier = strings.TrimPrefix(a, "--tier=")
			case a == "--rm":
				rmFlag = true
			case a == "--notify":
				notify = true
			case a == "--json":
				jsonFlag = true
			case a == "--prefer-fresh":
				preferFresh = true
			case a == "-":
				src = "-"
			case strings.HasPrefix(a, "-"):
				die("unknown flag for auto: " + a)
			default:
				if src == "" {
					src = a
				} else {
					die("extra arg: " + a)
				}
			}
		}
		if reqTier != "" && !worker.IsKnownTier(reqTier) {
			die(fmt.Sprintf("unknown --tier '%s' (valid: %s)", reqTier, strings.Join(worker.TierNames(), " ")))
		}
		if src == "" {
			die("auto: prompt source required (file path or -)")
		}
		if src != "-" {
			if _, err := os.Open(src); err != nil {
				die(fmt.Sprintf("auto: '%s' is not a readable file. Source must be a FILE path or '-' for stdin.", src))
			}
		}
		prompt := readSource(src, "auto")
		code := doDaemon("auto", struct {
			Tier        string `json:"tier"`
			RM          bool   `json:"rm"`
			Notify      bool   `json:"notify"`
			JSON        bool   `json:"json"`
			PreferFresh bool   `json:"prefer_fresh"`
			PromptB64   string `json:"prompt_b64"`
		}{reqTier, rmFlag, notify, jsonFlag, preferFresh, b64(prompt)})
		if code != 0 {
			osExit(code)
		}
	},
}

// --- steer ------------------------------------------------------------------

var piSteerCmd = &cobra.Command{
	Use:                "steer <callsign|all> (FILE | -)",
	Short:              "Inject a message NOW into the worker's current turn",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		name, src := "", ""
		i := 0
		for ; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "-":
				src = "-"
			case strings.HasPrefix(a, "-"):
				die("unknown flag for steer: " + a)
			default:
				switch {
				case name == "":
					name = a
				case src == "":
					src = a
				default:
					die("extra arg: " + a)
				}
			}
		}
		if name == "" {
			die("steer: callsign required")
		}
		if src == "" {
			die("steer: message source required (file path or -)")
		}
		if src != "-" {
			if _, err := os.Open(src); err != nil {
				die(fmt.Sprintf("steer: '%s' is not a readable file. Source must be a FILE path or '-' for stdin.", src))
			}
		}
		msg := trimTrailingNewlines(string(readSource(src, "steer")))
		code := doDaemon("steer", struct {
			Callsign   string `json:"callsign"`
			MessageB64 string `json:"message_b64"`
		}{name, b64([]byte(msg))})
		if code != 0 {
			osExit(code)
		}
	},
}

// --- resume -----------------------------------------------------------------

var piResumeCmd = &cobra.Command{
	Use:                "resume <callsign>",
	Short:              "Recover an interrupted worker without a context wipe",
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

// --- status -----------------------------------------------------------------

var piStatusCmd = &cobra.Command{
	Use:                "status [--json]",
	Short:              "One row per worker: state, elapsed, queue, last task, tier, gen",
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
	Use:                "collect [--timeout SECONDS] [--json] [all | <callsign>]",
	Short:              "Fan-out join that never drops a finished task (cursor-based)",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		jsonFlag, timeout, target := false, 0, ""
		i := 0
		for ; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--json":
				jsonFlag = true
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
		}{jsonFlag, timeout, target})
		if code != 0 {
			osExit(code)
		}
	},
}

// --- results ----------------------------------------------------------------

var piResultsCmd = &cobra.Command{
	Use:                "results [--json] [<callsign> | <task-id>]",
	Short:              "Re-read durable task outcomes from the per-id store",
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

// --- logs (CLI-local, read-only tmux) ---------------------------------------

var piLogsCmd = &cobra.Command{
	Use:                "logs [--lines N] <callsign>",
	Short:              "Dump a worker's pi REPL scrollback (read-only)",
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

func init() {
	piCmd.AddCommand(
		piSendCmd,
		piAutoCmd,
		piSteerCmd,
		newWaitCmd("wait", false),
		newWaitCmd("wait-all", true),
		piStatusCmd,
		piCollectCmd,
		piResultsCmd,
		piResumeCmd,
		piClearCmd,
		newRmCmd("rm"),
		newRmCmd("remove"),
		piPopidleCmd,
		piLogsCmd,
		piUpdateCmd,
	)
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

func isPositiveInt(s string) bool {
	if s == "" || s[0] < '1' || s[0] > '9' {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
