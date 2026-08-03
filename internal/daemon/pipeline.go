package daemon

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/worker"
)

// A pipeline is a DECLARATIVE spec (data, not code) executed by the daemon:
// an ordered list of stages. One trigger in, one digest out. See
// docs/DESIGN.md "Declarative pipelines" and docs/pipeline.schema.json. There
// is no scripting — a stage is a fixed shape:
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
