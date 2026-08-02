package client

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"time"
)

// Response is the line-delimited JSON reply from ttd. ops return pre-formatted
// stdout/stderr plus an exit code, so the CLI relays byte-for-byte what bash
// would have written.
type Response struct {
	OK       bool   `json:"ok"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Request is one line-delimited JSON request on the socket.
type Request struct {
	Op      string            `json:"op"`
	Session string            `json:"session"`
	Cwd     string            `json:"cwd"`
	SyncEnv map[string]string `json:"sync_env,omitempty"`
	Args    any               `json:"args"`
}

// Client dials the ttd unix socket, auto-starting the daemon on first use.
type Client struct {
	Sock string
}

// New builds a client for the state base's ttd socket.
func New() *Client {
	return &Client{Sock: SocketPath()}
}

// Do sends one request and reads the response. wait/collect may block for a
// long time (bounded only by their --timeout); Ctrl-C cancels the CLI.
func (c *Client) Do(op, sessionName, cwd string, args any) (Response, error) {
	if err := c.ensure(); err != nil {
		return Response{}, err
	}
	conn, err := net.DialTimeout("unix", c.Sock, 2*time.Second)
	if err != nil {
		return Response{}, fmt.Errorf("cannot reach ttd at %s: %w", c.Sock, err)
	}
	defer conn.Close()
	req := Request{Op: op, Session: sessionName, Cwd: cwd, SyncEnv: syncEnv(), Args: args}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}

// Ping checks daemon liveness over the socket.
func Ping() bool {
	conn, err := net.DialTimeout("unix", SocketPath(), time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(Request{Op: "ping"}); err != nil {
		return false
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return false
	}
	return resp.OK
}

func (c *Client) ensure() error {
	if conn, err := net.DialTimeout("unix", c.Sock, time.Second); err == nil {
		conn.Close()
		return nil
	}
	_, _, err := Start()
	return err
}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// syncEnv extracts TT_PI_ENV_VARS entries from the calling process env — the
// values bash's sync_pi_env would copy into the tmux session at spawn.
func syncEnv() map[string]string {
	env := map[string]string{}
	for _, key := range fields(os.Getenv("TT_PI_ENV_VARS")) {
		if !envNameRe.MatchString(key) {
			continue
		}
		if v, ok := os.LookupEnv(key); ok {
			env[key] = v
		}
	}
	return env
}

func fields(s string) []string {
	var out []string
	for _, f := range regexp.MustCompile(`\s+`).Split(strings.TrimSpace(s), -1) {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}
