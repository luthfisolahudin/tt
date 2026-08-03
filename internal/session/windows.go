package session

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultClaudeCmd is the default orchestrator launch, used when
// .tt/windows.json does not override the claude window's command.
const DefaultClaudeCmd = "claude --continue --allow-dangerously-skip-permissions || claude --allow-dangerously-skip-permissions"

// normalizeJQ is the byte-faithful port of the bash NORMALIZE_JQ: read an
// optional <project>/.tt/windows.json and emit a normalized, defaults-applied
// window array [{role,name,layout,panes:[{cmd,enter}]}]. Absent/empty input
// ({}) yields the built-in default (dev + claude) — one code path serves both.
// $cc carries DefaultClaudeCmd. jq must produce this exactly.
const normalizeJQ = `def norm: {cmd: (.cmd // ""), enter: (.enter != false)};
[ ( (.dev    // {}) | {role:"dev",    name:(.name // "dev"),    layout:(.layout // null), panes:((.panes // [{}])       | map(norm))} ),
  ( (.claude // {}) | {role:"claude", name:(.name // "claude"), layout:(.layout // null), panes:((.panes // [{cmd:$cc}]) | map(norm))} ) ]
+ ((.extra_windows // []) | map({role:"extra", name:.name, layout:(.layout // null), panes:((.panes // [{}]) | map(norm))}))`

// Pane is one configured pane of a fixed window.
type Pane struct {
	Cmd   string `json:"cmd"`
	Enter bool   `json:"enter"`
}

// Window is one normalized fixed window (dev/claude/extra).
type Window struct {
	Role   string  `json:"role"`
	Name   string  `json:"name"`
	Layout *string `json:"layout"` // null (jq `.layout // null`) unless set
	Panes  []Pane  `json:"panes"`
}

func haveJQ() bool {
	_, err := exec.LookPath("jq")
	return err == nil
}

// FileExists reports whether path exists (os.Stat follows symlinks).
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// timeSleep is a thin wrapper so the 0.5s down-sleep reads like bash's `sleep 0.5`.
func timeSleep(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// windowsConfig emits the normalized window array for cwd. ok=false when jq
// is unavailable — the caller falls back to the legacy dev+claude layout
// (the same code path the bash ensure_standard_windows uses). A malformed
// config file falls back to the built-in default with a note.
func windowsConfig(cwd string, note func(string)) ([]Window, bool) {
	if !haveJQ() {
		return nil, false
	}
	file := filepath.Join(cwd, ".tt", "windows.json")
	if FileExists(file) {
		data, err := os.ReadFile(file)
		if err == nil {
			if out, jerr := runNormalizeJQ(string(data)); jerr == nil {
				if wins, perr := parseWindows(out); perr == nil {
					return wins, true
				}
			}
		}
		note("ignoring malformed " + file + "; using built-in window layout")
	}
	out, err := runNormalizeJQ("{}")
	if err != nil {
		return nil, false
	}
	wins, perr := parseWindows(out)
	return wins, perr == nil
}

func runNormalizeJQ(input string) (string, error) {
	cmd := exec.Command("jq", "-c", "--arg", "cc", DefaultClaudeCmd, normalizeJQ)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseWindows(j string) ([]Window, error) {
	var wins []Window
	err := json.Unmarshal([]byte(j), &wins)
	return wins, err
}

func firstWindowName(wins []Window, ok bool) string {
	if ok && len(wins) > 0 {
		return wins[0].Name
	}
	return "dev"
}

func orchestratorWindowName(wins []Window, ok bool) string {
	if ok {
		for _, w := range wins {
			if w.Role == "claude" {
				return w.Name
			}
		}
		return "claude"
	}
	return "claude"
}

func configuredWindowNames(wins []Window, ok bool) []string {
	if !ok {
		return []string{"dev", "claude"}
	}
	names := make([]string, len(wins))
	for i, w := range wins {
		names[i] = w.Name
	}
	return names
}

// SyncEnvMap copies TT_PI_ENV_VARS entries from the process env (the values
