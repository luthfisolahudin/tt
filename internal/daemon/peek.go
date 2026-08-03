package daemon

import (
	"fmt"
	"strings"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/worker"
)

// PeekArgs is tt_peek_cmd: a read-only state query returning a window's
// current pane content (the agent-readable "see window X" primitive).
type PeekArgs struct {
	Target string `json:"target"` // bare window, callsign, or pi-<cs>
	Lines  int    `json:"lines"`
}

// peekOp returns a window's current pane content — read-only. Target may be a
// bare window name (dev), a worker callsign (alfa -> pi-alfa), or a full
// pi-<cs>. Unlike `tt pi logs` (workers only), peek sees ANY window in the
// session, so an agent can read the dev server, the orchestrator pane, or a
// user window as a state query instead of scraping tmux itself.
func peekOp(s *Session, a PeekArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	target := strings.TrimSpace(a.Target)
	if target == "" {
		return die("peek: target window or callsign required")
	}
	window := target
	if !tmux.WindowExists(s.Name, window) {
		// Try as a worker callsign -> its pi-<cs> window.
		if worker.ValidCallsign(target) && tmux.WindowExists(s.Name, "pi-"+target) {
			window = "pi-" + target
		} else {
			return die(fmt.Sprintf("no window '%s' (or worker pi-%s) in session %s", target, target, s.Name))
		}
	}
	lines := a.Lines
	if lines <= 0 {
		lines = 200
	}
	content, err := tmux.CapturePane(s.Name, window, lines)
	if err != nil {
		return die(err.Error())
	}
	out.WriteString(content)
	return ok(&out, &errb, 0)
}
