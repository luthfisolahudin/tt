package worker

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/luthfisolahudin/tt/internal/tmux"
)

// NATO is the ordered callsign pool (alfa..zulu); none is special.
var NATO = []string{
	"alfa", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel",
	"india", "juliett", "kilo", "lima", "mike", "november", "oscar", "papa",
	"quebec", "romeo", "sierra", "tango", "uniform", "victor", "whiskey",
	"xray", "yankee", "zulu",
}

// ValidCallsign reports whether name is one of the NATO callsigns.
func ValidCallsign(name string) bool {
	for _, n := range NATO {
		if n == name {
			return true
		}
	}
	return false
}

// PiCap is the hard worker ceiling: min(cores-2, 26), floored at 1 — the
// runaway backstop that keeps lazy/auto spawning safe.
func PiCap() int {
	c := runtime.NumCPU()
	m := c - 2
	if m < 1 {
		m = 1
	}
	if m > 26 {
		m = 26
	}
	return m
}

// CountWorkers counts existing pi-* worker windows in the session.
func CountWorkers(session string) int {
	c := 0
	for _, n := range NATO {
		if tmux.WindowExists(session, "pi-"+n) {
			c++
		}
	}
	return c
}

// WipeWorkerFiles removes all of a worker's state: <cs>.* files, its durable
// results, and its pi-sessions dir. Shared dirs (queue/, results/ index) are
// untouched.
func WipeWorkerFiles(sdir, name string) {
	entries, err := os.ReadDir(sdir)
	if err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), name+".") {
				os.RemoveAll(filepath.Join(sdir, e.Name()))
			}
		}
	}
	rdir := filepath.Join(sdir, "results")
	if entries, err := os.ReadDir(rdir); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), name+"-") && strings.HasSuffix(e.Name(), ".result") {
				os.Remove(filepath.Join(rdir, e.Name()))
			}
		}
	}
	os.RemoveAll(filepath.Join(sdir, "pi-sessions", name))
}

// ReapEphemeralWorkers tears down any ephemeral (auto --rm) worker that has
// gone idle with an empty queue — its one-shot job (and any pinned follow-ups)
// is done. Swept opportunistically by auto/status.
func ReapEphemeralWorkers(sdir, session string) {
	for _, n := range NATO {
		if !FileExists(filepath.Join(sdir, n+".ephemeral")) {
			continue
		}
		if !tmux.WindowExists(session, "pi-"+n) {
			WipeWorkerFiles(sdir, n)
			continue
		}
		if WorkerState(sdir, session, n) != "idle" {
			continue
		}
		// leave pinned follow-ups (named sends) to drain first
		if QueueDepth(sdir, n) > 0 {
			continue
		}
		tmux.KillWindow(session, "pi-"+n)
		WipeWorkerFiles(sdir, n)
	}
}
