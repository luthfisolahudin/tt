package daemon

import (
	"fmt"
	"strings"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/worker"
)

func steerOp(s *Session, a SteerArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	if !worker.ValidCallsign(a.Callsign) {
		return die("invalid callsign: " + a.Callsign)
	}
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	if !tmux.WindowExists(s.Name, "pi-"+a.Callsign) {
		return die(fmt.Sprintf("pi-%s does not exist", a.Callsign))
	}
	if err := worker.EnsureReplReady(s.Dir, s.Name, s.Cwd, a.Callsign, &errb); err != nil {
		return die(err.Error())
	}
	msg, err := decodePrompt(a.MessageB64)
	if err != nil {
		return die(err.Error())
	}
	if len(msg) == 0 {
		return die("steer: empty message")
	}
	if err := worker.WriteSteerFile(s.Dir, a.Callsign, string(msg)); err != nil {
		return die(err.Error())
	}
	return ok(&out, &errb, 0)
}

func steerAllOp(s *Session, a SteerArgs) client.Response {
	var out, errb strings.Builder
	die := func(msg string) client.Response {
		fmt.Fprintf(&errb, "tt: %s\n", msg)
		return ok(&out, &errb, 1)
	}
	note := func(msg string) { fmt.Fprintf(&errb, "[tt] %s\n", msg) }
	if !tmux.HasSession(s.Name) {
		return die("no session for " + s.Cwd + "; run `tt up` first")
	}
	msg, err := decodePrompt(a.MessageB64)
	if err != nil {
		return die(err.Error())
	}
	if len(msg) == 0 {
		return die("steer-all: empty message")
	}
	sent := 0
	for _, n := range worker.NATO {
		if !tmux.WindowExists(s.Name, "pi-"+n) {
			continue
		}
		if !worker.ReplRunning(s.Dir, n) {
			continue
		}
		if err := worker.WriteSteerFile(s.Dir, n, string(msg)); err != nil {
			return die(err.Error())
		}
		fmt.Fprintf(&out, "%s\n", n)
		sent++
	}
	if sent == 0 {
		note("steer-all: no live workers")
	}
	return ok(&out, &errb, 0)
}
