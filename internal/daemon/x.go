package daemon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/session"
	"github.com/luthfisolahudin/tt/internal/tmux"
)

// XSendArgs is `tt x send [--timeout N] <session-id> (FILE | -)`: push a
// message into another tt session's orchestrator pane and submit it once the
// Claude Code TUI can safely accept input. The daemon is the single
// cross-session owner, so delivery runs here, not in the CLI.
type XSendArgs struct {
	Target     string `json:"target"`
	Timeout    int    `json:"timeout"`
	MessageB64 string `json:"message_b64"`
}

// XListArgs is `tt x list [--all]`.
type XListArgs struct {
	All bool `json:"all"`
}

// XObserveArgs is `tt x observe [run] [--interval N] [--duration N] [--all]`.
type XObserveArgs struct {
	Interval int  `json:"interval"`
	Duration int  `json:"duration"`
	All      bool `json:"all"`
}

// --- the safe-input classifier ----------------------------------------------
//
// These are the EXACT ANSI heuristics of the bash x_classify_claude_input.
// The orchestrator is a live Claude Code TUI with no file control channel, so
// delivery must wait for a safe input state: it rejects in-flight/interrupt
// states and a non-empty `❯` draft, and treats an empty prompt, dim
// suggestion text, and queued-message banners as safe (a fresh paste joins
// Claude Code's input queue or replaces its suggestion).

var (
	reInterrupt = regexp.MustCompile(`(?i)esc interrupt|ctrl\+c cancel|ctrl\+c to cancel`)
	reQueued    = regexp.MustCompile(`(?i)queued messages?|paste again to expand`)
	reActive    = regexp.MustCompile(`(?i)Churning|Blanching`)
	reANSICSI   = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	// NBSP is a literal char in the source string (Go compiles \u00a0);
	// regexp itself cannot parse the \u escape, so feed it the character.
	reSpaceOnly  = regexp.MustCompile("^[[:space:]\u00a0]*$")
	reSuggestion = regexp.MustCompile("^[[:space:]\u00a0]*\x1b\\[2m")
	reCursorSugg = regexp.MustCompile("^[[:space:]\u00a0]*\x1b\\[7m.\x1b\\[0;2m")
)

// xClassification is the full classifier output — the bash X_CLASSIFIER /
// X_UNSAFE_MARKER / X_PROMPT_PLAIN / X_PROMPT_ESCAPED / X_STRIPPED_AFTER
// globals, returned as one value so the observe loop can record them all.
type xClassification struct {
	classifier    string
	unsafeMarker  string
	promptPlain   string
	promptEscaped string
	strippedAfter string
}

func lastLineWith(s, marker string) (string, bool) {
	line := ""
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, marker) {
			line = l
		}
	}
	return line, line != ""
}

// escapeESC is bash's `perl -pe 's/\e/\\e/g'`: ESC -> the two chars `\e`.
func escapeESC(s string) string {
	return strings.ReplaceAll(s, "\x1b", `\e`)
}

func xClassifyClaudeInput(target string) xClassification {
	c := xClassification{}
	plain, err := tmux.CapturePanePlain(target, "claude", 12)
	if err != nil {
		c.classifier = "capture_error"
		return c
	}
	if reInterrupt.MatchString(plain) {
		c.classifier = "wait_active"
		c.unsafeMarker = "interrupt/cancel"
		return c
	}
	if reQueued.MatchString(plain) {
		c.classifier = "safe_queued"
		c.unsafeMarker = "queued-message"
		return c
	}
	if reActive.MatchString(plain) {
		c.classifier = "wait_active"
		c.unsafeMarker = "active-status"
		return c
	}
	escaped, err := tmux.CapturePaneEscaped(target, "claude", 8)
	if err != nil {
		c.classifier = "capture_error"
		return c
	}
	prompt, ok := lastLineWith(escaped, "❯")
	if !ok {
		c.classifier = "wait_no_prompt"
		return c
	}
	c.promptEscaped = escapeESC(prompt)
	if pp, ok2 := lastLineWith(plain, "❯"); ok2 {
		c.promptPlain = pp
	}
	after := prompt
	if i := strings.Index(after, "❯"); i >= 0 {
		// bash's `${prompt#*❯}` removes the whole character, not one byte.
		after = after[i+len("❯"):]
	}
	c.strippedAfter = reANSICSI.ReplaceAllString(after, "")
	if reSpaceOnly.MatchString(c.strippedAfter) {
		c.classifier = "safe_empty"
		return c
	}
	if reSuggestion.MatchString(after) || reCursorSugg.MatchString(after) {
		c.classifier = "safe_suggestion"
		return c
	}
	c.classifier = "wait_real_input"
	return c
}

func xClaudeAcceptsInput(target string) bool {
	switch xClassifyClaudeInput(target).classifier {
	case "safe_empty", "safe_suggestion", "safe_queued":
		return true
	}
	return false
}

// --- per-target send lock ---------------------------------------------------

// xTargetStateDir is bash's x_target_state_dir: the target session's state
// dir must exist, else the send refuses.
func xTargetStateDir(target string) (string, error) {
	d := filepath.Join(session.StateBase(), target)
	if _, err := os.Stat(d); err != nil {
		return "", fmt.Errorf("x send: no state dir for session: %s", target)
	}
	return d, nil
}

// xAcquireLock takes the per-target x-send.lock (mkdir is the one
// concurrency primitive, exactly like bash). A stale lock whose owner pid is
// dead is removed and retried. Returns the lockdir; the caller removes it on
// exit. Waits forever unless deadline; cancel (client Ctrl-C) aborts.
func xAcquireLock(target string, deadline int64, sender string, cancel <-chan struct{}) (string, error) {
	tdir, err := xTargetStateDir(target)
	if err != nil {
		return "", err
	}
	lockdir := filepath.Join(tdir, "x-send.lock")
	noted := false
	for {
		err := os.Mkdir(lockdir, 0755)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return "", fmt.Errorf("x send: cannot create lock %s: %v", lockdir, err)
		}
		if data, rerr := os.ReadFile(filepath.Join(lockdir, "pid")); rerr == nil {
			owner := strings.TrimSpace(string(data))
			if isAllDigits(owner) {
				pid, _ := strconv.Atoi(owner)
				if pid > 0 && syscall.Kill(pid, 0) != nil {
					os.RemoveAll(lockdir)
					continue
				}
			}
		}
		if !noted {
			fmt.Fprintf(os.Stderr, "[tt] x send: waiting behind another send to %s; Ctrl-C cancels\n", target)
			noted = true
		}
		if deadlineExpired(deadline) {
			return "", fmt.Errorf("x send: timed out waiting for x-send lock for %s", target)
		}
		select {
		case <-cancel:
			return "", fmt.Errorf("x send: cancelled")
		case <-time.After(500 * time.Millisecond):
		}
	}
	os.WriteFile(filepath.Join(lockdir, "pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0644)
	os.WriteFile(filepath.Join(lockdir, "from"), []byte(sender+"\n"), 0644)
	os.WriteFile(filepath.Join(lockdir, "created_at"), []byte(strconv.FormatInt(time.Now().Unix(), 10)+"\n"), 0644)
	return lockdir, nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// xWaitClaudeAcceptsInput blocks until the target's claude pane reaches a
// safe input state (bash's x_wait_claude_accepts_input).
func xWaitClaudeAcceptsInput(target string, deadline int64, cancel <-chan struct{}) error {
	noted := false
	for !xClaudeAcceptsInput(target) {
		if !noted {
			fmt.Fprintf(os.Stderr, "[tt] x send: waiting for %s:claude safe input; Ctrl-C cancels\n", target)
			noted = true
		}
		if deadlineExpired(deadline) {
			return fmt.Errorf("x send: timed out waiting for %s:claude safe input", target)
		}
		select {
		case <-cancel:
			return fmt.Errorf("x send: cancelled")
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil
}

// xDeliver pushes msg into target's claude pane: take the per-target lock,
// wait for a safe-input state, bracketed-paste (newline-safe), then submit
// with one Enter. The per-target lock + trap-on-exit removal mirror bash.
func xDeliver(s *Session, target, msg string, timeout int) error {
	deadline := int64(0)
	if timeout > 0 {
		deadline = time.Now().Unix() + int64(timeout)
	}
	lockdir, err := xAcquireLock(target, deadline, s.Name, s.Cancel)
	if err != nil {
		return err
	}
	defer os.RemoveAll(lockdir)
	if err := xWaitClaudeAcceptsInput(target, deadline, s.Cancel); err != nil {
		return err
	}
	buf := fmt.Sprintf("tt-x-%d", os.Getpid())
	if err := tmux.LoadBuffer(buf, msg); err != nil {
		return err
	}
	if err := tmux.PasteBuffer(target, "claude", buf); err != nil {
		return err
	}
	time.Sleep(300 * time.Millisecond)
	return tmux.SendKeys("="+target+":claude", "Enter")
}

func xSendOp(s *Session, a XSendArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	if !tmux.HasSession(a.Target) {
		return die("x send: no tmux session: " + a.Target)
	}
	if _, err := xTargetStateDir(a.Target); err != nil {
		return die(err.Error())
	}
	if !tmux.WindowExists(a.Target, "claude") {
		return die("x send: session " + a.Target + " has no claude window")
	}
	// Refuse if the orchestrator is not actually running (bare shell).
	cmd, err := tmux.PaneCurrentCommand("=" + a.Target + ":claude")
	if err != nil {
		return die("x send: cannot inspect " + a.Target + ":claude")
	}
	switch cmd {
	case "bash", "zsh", "sh", "fish":
		return die("x send: orchestrator not running in " + a.Target + " (bare shell)")
	}
	body, err := decodePrompt(a.MessageB64)
	if err != nil {
		return die(err.Error())
	}
	if len(body) == 0 {
		return die("x send: empty message")
	}
	// Attribution header so the receiver knows it is cross-session traffic.
	msg := "[tt x from " + s.Name + "]\n" + string(body)
	if err := xDeliver(s, a.Target, msg, a.Timeout); err != nil {
		return die(err.Error())
	}
	return ok(&out, &errb, 0)
}

// --- x list -----------------------------------------------------------------

func xListOp(s *Session, a XListArgs) client.Response {
	var out, errb strings.Builder
	base := session.StateBase()
	if a.All {
		fmt.Fprintf(&out, "%-20s  %-15s  %s\n", "SESSION", "STATUS", "PWD")
	} else {
		fmt.Fprintf(&out, "%-20s  %s\n", "SESSION", "PWD")
	}
	found := 0
	for _, sname := range stateDirNames() {
		pwdVal := "-"
		if data, err := os.ReadFile(filepath.Join(base, sname, "project")); err == nil {
			pwdVal = strings.TrimRight(string(data), "\n")
		}
		if !tmux.HasSession(sname) {
			if a.All {
				fmt.Fprintf(&out, "%-20s  %-15s  %s\n", sname, "down", pwdVal)
				found = 1
			}
			continue
		}
		status := "no-orchestrator"
		if tmux.WindowExists(sname, "claude") {
			paneCmd, _ := tmux.PaneCurrentCommand("=" + sname + ":claude")
			switch paneCmd {
			case "bash", "zsh", "sh", "fish", "":
				status = "no-orchestrator"
			default:
				status = "ready"
			}
		}
		if a.All {
			fmt.Fprintf(&out, "%-20s  %-15s  %s\n", sname, status, pwdVal)
			found = 1
		} else if status == "ready" {
			fmt.Fprintf(&out, "%-20s  %s\n", sname, pwdVal)
			found = 1
		}
	}
	if found == 0 {
		if a.All {
			fmt.Fprintf(&out, "(no tt sessions found)\n")
		} else {
			fmt.Fprintf(&out, "(no tt sessions with a live orchestrator)\n")
		}
	}
	return ok(&out, &errb, 0)
}

// stateDirNames lists the session state dirs under the tt state base
// (dirs only, sorted — bash's `"$base"/*/` glob order).
func stateDirNames() []string {
	entries, err := os.ReadDir(session.StateBase())
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// --- x observe --------------------------------------------------------------
//
// A passive, read-only diagnostics loop for tuning the x send classifier. It
// samples every running tt session's claude pane with the same classifier and
// writes rows to <state base>/x-observe.sqlite, deduping on a payload key that
// ignores the ts field. It never takes x-send.lock, pastes, or sends keys —
// but it does log pane text, so the startup warning stands.

const xObserveDDL = `PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS x_observe_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  session TEXT NOT NULL,
  status TEXT NOT NULL,
  project TEXT NOT NULL,
  pane_cmd TEXT NOT NULL,
  classifier TEXT NOT NULL,
  unsafe_marker TEXT NOT NULL,
  plain_tail TEXT NOT NULL,
  escaped_tail TEXT NOT NULL,
  prompt_plain TEXT NOT NULL,
  prompt_escaped_visible TEXT NOT NULL,
  stripped_after_prompt TEXT NOT NULL,
  payload_key TEXT NOT NULL UNIQUE
);
CREATE INDEX IF NOT EXISTS x_observe_events_ts_idx ON x_observe_events(ts);
CREATE INDEX IF NOT EXISTS x_observe_events_classifier_idx ON x_observe_events(classifier);
CREATE INDEX IF NOT EXISTS x_observe_events_session_idx ON x_observe_events(session);
`

func xObserveInitDB(db string) error {
	if err := os.MkdirAll(filepath.Dir(db), 0755); err != nil {
		return err
	}
	cmd := exec.Command("sqlite3", db)
	cmd.Stdin = strings.NewReader(xObserveDDL)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sqlite3 init: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// xObservePayloadKey shells out to perl exactly as bash does: the sha1 hex of
// the canonical (sorted-key, ascii) JSON of the 11 named fields, so the
// dedup key stays byte-identical to the bash x_observe_payload_key.
func xObservePayloadKey(fields ...string) (string, error) {
	script := `my @names = qw(session status project pane_cmd classifier unsafe_marker plain_tail escaped_tail prompt_plain prompt_escaped_visible stripped_after_prompt);
my %obj;
@obj{@names} = @ARGV;
print sha1_hex(JSON::PP->new->ascii->canonical->encode(\%obj));
`
	args := append([]string{"-MJSON::PP", "-MDigest::SHA=sha1_hex", "-e", script}, fields...)
	out, err := exec.Command("perl", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// xObserveInsert builds the INSERT via the same perl quoting script bash
// pipes into sqlite3 — byte-faithful (single-quote doubling, ts unquoted).
func xObserveInsert(db string, ts int64, sname, status, project, paneCmd, classifier, unsafe, plain, escaped, promptPlain, promptEscaped, stripped string) error {
	key, err := xObservePayloadKey(sname, status, project, paneCmd, classifier, unsafe, plain, escaped, promptPlain, promptEscaped, stripped)
	if err != nil {
		return err
	}
	script := `sub sql_quote {
  my ($s) = @_;
  $s =~ s/\x27/\x27\x27/g;
  return chr(39) . $s . chr(39);
}
my @v = @ARGV;
print "INSERT OR IGNORE INTO x_observe_events (ts, session, status, project, pane_cmd, classifier, unsafe_marker, plain_tail, escaped_tail, prompt_plain, prompt_escaped_visible, stripped_after_prompt, payload_key) VALUES (";
print join(",", $v[0], map { sql_quote($_) } @v[1..12]);
print ");\n";
`
	args := []string{"-CS", "-e", script, strconv.FormatInt(ts, 10),
		sname, status, project, paneCmd, classifier, unsafe, plain, escaped,
		promptPlain, promptEscaped, stripped, key}
	out, err := exec.Command("perl", args...).Output()
	if err != nil {
		return err
	}
	cmd := exec.Command("sqlite3", db)
	cmd.Stdin = bytes.NewReader(out)
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func xObserveOp(s *Session, a XObserveArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	note := func(msg string) { fmt.Fprintf(&errb, "[tt] %s\n", msg) }
	db := filepath.Join(session.StateBase(), "x-observe.sqlite")
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return die("x observe: sqlite3 is required")
	}
	if err := xObserveInitDB(db); err != nil {
		return die("x observe: " + err.Error())
	}
	deadline := int64(0)
	if a.Duration > 0 {
		deadline = time.Now().Unix() + int64(a.Duration)
	}
	note("x observe: logging Claude pane captures to " + db + "; Ctrl-C stops collection")
	for {
		for _, dir := range stateDirPaths() {
			sname := filepath.Base(dir)
			project := "-"
			if data, err := os.ReadFile(filepath.Join(dir, "project")); err == nil {
				project = strings.TrimRight(string(data), "\n")
			}
			status := "ready"
			paneCmd := ""
			plain := ""
			escaped := ""
			cls := xClassification{}
			if !tmux.HasSession(sname) {
				if !a.All {
					continue
				}
				status = "down"
				cls.classifier = "not_observed"
			} else if !tmux.WindowExists(sname, "claude") {
				if !a.All {
					continue
				}
				status = "no-claude-window"
				cls.classifier = "not_observed"
			} else {
				paneCmd, _ = tmux.PaneCurrentCommand("=" + sname + ":claude")
				switch paneCmd {
				case "bash", "zsh", "sh", "fish", "":
					if !a.All {
						continue
					}
					status = "no-orchestrator"
					cls.classifier = "not_observed"
				default:
					cls = xClassifyClaudeInput(sname)
					plain, _ = tmux.CapturePanePlain(sname, "claude", 12)
					if e, err := tmux.CapturePaneEscaped(sname, "claude", 8); err == nil {
						escaped = escapeESC(e)
					}
				}
			}
			if err := xObserveInsert(db, time.Now().Unix(), sname, status, project, paneCmd,
				cls.classifier, cls.unsafeMarker, plain, escaped,
				cls.promptPlain, cls.promptEscaped, cls.strippedAfter); err != nil {
				return die("x observe: " + err.Error())
			}
		}
		if deadlineExpired(deadline) {
			return ok(&out, &errb, 0)
		}
		select {
		case <-s.Cancel:
			return ok(&out, &errb, 0)
		case <-time.After(time.Duration(a.Interval) * time.Second):
		}
	}
}

func stateDirPaths() []string {
	names := stateDirNames()
	dirs := make([]string, 0, len(names))
	for _, n := range names {
		dirs = append(dirs, filepath.Join(session.StateBase(), n))
	}
	return dirs
}
