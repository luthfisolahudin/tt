package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/luthfisolahudin/tt/internal/worker"
	"github.com/spf13/cobra"
)

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

var piSteerCmd = newSteerCmd("steer", false)

// newSteerCmd builds `steer` and its `steer-all` sibling. forceAll pins the
// target to every live worker, so `steer-all <msg>` takes only a source (the
// bash pi_steer_all_cmd shape) while `steer <cs|all> <msg>` takes both.
func newSteerCmd(use string, forceAll bool) *cobra.Command {
	short := "Inject a message NOW into the worker's current turn"
	usage := use + " <callsign|all> (FILE | -)"
	if forceAll {
		short = "Inject a message NOW into every live worker's current turn"
		usage = use + " (FILE | -)"
	}
	return &cobra.Command{
		Use:                usage,
		Short:              short,
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, args []string) {
			if helpRequested(args) {
				showHelp(cmd)
			}
			name, src := "", ""
			if forceAll {
				name = "all"
			}
			i := 0
			for ; i < len(args); i++ {
				a := args[i]
				switch {
				case a == "-":
					src = "-"
				case strings.HasPrefix(a, "-"):
					die("unknown flag for " + use + ": " + a)
				default:
					switch {
					case !forceAll && name == "":
						name = a
					case src == "":
						src = a
					default:
						die("extra arg: " + a)
					}
				}
			}
			if name == "" {
				die(use + ": callsign required")
			}
			if src == "" {
				die(use + ": message source required (file path or -)")
			}
			if src != "-" {
				if _, err := os.Open(src); err != nil {
					die(fmt.Sprintf("%s: '%s' is not a readable file. Source must be a FILE path or '-' for stdin.", use, src))
				}
			}
			msg := trimTrailingNewlines(string(readSource(src, use)))
			code := doDaemon("steer", struct {
				Callsign   string `json:"callsign"`
				MessageB64 string `json:"message_b64"`
			}{name, b64([]byte(msg))})
			if code != 0 {
				osExit(code)
			}
		},
	}
}
