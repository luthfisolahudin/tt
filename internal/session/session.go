package session

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func sessionBase(dir string) string {
	base := filepath.Base(dir)
	re := regexp.MustCompile(`[^A-Za-z0-9_\-]`)
	base = re.ReplaceAllString(base, "-")

	re2 := regexp.MustCompile(`-{2,}`)
	base = re2.ReplaceAllString(base, "-")

	base = strings.Trim(base, "-")
	return base
}

func sessionHash(dir string) string {
	// nosemgrep: use-of-sha1 -- must match bash's `sha1sum | cut -c1-4`
	// session naming byte-for-byte (DESIGN.md); an identifier, not security.
	h := sha1.Sum([]byte(dir))
	return fmt.Sprintf("%x", h)[:4]
}

func SessionName() string {
	dir, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return fmt.Sprintf("%s-%s", sessionBase(dir), sessionHash(dir))
}

// SessionNameFor computes the session name for an explicit cwd (the daemon
// serves requests from other directories, so it cannot use its own cwd).
func SessionNameFor(dir string) string {
	return fmt.Sprintf("%s-%s", sessionBase(dir), sessionHash(dir))
}

// StateBase is the tt state root: $TT_STATE_DIR, else
// ${XDG_STATE_HOME:-$HOME/.local/state}/tt — one base for all sessions.
func StateBase() string {
	if base := os.Getenv("TT_STATE_DIR"); base != "" {
		return base
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home := os.Getenv("HOME")
		if home == "" {
			return ""
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "tt")
}

// SessionStateDir returns (creating) the per-session state dir.
func SessionStateDir(name string) string {
	dir := filepath.Join(StateBase(), name)
	os.MkdirAll(dir, 0755)
	return dir
}

// DataDir is the tt data root: $XDG_DATA_HOME/tt, else
// $HOME/.local/share/tt.
func DataDir() string {
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

func StateDir() (string, error) {
	return SessionStateDir(SessionName()), nil
}
