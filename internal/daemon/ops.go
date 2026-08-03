package daemon

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/luthfisolahudin/tt/internal/client"
)

// SendArgs mirrors the bash pi_send_cmd dispatch: lazy spawn under the cap,
// tier guard, enqueue to the worker's own queue; prints the task id.
type SendArgs struct {
	Callsign  string `json:"callsign"`
	Tier      string `json:"tier"`
	Notify    bool   `json:"notify"`
	PromptB64 string `json:"prompt_b64"`
}

// AutoArgs mirrors pi_auto_cmd: reuse idle -> spawn under cap -> shared pool.
type AutoArgs struct {
	Tier        string `json:"tier"`
	RM          bool   `json:"rm"`
	Notify      bool   `json:"notify"`
	JSON        bool   `json:"json"`
	PreferFresh bool   `json:"prefer_fresh"`
	PromptB64   string `json:"prompt_b64"`
}

// SteerArgs is pi_steer_cmd: run-now injection, bypassing the queue.
type SteerArgs struct {
	Callsign   string `json:"callsign"`
	MessageB64 string `json:"message_b64"`
}

// ResumeArgs is pi_resume_cmd: re-drive the interrupted task on the live REPL.
type ResumeArgs struct {
	Callsign string `json:"callsign"`
}

// ClearArgs is pi_clear_cmd: bump gen, respawn on a fresh session-dir.
type ClearArgs struct {
	Callsign string `json:"callsign"`
	Force    bool   `json:"force"`
}

// RmArgs is pi_rm_cmd: kill the window and wipe the worker's state.
type RmArgs struct {
	Callsign string `json:"callsign"`
	Force    bool   `json:"force"`
}

// PopidleArgs is pi_popidle_cmd (no args).
type PopidleArgs struct{}

func dispatchOp(s *Session, req client.Request) client.Response {
	switch req.Op {
	case "send":
		var a SendArgs
		decodeArgs(req.Args, &a)
		return sendOp(s, a)
	case "auto":
		var a AutoArgs
		decodeArgs(req.Args, &a)
		return autoOp(s, a)
	case "steer":
		var a SteerArgs
		decodeArgs(req.Args, &a)
		if a.Callsign == "all" {
			return steerAllOp(s, a)
		}
		return steerOp(s, a)
	case "resume":
		var a ResumeArgs
		decodeArgs(req.Args, &a)
		return resumeOp(s, a)
	case "clear":
		var a ClearArgs
		decodeArgs(req.Args, &a)
		return clearOp(s, a)
	case "rm":
		var a RmArgs
		decodeArgs(req.Args, &a)
		return rmOp(s, a)
	case "popidle":
		return popidleOp(s)
	case "wait":
		var a WaitArgs
		decodeArgs(req.Args, &a)
		return waitOp(s, a)
	case "status":
		var a StatusArgs
		decodeArgs(req.Args, &a)
		return statusOp(s, a)
	case "collect":
		var a CollectArgs
		decodeArgs(req.Args, &a)
		return collectOp(s, a)
	case "results":
		var a ResultsArgs
		decodeArgs(req.Args, &a)
		return resultsOp(s, a)
	case "peek":
		var a PeekArgs
		decodeArgs(req.Args, &a)
		return peekOp(s, a)
	case "pipeline":
		var a PipelineArgs
		decodeArgs(req.Args, &a)
		return pipelineOp(s, a)
	case "x-send":
		var a XSendArgs
		decodeArgs(req.Args, &a)
		return xSendOp(s, a)
	case "x-list":
		var a XListArgs
		decodeArgs(req.Args, &a)
		return xListOp(s, a)
	case "x-observe":
		var a XObserveArgs
		decodeArgs(req.Args, &a)
		return xObserveOp(s, a)
	case "deliver":
		var a XDeliverArgs
		decodeArgs(req.Args, &a)
		return xDeliverOp(s, a)
	}
	return client.Response{OK: false, Error: "unknown op: " + req.Op}
}

// decodeArgs re-marshals the request's decoded args into a typed struct.
func decodeArgs(raw any, into any) {
	if raw == nil {
		return
	}
	if data, err := json.Marshal(raw); err == nil {
		json.Unmarshal(data, into)
	}
}

func ok(out, errb *strings.Builder, code int) client.Response {
	return client.Response{OK: true, Stdout: out.String(), Stderr: errb.String(), ExitCode: code}
}

func decodePrompt(b64 string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("bad prompt encoding")
	}
	return data, nil
}
