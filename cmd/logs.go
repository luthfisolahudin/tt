package cmd

import (
	"github.com/luthfisolahudin/tt/internal/tmux"
)

// hasSession mirrors bash's session_exists.
func hasSession(sessionName string) bool {
	return tmux.HasSession(sessionName)
}

// windowExists mirrors bash's window_exists.
func windowExists(sessionName, windowName string) bool {
	return tmux.WindowExists(sessionName, windowName)
}

// capturePane mirrors bash's `tmux capture-pane -p -J -S -<lines>`.
func capturePane(sessionName, window string, lines int) (string, error) {
	return tmux.CapturePane(sessionName, window, lines)
}
