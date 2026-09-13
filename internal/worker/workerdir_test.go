package worker

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const historical46e8125Manifest = `{
  "name": "tt-pi-worker",
  "private": true,
  "type": "module",
  "dependencies": {
    "@luthfisolahudin/9router-integrations": "github:luthfisolahudin/9router-integrations#46e8125"
  }
}
`

type workerDirFixture struct {
	repo        string
	runtime     string
	packageSrc  string
	providerSrc string
	pnpmLog     string
}

func newWorkerDirFixture(t *testing.T) workerDirFixture {
	t.Helper()
	repo := t.TempDir()
	runtime := filepath.Join(t.TempDir(), "pi-worker")
	packageSrc := filepath.Join(repo, "pi-worker", "package.json")
	providerSrc := filepath.Join(repo, "pi-worker", "extensions", "anthropic-compatible-providers.ts")
	for path, content := range map[string]string{
		filepath.Join(repo, "pi-worker", "APPEND_SYSTEM.md"):             "worker rules\n",
		filepath.Join(repo, "pi-worker", "extensions", "tt-worker.ts"):   "export {};\n",
		filepath.Join(repo, "pi-worker", "extensions", "commandcode.ts"): "export {};\n",
		providerSrc: "export const provider = 'one';\n",
		packageSrc:  "{\"name\":\"tt-pi-worker\",\"version\":\"one\"}\n",
		filepath.Join(repo, "pi-worker", "settings.json"): "{}\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	binDir := t.TempDir()
	pnpmLog := filepath.Join(t.TempDir(), "pnpm-manifest")
	pnpm := filepath.Join(binDir, "pnpm")
	fakePnpm := `#!/bin/sh
set -eu
dir="$2"
cat "$dir/package.json" > "$PNPM_LOG"
mkdir -p "$dir/node_modules/@luthfisolahudin/9router-integrations/extensions"
: > "$dir/node_modules/@luthfisolahudin/9router-integrations/extensions/pi.ts"
`
	if err := os.WriteFile(pnpm, []byte(fakePnpm), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TT_REPO_DIR", repo)
	t.Setenv("TT_PI_WORKER_DIR", runtime)
	t.Setenv("PI_CODING_AGENT_USER_DIR", filepath.Join(t.TempDir(), "no-global-agent"))
	t.Setenv("PNPM_LOG", pnpmLog)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return workerDirFixture{repo: repo, runtime: runtime, packageSrc: packageSrc, providerSrc: providerSrc, pnpmLog: pnpmLog}
}

func TestComposeWorkerPromptLayersCoreAndProjectOverlay(t *testing.T) {
	newWorkerDirFixture(t)
	ensureWorkerDir(t)
	cwd := t.TempDir()
	local := filepath.Join(cwd, ".pi", "APPEND_SYSTEM.md")
	if err := os.MkdirAll(filepath.Dir(local), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("project rules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sessionDir := t.TempDir()
	path, err := composeWorkerPrompt(cwd, sessionDir)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(sessionDir, "APPEND_SYSTEM.md") {
		t.Fatalf("prompt path = %q", path)
	}
	want := "worker rules\n\n\nproject rules\n"
	if got := readWorkerFile(t, path); got != want {
		t.Fatalf("composed prompt = %q, want %q", got, want)
	}
	if got := strings.Count(readWorkerFile(t, path), "worker rules"); got != 1 {
		t.Fatalf("core count = %d, want 1", got)
	}
}

func ensureWorkerDir(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	if err := EnsurePiWorkerDir(&output); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func readWorkerFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEnsurePiWorkerDirCopiesManifestAndInstallsCurrentContent(t *testing.T) {
	for _, test := range []struct {
		name    string
		symlink bool
	}{
		{name: "missing"},
		{name: "symlink", symlink: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWorkerDirFixture(t)
			if err := os.MkdirAll(fixture.runtime, 0755); err != nil {
				t.Fatal(err)
			}
			packageDst := filepath.Join(fixture.runtime, "package.json")
			if test.symlink {
				oldTarget := filepath.Join(t.TempDir(), "old-package.json")
				if err := os.WriteFile(oldTarget, []byte("old\n"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(oldTarget, packageDst); err != nil {
					t.Fatal(err)
				}
			}

			ensureWorkerDir(t)

			info, err := os.Lstat(packageDst)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				t.Fatal("runtime package.json remains a symlink")
			}
			wantPackage := readWorkerFile(t, fixture.packageSrc)
			if got := readWorkerFile(t, packageDst); got != wantPackage {
				t.Fatalf("runtime package.json = %q, want %q", got, wantPackage)
			}
			if got := readWorkerFile(t, fixture.pnpmLog); got != wantPackage {
				t.Fatalf("pnpm saw package.json %q, want %q", got, wantPackage)
			}
			meta := readMeta(filepath.Join(fixture.runtime, ".tt-version"))
			packageSHA := fileSHA(fixture.packageSrc)
			for key, want := range map[string]string{
				"file.package.json.desired_sha":   packageSHA,
				"file.package.json.actual_sha":    packageSHA,
				"file.package.json.installed_sha": packageSHA,
				"file.package.json.managed":       "managed",
			} {
				if got := meta[key]; got != want {
					t.Errorf("metadata %s = %q, want %q", key, got, want)
				}
			}
		})
	}
}

func TestEnsurePiWorkerDirRefreshesManagedManifestBeforeInstall(t *testing.T) {
	fixture := newWorkerDirFixture(t)
	ensureWorkerDir(t)
	wantPackage := "{\"name\":\"tt-pi-worker\",\"version\":\"two\"}\n"
	if err := os.WriteFile(fixture.packageSrc, []byte(wantPackage), 0644); err != nil {
		t.Fatal(err)
	}

	ensureWorkerDir(t)

	if got := readWorkerFile(t, filepath.Join(fixture.runtime, "package.json")); got != wantPackage {
		t.Fatalf("runtime package.json = %q, want refreshed %q", got, wantPackage)
	}
	if got := readWorkerFile(t, fixture.pnpmLog); got != wantPackage {
		t.Fatalf("pnpm saw %q, want refreshed manifest %q", got, wantPackage)
	}
	meta := readMeta(filepath.Join(fixture.runtime, ".tt-version"))
	if wantSHA := fileSHA(fixture.packageSrc); meta["file.package.json.desired_sha"] != wantSHA || meta["file.package.json.actual_sha"] != wantSHA || meta["file.package.json.installed_sha"] != wantSHA {
		t.Fatalf("manifest hashes = desired %q actual %q installed %q, want %q", meta["file.package.json.desired_sha"], meta["file.package.json.actual_sha"], meta["file.package.json.installed_sha"], wantSHA)
	}
}

func TestEnsurePiWorkerDirRecoversHistoricalManifestWithMisleadingMetadata(t *testing.T) {
	fixture := newWorkerDirFixture(t)
	if got := fileSHAFromString(historical46e8125Manifest); got != "b282cf7eb2e27c1aa27d7be0153109d079f92f6992b43c79e43d4af8111d5a94" {
		t.Fatalf("historical fixture SHA = %s", got)
	}
	if err := os.MkdirAll(fixture.runtime, 0755); err != nil {
		t.Fatal(err)
	}
	packageDst := filepath.Join(fixture.runtime, "package.json")
	if err := os.WriteFile(packageDst, []byte(historical46e8125Manifest), 0644); err != nil {
		t.Fatal(err)
	}
	wantPackage := readWorkerFile(t, fixture.packageSrc)
	if err := os.WriteFile(filepath.Join(fixture.runtime, ".tt-version"), []byte("future_key=kept\nfile.package.json.sha="+fileSHA(fixture.packageSrc)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	output := ensureWorkerDir(t)

	if strings.Contains(output, "package.json is customized") {
		t.Fatalf("historical manifest was treated as customized: %s", output)
	}
	if got := readWorkerFile(t, packageDst); got != wantPackage {
		t.Fatalf("recovered manifest = %q, want %q", got, wantPackage)
	}
	if got := readWorkerFile(t, fixture.pnpmLog); got != wantPackage {
		t.Fatalf("pnpm saw %q, want recovered manifest %q", got, wantPackage)
	}
	meta := readMeta(filepath.Join(fixture.runtime, ".tt-version"))
	if meta["future_key"] != "kept" {
		t.Fatalf("unrelated metadata key was lost: %#v", meta)
	}
	wantSHA := fileSHA(fixture.packageSrc)
	if meta["file.package.json.desired_sha"] != wantSHA || meta["file.package.json.actual_sha"] != wantSHA {
		t.Fatalf("recovered hashes = desired %q actual %q, want %q", meta["file.package.json.desired_sha"], meta["file.package.json.actual_sha"], wantSHA)
	}
}

func TestEnsurePiWorkerDirPreservesCustomizedManifestAndWarnsPersistently(t *testing.T) {
	fixture := newWorkerDirFixture(t)
	if err := os.MkdirAll(fixture.runtime, 0755); err != nil {
		t.Fatal(err)
	}
	customManifest := "{\"name\":\"my-custom-worker\"}\n"
	packageDst := filepath.Join(fixture.runtime, "package.json")
	if err := os.WriteFile(packageDst, []byte(customManifest), 0644); err != nil {
		t.Fatal(err)
	}

	first := ensureWorkerDir(t)
	second := ensureWorkerDir(t)

	for call, output := range map[string]string{"first": first, "second": second} {
		if !strings.Contains(output, "package.json is customized") || !strings.Contains(output, "remove it to restore") {
			t.Errorf("%s customization warning = %q", call, output)
		}
	}
	if got := readWorkerFile(t, packageDst); got != customManifest {
		t.Fatalf("custom package.json was overwritten: %q", got)
	}
	if err := fileDoesNotExist(fixture.pnpmLog); err != nil {
		t.Fatalf("pnpm ran for a customized manifest: %v", err)
	}
	meta := readMeta(filepath.Join(fixture.runtime, ".tt-version"))
	if meta["file.package.json.managed"] != "customized" || meta["file.package.json.actual_sha"] != fileSHA(packageDst) || meta["file.package.json.desired_sha"] != fileSHA(fixture.packageSrc) {
		t.Fatalf("custom manifest metadata = %#v", meta)
	}
}

func TestEnsurePiWorkerDirRefreshesManagedProviderAndPreservesCustomization(t *testing.T) {
	fixture := newWorkerDirFixture(t)
	ensureWorkerDir(t)
	providerDst := filepath.Join(fixture.runtime, "extensions", "anthropic-compatible-providers.ts")
	wantProvider := "export const provider = 'two';\n"
	if err := os.WriteFile(fixture.providerSrc, []byte(wantProvider), 0644); err != nil {
		t.Fatal(err)
	}

	ensureWorkerDir(t)

	if got := readWorkerFile(t, providerDst); got != wantProvider {
		t.Fatalf("runtime provider = %q, want %q", got, wantProvider)
	}
	meta := readMeta(filepath.Join(fixture.runtime, ".tt-version"))
	wantSHA := fileSHA(fixture.providerSrc)
	if meta["file.extensions/anthropic-compatible-providers.ts.desired_sha"] != wantSHA || meta["file.extensions/anthropic-compatible-providers.ts.actual_sha"] != wantSHA {
		t.Fatalf("provider metadata = %#v", meta)
	}

	customProvider := "export const provider = 'custom';\n"
	if err := os.WriteFile(providerDst, []byte(customProvider), 0644); err != nil {
		t.Fatal(err)
	}
	first := ensureWorkerDir(t)
	second := ensureWorkerDir(t)
	for call, output := range map[string]string{"first": first, "second": second} {
		if !strings.Contains(output, "provider extension is customized") || !strings.Contains(output, "remove it to restore") {
			t.Errorf("%s provider warning = %q", call, output)
		}
	}
	if got := readWorkerFile(t, providerDst); got != customProvider {
		t.Fatalf("custom provider was overwritten: %q", got)
	}
}

func fileDoesNotExist(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		return fmt.Errorf("%s exists with mode %s", path, info.Mode())
	}
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func fileSHAFromString(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
