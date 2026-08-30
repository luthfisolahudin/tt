package cmd

import (
	"fmt"
	"os"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/daemon"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:     "daemon",
	Short:   "ttd daemon control (one process serving all sessions)",
	Example: "  tt daemon status\n  tt daemon start",
	Run: func(cmd *cobra.Command, args []string) {
		die("daemon: subcommand required (start|stop|status)")
	},
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the ttd daemon (no-op if already running)",
	Long: `Start the background daemon process. Most tt commands auto-start
the daemon on first use, so manual start is rarely needed.`,
	Example: "  tt daemon start",
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		pid, started, err := client.Start()
		if err != nil {
			die(err.Error())
		}
		if started {
			note(fmt.Sprintf("ttd started (pid %d)", pid))
		} else {
			note(fmt.Sprintf("ttd already running (pid %d)", pid))
		}
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the ttd daemon",
	Long: `Stop the running daemon. After a rebuild ("go-task install"), the
daemon must be restarted for the new binary to take effect; install
handles this automatically, but manual stop + start works too.`,
	Example: "  tt daemon stop",
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if _, alive := client.Running(); !alive {
			note("ttd not running")
			return
		}
		if err := client.Stop(); err != nil {
			die(err.Error())
		}
		note("ttd stopped")
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:     "status",
	Short:   "Report whether the ttd daemon is running",
	Example: "  tt daemon status",
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		pid, alive := client.Running()
		if !alive {
			die("ttd not running")
		}
		if !client.Ping() {
			die(fmt.Sprintf("ttd pid %d is alive but not responding on %s", pid, client.SocketPath()))
		}
		note(fmt.Sprintf("ttd running (pid %d)", pid))
	},
}

var daemonServeCmd = &cobra.Command{
	Use:     "serve",
	Short:   "Run the daemon in the foreground (internal)",
	Example: "  tt daemon serve",
	Hidden:  true,
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if err := daemon.Serve(); err != nil {
			fmt.Fprintf(os.Stderr, "tt: ttd: %s\n", err)
			osExit(1)
		}
	},
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd, daemonStopCmd, daemonStatusCmd, daemonServeCmd)
}
