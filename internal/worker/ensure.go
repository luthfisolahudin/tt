package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/luthfisolahudin/tt/internal/tmux"
	"github.com/luthfisolahudin/tt/internal/version"
)

// EnsurePiWorkerDir installs the worker templates into the private pi worker
// runtime dir — a lazy, missing-only port of the bash ensure_pi_worker_dir
// (symlinked templates stay linked; drift notes go to err).
func EnsurePiWorkerDir(err io.Writer) error {
	repo, repoErr := RepoDir()
	if repoErr != nil {
		return repoErr
	}
	dir := PiWorkerDir()
	if info, lerr := os.Lstat(dir); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
		if real, rerr := filepath.EvalSymlinks(dir); rerr == nil && real == filepath.Join(repo, "pi-worker") {
			os.Remove(dir)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "extensions"), 0755); err != nil {
		return err
	}
	metaPath := filepath.Join(dir, ".tt-version")
	prev := readMeta(metaPath)
	note := func(msg string) { fmt.Fprintf(err, "[tt] %s\n", msg) }

	// APPEND_SYSTEM.md — symlink to the repo template; a customized copy is kept.
	appSrc := filepath.Join(repo, "pi-worker", "APPEND_SYSTEM.md")
	appDst := filepath.Join(dir, "APPEND_SYSTEM.md")
	appSHA := fileSHA(appSrc)
	switch {
	case !FileExists(appDst) || isDanglingSymlink(appDst):
		if e := symlinkForce(appSrc, appDst); e != nil {
			return e
		}
	case isSymlinkTo(appDst, appSrc):
		if e := symlinkForce(appSrc, appDst); e != nil {
			return e
		}
	case prev["file.APPEND_SYSTEM.md.sha"] != "" && prev["file.APPEND_SYSTEM.md.sha"] != appSHA:
		note("pi-worker APPEND_SYSTEM.md template changed; keeping customized " + appDst + " (remove it to regenerate)")
	}

	// extensions/tt-worker.ts — same policy.
	extSrc := filepath.Join(repo, "pi-worker", "extensions", "tt-worker.ts")
	extDst := filepath.Join(dir, "extensions", "tt-worker.ts")
	extSHA := fileSHA(extSrc)
	switch {
	case !FileExists(extDst) || isDanglingSymlink(extDst):
		if e := symlinkForce(extSrc, extDst); e != nil {
			return e
		}
	case isSymlinkTo(extDst, extSrc):
		if e := symlinkForce(extSrc, extDst); e != nil {
			return e
		}
	case prev["file.extensions/tt-worker.ts.sha"] != "" && prev["file.extensions/tt-worker.ts.sha"] != extSHA:
		note("pi-worker tt-worker.ts template changed; keeping customized " + extDst + " (remove it to regenerate)")
	}

	// package.json — real copy; drift or a missing pinned integration reinstall.
	packageSrc := filepath.Join(repo, "pi-worker", "package.json")
	packageDst := filepath.Join(dir, "package.json")
	packageSHA := fileSHA(packageSrc)
	needsInstall := false
	if !FileExists(packageDst) || isSymlink(packageDst) {
		if e := copyFileForce(packageSrc, packageDst); e != nil {
			return e
		}
		needsInstall = true
	} else if prev["file.package.json.sha"] != "" && prev["file.package.json.sha"] != packageSHA {
		note("pi-worker package.json changed; keeping customized " + packageDst + " (remove it to regenerate)")
		needsInstall = true
	}
	if !FileExists(filepath.Join(dir, "node_modules", "@luthfisolahudin", "9router-integrations", "extensions", "pi.ts")) {
		needsInstall = true
	}
	if needsInstall {
		if _, lerr := exec.LookPath("pnpm"); lerr != nil {
			return fmt.Errorf("pnpm is required to install the tt Pi worker integrations")
		}
		cmd := exec.Command("pnpm", "--dir", dir, "install", "--prod", "--no-lockfile", "--ignore-scripts")
		cmd.Stdout = err
		cmd.Stderr = err
		if cerr := cmd.Run(); cerr != nil {
			return fmt.Errorf("pnpm install: %w", cerr)
		}
	}

	// anthropic-compatible-providers.ts — copy when missing.
	providerSrc := filepath.Join(repo, "pi-worker", "extensions", "anthropic-compatible-providers.ts")
	providerDst := filepath.Join(dir, "extensions", "anthropic-compatible-providers.ts")
	providerSHA := fileSHA(providerSrc)
	switch {
	case !FileExists(providerDst) || isSymlink(providerDst):
		if e := copyFileForce(providerSrc, providerDst); e != nil {
			return e
		}
	case prev["file.extensions/anthropic-compatible-providers.ts.sha"] != "" && prev["file.extensions/anthropic-compatible-providers.ts.sha"] != providerSHA:
		note("pi-worker provider extension changed; keeping customized " + providerDst + " (remove it to regenerate)")
	}

	// settings.json — copy when missing.
	settingsSrc := filepath.Join(repo, "pi-worker", "settings.json")
	settingsDst := filepath.Join(dir, "settings.json")
	settingsSHA := fileSHA(settingsSrc)
	preexisted := FileExists(settingsDst) || isSymlink(settingsDst)
	if isSymlinkTo(settingsDst, settingsSrc) {
		os.Remove(settingsDst)
		preexisted = false
	}
	if !FileExists(settingsDst) {
		if e := copyFile(settingsSrc, settingsDst); e != nil {
			return e
		}
		preexisted = false
	} else if preexisted && prev["file.settings.json.sha"] != "" && prev["file.settings.json.sha"] != settingsSHA {
		note("pi-worker settings.json template changed; keeping customized " + settingsDst + " (remove it to regenerate)")
	}

	// auth.json / models.json — symlink user-global agent files when present.
	globalAgent := os.Getenv("PI_CODING_AGENT_USER_DIR")
	if globalAgent == "" {
		if home := os.Getenv("HOME"); home != "" {
			globalAgent = filepath.Join(home, ".pi", "agent")
		}
	}
	authDst := filepath.Join(dir, "auth.json")
	if FileExists(filepath.Join(globalAgent, "auth.json")) {
		switch {
		case !FileExists(authDst) || isDanglingSymlink(authDst):
			if e := symlinkForce(filepath.Join(globalAgent, "auth.json"), authDst); e != nil {
				return e
			}
		case !isSymlink(authDst) && stripAllWhitespace(readFile(authDst)) == "{}":
			if e := symlinkForce(filepath.Join(globalAgent, "auth.json"), authDst); e != nil {
				return e
			}
		}
	}
	modelsDst := filepath.Join(dir, "models.json")
	if FileExists(filepath.Join(globalAgent, "models.json")) && (!FileExists(modelsDst) || isDanglingSymlink(modelsDst)) {
		if e := symlinkForce(filepath.Join(globalAgent, "models.json"), modelsDst); e != nil {
			return e
		}
	}

	updatedAt := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	meta := fmt.Sprintf("version=%s\nrepo=%s\nfile.APPEND_SYSTEM.md.sha=%s\nfile.extensions/tt-worker.ts.sha=%s\nfile.extensions/anthropic-compatible-providers.ts.sha=%s\nfile.package.json.sha=%s\nfile.settings.json.sha=%s\nupdated_at=%s\n",
		version.Version, repo, appSHA, extSHA, providerSHA, packageSHA, settingsSHA, updatedAt)
	return os.WriteFile(metaPath, []byte(meta), 0644)
}

// StartRepl launches the pi REPL in the worker's window — non-blocking. Port
// of the bash start_repl: tmux respawn-pane with the exact env
// PI_CODING_AGENT_DIR TT_VERSION TT_WORKER_CS TT_WORKER_STATE
// [TT_WORKER_EPHEMERAL=1], nice/ionice, --model <model>:<effort> --no-skills,
// optional --append-system-prompt, `; exec bash` tail; stamps <cs>.starting.
func StartRepl(sdir, sessionName, cwd, name string, err io.Writer) error {
	if e := EnsurePiWorkerDir(err); e != nil {
		return e
	}
	tier := CurrentTier(sdir, name)
	if e := os.WriteFile(filepath.Join(sdir, name+".tier"), []byte(tier), 0644); e != nil {
		return e
	}
	psdir := SessionDir(sdir, name)
	if e := os.MkdirAll(psdir, 0755); e != nil {
		return e
	}
	// Reset the control channel for the fresh REPL.
	for _, f := range []string{name + ".result", name + ".ready", name + ".busy", name + ".steer", name + ".steer.consuming"} {
		os.Remove(filepath.Join(sdir, f))
	}
	os.RemoveAll(filepath.Join(sdir, name+".queue"))
	if e := os.MkdirAll(filepath.Join(sdir, name+".queue"), 0755); e != nil {
		return e
	}
	globalAppend := filepath.Join(PiWorkerDir(), "APPEND_SYSTEM.md")
	appendFlag := ""
	if !FileExists(filepath.Join(cwd, ".pi", "APPEND_SYSTEM.md")) && FileExists(globalAppend) {
		appendFlag = " --append-system-prompt '" + globalAppend + "'"
	}
	nicePrefix := "nice -n 19"
	if _, e := exec.LookPath("ionice"); e == nil {
		nicePrefix = "ionice -c3 " + nicePrefix
	}
	ephemeralEnv := ""
	if FileExists(filepath.Join(sdir, name+".ephemeral")) {
		ephemeralEnv = "TT_WORKER_EPHEMERAL=1 "
	}
	t, _ := GetTier(tier)
	modelArg := t.Model + ":" + t.Effort
	launch := fmt.Sprintf("%s env PI_CODING_AGENT_DIR='%s' TT_VERSION='%s' TT_WORKER_CS=%s TT_WORKER_STATE='%s' %spi --session-dir '%s' --model %s --no-skills%s; exec bash",
		nicePrefix, PiWorkerDir(), version.Version, name, sdir, ephemeralEnv, psdir, modelArg, appendFlag)
	if e := tmux.RespawnPane(sessionName, "pi-"+name, cwd, launch); e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(sdir, name+".starting"), []byte(fmt.Sprintf("%d\n", time.Now().Unix())), 0644)
}

// WaitReplReady blocks until the worker's REPL process is alive and the
// extension's queue pump + steer watch are live (<cs>.ready), 40s deadline.
func WaitReplReady(sdir, name string) error {
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if ReplRunning(sdir, name) && FileExists(filepath.Join(sdir, name+".ready")) {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("pi REPL failed to start on pi-%s", name)
}

// EnsureReplReady is the lazy wait: re-start only a genuinely dead worker
// (not one still booting), then block for readiness.
func EnsureReplReady(sdir, sessionName, cwd, name string, err io.Writer) error {
	if !ReplRunning(sdir, name) && !ReplStarting(sdir, name) {
		if e := StartRepl(sdir, sessionName, cwd, name, err); e != nil {
			return e
		}
	}
	return WaitReplReady(sdir, name)
}

// SpawnPiWindow creates the worker window and launches its REPL. Callers
// write <cs>.tier first so start_repl launches on the right model. syncEnv
// is applied to the tmux session env (bash's sync_pi_env).
func SpawnPiWindow(sdir, sessionName, cwd, name string, syncEnv map[string]string, err io.Writer) error {
	if e := tmux.NewWindow(sessionName, "pi-"+name, cwd); e != nil {
		return e
	}
	tmux.SetWindowOption(sessionName, "pi-"+name, "automatic-rename", "off")
	for k, v := range syncEnv {
		tmux.SetEnvironment(sessionName, k, v)
	}
	if e := os.WriteFile(filepath.Join(sdir, name+".gen"), []byte("0"), 0644); e != nil {
		return e
	}
	if e := os.WriteFile(filepath.Join(sdir, name+".tasks.jsonl"), nil, 0644); e != nil {
		return e
	}
	return StartRepl(sdir, sessionName, cwd, name, err)
}

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
