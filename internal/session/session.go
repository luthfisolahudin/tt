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

func StateDir() (string, error) {
	base := os.Getenv("TT_STATE_DIR")
	if base == "" {
		base = os.Getenv("XDG_STATE_HOME")
		if base == "" {
			home := os.Getenv("HOME")
			if home == "" {
				return "", fmt.Errorf("cannot determine state directory: HOME not set")
			}
			base = filepath.Join(home, ".local", "state")
		}
		base = filepath.Join(base, "tt")
	}
	
	dir := filepath.Join(base, SessionName())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}
	return dir, nil
}
