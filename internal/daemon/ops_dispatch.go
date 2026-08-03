package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/worker"
)

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
