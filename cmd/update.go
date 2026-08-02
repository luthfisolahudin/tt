package cmd

import (
	"os"
	"os/exec"

	"github.com/luthfisolahudin/tt/internal/worker"
	"github.com/spf13/cobra"
)

// piUpdateCmd runs `pi update` against the worker's private config dir
// (PI_CODING_AGENT_DIR=$TT_PI_WORKER_DIR), so extensions installed into the
// worker pool get updated — not the orchestrator's own pi config. Forwards
// all args and the exit code; no tt session required.
var piUpdateCmd = &cobra.Command{
	Use:                "update [<args>...]",
	Short:              "Run `pi update` against the worker's private config dir",
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		if helpRequested(args) {
			showHelp(cmd)
		}
		if err := worker.EnsurePiWorkerDir(os.Stderr); err != nil {
			die(err.Error())
		}
		full := append([]string{"update"}, args...)
		c := exec.Command("pi", full...)
		c.Env = append(os.Environ(), "PI_CODING_AGENT_DIR="+worker.PiWorkerDir())
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				osExit(ee.ExitCode())
			}
			die(err.Error())
		}
	},
}
