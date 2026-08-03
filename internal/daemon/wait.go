package daemon

import (
	"fmt"
	"path/filepath"
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
