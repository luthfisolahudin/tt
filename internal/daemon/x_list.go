package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/session"
	"github.com/luthfisolahudin/tt/internal/tmux"
)

// --- x list -----------------------------------------------------------------

func xListOp(s *Session, a XListArgs) client.Response {
	var out, errb strings.Builder
	base := session.StateBase()
	if a.All {
		fmt.Fprintf(&out, "%-20s  %-15s  %s\n", "SESSION", "STATUS", "PWD")
	} else {
		fmt.Fprintf(&out, "%-20s  %s\n", "SESSION", "PWD")
	}
	found := 0
	for _, sname := range stateDirNames() {
		pwdVal := "-"
		if data, err := os.ReadFile(filepath.Join(base, sname, "project")); err == nil {
			pwdVal = strings.TrimRight(string(data), "\n")
		}
		if !tmux.HasSession(sname) {
			if a.All {
				fmt.Fprintf(&out, "%-20s  %-15s  %s\n", sname, "down", pwdVal)
				found = 1
			}
			continue
		}
		status := "no-orchestrator"
		if tmux.WindowExists(sname, "claude") {
			paneCmd, _ := tmux.PaneCurrentCommand("=" + sname + ":claude")
			switch paneCmd {
			case "bash", "zsh", "sh", "fish", "":
				status = "no-orchestrator"
			default:
				status = "ready"
			}
		}
		if a.All {
			fmt.Fprintf(&out, "%-20s  %-15s  %s\n", sname, status, pwdVal)
			found = 1
		} else if status == "ready" {
			fmt.Fprintf(&out, "%-20s  %s\n", sname, pwdVal)
			found = 1
		}
	}
	if found == 0 {
		if a.All {
			fmt.Fprintf(&out, "(no tt sessions found)\n")
		} else {
			fmt.Fprintf(&out, "(no tt sessions with a live orchestrator)\n")
		}
	}
	return ok(&out, &errb, 0)
}

// stateDirNames lists the session state dirs under the tt state base
// (dirs only, sorted — bash's `"$base"/*/` glob order).
func stateDirNames() []string {
	entries, err := os.ReadDir(session.StateBase())
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}
