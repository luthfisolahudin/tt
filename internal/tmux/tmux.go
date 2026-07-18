package tmux

import (
	"bytes"
	"fmt"
	"io"
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
