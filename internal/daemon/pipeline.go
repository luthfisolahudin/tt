package daemon

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/worker"
)

// A pipeline is a DECLARATIVE spec (data, not code) executed by the daemon:
// an ordered list of stages. One trigger in, one digest out. See docs/PLAN.md
// Phase 3. There is no scripting — a stage is a fixed shape:
//
//	{"fanout": [{"task": "..."}, ...], "join": "digest"|"full"}
//	{"review": {"prompt": "...", "introduction": "..."}, "against": "previous"}
//
// A fanout stage dispatches N tasks (the auto policy: idle -> spawn -> pool)
// and joins them. A review stage dispatches ONE worker to verify the previous
// stage's results and returns pass/fail; on fail the pipeline re-runs the
// fanout stage, bounded by the spec's `retries` (default 0).
type PipelineSpec struct {
	Name    string          `json:"name"`
	Retries int             `json:"retries"`
	Stages  []PipelineStage `json:"stages"`
}

// PipelineStage is one stage; exactly one of Fanout/Review is set.
type PipelineStage struct {
	Fanout []PipelineTask `json:"fanout,omitempty"`
	Review *ReviewSpec    `json:"review,omitempty"`
	Join   string         `json:"join,omitempty"` // fanout only: "digest" (default) | "full"
}

// PipelineTask is one fan-out dispatch: a prompt body, plus an optional label
// used in the digest.
type PipelineTask struct {
	Label string `json:"label"`
	Task  string `json:"task"`
}

// ReviewSpec is the review-gate prompt. The worker is handed the previous
// stage's results and must end with a terminal verdict line:
// "PIPELINE_PASS" or "PIPELINE_FAIL: <reason>".
type ReviewSpec struct {
	Prompt string `json:"prompt"`
}

// PipelineArgs is tt_pipeline_run_cmd: run a declarative spec end-to-end.
type PipelineArgs struct {
	SpecB64 string `json:"spec_b64"` // base64 of the PipelineSpec JSON
	JSON    bool   `json:"json"`
	Timeout int    `json:"timeout"` // per-stage join bound (seconds); 0 = wait forever
}

// stageResult is one finished task in a stage (the unit the review gate and
// the final digest read).
type stageResult struct {
	ID      string
	Status  string
	Dur     string
	Summary string
	Text    string // full body (for the review prompt; not in the digest)
}

func pipelineOp(s *Session, a PipelineArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	note := func(msg string) { fmt.Fprintf(&errb, "[tt] %s\n", msg) }
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	spec, err := parsePipelineSpec(a.SpecB64)
	if err != nil {
		return die("pipeline: " + err.Error())
	}
	name := spec.Name
	if name == "" {
		name = "pipeline"
	}

	// Execute stages in order. A review gate reads the previous stage's
	// results; on PIPELINE_FAIL it re-runs the immediately-preceding fanout
	// stage, bounded by spec.Retries.
	var prev []stageResult
	attempts := 0
	for i := 0; i < len(spec.Stages); i++ {
		st := spec.Stages[i]
		switch {
		case st.Fanout != nil:
			res, rerr := s.runFanoutStage(st, a.Timeout, note)
			if rerr != "" {
				return die(rerr)
			}
			prev = res
		case st.Review != nil:
			if len(prev) == 0 {
				return die("pipeline: review stage has no previous results to review")
			}
			pass, reason, rerr := s.runReviewStage(st.Review, prev, a.Timeout, note)
			if rerr != "" {
				return die(rerr)
			}
			if pass {
				note(fmt.Sprintf("pipeline %s: review gate PASS", name))
				continue
			}
			note(fmt.Sprintf("pipeline %s: review gate FAIL — %s", name, reason))
			if attempts >= spec.Retries {
				return die(fmt.Sprintf("pipeline %s: review gate failed and retries exhausted (%d): %s", name, spec.Retries, reason))
			}
			attempts++
			// Step back to the preceding fanout stage and re-run it.
			j := i - 1
			for j >= 0 && spec.Stages[j].Fanout == nil {
				j--
			}
			if j < 0 {
				return die("pipeline: review gate failed but no prior fanout stage to retry")
			}
			note(fmt.Sprintf("pipeline %s: retrying fanout (attempt %d/%d)", name, attempts, spec.Retries))
			i = j - 1 // loop increments to j
		default:
			return die(fmt.Sprintf("pipeline: stage %d is neither fanout nor review", i))
		}
	}

	// One digest out. Default digest; join:"full" inlines bodies for a stage
	// that opted in (kept small — the point is to NOT flood context by default).
	if a.JSON {
		out.WriteString("[")
		for i, r := range prev {
			if i > 0 {
				out.WriteString(",")
			}
			out.WriteString(worker.EmitResultJSON(s.Dir, r.ID, r.Status, r.Text))
		}
		out.WriteString("]\n")
		return ok(&out, &errb, 0)
	}
	full := false
	if len(spec.Stages) > 0 {
		last := spec.Stages[len(spec.Stages)-1]
		full = last.Join == "full"
	}
	if full {
		for _, r := range prev {
			fmt.Fprintf(&out, "== %s (%s) ==\n%s\n\n", r.ID, r.Status, strings.TrimRight(r.Text, "\n"))
		}
	} else {
		for _, r := range prev {
			fmt.Fprintf(&out, "%s\n", digestLine(s.Dir, r.ID, r.Status, r.Text))
		}
	}
	return ok(&out, &errb, 0)
}

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
func parsePipelineSpec(b64 string) (PipelineSpec, error) {
	var spec PipelineSpec
	data, err := decodePrompt(b64)
	if err != nil {
		return spec, fmt.Errorf("bad spec encoding")
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		return spec, fmt.Errorf("bad spec JSON: %v", err)
	}
	if len(spec.Stages) == 0 {
		return spec, fmt.Errorf("spec has no stages")
	}
	fanouts := 0
	for i, st := range spec.Stages {
		if st.Fanout == nil && st.Review == nil {
			return spec, fmt.Errorf("stage %d is neither fanout nor review", i)
		}
		if st.Fanout != nil && st.Review != nil {
			return spec, fmt.Errorf("stage %d sets both fanout and review", i)
		}
		if st.Fanout != nil {
			if len(st.Fanout) == 0 {
				return spec, fmt.Errorf("stage %d fanout is empty", i)
			}
			for j, t := range st.Fanout {
				if strings.TrimSpace(t.Task) == "" {
					return spec, fmt.Errorf("stage %d fanout task %d has an empty task", i, j)
				}
			}
			fanouts++
		}
		if st.Review != nil && strings.TrimSpace(st.Review.Prompt) == "" {
			return spec, fmt.Errorf("stage %d review has an empty prompt", i)
		}
	}
	if fanouts == 0 {
		return spec, fmt.Errorf("spec has no fanout stage (nothing to run)")
	}
	if spec.Retries < 0 {
		spec.Retries = 0
	}
	return spec, nil
}
