package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/worker"
)

// paneIsBare mirrors the bash guard: is the pane (by pane_id) sitting at a
// bare shell? pane_current_command is reliable here because we target a
// specific pane, not a grandchild-hosting pi window.
func paneIsBare(paneTarget string) bool {
	cmd, err := tmux.PaneCurrentCommand(paneTarget)
	if err != nil {
		return false
	}
	switch cmd {
	case "bash", "zsh", "sh", "fish":
		return true
	}
	return false
}

// applyWindowPanes splits a freshly-created window to its configured pane
// count and (re)sends each pane's command, bare-shell-guarded. Heals at
// WINDOW granularity: a window with >1 pane is never re-split, but any
// bare-shell pane still gets its command re-sent. coldStart marks a brand-new
// session (bash's _TT_COLD_START): only then do enter:false prefills fire.
func applyWindowPanes(sess, cwd string, wins []Window, i, created int, coldStart bool) {
	w := wins[i]
	name := w.Name
	npanes := len(w.Panes)
	pids := tmux.ListPanes(sess, name)
	if len(pids) == 0 {
		return
	}
	// Fresh single bare-shell window -> split up to the configured pane count.
	didSplit := 0
	if len(pids) == 1 && npanes > 1 && paneIsBare(pids[0]) {
		last := pids[0]
		for j := 1; j < npanes; j++ {
			newid, err := tmux.SplitWindow(last, cwd)
			if err != nil {
				break
			}
			last = newid
		}
		layout := "even-horizontal"
		if w.Layout != nil {
			layout = *w.Layout
		}
		tmux.SelectLayout(sess, name, layout)
		pids = tmux.ListPanes(sess, name)
		if len(pids) > 0 {
			tmux.SelectPane(pids[0])
		}
		didSplit = 1
	}
	fresh := coldStart || created == 1 || didSplit == 1
	lim := len(pids)
	if npanes < lim {
		lim = npanes
	}
	for j := 0; j < lim; j++ {
		p := w.Panes[j]
		if p.Cmd == "" {
			continue
		}
		if !paneIsBare(pids[j]) {
			continue
		}
		if p.Enter {
			tmux.SendKeys(pids[j], p.Cmd, "Enter")
		} else if fresh {
			tmux.SendKeys(pids[j], p.Cmd)
		}
	}
}

// legacyLaunchClaude is the no-jq fallback: auto-launch claude in the claude
// window if the pane is a bare shell (bash's legacy_launch_claude).
func legacyLaunchClaude(sess string) {
	cmd, err := tmux.PaneCurrentCommand("=" + sess + ":claude")
	if err != nil {
		return
	}
	switch cmd {
	case "bash", "zsh", "sh", "fish":
	default:
		return
	}
	tmux.SendKeys("="+sess+":claude", DefaultClaudeCmd, "Enter")
}

// dedupWindows collapses duplicate standard windows. A tmux-resurrect restore
// racing with `tt up` can recreate the session, leaving two pi-bravo windows
// etc.; a duplicated name makes every `tmux ... -t <name>` target ambiguous.
// A duplicated pi window is dropped and respawned clean; for dev/claude one
// copy is kept (never the window tt is being run from). Ad-hoc user windows
// are left alone even if they collide.
func dedupWindows(sess, cwd, stateDir string, wins []Window, ok bool, note func(string)) {
	cur := tmux.CurrentWindowID()
	allwin := tmux.ListWindowIDs(sess)
	if len(allwin) == 0 {
		return
	}
	stdnames := configuredWindowNames(wins, ok)
	names := make([]string, 0, len(stdnames)+len(worker.NATO))
	names = append(names, stdnames...)
	for _, n := range worker.NATO {
		names = append(names, "pi-"+n)
	}
	for _, name := range names {
		var ids []string
		for _, line := range allwin {
			f := strings.Fields(line)
			if len(f) >= 2 && f[1] == name {
				ids = append(ids, f[0])
			}
		}
		if len(ids) <= 1 {
			continue
		}
		note(fmt.Sprintf("collapsing %d duplicate '%s' windows", len(ids), name))
		if strings.HasPrefix(name, "pi-") {
			for _, id := range ids {
				tmux.KillWindowByID(id)
			}
			cs := strings.TrimPrefix(name, "pi-")
			// Heal path: no --tier is set, so restore the default explicitly
			// before spawn (spawn_pi_window no longer writes the tier itself).
			os.WriteFile(filepath.Join(stateDir, cs+".tier"), []byte(worker.TierDefault), 0644)
			env, _ := SyncEnvMap()
			if err := worker.SpawnPiWindow(stateDir, sess, cwd, cs, env, os.Stderr); err != nil {
				note("respawn of " + name + " failed: " + err.Error())
			}
		} else {
			kept := ""
			for _, id := range ids {
				if kept == "" {
					kept = id
					continue
				}
				if id == cur {
					continue
				}
				tmux.KillWindowByID(id)
			}
		}
	}
}

// ensureStandardWindows brings the session to the standard layout. Idempotent
// — this is what makes `tt up` heal a session degraded by a crash, an aborted
// teardown, or a tmux-resurrect restore, not just build one from cold.
func ensureStandardWindows(sess, cwd, stateDir string, wins []Window, ok bool, coldStart bool, note func(string)) error {
	dedupWindows(sess, cwd, stateDir, wins, ok, note)
	if !ok {
		// Legacy fallback (no jq): the historical dev + claude layout.
		if !tmux.WindowExists(sess, "dev") {
			if err := tmux.NewWindow(sess, "dev", cwd); err != nil {
				return err
			}
		}
		if !tmux.WindowExists(sess, "claude") {
			if err := tmux.NewWindow(sess, "claude", cwd); err != nil {
				return err
			}
		}
		if FileExists(filepath.Join(cwd, ".tt", "windows.json")) {
			note("jq not found; ignoring .tt/windows.json")
		}
		legacyLaunchClaude(sess)
		return nil
	}
	for i, w := range wins {
		created := 0
		if !tmux.WindowExists(sess, w.Name) {
			if err := tmux.NewWindow(sess, w.Name, cwd); err != nil {
				return err
			}
			created = 1
		}
		// Keep the name stable for window_exists even while a long-running
		// command would otherwise trigger tmux automatic-rename.
		tmux.SetWindowOption(sess, w.Name, "automatic-rename", "off")
		applyWindowPanes(sess, cwd, wins, i, created, coldStart)
	}
	return nil
}
