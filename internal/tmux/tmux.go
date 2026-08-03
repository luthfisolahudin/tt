package tmux

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func HasSession(sessionName string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", "="+sessionName)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

func WindowExists(sessionName, windowName string) bool {
	out, err := exec.Command("tmux", "list-windows", "-t", "="+sessionName, "-F", "#W").Output()
	if err != nil {
		return false
	}
	for _, name := range strings.Split(string(out), "\n") {
		if name == windowName {
			return true
		}
	}
	return false
}

func DisplayMessage(target, format string) (string, error) {
	out, err := exec.Command("tmux", "display-message", "-t", target, "-p", format).Output()
	if err != nil {
		return "", fmt.Errorf("display-message: %w", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func PaneCurrentCommand(paneTarget string) (string, error) {
	return DisplayMessage(paneTarget, "#{pane_current_command}")
}

func SendKeys(target string, keys ...string) error {
	args := []string{"send-keys", "-t", target}
	args = append(args, keys...)
	var stderr bytes.Buffer
	cmd := exec.Command("tmux", args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("send-keys: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func NewWindow(session, name, cwd string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("tmux", "new-window", "-t", "="+session+":", "-n", name, "-c", cwd)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("new-window: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func SetWindowOption(session, window, option, value string) error {
	cmd := exec.Command("tmux", "set-window-option", "-t", "="+session+":"+window, option, value)
	return cmd.Run()
}

func RespawnPane(session, window, cwd, command string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("tmux", "respawn-pane", "-k", "-t", "="+session+":"+window, "-c", cwd, command)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("respawn-pane: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func KillWindow(session, window string) {
	exec.Command("tmux", "kill-window", "-t", "="+session+":"+window).Run()
}

func CapturePane(session, window string, lines int) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-p", "-J", "-S", fmt.Sprintf("-%d", lines), "-t", "="+session+":"+window).Output()
	if err != nil {
		return "", fmt.Errorf("capture-pane: %w", err)
	}
	return string(out), nil
}

func SetEnvironment(session, key, value string) error {
	cmd := exec.Command("tmux", "set-environment", "-t", "="+session, key, value)
	return cmd.Run()
}

// NewSession creates a detached session whose first window is named win.
func NewSession(sessionName, win, cwd string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-n", win, "-c", cwd)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("new-session: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// SetOption sets a session option with the bare-name "$s:" target form —
// set-option rejects the `=` exact-match prefix (AGENTS.md invariant).
func SetOption(sessionName, option, value string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("tmux", "set-option", "-t", sessionName+":", option, value)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("set-option: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// SelectWindow focuses a window in the session.
func SelectWindow(sessionName, window string) error {
	cmd := exec.Command("tmux", "select-window", "-t", "="+sessionName+":"+window)
	return cmd.Run()
}

// KillSession tears down a session; failures are tolerated.
func KillSession(sessionName string) {
	exec.Command("tmux", "kill-session", "-t", "="+sessionName).Run()
}

// KillWindowByID kills a window by its raw id (e.g. @12) — the dedup path.
func KillWindowByID(windowID string) {
	exec.Command("tmux", "kill-window", "-t", windowID).Run()
}

// CurrentSessionName returns the client's current session name (#S), or ""
// when there is no current session (outside tmux).
func CurrentSessionName() string {
	out, err := exec.Command("tmux", "display-message", "-p", "#S").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// CurrentWindowID returns the client's current window_id (#{window_id}), or
// "" when there is no current window.
func CurrentWindowID() string {
	out, err := exec.Command("tmux", "display-message", "-p", "#{window_id}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ListWindowIDs returns "<window_id> <window_name>" lines for a session.
func ListWindowIDs(sessionName string) []string {
	out, err := exec.Command("tmux", "list-windows", "-t", "="+sessionName, "-F", "#{window_id} #{window_name}").Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// SwitchClient switches the calling tmux client to a target — the only way
// to "attach" when already inside tmux (never `tmux attach` then; AGENTS.md).
func SwitchClient(target string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("tmux", "switch-client", "-t", target)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("switch-client: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Attach attaches to a target (outside tmux); needs a terminal.
func Attach(target string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("tmux", "attach", "-t", target)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("attach: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// LoadBuffer loads content into a named per-process tmux buffer from stdin
// (bracketed-paste source for tt x send).
func LoadBuffer(buf, content string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("tmux", "load-buffer", "-b", buf, "-")
	cmd.Stdin = strings.NewReader(content)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("load-buffer: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// PasteBuffer pastes a named buffer into a window with bracketed paste (-p),
// deleting the buffer after (-d) — tt x send's newline-safe delivery.
func PasteBuffer(sessionName, window, buf string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("tmux", "paste-buffer", "-d", "-p", "-b", buf, "-t", "="+sessionName+":"+window)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("paste-buffer: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// ListWindowNames returns the window names of a session.
func ListWindowNames(sessionName string) []string {
	out, err := exec.Command("tmux", "list-windows", "-t", "="+sessionName, "-F", "#W").Output()
	if err != nil {
		return nil
	}
	var names []string
	for _, l := range strings.Split(string(out), "\n") {
		if l != "" {
			names = append(names, l)
		}
	}
	return names
}

// CapturePanePlain captures a window's pane without joining lines
// (`capture-pane -p -S -<lines>`) — the x-classifier's plain capture.
func CapturePanePlain(sessionName, window string, lines int) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-p", "-S", fmt.Sprintf("-%d", lines), "-t", "="+sessionName+":"+window).Output()
	if err != nil {
		return "", fmt.Errorf("capture-pane: %w", err)
	}
	return string(out), nil
}

// CapturePaneEscaped captures with escape sequences preserved
// (`capture-pane -e -p -S -<lines>`) — the x-classifier's escaped capture.
func CapturePaneEscaped(sessionName, window string, lines int) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-e", "-p", "-S", fmt.Sprintf("-%d", lines), "-t", "="+sessionName+":"+window).Output()
	if err != nil {
		return "", fmt.Errorf("capture-pane: %w", err)
	}
	return string(out), nil
}
