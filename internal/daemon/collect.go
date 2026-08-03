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

// CollectArgs is pi_collect_cmd: cursor-based fan-out join.
type CollectArgs struct {
	JSON    bool   `json:"json"`
	Timeout int    `json:"timeout"`
	Target  string `json:"target"`
	Digest  bool   `json:"digest"`
}

// ResultsArgs is pi_results_cmd: read durable outcomes from the per-id store.
type ResultsArgs struct {
	JSON   bool   `json:"json"`
	Target string `json:"target"`
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
