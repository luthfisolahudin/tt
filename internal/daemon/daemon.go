package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/luthfisolahudin/tt/internal/client"
	"github.com/luthfisolahudin/tt/internal/session"
	"github.com/luthfisolahudin/tt/internal/tmux"
)

// Session carries the per-request session context. The daemon holds no
// authoritative state in memory — disk is the source of truth; Session is
// just the resolved request context.
type Session struct {
	Name    string
	Dir     string // per-session state dir
	Cwd     string
	SyncEnv map[string]string
}

// writeMu serializes file-mutating ops (turn assignment, spawns, wipes) so
// the daemon is the single safe writer; read/watch ops run concurrently.
var writeMu sync.Mutex

// Serve runs the daemon: unix socket at <state base>/ttd.sock, one
// line-delimited JSON request per connection, restartable and idempotent.
func Serve() error {
	base := session.StateBase()
	if base == "" {
		return fmt.Errorf("cannot determine state directory")
	}
	if err := os.MkdirAll(base, 0755); err != nil {
		return err
	}
	pidFile := filepath.Join(base, "ttd.pid")
	if err := acquirePidFile(pidFile); err != nil {
		return err
	}
	defer os.Remove(pidFile)
	sock := filepath.Join(base, "ttd.sock")
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("listen %s: %w", sock, err)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		ln.Close()
		os.Remove(sock)
		os.Remove(pidFile)
		os.Exit(0)
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil // shutdown signal closed the listener
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				continue
			}
			return err
		}
		go handleConn(conn)
	}
}

func acquirePidFile(path string) error {
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			return nil
		}
		if !os.IsExist(err) {
			return err
		}
		other := 0
		if data, rerr := os.ReadFile(path); rerr == nil {
			fmt.Sscanf(string(data), "%d", &other)
		}
		if other > 0 && processAlive(other) {
			return fmt.Errorf("ttd already running (pid %d)", other)
		}
		os.Remove(path) // stale pidfile
	}
	return fmt.Errorf("cannot acquire %s", path)
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func handleConn(conn net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			// a panicking op must not kill the daemon
			json.NewEncoder(conn).Encode(client.Response{OK: false, Error: fmt.Sprintf("internal error: %v", r)})
		}
		conn.Close()
	}()
	var req client.Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		json.NewEncoder(conn).Encode(client.Response{OK: false, Error: "bad request"})
		return
	}
	resp := dispatch(req)
	json.NewEncoder(conn).Encode(resp)
}

func dispatch(req client.Request) client.Response {
	s := &Session{
		Name:    req.Session,
		Dir:     session.SessionStateDir(req.Session),
		Cwd:     req.Cwd,
		SyncEnv: req.SyncEnv,
	}
	switch req.Op {
	case "ping":
		return client.Response{OK: true}
	case "send", "auto", "clear", "rm", "popidle", "steer", "resume", "status":
		writeMu.Lock()
		defer writeMu.Unlock()
	}
	return dispatchOp(s, req)
}

// applySyncEnv copies TT_PI_ENV_VARS entries into the tmux session env before
// a spawn (bash's sync_pi_env).
func applySyncEnv(s *Session) {
	for k, v := range s.SyncEnv {
		tmux.SetEnvironment(s.Name, k, v)
	}
}
