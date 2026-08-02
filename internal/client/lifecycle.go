package client

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/luthfisolahudin/tt/internal/session"
)

// SocketPath is the ttd unix socket: <state base>/ttd.sock.
func SocketPath() string {
	return filepath.Join(session.StateBase(), "ttd.sock")
}

// PidPath is the ttd single-instance pidfile: <state base>/ttd.pid.
func PidPath() string {
	return filepath.Join(session.StateBase(), "ttd.pid")
}

// Running reports the daemon pid from the pidfile when its process is alive.
func Running() (int, bool) {
	data, err := os.ReadFile(PidPath())
	if err != nil {
		return 0, false
	}
	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil || pid <= 0 {
		return 0, false
	}
	if !processAlive(pid) {
		return 0, false
	}
	return pid, true
}

// Start ensures the daemon is running: reuses a live one, else spawns itself
// detached (`tt daemon serve`). Returns (pid, started, err).
func Start() (int, bool, error) {
	base := session.StateBase()
	if base == "" {
		return 0, false, fmt.Errorf("cannot determine state directory (HOME not set)")
	}
	if err := os.MkdirAll(base, 0755); err != nil {
		return 0, false, err
	}
	if pid, alive := Running(); alive {
		if Ping() {
			return pid, false, nil
		}
		if waitSocket(3 * time.Second) {
			return pid, false, nil
		}
		return 0, false, fmt.Errorf("ttd (pid %d) is not responding on %s; run `tt daemon stop` first", pid, SocketPath())
	}
	// stale pidfile/socket cleanup
	os.Remove(PidPath())
	os.Remove(SocketPath())
	logf, err := os.OpenFile(filepath.Join(base, "ttd.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return 0, false, err
	}
	exe, err := os.Executable()
	if err != nil {
		return 0, false, err
	}
	cmd := exec.Command(exe, "daemon", "serve")
	cmd.Env = os.Environ()
	cmd.Stdin = nil
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, false, err
	}
	pid := cmd.Process.Pid
	cmd.Process.Release()
	if !waitSocket(10 * time.Second) {
		return 0, false, fmt.Errorf("ttd failed to start (no socket at %s)", SocketPath())
	}
	return pid, true, nil
}

// Stop SIGTERMs the daemon (then SIGKILLs if it lingers) and cleans up.
func Stop() error {
	pid, alive := Running()
	if !alive {
		os.Remove(PidPath())
		os.Remove(SocketPath())
		return nil
	}
	syscall.Kill(pid, syscall.SIGTERM)
	for i := 0; i < 50; i++ {
		if !processAlive(pid) {
			os.Remove(PidPath())
			os.Remove(SocketPath())
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	syscall.Kill(pid, syscall.SIGKILL)
	return fmt.Errorf("ttd (pid %d) did not stop gracefully; killed", pid)
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func waitSocket(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("unix", SocketPath(), time.Second); err == nil {
			conn.Close()
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
