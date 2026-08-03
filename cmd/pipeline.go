package cmd

import (
	"github.com/spf13/cobra"
)

// pipelineCmd runs a declarative pipeline spec end-to-end in the daemon: an
// ordered list of fanout / review stages, one trigger in, one digest out. The
// spec is JSON read from FILE or stdin. See docs/DESIGN.md "Declarative pipelines"
// and docs/pipeline.schema.json.
var pipelineCmd = &cobra.Command{
	Use:     "pipeline",
	Short:   "Run a declarative fan-out + review pipeline",
	Example: "  tt pipeline run - <<<'{\"stages\":[{\"fanout\":[{\"task\":\"TASK: run focused tests; SUCCESS: report the result.\"}]}]}'",
	Run: func(cmd *cobra.Command, args []string) {
		die("pipeline: subcommand required (try `tt pipeline --help`)")
	},
}

var pipelineRunCmd = &cobra.Command{
	Use:                "run [--timeout SECONDS] [--json] (FILE | -)",
	Short:              "Run a pipeline spec (fan-out stages + review gates) to one digest",
	Example:            "  tt pipeline run --timeout 300 --json - <<<'{\"stages\":[{\"fanout\":[{\"label\":\"tests\",\"task\":\"TASK: run focused tests; SUCCESS: report the result.\"}]}]}'",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		timeout, jsonFlag, src := 0, false, ""
		i := 0
		for ; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--json":
				jsonFlag = true
			case a == "--timeout":
				i++
				if i >= len(args) {
					die("pipeline run: --timeout requires seconds")
				}
				if !isPositiveInt(args[i]) && args[i] != "0" {
					die("pipeline run: --timeout must be a non-negative integer")
				}
				timeout = atoi(args[i])
			case a == "-":
				src = "-"
			case len(a) > 0 && a[0] == '-':
				die("unknown flag for pipeline run: " + a)
			default:
				if src == "" {
					src = a
				} else {
					die("extra arg: " + a)
				}
			}
		}
		if src == "" {
			die("pipeline run: spec source required (file path or -)")
		}
		spec := readSource(src, "pipeline run")
		code := doDaemon("pipeline", struct {
			SpecB64 string `json:"spec_b64"`
			JSON    bool   `json:"json"`
			Timeout int    `json:"timeout"`
		}{b64(spec), jsonFlag, timeout})
		if code != 0 {
			osExit(code)
		}
	},
}

func init() {
	pipelineCmd.AddCommand(pipelineRunCmd)
	rootCmd.AddCommand(pipelineCmd)
}
