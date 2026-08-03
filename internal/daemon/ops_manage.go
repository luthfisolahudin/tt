package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/worker"
)

func resumeOp(s *Session, a ResumeArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	note := func(msg string) { fmt.Fprintf(&errb, "[tt] %s\n", msg) }
	if !worker.ValidCallsign(a.Callsign) {
		return die("invalid callsign: " + a.Callsign)
	}
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	if !tmux.WindowExists(s.Name, "pi-"+a.Callsign) {
		return die(fmt.Sprintf("pi-%s does not exist", a.Callsign))
	}
	if !worker.ReplRunning(s.Dir, a.Callsign) {
		return die(fmt.Sprintf("pi-%s REPL is not running; resume needs its live context (use `tt pi clear %s` to start fresh)", a.Callsign, a.Callsign))
	}
	st := worker.WorkerState(s.Dir, s.Name, a.Callsign)
	if st != "interrupted" {
		return die(fmt.Sprintf("pi-%s is %s, not interrupted — nothing to resume", a.Callsign, st))
	}
	if err := worker.WriteResumeFile(s.Dir, a.Callsign); err != nil {
		return die(err.Error())
	}
	note(fmt.Sprintf("pi-%s resuming its interrupted task (context preserved); join with `tt pi wait %s`", a.Callsign, a.Callsign))
	return ok(&out, &errb, 0)
}

func clearOp(s *Session, a ClearArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	note := func(msg string) { fmt.Fprintf(&errb, "[tt] %s\n", msg) }
	if !worker.ValidCallsign(a.Callsign) {
		return die("invalid callsign: " + a.Callsign)
	}
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	if !tmux.WindowExists(s.Name, "pi-"+a.Callsign) {
		return die(fmt.Sprintf("pi-%s does not exist", a.Callsign))
	}
	if !a.Force && !worker.EnsureIdleOrBlocked(s.Dir, s.Name, a.Callsign) {
		return die(fmt.Sprintf("pi-%s is busy; use --force to clear anyway", a.Callsign))
	}
	if a.Force && worker.WorkerState(s.Dir, s.Name, a.Callsign) == "busy" {
		note(fmt.Sprintf("interrupting running pi turn on pi-%s", a.Callsign))
	}
	worker.BumpGen(s.Dir, a.Callsign)
	line := fmt.Sprintf("{\"clear\":%d,\"at\":%d}", worker.CurrentGen(s.Dir, a.Callsign), timeNow().Unix())
	appendJSONL(s.Dir, a.Callsign+".tasks.jsonl", line)
	// rm -f <cs>.in.*.txt
	if matches, err := filepath.Glob(filepath.Join(s.Dir, a.Callsign+".in.*.txt")); err == nil {
		for _, m := range matches {
			os.Remove(m)
		}
	}
	os.WriteFile(filepath.Join(s.Dir, a.Callsign+".tier"), []byte(worker.TierDefault), 0644)
	applySyncEnv(s)
	if err := worker.StartRepl(s.Dir, s.Name, s.Cwd, a.Callsign, &errb); err != nil {
		return die(err.Error())
	}
	note(fmt.Sprintf("pi-%s cleared (gen %d)", a.Callsign, worker.CurrentGen(s.Dir, a.Callsign)))
	return ok(&out, &errb, 0)
}

func rmOp(s *Session, a RmArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	if !worker.ValidCallsign(a.Callsign) {
		return die("invalid callsign: " + a.Callsign)
	}
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	if !tmux.WindowExists(s.Name, "pi-"+a.Callsign) {
		return die(fmt.Sprintf("pi-%s does not exist", a.Callsign))
	}
	if !a.Force && !worker.EnsureIdleOrBlocked(s.Dir, s.Name, a.Callsign) {
		return die(fmt.Sprintf("pi-%s is busy; use --force", a.Callsign))
	}
	tmux.KillWindow(s.Name, "pi-"+a.Callsign)
	worker.WipeWorkerFiles(s.Dir, a.Callsign)
	return ok(&out, &errb, 0)
}

func popidleOp(s *Session) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	// Highest NATO callsign first.
	for i := len(worker.NATO) - 1; i >= 0; i-- {
		n := worker.NATO[i]
		if !tmux.WindowExists(s.Name, "pi-"+n) {
			continue
		}
		if worker.EnsureIdleOrBlocked(s.Dir, s.Name, n) {
			tmux.KillWindow(s.Name, "pi-"+n)
			worker.WipeWorkerFiles(s.Dir, n)
			fmt.Fprintf(&out, "%s\n", n)
			return ok(&out, &errb, 0)
		}
	}
	return ok(&out, &errb, 0)
}

func appendJSONL(sdir, name, line string) {
	f := filepath.Join(sdir, name)
	file, err := os.OpenFile(f, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer file.Close()
	file.WriteString(line + "\n")
}

func timeNow() time.Time {
	return time.Now()
}
