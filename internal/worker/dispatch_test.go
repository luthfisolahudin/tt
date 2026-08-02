package worker

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestEnqueueToWorkerFormat pins the on-disk task format the tt-worker
// extension depends on: line 1 = `<id> <tier> <nonce>[ notify]`, remainder =
// prompt body ending in `\nnonce: <nonce>\n`; tasks.jsonl row
// {"turn":N,"id":"<cs>-N","sent_at":<epoch>,"tier":"...","nonce":"...","notify":0|1};
// prompt file = body + "\nnonce: <nonce>\n"; turn = line count + 1.
func TestEnqueueToWorkerFormat(t *testing.T) {
	sdir := t.TempDir()
	prompt := []byte("TASK: do the thing\nFILES: a.go\n")
	id, err := EnqueueToWorker(sdir, "alfa", "default", prompt, false)
	if err != nil {
		t.Fatal(err)
	}
	if id != "alfa-1" {
		t.Fatalf("id = %q, want alfa-1", id)
	}

	// <cs>.in.<turn>.txt = prompt + "\nnonce: <nonce>\n"
	in, err := os.ReadFile(filepath.Join(sdir, "alfa.in.1.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(in), string(prompt)) {
		t.Fatalf("prompt file missing prompt prefix")
	}
	nonceRe := regexp.MustCompile(`\nnonce: ([0-9a-f]{16})\n$`)
	m := nonceRe.FindStringSubmatch(string(in))
	if m == nil {
		t.Fatalf("prompt file missing trailing \\nnonce: <16hex>\\n: %q", in)
	}
	nonce := m[1]

	// <cs>.queue/<turn>.task: line1 = "<id> <tier> <nonce>[ notify]"
	task, err := os.ReadFile(filepath.Join(sdir, "alfa.queue", "1.task"))
	if err != nil {
		t.Fatal(err)
	}
	nl := strings.IndexByte(string(task), '\n')
	head := string(task[:nl])
	fields := strings.Fields(head)
	if len(fields) != 3 || fields[0] != "alfa-1" || fields[1] != "default" || fields[2] != nonce {
		t.Fatalf("queue head = %q, want 'alfa-1 default <nonce>'", head)
	}
	if string(task) != head+"\n"+string(in) {
		t.Fatalf("queue file != head + prompt body")
	}

	// tasks.jsonl row
	row, err := os.ReadFile(filepath.Join(sdir, "alfa.tasks.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"turn":1,"id":"alfa-1","sent_at":` + regexp.MustCompile(`"sent_at":(\d+)`).FindStringSubmatch(string(row))[1] + `,"tier":"default","nonce":"` + nonce + `","notify":0}`
	if string(row) != want+"\n" {
		t.Fatalf("tasks.jsonl = %q, want %q", row, want)
	}

	// second send -> turn 2
	id2, err := EnqueueToWorker(sdir, "alfa", "default", prompt, true)
	if err != nil {
		t.Fatal(err)
	}
	if id2 != "alfa-2" {
		t.Fatalf("id2 = %q, want alfa-2", id2)
	}
	head2, err := os.ReadFile(filepath.Join(sdir, "alfa.queue", "2.task"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(head2), "alfa-2 default ") || !strings.HasSuffix(strings.SplitN(string(head2), "\n", 2)[0], " notify") {
		t.Fatalf("queue 2 head should end with ' notify': %q", strings.SplitN(string(head2), "\n", 2)[0])
	}
}

// TestEnqueuePoolFormat pins the shared-pool format: id pool-<seq>, counter
// pool.seq, and a queue/<seq>.task with the same line-1 shape.
func TestEnqueuePoolFormat(t *testing.T) {
	sdir := t.TempDir()
	prompt := []byte("TASK: pool task\n")
	id, err := EnqueuePool(sdir, "default", prompt, false)
	if err != nil {
		t.Fatal(err)
	}
	if id != "pool-1" {
		t.Fatalf("id = %q, want pool-1", id)
	}
	seq, err := os.ReadFile(filepath.Join(sdir, "pool.seq"))
	if err != nil {
		t.Fatal(err)
	}
	if string(seq) != "1" {
		t.Fatalf("pool.seq = %q, want 1", seq)
	}
	task, err := os.ReadFile(filepath.Join(sdir, "queue", "1.task"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(task), "pool-1 default ") {
		t.Fatalf("pool task head = %q", strings.SplitN(string(task), "\n", 2)[0])
	}
	// second pool task -> seq 2
	id2, err := EnqueuePool(sdir, "default", prompt, false)
	if err != nil {
		t.Fatal(err)
	}
	if id2 != "pool-2" {
		t.Fatalf("id2 = %q, want pool-2", id2)
	}
}

// TestClearMarker pins the clear marker appended to tasks.jsonl (never
// truncating) and the gen bump.
func TestClearMarker(t *testing.T) {
	sdir := t.TempDir()
	if _, err := EnqueueToWorker(sdir, "alfa", "default", []byte("TASK: x\n"), false); err != nil {
		t.Fatal(err)
	}
	if CurrentGen(sdir, "alfa") != 0 {
		t.Fatal("gen should start at 0")
	}
	BumpGen(sdir, "alfa")
	if CurrentGen(sdir, "alfa") != 1 {
		t.Fatalf("gen = %d, want 1", CurrentGen(sdir, "alfa"))
	}
	before, _ := os.ReadFile(filepath.Join(sdir, "alfa.tasks.jsonl"))
	lines := strings.Count(string(before), "\n")
	if lines != 1 {
		t.Fatalf("tasks.jsonl should have 1 line before clear, got %d", lines)
	}
	// appendLine is the mechanism; verify append keeps prior rows.
	if err := appendLine(filepath.Join(sdir, "alfa.tasks.jsonl"), `{"clear":1,"at":12345}`); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(sdir, "alfa.tasks.jsonl"))
	if !strings.HasPrefix(string(after), string(before)) {
		t.Fatalf("clear marker must not truncate tasks.jsonl")
	}
	if !strings.Contains(string(after), `{"clear":1,"at":12345}`) {
		t.Fatalf("clear marker missing from tasks.jsonl")
	}
	if strings.Count(string(after), "\n") != 2 {
		t.Fatalf("tasks.jsonl should have 2 lines after clear marker")
	}
	// next turn counts the marker: 3 lines -> turn 3
	id, err := EnqueueToWorker(sdir, "alfa", "default", []byte("TASK: y\n"), false)
	if err != nil {
		t.Fatal(err)
	}
	if id != "alfa-3" {
		t.Fatalf("id = %q, want alfa-3 (turn counts clear markers)", id)
	}
}

// TestNonceLength pins the openssl rand -hex 8 format: 16 lowercase hex chars.
func TestNonceLength(t *testing.T) {
	for i := 0; i < 5; i++ {
		n := Nonce()
		if len(n) != 16 {
			t.Fatalf("nonce = %q, want 16 hex chars", n)
		}
		for _, r := range n {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
				t.Fatalf("nonce %q has non-hex char", n)
			}
		}
	}
}

// TestAtomicWrite pins the .tmp + mv pattern.
func TestAtomicWrite(t *testing.T) {
	sdir := t.TempDir()
	p := filepath.Join(sdir, "x")
	if err := AtomicWrite(p, "hello"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "hello" {
		t.Fatalf("content = %q", data)
	}
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("leftover .tmp file")
	}
}
