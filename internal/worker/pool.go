package worker

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/luthfisolahudin/tt/internal/tmux"
)

// ResultsRetained caps the durable result store. Results are small text files
// and are the product of the work, so the cap is generous — it exists to stop
// a long-lived session growing without bound, not to reclaim space.
const ResultsRetained = 500

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

// WipeWorkerFiles removes a worker's own state — its <cs>.* files and its
// pi-sessions dir — so the callsign is free to be spawned again.
//
// It deliberately does NOT touch `results/<cs>-*.result`. Reclaiming a worker
// is reclaiming compute; the results are the durable product of the work and
// outlive the worker that produced it, so `tt pi rm` is lossless and a task id
// stays readable with `tt pi results <id>` afterwards. Task ids are never
// reused (see NextSeq), so a later incarnation of the callsign cannot collide
// with what is kept here. Bounding that store is PruneResults' job.
func WipeWorkerFiles(sdir, name string) {
	entries, err := os.ReadDir(sdir)
	if err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), name+".") {
				os.RemoveAll(filepath.Join(sdir, e.Name()))
			}
		}
	}
	os.RemoveAll(filepath.Join(sdir, "pi-sessions", name))
	PruneResults(sdir, ResultsRetained)
}

// PruneResults keeps the newest `keep` result files and deletes the rest.
//
// Retention is by modification time, not by id: the sequence is session-wide,
// so a callsign's numbering is no longer contiguous and "oldest id" is not
// "oldest result". Called on teardown, the one moment the store is known to
// have grown.
func PruneResults(sdir string, keep int) {
	rdir := filepath.Join(sdir, "results")
	entries, err := os.ReadDir(rdir)
	if err != nil {
		return
	}
	type result struct {
		name string
		mod  int64
	}
	files := make([]result, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".result") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		files = append(files, result{e.Name(), info.ModTime().UnixNano()})
	}
	if len(files) <= keep {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod > files[j].mod })
	for _, f := range files[keep:] {
		os.Remove(filepath.Join(rdir, f.name))
	}
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
		// Reserved by a dispatcher that has not enqueued its task yet. The
		// worker is legitimately idle with an empty queue during the REPL boot
		// wait, which can be 40 s, so without this the sweep would reap a
		// pipeline's worker out from under it.
		if FileExists(filepath.Join(sdir, n+".reserving")) {
			continue
		}
		// An ephemeral worker is one-shot: no one is coming to `tt pi resume`
		// it, and its result is already durable in results/. So a settled but
		// non-idle outcome must not pin the slot forever — only genuinely
		// unfinished work (busy/starting) is spared.
		switch WorkerState(sdir, session, n) {
		case "idle", "blocked", "interrupted":
		default:
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
