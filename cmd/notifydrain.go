package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/session"
	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/spf13/cobra"
)

// notifyDrainCmd is `tt pi notify-drain <session>` — the single lazy drainer
// the tt-worker extension spawns (detached) on --notify completion. It runs in
// the FOREGROUND (the extension owns detaching); each cycle it coalesces all
// pending notify/*.msg into one `[tt] …` paste delivered via the daemon's
// deliver op, then idles out. Faithful port of the bash pi_notify_drain_cmd.
var notifyDrainCmd = &cobra.Command{
	Use:    "notify-drain <session>",
	Short:  "Drain the --notify completion queue to the orchestrator (extension-internal)",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runNotifyDrain(args[0])
	},
}

func runNotifyDrain(sess string) {
	sdir := filepath.Join(session.StateBase(), sess)
	if _, err := os.Stat(sdir); err != nil {
		return
	}
	ndir := filepath.Join(sdir, "notify")
	lockdir := filepath.Join(sdir, "notify-drain.lock")

	// Single instance, stale-pid aware: a redundant drainer exits immediately.
	if err := os.Mkdir(lockdir, 0755); err != nil {
		if owner, rerr := os.ReadFile(filepath.Join(lockdir, "pid")); rerr == nil {
			var pid int
			if _, serr := fmt.Sscanf(strings.TrimSpace(string(owner)), "%d", &pid); serr == nil && pid > 0 && processAlive(pid) {
				return
			}
		}
		os.RemoveAll(lockdir)
		if err := os.Mkdir(lockdir, 0755); err != nil {
			return
		}
	}
	fmt.Fprintf(mustCreate(filepath.Join(lockdir, "pid")), "%d\n", os.Getpid())
	defer os.RemoveAll(lockdir)

	idle := 0
	for idle < 5 {
		// Target session gone → notifications are moot; discard and stop.
		if !tmux.HasSession(sess) {
			matches, _ := filepath.Glob(filepath.Join(ndir, "*.msg"))
			for _, m := range matches {
				os.Remove(m)
			}
			return
		}
		var msgs []string
		if entries, err := filepath.Glob(filepath.Join(ndir, "*.msg")); err == nil {
			sort.Strings(entries)
			msgs = entries
		}
		if len(msgs) == 0 {
			idle++
			time.Sleep(time.Second)
			continue
		}
		idle = 0

		// No orchestrator running yet (bare shell / no claude window) → leave
		// the messages for a later drainer and stop, rather than parking for the
		// full safe-input timeout against a window that will never accept input.
		cmd := ""
		if tmux.WindowExists(sess, "claude") {
			cmd, _ = tmux.PaneCurrentCommand("=" + sess + ":claude")
		}
		switch cmd {
		case "bash", "zsh", "sh", "fish", "":
			return
		}

		// Coalesce all currently-pending messages into one paste.
		var lines []string
		for _, m := range msgs {
			if data, err := os.ReadFile(m); err == nil {
				if t := strings.TrimRight(string(data), "\n"); t != "" {
					lines = append(lines, t)
				}
			}
		}
		var body string
		if len(lines) == 1 {
			body = "[tt] " + lines[0]
		} else {
			body = fmt.Sprintf("[tt] %d tasks finished:\n%s", len(lines), strings.Join(lines, "\n"))
		}

		// Deliver via the daemon. Success → drop the delivered messages.
		// Failure → leave them for a later drainer and stop (avoid spinning).
		if deliverViaDaemon(sess, body, 300) {
			for _, m := range msgs {
				os.Remove(m)
			}
		} else {
			return
		}
	}
}

// deliverViaDaemon sends one coalesced body through the daemon's deliver op;
// returns false on any error (the caller leaves the messages and stops).
func deliverViaDaemon(sess, body string, timeout int) bool {
	c := client.New()
	resp, err := c.Do("deliver", sess, cwd(), struct {
		Target     string `json:"target"`
		Timeout    int    `json:"timeout"`
		MessageB64 string `json:"message_b64"`
	}{sess, timeout, b64([]byte(body))})
	if err != nil || !resp.OK || resp.ExitCode != 0 {
		return false
	}
	return true
}

func mustCreate(path string) *os.File {
	f, err := os.Create(path)
	if err != nil {
		return os.Stderr
	}
	return f
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func init() {
	piCmd.AddCommand(notifyDrainCmd)
}
