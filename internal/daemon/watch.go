package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/worker"
)

// WaitArgs is pi_wait_cmd: block until the task's result is recorded. Target
// is a callsign, a bare task-id, a pool-<n> id, or "all".
type WaitArgs struct {
	Target  string `json:"target"`
	TaskID  string `json:"task_id"`
	Timeout int    `json:"timeout"`
	JSON    bool   `json:"json"`
}

// StatusArgs is pi_status_cmd (--json flag).
type StatusArgs struct {
	JSON bool `json:"json"`
}

// CollectArgs is pi_collect_cmd: cursor-based fan-out join.
type CollectArgs struct {
	JSON    bool   `json:"json"`
	Timeout int    `json:"timeout"`
	Target  string `json:"target"`
	Digest  bool   `json:"digest"`
}

// PeekArgs is tt_peek_cmd: a read-only state query returning a window's
// current pane content (the agent-readable "see window X" primitive).
type PeekArgs struct {
	Target string `json:"target"` // bare window, callsign, or pi-<cs>
	Lines  int    `json:"lines"`
}

// ResultsArgs is pi_results_cmd: read durable outcomes from the per-id store.
type ResultsArgs struct {
	JSON   bool   `json:"json"`
	Target string `json:"target"`
}

func deadlineIn(timeout int) int64 {
	if timeout <= 0 {
		return 0
	}
	return time.Now().Unix() + int64(timeout)
}

func deadlineExpired(deadline int64) bool {
	return deadline != 0 && time.Now().Unix() >= deadline
}

func waitOp(s *Session, a WaitArgs) client.Response {
	name, taskID := a.Target, a.TaskID
	// 'all' is a pseudo-callsign meaning every busy worker — fan-out join.
	if name == "all" {
		if taskID != "" {
			return waitErr("wait all: a task-id cannot be combined with 'all'")
		}
		return waitAllOp(s, a)
	}
	// A pool task id is its own handle (results live in the unified store).
	if strings.HasPrefix(name, "pool-") {
		if taskID != "" {
			return waitErr("wait: a pool id takes no second argument")
		}
		return waitPoolOp(s, name, a.Timeout, a.JSON)
	}
	// Accept a bare task-id (`alfa-3`) as the sole argument — the callsign is
	// embedded, so `tt pi wait $(tt pi auto …)` just works.
	if taskID == "" && strings.Contains(name, "-") {
		i := strings.LastIndex(name, "-")
		cs := name[:i]
		if worker.IsTaskID(name) && worker.ValidCallsign(cs) {
			taskID, name = name, cs
		}
	}
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	if !worker.ValidCallsign(name) {
		return die("invalid callsign: " + name)
	}
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	if !tmux.WindowExists(s.Name, "pi-"+name) {
		return die(fmt.Sprintf("pi-%s does not exist", name))
	}
	// Task-id is optional: default to the worker's latest dispatch.
	if taskID == "" {
		tid, ok := worker.LastTaskID(s.Dir, name)
		if !ok {
			return die(fmt.Sprintf("wait: pi-%s has no task to wait on", name))
		}
		taskID = tid
	}
	if i := strings.Index(taskID, "-"); i >= 0 {
		if expected := taskID[:i]; expected != name {
			return die(fmt.Sprintf("task-id callsign mismatch (%s vs %s)", expected, name))
		}
	}
	// Block until the extension records this task's result in the per-id store.
	deadline := deadlineIn(a.Timeout)
	turn := taskID[strings.Index(taskID, "-")+1:]
	qf := filepath.Join(s.Dir, name+".queue", turn+".task")
	stuckSince := int64(0)
	rst := ""
	for {
		head, _ := worker.ResultHeadByID(s.Dir, taskID)
		if head != "" {
			rid, r2 := worker.SplitHead(head)
			if rid == taskID {
				switch r2 {
				case "done", "blocked":
					if a.JSON {
						fmt.Fprintf(&out, "%s\n", worker.EmitResultJSON(s.Dir, taskID, r2, worker.ResultTextByID(s.Dir, taskID)))
					} else {
						out.WriteString(worker.ResultTextByID(s.Dir, taskID))
					}
					// Reap promptly if this was an ephemeral (--rm) worker's last task.
					if worker.FileExists(filepath.Join(s.Dir, name+".ephemeral")) {
						worker.ReapEphemeralWorkers(s.Dir, s.Name)
					}
					return ok(&out, &errb, 0)
				case "error":
					if a.JSON {
						fmt.Fprintf(&out, "%s\n", worker.EmitResultJSON(s.Dir, taskID, "error", worker.ResultTextByID(s.Dir, taskID)))
						return ok(&out, &errb, 1)
					}
					errb.WriteString(worker.ResultTextByID(s.Dir, taskID))
					return die(fmt.Sprintf("pi-%s reported an internal error for %s", name, taskID))
				case "other":
					if a.JSON {
						fmt.Fprintf(&out, "%s\n", worker.EmitResultJSON(s.Dir, taskID, "other", worker.ResultTextByID(s.Dir, taskID)))
						return ok(&out, &errb, 1)
					}
					errb.WriteString(worker.ResultTextByID(s.Dir, taskID))
					return die(fmt.Sprintf("pi finished %s without WORKER_DONE/BLOCKED (text above)", taskID))
				}
			}
			rst = r2
		}
		if !worker.ReplRunning(s.Dir, name) {
			return die(fmt.Sprintf("pi REPL on pi-%s stopped before %s completed", name, taskID))
		}
		// Stuck guard: our task is still queued while the worker is NOT running
		// anything — a dead pump / watch. Running an earlier task is legitimate.
		if worker.FileExists(qf) {
			if rst == "running" {
				stuckSince = 0
			} else {
				if stuckSince == 0 {
					stuckSince = time.Now().Unix()
				}
				if time.Now().Unix()-stuckSince >= 20 {
					return die(fmt.Sprintf("pi-%s never picked up %s (worker idle but task still queued; pump/watch dead)", name, taskID))
				}
			}
		} else {
			stuckSince = 0
		}
		if deadlineExpired(deadline) {
			return die(fmt.Sprintf("wait timeout for %s (%ds)", taskID, a.Timeout))
		}
		time.Sleep(time.Second)
	}
}

// waitErr is a plain failure without a session context (used for early
// argument errors before the loop).
func waitErr(msg string) client.Response {
	var out, errb strings.Builder
	die := func(m string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", m)
		return ok(&out, &errb, 1)
	}
	return die(msg)
}

// waitPoolOp waits on a shared-pool task by its pool-<seq> id. No stuck guard:
// a pool task legitimately waits for a worker to free up (--timeout bounds it).
func waitPoolOp(s *Session, id string, timeout int, jsonMode bool) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	prf := worker.ResultPathByID(s.Dir, id)
	deadline := deadlineIn(timeout)
	for {
		head, _ := worker.ResultHeadFile(prf)
		if head != "" {
			rid, rst := worker.SplitHead(head)
			if rid == id {
				switch rst {
				case "done", "blocked":
					if jsonMode {
						fmt.Fprintf(&out, "%s\n", worker.EmitResultJSON(s.Dir, id, rst, worker.ResultTextFile(prf)))
					} else {
						out.WriteString(worker.ResultTextFile(prf))
					}
					return ok(&out, &errb, 0)
				case "error":
					if jsonMode {
						fmt.Fprintf(&out, "%s\n", worker.EmitResultJSON(s.Dir, id, "error", worker.ResultTextFile(prf)))
						return ok(&out, &errb, 1)
					}
					errb.WriteString(worker.ResultTextFile(prf))
					return die(fmt.Sprintf("pool task %s reported an internal error", id))
				case "other":
					if jsonMode {
						fmt.Fprintf(&out, "%s\n", worker.EmitResultJSON(s.Dir, id, "other", worker.ResultTextFile(prf)))
						return ok(&out, &errb, 1)
					}
					errb.WriteString(worker.ResultTextFile(prf))
					return die(fmt.Sprintf("pool task %s finished without WORKER_DONE/BLOCKED (text above)", id))
				}
			}
		}
		if deadlineExpired(deadline) {
			return die(fmt.Sprintf("wait timeout for %s (%ds)", id, timeout))
		}
		time.Sleep(time.Second)
	}
}

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

func statusOp(s *Session, a StatusArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	worker.ReapEphemeralWorkers(s.Dir, s.Name)

	type row struct {
		cs, st, tier, lastID, el, reason string
		gen, q                           int
	}
	rows := []row{}
	for _, n := range worker.NATO {
		if !tmux.WindowExists(s.Name, "pi-"+n) {
			continue
		}
		st := worker.WorkerState(s.Dir, s.Name, n)
		tier := worker.TierLabel(s.Dir, n)
		gen := worker.CurrentGen(s.Dir, n)
		lastID, _ := worker.LastTaskID(s.Dir, n)
		q := worker.QueueDepth(s.Dir, n)
		el := ""
		if st == "busy" {
			if v, ok := worker.SecsSinceFile(filepath.Join(s.Dir, n+".busy")); ok && v >= 0 {
				el = strconv.Itoa(v)
			}
		}
		reason := ""
		if (st == "interrupted" || st == "blocked") && lastID != "" {
			reason = worker.ResultReasonHint(worker.ResultTextByID(s.Dir, lastID))
		}
		rows = append(rows, row{n, st, tier, lastID, el, reason, gen, q})
	}
	if a.JSON {
		out.WriteString("[")
		for i, r := range rows {
			if i > 0 {
				out.WriteString(",")
			}
			el := "null"
			if r.el != "" {
				el = r.el
			}
			fmt.Fprintf(&out, `{"callsign":"%s","state":"%s","elapsed_s":%s,"queued":%d,"last_task":"%s","tier":"%s","gen":%d,"reason":"%s"}`,
				worker.JSONEscape(r.cs), worker.JSONEscape(r.st), el, r.q, worker.JSONEscape(r.lastID), worker.JSONEscape(r.tier), r.gen, worker.JSONEscape(r.reason))
		}
		out.WriteString("]\n")
		return ok(&out, &errb, 0)
	}
	fmt.Fprintf(&out, "%-9s  %-11s  %-7s  %-5s  %-12s  %-8s  %s\n", "CALLSIGN", "STATE", "ELAPSED", "QUEUE", "LAST-TASK", "TIER", "GEN")
	for _, r := range rows {
		elDisp, qDisp := "-", "-"
		if r.el != "" {
			if v, err := strconv.Atoi(r.el); err == nil {
				elDisp = worker.FmtElapsed(v)
			}
		}
		if r.q > 0 {
			qDisp = fmt.Sprintf("+%d", r.q)
		}
		lastID := r.lastID
		if lastID == "" {
			lastID = "-"
		}
		if r.reason != "" {
			fmt.Fprintf(&out, "%-9s  %-11s  %-7s  %-5s  %-12s  %-8s  g%-4d — %s\n", r.cs, r.st, elDisp, qDisp, lastID, r.tier, r.gen, r.reason)
		} else {
			fmt.Fprintf(&out, "%-9s  %-11s  %-7s  %-5s  %-12s  %-8s  g%d\n", r.cs, r.st, elDisp, qDisp, lastID, r.tier, r.gen)
		}
	}
	// Shared pool: tasks dispatched via auto while every worker was busy.
	pending := 0
	if entries, err := os.ReadDir(filepath.Join(s.Dir, "queue")); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".task") {
				pending++
			}
		}
	}
	if pending > 0 {
		fmt.Fprintf(&out, "pool: %d task(s) queued (unclaimed)\n", pending)
	}
	return ok(&out, &errb, 0)
}

func collectOp(s *Session, a CollectArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	note := func(msg string) { fmt.Fprintf(&errb, "[tt] %s\n", msg) }
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	targets := []string{}
	if a.Target == "" || a.Target == "all" {
		for _, n := range worker.NATO {
			if tmux.WindowExists(s.Name, "pi-"+n) {
				targets = append(targets, n)
			}
		}
	} else {
		if !worker.ValidCallsign(a.Target) {
			return die(fmt.Sprintf("collect: '%s' is not a callsign or 'all'", a.Target))
		}
		if !tmux.WindowExists(s.Name, "pi-"+a.Target) {
			return die(fmt.Sprintf("pi-%s does not exist", a.Target))
		}
		targets = []string{a.Target}
	}
	deadline := deadlineIn(a.Timeout)
	envelopes := []string{}
	printed := false
	for _, cs := range targets {
		cursor := 0
		if data, err := os.ReadFile(filepath.Join(s.Dir, cs+".collected")); err == nil {
			if v, err2 := strconv.Atoi(strings.TrimSpace(string(data))); err2 == nil {
				cursor = v
			}
		}
		data, err := os.ReadFile(filepath.Join(s.Dir, cs+".tasks.jsonl"))
		if err != nil || len(data) == 0 {
			continue // no tasks logged for this worker
		}
		turns := []int{}
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, `"id":"`) {
				continue // skip {"clear":N} markers
			}
			if t := parseTurn(line); t > cursor {
				turns = append(turns, t)
			}
		}
		if len(turns) == 0 {
			continue
		}
		sort.Ints(turns)
		newcursor := cursor
		for _, t := range turns {
			id := fmt.Sprintf("%s-%d", cs, t)
			rst := ""
			for {
				head, _ := worker.ResultHeadByID(s.Dir, id)
				if head != "" {
					_, rst = worker.SplitHead(head)
					switch rst {
					case "done", "blocked", "other", "error":
						break
					default:
						rst = ""
					}
					if rst == "done" || rst == "blocked" || rst == "other" || rst == "error" {
						break
					}
				}
				if !worker.ReplRunning(s.Dir, cs) {
					rst = "down"
					break
				}
				if deadlineExpired(deadline) {
					rst = "timeout"
					break
				}
				time.Sleep(time.Second)
			}
			if rst == "timeout" || rst == "down" {
				break // stop; never skip a turn
			}
			text := worker.ResultTextByID(s.Dir, id)
			if a.JSON {
				envelopes = append(envelopes, worker.EmitResultJSON(s.Dir, id, rst, text))
			} else if a.Digest {
				// One lean line per result; the full body stays id-addressable
				// via `tt pi results <id>`. Kills the join's context tax.
				fmt.Fprintf(&out, "%s\n", digestLine(s.Dir, id, rst, text))
			} else {
				// bash: $(result_text_by_id) strips trailing newlines; printf
				// '%s\n\n' adds exactly two.
				fmt.Fprintf(&out, "== %s (%s) ==\n%s\n\n", id, rst, strings.TrimRight(text, "\n"))
			}
			newcursor = t
			printed = true
		}
		if newcursor > cursor {
			os.WriteFile(filepath.Join(s.Dir, cs+".collected"), []byte(strconv.Itoa(newcursor)), 0644)
		}
	}
	if a.JSON {
		out.WriteString("[")
		for i, e := range envelopes {
			if i > 0 {
				out.WriteString(",")
			}
			out.WriteString(e)
		}
		out.WriteString("]\n")
	} else if !printed {
		note("collect: nothing new")
	}
	return ok(&out, &errb, 0)
}

// digestLine is one lean row for `collect --digest`:
// "<id>  <status>  <dur>  <one-line summary/reason>". The full body is never
// inlined — pull it with `tt pi results <id>` when the digest is not enough.
func digestLine(sdir, id, rst, text string) string {
	sm := worker.ResultField(text, "summary")
	if sm == "" {
		sm = worker.ResultField(text, "reason")
	}
	if sm == "" {
		sm = worker.ResultReasonHint(text)
	}
	return fmt.Sprintf("%-12s  %-8s  %-6s  %s", id, rst, worker.ResultDurationFile(worker.ResultPathByID(sdir, id)), truncateRunes(sm, 80))
}

// peekOp returns a window's current pane content — read-only. Target may be a
// bare window name (dev), a worker callsign (alfa -> pi-alfa), or a full
// pi-<cs>. Unlike `tt pi logs` (workers only), peek sees ANY window in the
// session, so an agent can read the dev server, the orchestrator pane, or a
// user window as a state query instead of scraping tmux itself.
func peekOp(s *Session, a PeekArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	target := strings.TrimSpace(a.Target)
	if target == "" {
		return die("peek: target window or callsign required")
	}
	window := target
	if !tmux.WindowExists(s.Name, window) {
		// Try as a worker callsign -> its pi-<cs> window.
		if worker.ValidCallsign(target) && tmux.WindowExists(s.Name, "pi-"+target) {
			window = "pi-" + target
		} else {
			return die(fmt.Sprintf("no window '%s' (or worker pi-%s) in session %s", target, target, s.Name))
		}
	}
	lines := a.Lines
	if lines <= 0 {
		lines = 200
	}
	content, err := tmux.CapturePane(s.Name, window, lines)
	if err != nil {
		return die(err.Error())
	}
	out.WriteString(content)
	return ok(&out, &errb, 0)
}

// parseTurn extracts the numeric "turn":N from a tasks.jsonl line.
func parseTurn(line string) int {
	idx := strings.LastIndex(line, `"turn":`)
	if idx < 0 {
		return -1
	}
	rest := line[idx+len(`"turn":`):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return -1
	}
	v, _ := strconv.Atoi(rest[:end])
	return v
}

func resultsOp(s *Session, a ResultsArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	note := func(msg string) { fmt.Fprintf(&errb, "[tt] %s\n", msg) }
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	target := a.Target
	// A single task-id -> print (or emit) that one result.
	if target != "" && worker.IsTaskID(target) {
		f := worker.ResultPathByID(s.Dir, target)
		if !worker.FileExists(f) {
			return die(fmt.Sprintf("no result recorded for %s", target))
		}
		head, _ := worker.ResultHeadFile(f)
		_, rst := worker.SplitHead(head)
		// bash's $(result_text_file) strips trailing newlines, then printf
		// '%s\n' adds exactly one — match for byte parity.
		text := strings.TrimRight(worker.ResultTextFile(f), "\n")
		if a.JSON {
			fmt.Fprintf(&out, "%s\n", worker.EmitResultJSON(s.Dir, target, rst, worker.ResultTextFile(f)))
		} else {
			fmt.Fprintf(&out, "== %s (%s) ==\n%s\n", target, rst, text)
		}
		return ok(&out, &errb, 0)
	}
	// Otherwise a listing; an optional callsign filters to that worker's tasks.
	if target != "" && !worker.ValidCallsign(target) {
		return die(fmt.Sprintf("results: '%s' is not a callsign or task-id", target))
	}
	files := []string{}
	if entries, err := os.ReadDir(filepath.Join(s.Dir, "results")); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".result") {
				files = append(files, filepath.Join(s.Dir, "results", e.Name()))
			}
		}
	}
	// Newest first by mtime (ls -1t); ties broken by name desc.
	sort.Slice(files, func(i, j int) bool {
		mi, ei := os.Stat(files[i])
		mj, ej := os.Stat(files[j])
		if ei != nil || ej != nil {
			return files[i] > files[j]
		}
		if mi.ModTime() != mj.ModTime() {
			return mi.ModTime().After(mj.ModTime())
		}
		return files[i] > files[j]
	})
	if a.JSON {
		out.WriteString("[")
		first := true
		for _, f := range files {
			base := strings.TrimSuffix(filepath.Base(f), ".result")
			if target != "" && !strings.HasPrefix(base, target+"-") {
				continue
			}
			head, _ := worker.ResultHeadFile(f)
			_, rst := worker.SplitHead(head)
			text := worker.ResultTextFile(f)
			if !first {
				out.WriteString(",")
			}
			first = false
			out.WriteString(worker.EmitResultJSON(s.Dir, base, rst, text))
		}
		out.WriteString("]\n")
		return ok(&out, &errb, 0)
	}
	fmt.Fprintf(&out, "%-12s  %-8s  %-6s  %s\n", "TASK", "STATUS", "DUR", "SUMMARY")
	shown := false
	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".result")
		if target != "" && !strings.HasPrefix(base, target+"-") {
			continue
		}
		head, _ := worker.ResultHeadFile(f)
		_, rst := worker.SplitHead(head)
		text := worker.ResultTextFile(f)
		sm := worker.ResultField(text, "summary")
		if sm == "" {
			sm = worker.ResultReasonHint(text)
		}
		sm = truncateRunes(sm, 80)
		fmt.Fprintf(&out, "%-12s  %-8s  %-6s  %s\n", base, rst, worker.ResultDurationFile(f), sm)
		shown = true
	}
	if !shown {
		if target != "" {
			note("no results recorded for " + target)
		} else {
			note("no results recorded")
		}
	}
	return ok(&out, &errb, 0)
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
