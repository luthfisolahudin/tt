package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// --- meta helpers -----------------------------------------------------------

func readMeta(path string) map[string]string {
	m := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, "="); i > 0 {
			m[line[:i]] = line[i+1:]
		}
	}
	return m
}

func fileSHA(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func stripAllWhitespace(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\v', '\f':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func isDanglingSymlink(path string) bool {
	return isSymlink(path) && !FileExists(path)
}

func isSymlinkTo(path, target string) bool {
	if !isSymlink(path) {
		return false
	}
	real, err := filepath.EvalSymlinks(path)
	return err == nil && real == target
}

// symlinkForce is `ln -sfn src dst` — force-replace with a symlink to src.
func symlinkForce(src, dst string) error {
	os.Remove(dst)
	if err := os.Symlink(src, dst); err != nil {
		return err
	}
	return nil
}

// copyFileForce is `cp --remove-destination src dst`.
func copyFileForce(src, dst string) error {
	os.Remove(dst)
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
