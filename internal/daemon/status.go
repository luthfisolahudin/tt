package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/worker"
)

// StatusArgs is pi_status_cmd (--json flag).
type StatusArgs struct {
	JSON bool `json:"json"`
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
