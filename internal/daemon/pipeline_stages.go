package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/worker"
)

// runFanoutStage dispatches every task and joins them all, returning one
// stageResult per task in dispatch order.
//
// Parallelism is the whole point of a fan-out, so this is deliberately three
// phases rather than a dispatch-and-wait loop:
//
//  1. Reserve a worker per task, serially. Reserving is cheap — spawning
//     creates the window and fires the REPL without waiting for it.
//  2. Wait for each REPL and enqueue, concurrently. The boot wait is up to
//     40 s per worker and must not be serialized (nor hold the write mutex).
//  3. Join.
//
// The reservation set is what keeps tasks off the same worker: a worker does
// not flip to `busy` until the extension claims its task (a 200 ms poll), so
// a naive "pick the first idle worker" loop hands the next task to the worker
// that just got one and silently serializes the fan-out.
func (s *Session) runFanoutStage(st PipelineStage, timeout int, note func(string)) ([]stageResult, string) {
	n := len(st.Fanout)
	bodies := make([]string, n)
	for i, t := range st.Fanout {
		bodies[i] = t.Task
		if t.Label != "" {
			bodies[i] = "LABEL: " + t.Label + "\n" + t.Task
		}
	}

	reserved := make([]string, n) // "" => no worker free, use the shared pool
	claimed := map[string]bool{}
	for i := range bodies {
		cs, rerr := s.reserveWorker(claimed)
		if rerr != "" {
			return nil, rerr
		}
		if cs != "" {
			claimed[cs] = true
		}
		reserved[i] = cs
	}
	if len(claimed) < n {
		note(fmt.Sprintf("fanout: %d task(s) share %d worker(s) — at the cap, the rest queue on the pool", n, len(claimed)))
	}

	ids := make([]string, n)
	errs := make([]string, n)
	var wg sync.WaitGroup
	for i := range bodies {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i], errs[i] = s.dispatchTo(reserved[i], bodies[i])
		}(i)
	}
	wg.Wait()
	for _, e := range errs {
		if e != "" {
			return nil, e
		}
	}

	results := make([]stageResult, 0, n)
	for _, id := range ids {
		r, rerr := s.awaitTerminal(id, timeout)
		if rerr != "" {
			return nil, rerr
		}
		// A task that did not finish cleanly still flows into the review gate,
		// which can only judge what it is handed — say so on the console, or a
		// `down`/`timeout` looks like a review verdict about the work itself.
		if r.Status != "done" {
			note(fmt.Sprintf("fanout: %s ended %s — %s", r.ID, r.Status, r.Summary))
		}
		results = append(results, r)
	}
	return results, ""
}

// reserveWorker picks a worker for one pipeline task and returns its
// callsign, or "" when the cap is reached and the task must go to the shared
// pool. Spawning does NOT wait for the REPL, so this stays cheap.
//
// A pipeline ALWAYS spawns a fresh worker rather than reusing an idle one: a
// reused worker carries its previous task's context, which biases a stage
// that is supposed to judge only what it was handed (a review gate that has
// already seen the work it reviews is worthless). `claimed` additionally
// excludes callsigns this stage already took — a worker does not flip to
// `busy` until the extension claims its task (a 200 ms poll), so without it
// the fan-out silently stacks onto one worker.
//
// At the cap there is nothing fresh to hand out, so the task goes to the
// shared pool instead of stealing (and dirtying) someone's existing worker.
// Pipeline workers persist after the run — reclaim them with `tt pi popidle`
// or `tt pi rm <cs>`.
func (s *Session) reserveWorker(claimed map[string]bool) (string, string) {
	writeMu.Lock()
	defer writeMu.Unlock()
	worker.ReapEphemeralWorkers(s.Dir, s.Name)
	if worker.CountWorkers(s.Name) < worker.PiCap() {
		var errb strings.Builder
		for _, n := range worker.NATO {
			if claimed[n] || tmux.WindowExists(s.Name, "pi-"+n) {
				continue
			}
			// Mark ephemeral BEFORE spawning — StartRepl reads the marker to
			// set TT_WORKER_EPHEMERAL in the REPL's env, which is what stops an
			// ephemeral worker stealing shared-pool work. A pipeline never
			// reuses a worker, so its workers are one-shot and the reaper
			// reclaims them once they settle. Without this a run left its
			// workers behind, and the next run started at the cap and fell back
			// onto context-dirty pool workers.
			//
			// `.reserving` holds the reaper off until the task is actually
			// enqueued: dispatch waits for the REPL outside the write mutex, so
			// the worker sits idle-and-empty for up to 40 s first.
			if werr := os.WriteFile(filepath.Join(s.Dir, n+".ephemeral"), nil, 0644); werr != nil {
				return "", werr.Error()
			}
			if werr := os.WriteFile(filepath.Join(s.Dir, n+".reserving"), nil, 0644); werr != nil {
				return "", werr.Error()
			}
			if werr := worker.SpawnPiWindow(s.Dir, s.Name, s.Cwd, n, s.SyncEnv, &errb); werr != nil {
				return "", werr.Error()
			}
			return n, ""
		}
	}
	return "", "" // at the cap — caller falls back to the shared pool
}

// dispatchTo waits for a reserved worker's REPL and enqueues the task, or
// drops the task on the shared pool when no worker was reserved. The boot
// wait deliberately runs OUTSIDE the write mutex: it can take 40 s, which
// would otherwise block every other session's dispatch.
func (s *Session) dispatchTo(cs, body string) (string, string) {
	var errb strings.Builder
	if cs == "" {
		writeMu.Lock()
		defer writeMu.Unlock()
		tid, err := worker.EnqueuePool(s.Dir, worker.TierDefault, []byte(body), false)
		if err != nil {
			return "", err.Error()
		}
		return tid, ""
	}
	// Release the reservation however this returns: on failure the worker must
	// become reapable again rather than pinning a slot no one will ever use.
	defer os.Remove(filepath.Join(s.Dir, cs+".reserving"))
	if err := worker.EnsureReplReady(s.Dir, s.Name, s.Cwd, cs, &errb); err != nil {
		return "", err.Error()
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	tid, err := worker.EnqueueToWorker(s.Dir, cs, worker.CurrentTier(s.Dir, cs), []byte(body), false)
	if err != nil {
		return "", err.Error()
	}
	return tid, ""
}

// dispatchAuto sends one task through the auto policy — used by the review
// stage, which is a single dispatch with no parallelism to preserve.
func (s *Session) dispatchAuto(body string, timeout int) (string, string) {
	cs, rerr := s.reserveWorker(nil)
	if rerr != "" {
		return "", rerr
	}
	// A review gate MUST run on a fresh worker. Falling back to the shared
	// pool would let any idle worker steal the task — possibly one that just
	// produced the work being reviewed, which makes the verdict worthless.
	// Fail loudly instead of silently reviewing with a biased judge.
	if cs == "" {
		return "", fmt.Sprintf("review gate needs a fresh worker but the pool is at the cap (%s); reclaim one with `tt pi popidle` or `tt pi rm <cs>`", strconv.Itoa(worker.PiCap()))
	}
	return s.dispatchTo(cs, body)
}

// replDownConfirmations is how many CONSECUTIVE liveness misses must stack up
// before a join gives up on a worker. `ReplRunning` is a `pgrep` sample and is
// racy across a REPL restart, so one miss proves nothing.
const replDownConfirmations = 5

// awaitTerminal blocks until a task id reaches a terminal status
// (done/blocked/other/error), the worker dies, or the timeout lapses.
func (s *Session) awaitTerminal(id string, timeout int) (stageResult, string) {
	deadline := deadlineIn(timeout)
	cs := id
	if i := strings.Index(id, "-"); i >= 0 {
		cs = id[:i]
	}
	downPolls := 0
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
		// worker can be checked for a dead REPL. A single miss is not enough:
		// one false negative used to end the join with a fake terminal "down",
		// after which the stage advanced and handed its review gate work that
		// was still running.
		if strings.HasPrefix(id, "pool-") || worker.ReplRunning(s.Dir, cs) {
			downPolls = 0
		} else if downPolls++; downPolls >= replDownConfirmations {
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
