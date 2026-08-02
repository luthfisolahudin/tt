package worker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/luthfisolahudin/tt/internal/tmux"
)

// CurrentGen returns the worker's context generation (0 when absent).
func CurrentGen(sdir, name string) int {
	f := filepath.Join(sdir, name+".gen")
	data, err := os.ReadFile(f)
	if err != nil {
		return 0
	}
	g, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return g
}

// BumpGen increments the worker's generation file.
func BumpGen(sdir, name string) {
	g := CurrentGen(sdir, name) + 1
	os.WriteFile(filepath.Join(sdir, name+".gen"), []byte(strconv.Itoa(g)), 0644)
}

// SessionDir is the pi --session-dir for the worker's current generation.
func SessionDir(sdir, name string) string {
	return filepath.Join(sdir, "pi-sessions", name, fmt.Sprintf("g%d", CurrentGen(sdir, name)))
}

// ReplRunning reports whether the worker's pi REPL process is alive, matched
// by its unique --session-dir path (trailing slash, as bash's pgrep does).
// pane_current_command is unreliable: pi runs as a grandchild.
func ReplRunning(sdir, name string) bool {
	pattern := filepath.Join(sdir, "pi-sessions", name) + "/"
	cmd := exec.Command("pgrep", "-f", pattern)
	return cmd.Run() == nil
}

// ReplStarting reports whether the REPL is within its 45s boot window
// (a hair past wait_repl_ready's 40s deadline).
func ReplStarting(sdir, name string) bool {
	data, err := os.ReadFile(filepath.Join(sdir, name+".starting"))
	if err != nil {
		return false
	}
	t, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	return time.Now().Unix()-t < 45
}

// SplitHead splits a result head "<id> <status>" into its parts; a head with
// no space yields (head, "").
func SplitHead(head string) (id, status string) {
	if sp := strings.Index(head, " "); sp >= 0 {
		return head[:sp], head[sp+1:]
	}
	return head, ""
}

// ResultHead reads the worker's latest-pointer (<cs>.result) — "<id> <status>".
func ResultHead(sdir, name string) string {
	head, _ := ResultHeadFile(filepath.Join(sdir, name+".result"))
	return head
}

// WorkerState derives the worker's state from its window + control files:
// missing/starting/down/busy/blocked/interrupted/idle. Port of the bash
// worker_state(); the busy marker is the signal, not the result file.
func WorkerState(sdir, session, name string) string {
	if !tmux.WindowExists(session, "pi-"+name) {
		return "missing"
	}
	if !ReplRunning(sdir, name) {
		if ReplStarting(sdir, name) {
			return "starting"
		}
		return "down"
	}
	if !FileExists(filepath.Join(sdir, name+".ready")) {
		return "starting"
	}
	if FileExists(filepath.Join(sdir, name+".busy")) {
		return "busy"
	}
	tid, ok := LastTaskID(sdir, name)
	if !ok {
		return "idle"
	}
	head := ResultHead(sdir, name)
	if head == "" {
		return "idle"
	}
	rid, rst := SplitHead(head)
	if tid != rid {
		return "idle"
	}
	switch rst {
	case "blocked":
		return "blocked"
	case "other", "error":
		return "interrupted"
	default:
		return "idle"
	}
}

// EnsureIdleOrBlocked reports whether the worker is clearable/removable
// (not actively running a task).
func EnsureIdleOrBlocked(sdir, session, name string) bool {
	return WorkerState(sdir, session, name) != "busy"
}

// LastTaskRow returns the last line of the worker's tasks.jsonl.
func LastTaskRow(sdir, name string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(sdir, name+".tasks.jsonl"))
	if err != nil || len(data) == 0 {
		return "", false
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	return lines[len(lines)-1], true
}

// LastTaskID returns the worker's latest dispatched task id (false when none).
func LastTaskID(sdir, name string) (string, bool) {
	row, ok := LastTaskRow(sdir, name)
	if !ok {
		return "", false
	}
	idx := strings.Index(row, `"id":"`)
	if idx == -1 {
		return "", false
	}
	rest := row[idx+6:]
	end := strings.Index(rest, `"`)
	if end == -1 {
		return "", false
	}
	return rest[:end], true
}

// CurrentTier returns the worker's tier name; missing or removed values
// resolve to the default (for a fresh/restarted REPL).
func CurrentTier(sdir, name string) string {
	data, err := os.ReadFile(filepath.Join(sdir, name+".tier"))
	if err != nil {
		return TierDefault
	}
	t := strings.TrimSpace(string(data))
	if !IsKnownTier(t) {
		return TierDefault
	}
	return t
}

// StoredTier returns the raw <cs>.tier content ("" when absent).
func StoredTier(sdir, name string) string {
	data, err := os.ReadFile(filepath.Join(sdir, name+".tier"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// WorkerHasStaleTier reports a persisted tier removed from the registry.
func WorkerHasStaleTier(sdir, name string) bool {
	t := StoredTier(sdir, name)
	return t != "" && !IsKnownTier(t)
}

// TierLabel renders the worker's tier for display (stale:<name> when removed).
func TierLabel(sdir, name string) string {
	t := StoredTier(sdir, name)
	if t != "" && !IsKnownTier(t) {
		return "stale:" + t
	}
	if t == "" {
		return TierDefault
	}
	return t
}

// QueueDepth counts queued (<turn>.task) files in a worker's pinned queue dir.
func QueueDepth(sdir, name string) int {
	entries, err := os.ReadDir(filepath.Join(sdir, name+".queue"))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".task") {
			n++
		}
	}
	return n
}

// SecsSinceFile returns whole seconds since a file's mtime (false when
// absent) — times an in-flight turn from the <cs>.busy marker.
func SecsSinceFile(path string) (int, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return int(time.Since(info.ModTime()).Seconds()), true
}

// FmtElapsed formats a second count as M:SS.
func FmtElapsed(s int) string {
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// IsTaskID matches <callsign>-<turn> or pool-<seq> — a bare callsign does not.
func IsTaskID(s string) bool {
	i := strings.LastIndex(s, "-")
	if i <= 0 || i == len(s)-1 {
		return false
	}
	for _, r := range s[:i] {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	for _, r := range s[i+1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ValidTimeoutArg reports a non-negative integer timeout.
func ValidTimeoutArg(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// FileExists reports whether path exists (os.Stat follows symlinks).
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
