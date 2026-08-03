package cmd

import (
	"encoding/base64"
	"io"
	"os"
	"strings"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/session"
	"github.com/spf13/cobra"
)

var piCmd = &cobra.Command{
	Use:     "pi",
	Short:   "pi worker pool",
	Example: "  tt pi status\n  tt pi send alfa - <<<'TASK: inspect the latest change; SUCCESS: report findings.'",
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

func init() {
	piCmd.AddCommand(
		piSendCmd,
		piAutoCmd,
		piSteerCmd,
		newSteerCmd("steer-all", true),
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
