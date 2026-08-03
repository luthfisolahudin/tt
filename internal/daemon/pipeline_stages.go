package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/worker"
)

// runFanoutStage dispatches every task via the auto policy and joins them all
// (blocking until each reaches a terminal status), returning one stageResult
// per task in dispatch order.
func (s *Session) runFanoutStage(st PipelineStage, timeout int, note func(string)) ([]stageResult, string) {
	ids := make([]string, 0, len(st.Fanout))
	for _, t := range st.Fanout {
		body := t.Task
		if t.Label != "" {
			body = "LABEL: " + t.Label + "\n" + body
		}
		tid, rerr := s.dispatchAuto(body, timeout)
		if rerr != "" {
			return nil, rerr
		}
		ids = append(ids, tid)
	}
	results := make([]stageResult, 0, len(ids))
	for _, id := range ids {
		r, rerr := s.awaitTerminal(id, timeout)
		if rerr != "" {
			return nil, rerr
		}
		results = append(results, r)
	}
	return results, ""
}

// dispatchAuto sends one task through the auto policy (reuse idle -> spawn ->
// shared pool) and returns its task id. Shares the daemon's single writer
// path by going through the same primitives autoOp uses, minus the CLI arg
// surface.
func (s *Session) dispatchAuto(body string, timeout int) (string, string) {
	// Lock only the choose+spawn+enqueue critical section (turn assignment,
	// spawn) — NOT the whole pipeline, so other send/wait ops are not blocked
	// for the pipeline's lifetime.
	writeMu.Lock()
	defer writeMu.Unlock()
	worker.ReapEphemeralWorkers(s.Dir, s.Name)
	tier := worker.TierDefault
	// Reuse the first idle, non-stale worker (NATO order).
	chosen := ""
	for _, n := range worker.NATO {
		if !tmux.WindowExists(s.Name, "pi-"+n) {
			continue
		}
		if worker.WorkerState(s.Dir, s.Name, n) != "idle" {
			continue
		}
		if worker.WorkerHasStaleTier(s.Dir, n) {
			continue
		}
		chosen = n
		break
	}
	var errb strings.Builder
	if chosen == "" && worker.CountWorkers(s.Name) < worker.PiCap() {
		for _, n := range worker.NATO {
			if !tmux.WindowExists(s.Name, "pi-"+n) {
				chosen = n
				if werr := worker.SpawnPiWindow(s.Dir, s.Name, s.Cwd, n, s.SyncEnv, &errb); werr != nil {
					return "", werr.Error()
				}
				break
			}
		}
	}
	if chosen != "" {
		if err := worker.EnsureReplReady(s.Dir, s.Name, s.Cwd, chosen, &errb); err != nil {
			return "", err.Error()
		}
		tid, err := worker.EnqueueToWorker(s.Dir, chosen, worker.CurrentTier(s.Dir, chosen), []byte(body), false)
		if err != nil {
			return "", err.Error()
		}
		return tid, ""
	}
	// All workers busy at cap -> shared pool; the next idle worker steals it.
	tid, err := worker.EnqueuePool(s.Dir, tier, []byte(body), false)
	if err != nil {
		return "", err.Error()
	}
	return tid, ""
}

// awaitTerminal blocks until a task id reaches a terminal status
// (done/blocked/other/error), the worker dies, or the timeout lapses.
func (s *Session) awaitTerminal(id string, timeout int) (stageResult, string) {
	deadline := deadlineIn(timeout)
	cs := id
	if i := strings.Index(id, "-"); i >= 0 {
		cs = id[:i]
	}
	for {
		head, _ := worker.ResultHeadByID(s.Dir, id)
		if head != "" {
			rid, rst := worker.SplitHead(head)
			if rid == id {
				switch rst {
				case "done", "blocked", "other", "error":
					text := worker.ResultTextByID(s.Dir, id)
					sm := worker.ResultField(text, "summary")
					if sm == "" {
						sm = worker.ResultField(text, "reason")
					}
					if sm == "" {
						sm = worker.ResultReasonHint(text)
					}
					return stageResult{
						ID:      id,
						Status:  rst,
						Dur:     worker.ResultDurationFile(worker.ResultPathByID(s.Dir, id)),
						Summary: sm,
						Text:    text,
					}, ""
				}
			}
		}
		// For a pooled task the claiming worker is unknown; only a named task's
		// worker can be checked for a dead REPL.
		if !strings.HasPrefix(id, "pool-") && !worker.ReplRunning(s.Dir, cs) {
			return stageResult{ID: id, Status: "down", Summary: "worker REPL stopped"}, ""
		}
		if deadlineExpired(deadline) {
			return stageResult{ID: id, Status: "timeout", Summary: "stage join timed out"}, ""
		}
		time.Sleep(time.Second)
	}
}

// runReviewStage hands the previous stage's results to ONE worker and reads
// its terminal verdict. The review prompt must instruct the worker to end
// with PIPELINE_PASS or PIPELINE_FAIL: <reason>.
func (s *Session) runReviewStage(spec *ReviewSpec, prev []stageResult, timeout int, note func(string)) (bool, string, string) {
	var b strings.Builder
	b.WriteString(spec.Prompt)
	b.WriteString("\n\nReview these results from the previous stage. For each, decide whether it met its stated SUCCESS. Then end your reply with exactly one terminal line: PIPELINE_PASS (if every result passed) or PIPELINE_FAIL: <one-line reason> (otherwise).\n\n")
	for _, r := range prev {
		fmt.Fprintf(&b, "=== %s (%s) ===\n%s\n\n", r.ID, r.Status, strings.TrimRight(r.Text, "\n"))
	}
	tid, rerr := s.dispatchAuto(b.String(), timeout)
	if rerr != "" {
		return false, "", rerr
	}
	r, rerr := s.awaitTerminal(tid, timeout)
	if rerr != "" {
		return false, "", rerr
	}
	// Verdict is the LAST PIPELINE_PASS / PIPELINE_FAIL line in the body.
	pass, reason := false, "review worker gave no PIPELINE_PASS/FAIL verdict"
	for _, line := range strings.Split(r.Text, "\n") {
		line = strings.TrimSpace(line)
		if line == "PIPELINE_PASS" {
			pass, reason = true, ""
		} else if strings.HasPrefix(line, "PIPELINE_FAIL") {
			pass = false
			reason = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "PIPELINE_FAIL"), ":"))
			if reason == "" {
				reason = "review gate failed"
			}
		}
	}
	return pass, reason, ""
}

// parsePipelineSpec decodes the base64 spec JSON and validates its shape.
