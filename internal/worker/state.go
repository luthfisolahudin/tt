package worker

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/luthfisolahudin/tt/internal/session"
	"github.com/luthfisolahudin/tt/internal/tmux"
)

func CurrentGen(name string) int {
	sdir, _ := session.StateDir()
	f := filepath.Join(sdir, name+".gen")
	data, err := os.ReadFile(f)
	if err != nil {
		return 0
	}
	g, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return g
}

func BumpGen(name string) {
	sdir, _ := session.StateDir()
	g := CurrentGen(name) + 1
	f := filepath.Join(sdir, name+".gen")
	os.WriteFile(f, []byte(strconv.Itoa(g)), 0644)
}

func SessionDir(name string) string {
	sdir, _ := session.StateDir()
	g := CurrentGen(name)
	return filepath.Join(sdir, "pi-sessions", name, fmt.Sprintf("g%d", g))
}

func ReplRunning(name string) bool {
	sdir, _ := session.StateDir()
	pattern := filepath.Join(sdir, "pi-sessions", name)
	cmd := exec.Command("pgrep", "-f", pattern)
	return cmd.Run() == nil
}

func ReplStarting(name string) bool {
	sdir, _ := session.StateDir()
	f := filepath.Join(sdir, name+".starting")
	data, err := os.ReadFile(f)
	if err != nil {
		return false
	}
	t, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	return time.Now().Unix()-t < 45
}

func WorkerState(name string) string {
	sname := session.SessionName()
	if !tmux.WindowExists(sname, "pi-"+name) {
		return "missing"
	}
	if !ReplRunning(name) {
		if ReplStarting(name) {
			return "starting"
		}
		return "down"
	}
	sdir, _ := session.StateDir()
	if _, err := os.Stat(filepath.Join(sdir, name+".ready")); os.IsNotExist(err) {
		return "starting"
	}
	if _, err := os.Stat(filepath.Join(sdir, name+".busy")); err == nil {
		return "busy"
	}
	tid := LastTaskID(name)
	if tid == "" {
		return "idle"
	}
	head := ResultHead(name)
	if head == "" {
		return "idle"
	}
	parts := strings.SplitN(head, " ", 2)
	if len(parts) < 2 {
		return "idle"
	}
	rid, rst := parts[0], parts[1]
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

func CurrentTier(name string) string {
	sdir, _ := session.StateDir()
	f := filepath.Join(sdir, name+".tier")
	data, err := os.ReadFile(f)
	if err != nil {
		return TierDefault
	}
	t := strings.TrimSpace(string(data))
	if !IsKnownTier(t) {
		return TierDefault
	}
	return t
}

func StoredTier(name string) string {
	sdir, _ := session.StateDir()
	f := filepath.Join(sdir, name+".tier")
	data, err := os.ReadFile(f)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func WorkerHasStaleTier(name string) bool {
	t := StoredTier(name)
	return t != "" && !IsKnownTier(t)
}

func TierLabel(name string) string {
	t := StoredTier(name)
	if t != "" && !IsKnownTier(t) {
		return "stale:" + t
	}
	if t == "" {
		return TierDefault
	}
	return t
}

func LastTaskID(name string) string {
	sdir, _ := session.StateDir()
	f := filepath.Join(sdir, name+".tasks.jsonl")
	file, err := os.Open(f)
	if err != nil {
		return ""
	}
	defer file.Close()
	var last string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		last = scanner.Text()
	}
	if last == "" {
		return ""
	}
	idx := strings.Index(last, `"id":"`)
	if idx == -1 {
		return ""
	}
	rest := last[idx+6:]
	end := strings.Index(rest, `"`)
	if end == -1 {
		return ""
	}
	return rest[:end]
}

func EnsureIdleOrBlocked(name string) error {
	st := WorkerState(name)
	if st == "busy" {
		return fmt.Errorf("pi-%s is busy", name)
	}
	return nil
}
