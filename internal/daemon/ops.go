package daemon

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/worker"
)

// SendArgs mirrors the bash pi_send_cmd dispatch: lazy spawn under the cap,
// tier guard, enqueue to the worker's own queue; prints the task id.
type SendArgs struct {
	Callsign  string `json:"callsign"`
	Tier      string `json:"tier"`
	Notify    bool   `json:"notify"`
	PromptB64 string `json:"prompt_b64"`
}

// AutoArgs mirrors pi_auto_cmd: reuse idle -> spawn under cap -> shared pool.
type AutoArgs struct {
	Tier        string `json:"tier"`
	RM          bool   `json:"rm"`
	Notify      bool   `json:"notify"`
	JSON        bool   `json:"json"`
	PreferFresh bool   `json:"prefer_fresh"`
	PromptB64   string `json:"prompt_b64"`
}

// SteerArgs is pi_steer_cmd: run-now injection, bypassing the queue.
type SteerArgs struct {
	Callsign   string `json:"callsign"`
	MessageB64 string `json:"message_b64"`
}

// ResumeArgs is pi_resume_cmd: re-drive the interrupted task on the live REPL.
type ResumeArgs struct {
	Callsign string `json:"callsign"`
}

// ClearArgs is pi_clear_cmd: bump gen, respawn on a fresh session-dir.
type ClearArgs struct {
	Callsign string `json:"callsign"`
	Force    bool   `json:"force"`
}

// RmArgs is pi_rm_cmd: kill the window and wipe the worker's state.
type RmArgs struct {
	Callsign string `json:"callsign"`
	Force    bool   `json:"force"`
}

// PopidleArgs is pi_popidle_cmd (no args).
type PopidleArgs struct{}

func dispatchOp(s *Session, req client.Request) client.Response {
	switch req.Op {
	case "send":
		var a SendArgs
		decodeArgs(req.Args, &a)
		return sendOp(s, a)
	case "auto":
		var a AutoArgs
		decodeArgs(req.Args, &a)
		return autoOp(s, a)
	case "steer":
		var a SteerArgs
		decodeArgs(req.Args, &a)
		if a.Callsign == "all" {
			return steerAllOp(s, a)
		}
		return steerOp(s, a)
	case "resume":
		var a ResumeArgs
		decodeArgs(req.Args, &a)
		return resumeOp(s, a)
	case "clear":
		var a ClearArgs
		decodeArgs(req.Args, &a)
		return clearOp(s, a)
	case "rm":
		var a RmArgs
		decodeArgs(req.Args, &a)
		return rmOp(s, a)
	case "popidle":
		return popidleOp(s)
	case "wait":
		var a WaitArgs
		decodeArgs(req.Args, &a)
		return waitOp(s, a)
	case "status":
		var a StatusArgs
		decodeArgs(req.Args, &a)
		return statusOp(s, a)
	case "collect":
		var a CollectArgs
		decodeArgs(req.Args, &a)
		return collectOp(s, a)
	case "results":
		var a ResultsArgs
		decodeArgs(req.Args, &a)
		return resultsOp(s, a)
	case "peek":
		var a PeekArgs
		decodeArgs(req.Args, &a)
		return peekOp(s, a)
	case "pipeline":
		var a PipelineArgs
		decodeArgs(req.Args, &a)
		return pipelineOp(s, a)
	}
	return client.Response{OK: false, Error: "unknown op: " + req.Op}
}

// decodeArgs re-marshals the request's decoded args into a typed struct.
func decodeArgs(raw any, into any) {
	if raw == nil {
		return
	}
	if data, err := json.Marshal(raw); err == nil {
		json.Unmarshal(data, into)
	}
}

func ok(out, errb *strings.Builder, code int) client.Response {
	return client.Response{OK: true, Stdout: out.String(), Stderr: errb.String(), ExitCode: code}
}

func decodePrompt(b64 string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("bad prompt encoding")
	}
	return data, nil
}

func sendOp(s *Session, a SendArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	if a.Tier != "" && !worker.IsKnownTier(a.Tier) {
		return die(fmt.Sprintf("unknown --tier '%s' (valid: %s)", a.Tier, strings.Join(worker.TierNames(), " ")))
	}
	if !worker.ValidCallsign(a.Callsign) {
		return die("invalid callsign: " + a.Callsign)
	}
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	if tmux.WindowExists(s.Name, "pi-"+a.Callsign) && worker.WorkerHasStaleTier(s.Dir, a.Callsign) {
		return die(fmt.Sprintf("pi-%s uses removed tier '%s'; run `tt pi clear %s` before dispatching new work", a.Callsign, worker.StoredTier(s.Dir, a.Callsign), a.Callsign))
	}
	if a.Tier != "" && tmux.WindowExists(s.Name, "pi-"+a.Callsign) {
		cur := worker.CurrentTier(s.Dir, a.Callsign)
		if a.Tier != cur {
			return die(fmt.Sprintf("tier change on running worker requires `tt pi clear %s` (respawns the REPL; loses context); current tier '%s', requested '%s'", a.Callsign, cur, a.Tier))
		}
	}
	if a.Tier != "" {
		os.WriteFile(filepath.Join(s.Dir, a.Callsign+".tier"), []byte(a.Tier), 0644)
	}
	if !tmux.WindowExists(s.Name, "pi-"+a.Callsign) {
		cap := worker.PiCap()
		if worker.CountWorkers(s.Name) >= cap {
			return die(fmt.Sprintf("pi worker cap of %d reached (cores-2, max 26)", cap))
		}
		applySyncEnv(s)
		if err := worker.SpawnPiWindow(s.Dir, s.Name, s.Cwd, a.Callsign, s.SyncEnv, &errb); err != nil {
			return die(err.Error())
		}
	}
	tier := a.Tier
	if tier == "" {
		tier = worker.CurrentTier(s.Dir, a.Callsign)
	}
	if err := worker.EnsureReplReady(s.Dir, s.Name, s.Cwd, a.Callsign, &errb); err != nil {
		return die(err.Error())
	}
	if worker.WorkerState(s.Dir, s.Name, a.Callsign) == "interrupted" {
		return die(fmt.Sprintf("pi-%s has an interrupted task; run `tt pi clear %s` first", a.Callsign, a.Callsign))
	}
	prompt, err := decodePrompt(a.PromptB64)
	if err != nil {
		return die(err.Error())
	}
	tid, err := worker.EnqueueToWorker(s.Dir, a.Callsign, tier, prompt, a.Notify)
	if err != nil {
		return die(err.Error())
	}
	fmt.Fprintf(&out, "%s\n", tid)
	return ok(&out, &errb, 0)
}

func emitAutoJSON(b *strings.Builder, w, tid, routed string) {
	fmt.Fprintf(b, `{"worker":"%s","task_id":"%s","routed":"%s"}`+"\n", worker.JSONEscape(w), worker.JSONEscape(tid), routed)
}

func autoOp(s *Session, a AutoArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	note := func(msg string) { fmt.Fprintf(&errb, "[tt] %s\n", msg) }
	if a.Tier != "" && !worker.IsKnownTier(a.Tier) {
		return die(fmt.Sprintf("unknown --tier '%s' (valid: %s)", a.Tier, strings.Join(worker.TierNames(), " ")))
	}
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	worker.ReapEphemeralWorkers(s.Dir, s.Name)

	prompt, err := decodePrompt(a.PromptB64)
	if err != nil {
		return die(err.Error())
	}

	// --rm: a fresh ephemeral worker, never a reuse.
	if a.RM {
		cap := worker.PiCap()
		if worker.CountWorkers(s.Name) >= cap {
			return die(fmt.Sprintf("auto --rm: worker cap of %d reached; free one (tt pi rm) or wait", cap))
		}
		chosen := ""
		for _, n := range worker.NATO {
			if !tmux.WindowExists(s.Name, "pi-"+n) {
				chosen = n
				break
			}
		}
		if chosen == "" {
			return die("auto --rm: no free callsign available")
		}
		os.WriteFile(filepath.Join(s.Dir, chosen+".ephemeral"), nil, 0644)
		wtier := a.Tier
		if wtier == "" {
			wtier = worker.TierDefault
		}
		os.WriteFile(filepath.Join(s.Dir, chosen+".tier"), []byte(wtier), 0644)
		applySyncEnv(s)
		if err := worker.SpawnPiWindow(s.Dir, s.Name, s.Cwd, chosen, s.SyncEnv, &errb); err != nil {
			return die(err.Error())
		}
		if err := worker.EnsureReplReady(s.Dir, s.Name, s.Cwd, chosen, &errb); err != nil {
			return die(err.Error())
		}
		tid, err := worker.EnqueueToWorker(s.Dir, chosen, wtier, prompt, a.Notify)
		if err != nil {
			return die(err.Error())
		}
		if a.JSON {
			emitAutoJSON(&out, chosen, tid, "ephemeral")
		} else {
			note("using pi-" + chosen + " (ephemeral)")
			fmt.Fprintf(&out, "%s\n", tid)
		}
		return ok(&out, &errb, 0)
	}

	// --prefer-fresh: spawn a NEW worker before reusing an idle one (under cap).
	chosen, routed := "", ""
	if a.PreferFresh && worker.CountWorkers(s.Name) < worker.PiCap() {
		for _, n := range worker.NATO {
			if !tmux.WindowExists(s.Name, "pi-"+n) {
				chosen = n
				wtier := a.Tier
				if wtier == "" {
					wtier = worker.TierDefault
				}
				os.WriteFile(filepath.Join(s.Dir, n+".tier"), []byte(wtier), 0644)
				applySyncEnv(s)
				if err := worker.SpawnPiWindow(s.Dir, s.Name, s.Cwd, n, s.SyncEnv, &errb); err != nil {
					return die(err.Error())
				}
				routed = "spawn"
				break
			}
		}
	}
	// Reuse the first idle worker (NATO order), skipping tier mismatches.
	if chosen == "" {
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
			if a.Tier != "" && worker.CurrentTier(s.Dir, n) != a.Tier {
				continue
			}
			chosen = n
			routed = "idle"
			break
		}
	}
	// None idle but room under the cap -> spawn the next free callsign.
	if chosen == "" && worker.CountWorkers(s.Name) < worker.PiCap() {
		for _, n := range worker.NATO {
			if !tmux.WindowExists(s.Name, "pi-"+n) {
				chosen = n
				wtier := a.Tier
				if wtier == "" {
					wtier = worker.TierDefault
				}
				os.WriteFile(filepath.Join(s.Dir, n+".tier"), []byte(wtier), 0644)
				applySyncEnv(s)
				if err := worker.SpawnPiWindow(s.Dir, s.Name, s.Cwd, n, s.SyncEnv, &errb); err != nil {
					return die(err.Error())
				}
				routed = "spawn"
				break
			}
		}
	}
	if chosen != "" {
		tier := worker.CurrentTier(s.Dir, chosen)
		if err := worker.EnsureReplReady(s.Dir, s.Name, s.Cwd, chosen, &errb); err != nil {
			return die(err.Error())
		}
		tid, err := worker.EnqueueToWorker(s.Dir, chosen, tier, prompt, a.Notify)
		if err != nil {
			return die(err.Error())
		}
		if a.JSON {
			emitAutoJSON(&out, chosen, tid, routed)
		} else {
			note("using pi-" + chosen)
			fmt.Fprintf(&out, "%s\n", tid)
		}
		return ok(&out, &errb, 0)
	}
	// No compatible worker free; --tier refuses (a pool task could be stolen
	// by a worker on another tier).
	if a.Tier != "" {
		return die(fmt.Sprintf("auto --tier %s: no compatible worker (all on different tiers or busy at cap); free a worker (`tt pi rm <cs>`) or use `tt pi send <cs> --tier %s` to force a fresh worker", a.Tier, a.Tier))
	}
	// Refuse pool work while a worker still runs a removed tier.
	for _, n := range worker.NATO {
		if !tmux.WindowExists(s.Name, "pi-"+n) {
			continue
		}
		if worker.WorkerHasStaleTier(s.Dir, n) {
			return die(fmt.Sprintf("auto: pi-%s uses removed tier '%s'; run `tt pi clear %s` before queueing shared-pool work", n, worker.StoredTier(s.Dir, n), n))
		}
	}
	// Everyone exists and is busy at the cap -> shared pool queue.
	tier := a.Tier
	if tier == "" {
		tier = worker.TierDefault
	}
	tid, err := worker.EnqueuePool(s.Dir, tier, prompt, a.Notify)
	if err != nil {
		return die(err.Error())
	}
	if a.JSON {
		emitAutoJSON(&out, "", tid, "pool")
	} else {
		note(fmt.Sprintf("all workers busy at cap (%d); queued %s — the next idle worker claims it", worker.PiCap(), tid))
		fmt.Fprintf(&out, "%s\n", tid)
	}
	return ok(&out, &errb, 0)
}

func steerOp(s *Session, a SteerArgs) client.Response {
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
	if err := worker.EnsureReplReady(s.Dir, s.Name, s.Cwd, a.Callsign, &errb); err != nil {
		return die(err.Error())
	}
	msg, err := decodePrompt(a.MessageB64)
	if err != nil {
		return die(err.Error())
	}
	if len(msg) == 0 {
		return die("steer: empty message")
	}
	if err := worker.WriteSteerFile(s.Dir, a.Callsign, string(msg)); err != nil {
		return die(err.Error())
	}
	return ok(&out, &errb, 0)
}

func steerAllOp(s *Session, a SteerArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	note := func(msg string) { fmt.Fprintf(&errb, "[tt] %s\n", msg) }
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	msg, err := decodePrompt(a.MessageB64)
	if err != nil {
		return die(err.Error())
	}
	if len(msg) == 0 {
		return die("steer-all: empty message")
	}
	sent := 0
	for _, n := range worker.NATO {
		if !tmux.WindowExists(s.Name, "pi-"+n) {
			continue
		}
		if !worker.ReplRunning(s.Dir, n) {
			continue
		}
		if err := worker.WriteSteerFile(s.Dir, n, string(msg)); err != nil {
			return die(err.Error())
		}
		fmt.Fprintf(&out, "%s\n", n)
		sent++
	}
	if sent == 0 {
		note("steer-all: no live workers")
	}
	return ok(&out, &errb, 0)
}

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
