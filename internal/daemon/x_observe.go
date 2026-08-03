package daemon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/session"
	"github.com/luthfisolahudin/tt/internal/tmux"
)

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
