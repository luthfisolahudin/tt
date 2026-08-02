package session

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/version"
	"github.com/luthfisolahudin/tt/internal/worker"
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
// bash's sync_pi_env would set); an invalid entry name is an error, like bash.
func SyncEnvMap() (map[string]string, error) {
	env := map[string]string{}
	re := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	for _, key := range strings.Fields(os.Getenv("TT_PI_ENV_VARS")) {
		if !re.MatchString(key) {
			return nil, fmt.Errorf("invalid TT_PI_ENV_VARS entry: %s", key)
		}
		if v, ok := os.LookupEnv(key); ok {
			env[key] = v
		}
	}
	return env, nil
}

// SyncPiEnv copies TT_PI_ENV_VARS entries into the tmux session env (bash's
// sync_pi_env).
func SyncPiEnv(sess string) error {
	env, err := SyncEnvMap()
	if err != nil {
		return err
	}
	for k, v := range env {
		if e := tmux.SetEnvironment(sess, k, v); e != nil {
			return e
		}
	}
	return nil
}

// setSessionVersion stamps TT_VERSION + sync_pi_env into the session env and
// writes the version file (bash's set_session_version).
func setSessionVersion(sess, stateDir string) error {
	if err := tmux.SetEnvironment(sess, "TT_VERSION", version.Version); err != nil {
		return err
	}
	if err := SyncPiEnv(sess); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, "version"), []byte(version.Version+"\n"), 0644)
}

// paneIsBare mirrors the bash guard: is the pane (by pane_id) sitting at a
// bare shell? pane_current_command is reliable here because we target a
// specific pane, not a grandchild-hosting pi window.
func paneIsBare(paneTarget string) bool {
	cmd, err := tmux.PaneCurrentCommand(paneTarget)
	if err != nil {
		return false
	}
	switch cmd {
	case "bash", "zsh", "sh", "fish":
		return true
	}
	return false
}

// applyWindowPanes splits a freshly-created window to its configured pane
// count and (re)sends each pane's command, bare-shell-guarded. Heals at
// WINDOW granularity: a window with >1 pane is never re-split, but any
// bare-shell pane still gets its command re-sent. coldStart marks a brand-new
// session (bash's _TT_COLD_START): only then do enter:false prefills fire.
func applyWindowPanes(sess, cwd string, wins []Window, i, created int, coldStart bool) {
	w := wins[i]
	name := w.Name
	npanes := len(w.Panes)
	pids := tmux.ListPanes(sess, name)
	if len(pids) == 0 {
		return
	}
	// Fresh single bare-shell window -> split up to the configured pane count.
	didSplit := 0
	if len(pids) == 1 && npanes > 1 && paneIsBare(pids[0]) {
		last := pids[0]
		for j := 1; j < npanes; j++ {
			newid, err := tmux.SplitWindow(last, cwd)
			if err != nil {
				break
			}
			last = newid
		}
		layout := "even-horizontal"
		if w.Layout != nil {
			layout = *w.Layout
		}
		tmux.SelectLayout(sess, name, layout)
		pids = tmux.ListPanes(sess, name)
		if len(pids) > 0 {
			tmux.SelectPane(pids[0])
		}
		didSplit = 1
	}
	fresh := coldStart || created == 1 || didSplit == 1
	lim := len(pids)
	if npanes < lim {
		lim = npanes
	}
	for j := 0; j < lim; j++ {
		p := w.Panes[j]
		if p.Cmd == "" {
			continue
		}
		if !paneIsBare(pids[j]) {
			continue
		}
		if p.Enter {
			tmux.SendKeys(pids[j], p.Cmd, "Enter")
		} else if fresh {
			tmux.SendKeys(pids[j], p.Cmd)
		}
	}
}

// legacyLaunchClaude is the no-jq fallback: auto-launch claude in the claude
// window if the pane is a bare shell (bash's legacy_launch_claude).
func legacyLaunchClaude(sess string) {
	cmd, err := tmux.PaneCurrentCommand("=" + sess + ":claude")
	if err != nil {
		return
	}
	switch cmd {
	case "bash", "zsh", "sh", "fish":
	default:
		return
	}
	tmux.SendKeys("="+sess+":claude", DefaultClaudeCmd, "Enter")
}

// dedupWindows collapses duplicate standard windows. A tmux-resurrect restore
// racing with `tt up` can recreate the session, leaving two pi-bravo windows
// etc.; a duplicated name makes every `tmux ... -t <name>` target ambiguous.
// A duplicated pi window is dropped and respawned clean; for dev/claude one
// copy is kept (never the window tt is being run from). Ad-hoc user windows
// are left alone even if they collide.
func dedupWindows(sess, cwd, stateDir string, wins []Window, ok bool, note func(string)) {
	cur := tmux.CurrentWindowID()
	allwin := tmux.ListWindowIDs(sess)
	if len(allwin) == 0 {
		return
	}
	stdnames := configuredWindowNames(wins, ok)
	names := make([]string, 0, len(stdnames)+len(worker.NATO))
	names = append(names, stdnames...)
	for _, n := range worker.NATO {
		names = append(names, "pi-"+n)
	}
	for _, name := range names {
		var ids []string
		for _, line := range allwin {
			f := strings.Fields(line)
			if len(f) >= 2 && f[1] == name {
				ids = append(ids, f[0])
			}
		}
		if len(ids) <= 1 {
			continue
		}
		note(fmt.Sprintf("collapsing %d duplicate '%s' windows", len(ids), name))
		if strings.HasPrefix(name, "pi-") {
			for _, id := range ids {
				tmux.KillWindowByID(id)
			}
			cs := strings.TrimPrefix(name, "pi-")
			// Heal path: no --tier is set, so restore the default explicitly
			// before spawn (spawn_pi_window no longer writes the tier itself).
			os.WriteFile(filepath.Join(stateDir, cs+".tier"), []byte(worker.TierDefault), 0644)
			env, _ := SyncEnvMap()
			if err := worker.SpawnPiWindow(stateDir, sess, cwd, cs, env, os.Stderr); err != nil {
				note("respawn of " + name + " failed: " + err.Error())
			}
		} else {
			kept := ""
			for _, id := range ids {
				if kept == "" {
					kept = id
					continue
				}
				if id == cur {
					continue
				}
				tmux.KillWindowByID(id)
			}
		}
	}
}

// ensureStandardWindows brings the session to the standard layout. Idempotent
// — this is what makes `tt up` heal a session degraded by a crash, an aborted
// teardown, or a tmux-resurrect restore, not just build one from cold.
func ensureStandardWindows(sess, cwd, stateDir string, wins []Window, ok bool, coldStart bool, note func(string)) error {
	dedupWindows(sess, cwd, stateDir, wins, ok, note)
	if !ok {
		// Legacy fallback (no jq): the historical dev + claude layout.
		if !tmux.WindowExists(sess, "dev") {
			if err := tmux.NewWindow(sess, "dev", cwd); err != nil {
				return err
			}
		}
		if !tmux.WindowExists(sess, "claude") {
			if err := tmux.NewWindow(sess, "claude", cwd); err != nil {
				return err
			}
		}
		if FileExists(filepath.Join(cwd, ".tt", "windows.json")) {
			note("jq not found; ignoring .tt/windows.json")
		}
		legacyLaunchClaude(sess)
		return nil
	}
	for i, w := range wins {
		created := 0
		if !tmux.WindowExists(sess, w.Name) {
			if err := tmux.NewWindow(sess, w.Name, cwd); err != nil {
				return err
			}
			created = 1
		}
		// Keep the name stable for window_exists even while a long-running
		// command would otherwise trigger tmux automatic-rename.
		tmux.SetWindowOption(sess, w.Name, "automatic-rename", "off")
		applyWindowPanes(sess, cwd, wins, i, created, coldStart)
	}
	return nil
}

func createSession(sess, cwd, stateDir string, wins []Window, ok bool, note func(string)) error {
	// Brand-new session: every pane is fresh (prefills fire once).
	if err := tmux.NewSession(sess, firstWindowName(wins, ok), cwd); err != nil {
		return err
	}
	if err := setSessionVersion(sess, stateDir); err != nil {
		return err
	}
	// set-option does not accept the `=` exact-match prefix; the bare-name
	// "$s:" target form is handled inside tmux.SetOption.
	if err := tmux.SetOption(sess, "history-limit", "50000"); err != nil {
		return err
	}
	if err := ensureStandardWindows(sess, cwd, stateDir, wins, ok, true, note); err != nil {
		return err
	}
	return tmux.SelectWindow(sess, orchestratorWindowName(wins, ok))
}

// enterSession enters the project session: switch-client when already inside
// tmux (attach refuses to nest), attach otherwise.
func enterSession(sess, win string) error {
	target := "=" + sess
	if win != "" {
		target += ":" + win
	}
	if os.Getenv("TMUX") != "" {
		return tmux.SwitchClient(target)
	}
	return tmux.Attach(target)
}

func isTerminal() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			return true
		}
	}
	return false
}

// Up creates (or heals) the project's tmux session, stamps version + project
// into the state dir, and enters it. Prints nothing on success.
//
// Attach policy (the default flips on where you run it):
//   - Inside tmux ($TMUX set): do NOT switch by default. A bare `tt up` meant
//     "make sure my session is healthy", and an unsolicited switch-client
//     replaces the caller's current window — the window-theft bug. attach=true
//     (the --attach flag) switches deliberately. When the session is created/
//     healed without switching, a stderr note says how to enter it.
//   - Outside tmux: attach is the point of `tt up`; always attach. Off a tty
//     the attach fails harmlessly (exit 0) so scripted `tt up` stays green.
func Up(attach bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	sess := SessionNameFor(cwd)
	stateDir := filepath.Join(StateBase(), sess)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}
	note := func(m string) { fmt.Fprintf(os.Stderr, "[tt] %s\n", m) }
	wins, ok := windowsConfig(cwd, note)
	if tmux.HasSession(sess) {
		if err := setSessionVersion(sess, stateDir); err != nil {
			return err
		}
		if err := ensureStandardWindows(sess, cwd, stateDir, wins, ok, false, note); err != nil {
			return err
		}
	} else {
		if err := createSession(sess, cwd, stateDir, wins, ok, note); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(stateDir, "project"), []byte(cwd+"\n"), 0644); err != nil {
		return err
	}
	// Already inside this session — healing is done, nothing to switch to.
	if os.Getenv("TMUX") != "" && tmux.CurrentSessionName() == sess {
		return nil
	}
	if os.Getenv("TMUX") != "" {
		// Inside tmux: switch only when explicitly asked (--attach).
		if !attach {
			note(fmt.Sprintf("session %s ready (stay put; enter with `tt up --attach` or `tt attach`)", sess))
			return nil
		}
		if cur := tmux.CurrentSessionName(); cur != "" && cur != sess {
			note(fmt.Sprintf("switching from session %s to %s", cur, sess))
		}
		if err := enterSession(sess, orchestratorWindowName(wins, ok)); err != nil {
			return err
		}
		return nil
	}
	// Outside tmux: attach (the whole point); harmless off a tty.
	if err := enterSession(sess, orchestratorWindowName(wins, ok)); err != nil {
		if !isTerminal() {
			return nil
		}
		return err
	}
	return nil
}

// Attach enters the project session without creating it (bash's attach_cmd).
func Attach() error {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	sess := SessionNameFor(cwd)
	if !tmux.HasSession(sess) {
		return fmt.Errorf("no session for %s; run `tt up` first", cwd)
	}
	if err := enterSession(sess, ""); err != nil {
		if !isTerminal() {
			return nil
		}
		return err
	}
	return nil
}

// Down kills the project session after a y/N confirm (bash's down_cmd):
// SIGTERM each pi window's whole process group, kill the window, kill the
// session, and wipe the state dir. Non-interactive callers pipe `y`.
func Down() error {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	sess := SessionNameFor(cwd)
	stateDir := filepath.Join(StateBase(), sess)
	if !tmux.HasSession(sess) {
		return fmt.Errorf("no session for %s; run `tt up` first", cwd)
	}
	var busy []string
	for _, n := range worker.NATO {
		if !tmux.WindowExists(sess, "pi-"+n) {
			continue
		}
		if worker.WorkerState(stateDir, sess, n) == "busy" {
			busy = append(busy, n)
		}
	}
	if len(busy) > 0 {
		fmt.Fprintf(os.Stderr, "tt: workers busy: %s\n", strings.Join(busy, " "))
		fmt.Fprintf(os.Stderr, "tt: their in-flight tasks will be lost.\n")
	}
	fmt.Fprintf(os.Stderr, "tt: kill session %s? [y/N] ", sess)
	var ans string
	fmt.Scanln(&ans)
	if ans != "y" && ans != "Y" {
		return nil
	}
	// The group — wrapper bash, its node, and the pi grandchild — all share
	// the pane pid as PGID; killing a single pid would orphan the grandchild.
	for _, n := range worker.NATO {
		if !tmux.WindowExists(sess, "pi-"+n) {
			continue
		}
		if pid, ok := tmux.PanePID(sess, "pi-"+n); ok {
			tmux.KillProcessGroup(pid)
		}
		tmux.KillWindow(sess, "pi-"+n)
	}
	timeSleep(500)
	tmux.KillSession(sess)
	os.RemoveAll(stateDir)
	return nil
}
