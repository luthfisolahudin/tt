package daemon

import (
	"fmt"
	"os"
	"path/filepath"
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

// XDeliverArgs is the internal `deliver` op: push a pre-headed message into a
// target session's claude pane. Used by the notify drainer (a detached CLI
// process) which coalesces `notify/*.msg` and calls this per drain cycle.
type XDeliverArgs struct {
	Target     string `json:"target"`
	Timeout    int    `json:"timeout"`
	MessageB64 string `json:"message_b64"`
}

// xDeliverOp delivers an already-attributed message via xDeliver. Unlike
// xSendOp it does not prepend a header (the drainer builds its own `[tt] …`
// coalesced body) and does not require the orchestrator to be live up front —
// the drainer checks that itself per cycle.
func xDeliverOp(s *Session, a XDeliverArgs) client.Response {
	var out, errb strings.Builder
	msg, err := decodePrompt(a.MessageB64)
	if err != nil {
		fmt.Fprintf(&errb, "tt: %s\n", err.Error())
		return ok(&out, &errb, 1)
	}
	if err := xDeliver(s, a.Target, string(msg), a.Timeout); err != nil {
		fmt.Fprintf(&errb, "tt: %s\n", err.Error())
		return ok(&out, &errb, 1)
	}
	return ok(&out, &errb, 0)
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
