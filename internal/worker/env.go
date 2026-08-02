package worker

import (
	"errors"
	"os"
	"path/filepath"
)

// PiWorkerDir returns the writable pi worker runtime dir (the pi
// PI_CODING_AGENT_DIR), resolved like bash: TT_PI_WORKER_DIR, legacy
// TT_PI_AGENT_DIR, else $TT_DATA_DIR/pi-worker.
func PiWorkerDir() string {
	if v := os.Getenv("TT_PI_WORKER_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("TT_PI_AGENT_DIR"); v != "" {
		return v
	}
	return filepath.Join(sessionDataDir(), "pi-worker")
}

// sessionDataDir is the tt data root (inlined here to avoid a session import
// cycle at package level; session.DataDir() is the exported twin).
func sessionDataDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home := os.Getenv("HOME")
		if home == "" {
			return ""
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "tt")
}

// RepoDir locates the tt repo (owner of the pi-worker templates):
// TT_REPO_DIR env, else walk up from the executable, else resolve a template
// symlink in the runtime dir back into the repo.
func RepoDir() (string, error) {
	if v := os.Getenv("TT_REPO_DIR"); v != "" {
		return v, nil
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			if dir := findRepo(filepath.Dir(real)); dir != "" {
				return dir, nil
			}
		}
	}
	// Runtime-dir templates symlink back into the repo — resolve one.
	if wd := PiWorkerDir(); wd != "" {
		for _, rel := range []string{"APPEND_SYSTEM.md", "extensions/tt-worker.ts", "package.json"} {
			if real, err := filepath.EvalSymlinks(filepath.Join(wd, rel)); err == nil {
				if dir := findRepo(filepath.Dir(real)); dir != "" {
					return dir, nil
				}
			}
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if dir := findRepo(cwd); dir != "" {
			return dir, nil
		}
	}
	return "", errors.New("cannot locate the tt repo (set TT_REPO_DIR)")
}

func findRepo(start string) string {
	for dir := start; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if FileExists(filepath.Join(dir, "pi-worker", "package.json")) {
			return dir
		}
	}
	return ""
}
