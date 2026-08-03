package session

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/version"
	"github.com/luthfisolahudin/tt/internal/worker"
)

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
