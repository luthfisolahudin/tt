package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/worker"
)

// noteUncollectedSkip warns (stderr) when a NON-busy worker holds a terminal
// result past its collect cursor — finished before the join, so wait-all
// skipped it. A pure signal; never changes the result or stdout.
func noteUncollectedSkip(s *Session, errb *strings.Builder, joined []string) {
	joinedSet := map[string]bool{}
	for _, j := range joined {
		joinedSet[j] = true
	}
	skipped := 0
	for _, n := range worker.NATO {
		if !tmux.WindowExists(s.Name, "pi-"+n) {
			continue
		}
		if joinedSet[n] {
			continue
		}
		if worker.WorkerState(s.Dir, s.Name, n) == "busy" {
			continue
		}
		cursor := 0
		if data, err := os.ReadFile(filepath.Join(s.Dir, n+".collected")); err == nil {
			if v, err2 := strconv.Atoi(strings.TrimSpace(string(data))); err2 == nil {
				cursor = v
			}
		}
		lt, ok := worker.LastTaskID(s.Dir, n)
		if !ok {
			continue
		}
		i := strings.LastIndex(lt, "-")
		if i < 0 {
			continue
		}
		turn, err := strconv.Atoi(lt[i+1:])
		if err != nil {
			continue
		}
		if turn <= cursor {
			continue
		}
		head, _ := worker.ResultHeadByID(s.Dir, lt)
		_, rst := worker.SplitHead(head)
		switch rst {
		case "done", "blocked", "other", "error":
			skipped++
		}
	}
	if skipped > 0 {
		fmt.Fprintf(errb, "[tt] wait-all: %d finished worker(s) not shown — 'tt pi collect' to include them\n", skipped)
	}
}

// waitAllOp joins every busy worker (or the given ones) into one consolidated
// report. Exit 0 only if every target finished done/blocked.
func waitAllOp(s *Session, a WaitArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	note := func(msg string) { fmt.Fprintf(&errb, "[tt] %s\n", msg) }
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	// Default target set: every existing worker that is currently busy.
	names := []string{}
	for _, n := range worker.NATO {
		if !tmux.WindowExists(s.Name, "pi-"+n) {
			continue
		}
		if worker.WorkerState(s.Dir, s.Name, n) == "busy" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		noteUncollectedSkip(s, &errb, nil)
		if a.JSON {
			fmt.Fprintf(&out, "[]\n")
		} else {
			note("wait-all: no busy workers")
		}
		return ok(&out, &errb, 0)
	}
	// Resolve each target's task id (its latest dispatch) up front.
	tids := make([]string, len(names))
	pend := make([]bool, len(names))
	statuses := make([]string, len(names))
	texts := make([]string, len(names))
	for i, nm := range names {
		if !tmux.WindowExists(s.Name, "pi-"+nm) {
			return die(fmt.Sprintf("pi-%s does not exist", nm))
		}
		tid, ok := worker.LastTaskID(s.Dir, nm)
		if !ok {
			return die(fmt.Sprintf("pi-%s has no task to wait on", nm))
		}
		tids[i], pend[i] = tid, true
	}
	deadline := deadlineIn(a.Timeout)
	rc := 0
	for {
		remaining := 0
		for i, nm := range names {
			if !pend[i] {
				continue
			}
			tid := tids[i]
			head, _ := worker.ResultHeadByID(s.Dir, tid)
			if head != "" {
				rid, rst := worker.SplitHead(head)
				if rid == tid {
					switch rst {
					case "done", "blocked":
						statuses[i], texts[i] = rst, worker.ResultTextByID(s.Dir, tid)
						pend[i] = false
						continue
					case "error":
						statuses[i], texts[i] = "error", worker.ResultTextByID(s.Dir, tid)
						pend[i], rc = false, 1
						continue
					case "other":
						statuses[i], texts[i] = "other", worker.ResultTextByID(s.Dir, tid)
						pend[i], rc = false, 1
						continue
					}
				}
			}
			if !worker.ReplRunning(s.Dir, nm) {
				statuses[i], texts[i] = "down", "(pi REPL stopped before "+tid+" completed)"
				pend[i], rc = false, 1
				continue
			}
			remaining = 1
		}
		if remaining == 0 {
			break
		}
		if deadlineExpired(deadline) {
			for i := range names {
				if !pend[i] {
					continue
				}
				statuses[i], texts[i] = "timeout", fmt.Sprintf("(timed out after %ds)", a.Timeout)
				pend[i], rc = false, 1
			}
			break
		}
		time.Sleep(time.Second)
	}
	noteUncollectedSkip(s, &errb, names)
	if a.JSON {
		out.WriteString("[")
		for i := range names {
			if i > 0 {
				out.WriteString(",")
			}
			out.WriteString(worker.EmitResultJSON(s.Dir, tids[i], statuses[i], texts[i]))
		}
		out.WriteString("]\n")
		return ok(&out, &errb, rc)
	}
	for i := range names {
		fmt.Fprintf(&out, "== %s (%s) %s ==\n", names[i], tids[i], statuses[i])
		// bash: texts are $(...) command-substituted (trailing newlines
		// stripped); printf '%s\n\n' adds exactly two.
		fmt.Fprintf(&out, "%s\n\n", strings.TrimRight(texts[i], "\n"))
	}
	// One-line verdict on stderr — stdout stays exactly the joined bodies.
	tally := map[string]int{}
	for _, st := range statuses {
		tally[st]++
	}
	parts := []string{}
	for _, st := range []string{"done", "blocked", "other", "error", "down", "timeout"} {
		if tally[st] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", tally[st], st))
		}
	}
	note(fmt.Sprintf("wait-all: %d task(s) — %s", len(names), strings.Join(parts, " · ")))
	return ok(&out, &errb, rc)
}
