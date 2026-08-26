package worker

import (
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

var historicalPackageSHAs = map[string]struct{}{
	"c3a57cb08ef1430642ffe7b6fe50126c4f1823d5b1739c7ff4f5642e5961614a": {},
	"b282cf7eb2e27c1aa27d7be0153109d079f92f6992b43c79e43d4af8111d5a94": {},
}

var historicalProviderSHAs = map[string]struct{}{
	"806fc31f7800266977546023260fb739da30bbf0751d65f15bf3c0c87c5d9ef":  {},
	"5f1c854832d6994e3aae7658582ed340d51be6b27ca84ae2eeb7c151d1afe734": {},
	"c208a92a65e19336d71c9ef4c91066b2327d82a3e31957f49fc7cd998b044e15": {},
}

// syncManagedCopy refreshes an untouched copied template without replacing a
// runtime customization. Historical source hashes repair metadata written by
// older tt versions that recorded the desired hash instead of the copy's hash.
func syncManagedCopy(src, dst, metaKey string, prev map[string]string, historical map[string]struct{}) (desired, actual string, updated, customized bool, err error) {
	desired = fileSHA(src)
	if !FileExists(dst) || isSymlink(dst) {
		if err := copyFileForce(src, dst); err != nil {
			return "", "", false, false, err
		}
		return desired, desired, true, false, nil
	}

	actual = fileSHA(dst)
	if actual == desired {
		return desired, actual, false, false, nil
	}
	if prev[metaKey+".managed"] != "customized" && (actual == prev[metaKey+".actual_sha"] || actual == prev[metaKey+".desired_sha"] || actual == prev[metaKey+".sha"]) {
		if err := copyFileForce(src, dst); err != nil {
			return "", "", false, false, err
		}
		return desired, desired, true, false, nil
	}
	if prev[metaKey+".managed"] != "customized" {
		if _, ok := historical[actual]; ok {
			if err := copyFileForce(src, dst); err != nil {
				return "", "", false, false, err
			}
			return desired, desired, true, false, nil
		}
	}
	return desired, actual, false, true, nil
}

func recordCopiedTemplate(meta map[string]string, metaKey, desired, actual string, customized bool) {
	meta[metaKey+".sha"] = desired // Compatibility with metadata written before desired_sha existed.
	meta[metaKey+".desired_sha"] = desired
	meta[metaKey+".actual_sha"] = actual
	if customized {
		meta[metaKey+".managed"] = "customized"
		return
	}
	meta[metaKey+".managed"] = "managed"
}

// EnsurePiWorkerDir installs the worker templates into the private pi worker
// runtime dir. Symlinked templates retain their existing policy; copied tracked
// templates refresh only when tt can prove they remain managed.
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

	// package.json is a copied manifest. Refresh only a known tt-managed copy,
	// then install from that exact manifest; a custom manifest is never installed
	// as though it supplied tt's integration.
	packageKey := "file.package.json"
	packageSrc := filepath.Join(repo, "pi-worker", "package.json")
	packageDst := filepath.Join(dir, "package.json")
	packageSHA, packageActualSHA, packageUpdated, packageCustomized, e := syncManagedCopy(packageSrc, packageDst, packageKey, prev, historicalPackageSHAs)
	if e != nil {
		return e
	}
	recordCopiedTemplate(prev, packageKey, packageSHA, packageActualSHA, packageCustomized)
	if packageCustomized {
		note("pi-worker package.json is customized at " + packageDst + "; remove it to restore the tt-managed manifest (integration install skipped)")
	} else {
		integrationPath := filepath.Join(dir, "node_modules", "@luthfisolahudin", "9router-integrations", "extensions", "pi.ts")
		needsInstall := packageUpdated || prev[packageKey+".installed_sha"] != packageSHA || !FileExists(integrationPath)
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
			prev[packageKey+".installed_sha"] = packageSHA
		}
	}

	// anthropic-compatible-providers.ts follows the same copied-template policy.
	providerKey := "file.extensions/anthropic-compatible-providers.ts"
	providerSrc := filepath.Join(repo, "pi-worker", "extensions", "anthropic-compatible-providers.ts")
	providerDst := filepath.Join(dir, "extensions", "anthropic-compatible-providers.ts")
	providerSHA, providerActualSHA, _, providerCustomized, e := syncManagedCopy(providerSrc, providerDst, providerKey, prev, historicalProviderSHAs)
	if e != nil {
		return e
	}
	recordCopiedTemplate(prev, providerKey, providerSHA, providerActualSHA, providerCustomized)
	if providerCustomized {
		note("pi-worker provider extension is customized at " + providerDst + "; remove it to restore the tt-managed template")
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

	prev["version"] = version.Version
	prev["repo"] = repo
	prev["file.APPEND_SYSTEM.md.sha"] = appSHA
	prev["file.extensions/tt-worker.ts.sha"] = extSHA
	prev["file.settings.json.sha"] = settingsSHA
	prev["updated_at"] = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	return writeMeta(metaPath, prev)
}

func composeWorkerPrompt(cwd, sessionDir string) (string, error) {
	parts := []string{filepath.Join(PiWorkerDir(), "APPEND_SYSTEM.md")}
	if local := filepath.Join(cwd, ".pi", "APPEND_SYSTEM.md"); FileExists(local) {
		parts = append(parts, local)
	}

	var prompt strings.Builder
	for _, path := range parts {
		content, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read worker prompt %s: %w", path, err)
		}
		if prompt.Len() > 0 {
			prompt.WriteString("\n\n")
		}
		prompt.WriteString(strings.TrimSpace(string(content)))
		prompt.WriteByte('\n')
	}
	path := filepath.Join(sessionDir, "APPEND_SYSTEM.md")
	if err := AtomicWrite(path, prompt.String()); err != nil {
		return "", err
	}
	return path, nil
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
	t, tierErr := GetTier(tier)
	if tierErr != nil {
		return tierErr
	}
	appendPrompt, promptErr := composeWorkerPrompt(cwd, psdir)
	if promptErr != nil {
		return promptErr
	}
	appendFlag := " --append-system-prompt '" + appendPrompt + "'"
	nicePrefix := "nice -n 19"
	if _, e := exec.LookPath("ionice"); e == nil {
		nicePrefix = "ionice -c3 " + nicePrefix
	}
	ephemeralEnv := ""
	if FileExists(filepath.Join(sdir, name+".ephemeral")) {
		ephemeralEnv = "TT_WORKER_EPHEMERAL=1 "
	}
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
