package tmux

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// ListPanes returns the pane_ids of a window (empty on error).
func ListPanes(sessionName, window string) []string {
	out, err := exec.Command("tmux", "list-panes", "-t", "="+sessionName+":"+window, "-F", "#{pane_id}").Output()
	if err != nil {
		return nil
	}
	var pids []string
	for _, l := range strings.Split(string(out), "\n") {
		if l != "" {
			pids = append(pids, l)
		}
	}
	return pids
}

// SplitWindow splits a pane (targeted by pane_id) and returns the new pane_id.
func SplitWindow(paneTarget, cwd string) (string, error) {
	out, err := exec.Command("tmux", "split-window", "-t", paneTarget, "-c", cwd, "-P", "-F", "#{pane_id}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// SelectLayout applies a layout to a window; failures are tolerated (bash `|| true`).
func SelectLayout(sessionName, window, layout string) {
	exec.Command("tmux", "select-layout", "-t", "="+sessionName+":"+window, layout).Run()
}

// SelectPane focuses a pane by pane_id; failures are tolerated.
func SelectPane(paneID string) {
	exec.Command("tmux", "select-pane", "-t", paneID).Run()
}

// PanePID returns the pane's pid, for whole-process-group teardown (down).
func PanePID(sessionName, window string) (int, bool) {
	out, err := exec.Command("tmux", "display-message", "-t", "="+sessionName+":"+window, "-p", "#{pane_pid}").Output()
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// KillProcessGroup SIGTERMs the process group with the given pgid — bash's
// `kill -TERM -- -<pgid>`, so wrapper bash, its node, and the pi grandchild
// all die together instead of orphaning the grandchild.
func KillProcessGroup(pgid int) {
	syscall.Kill(-pgid, syscall.SIGTERM)
}
