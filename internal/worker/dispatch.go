package worker

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Nonce returns a fresh 16-hex-char task nonce — the format of
// `openssl rand -hex 8`, sourced from crypto/rand (no openssl dependency).
func Nonce() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// AtomicWrite writes content to path via <path>.tmp + mv — the one
// concurrency primitive of the control channel.
func AtomicWrite(path, content string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// NextSeq returns the next value of the session-wide task counter, the source
// of every task id.
//
// The counter is session-wide, not per worker, because task ids must never be
// reused: results are addressed by id under `results/<id>.result`, and removing
// a worker deletes its `<cs>.tasks.jsonl`. A turn derived from that file's line
// count therefore restarted at 1 on the next spawn, so a fresh `alfa-1` aliased
// the previous incarnation's result. A callsign names a slot, not an identity.
//
// `task.seq` is seeded from existing state on first use so ids minted before
// this counter existed can never be re-issued.
func NextSeq(sdir string) (int, error) {
	seqf := filepath.Join(sdir, "task.seq")
	seq := 0
	if data, err := os.ReadFile(seqf); err == nil {
		if v, err2 := strconv.Atoi(strings.TrimSpace(string(data))); err2 == nil {
			seq = v
		}
	}
	if seq == 0 {
		seq = highestExistingSeq(sdir)
	}
	seq++
	if err := os.WriteFile(seqf, []byte(strconv.Itoa(seq)), 0644); err != nil {
		return 0, err
	}
	return seq, nil
}

// highestExistingSeq finds the largest task number already issued in this
// session, across the retired per-worker counters and the pool's own.
func highestExistingSeq(sdir string) int {
	max := 0
	if data, err := os.ReadFile(filepath.Join(sdir, "pool.seq")); err == nil {
		if v, err2 := strconv.Atoi(strings.TrimSpace(string(data))); err2 == nil && v > max {
			max = v
		}
	}
	entries, err := os.ReadDir(filepath.Join(sdir, "results"))
	if err != nil {
		return max
	}
	for _, e := range entries {
		base := strings.TrimSuffix(e.Name(), ".result")
		i := strings.LastIndex(base, "-")
		if i < 0 {
			continue
		}
		if v, err2 := strconv.Atoi(base[i+1:]); err2 == nil && v > max {
			max = v
		}
	}
	return max
}

// mintTaskID allocates the next id for a callsign (or for "pool") that no
// result file already claims.
//
// The counter alone ought to be enough, but the durable result store is the
// real authority on what has been issued: if `task.seq` is lost or rolled back
// with the state dir, a stale counter would mint an id whose result already
// exists and silently overwrite someone's work. The check is one stat and
// makes aliasing impossible regardless of the counter's state.
func mintTaskID(sdir, prefix string) (int, string, error) {
	rdir := filepath.Join(sdir, "results")
	for attempt := 0; attempt < 1000; attempt++ {
		seq, err := NextSeq(sdir)
		if err != nil {
			return 0, "", err
		}
		id := fmt.Sprintf("%s-%d", prefix, seq)
		if !FileExists(filepath.Join(rdir, id+".result")) {
			return seq, id, nil
		}
	}
	return 0, "", fmt.Errorf("no unused task id for %q after 1000 attempts", prefix)
}

// EnqueueToWorker assigns id <name>-<seq>, persists the prompt (+nonce),
// logs the task, and appends it to the worker's own queue dir; returns the
// id. Each queue file is line 1 = `<task id> <tier> <nonce> [notify]`,
// remainder = prompt body — the format the tt-worker extension claims.
func EnqueueToWorker(sdir, name, tier string, prompt []byte, notify bool) (string, error) {
	f := filepath.Join(sdir, name+".tasks.jsonl")
	turn, id, err := mintTaskID(sdir, name)
	if err != nil {
		return "", err
	}
	nonce := Nonce()
	promptFile := filepath.Join(sdir, fmt.Sprintf("%s.in.%d.txt", name, turn))
	content := append(append([]byte{}, prompt...), []byte(fmt.Sprintf("\nnonce: %s\n", nonce))...)
	if err := os.WriteFile(promptFile, content, 0644); err != nil {
		return "", err
	}
	n := 0
	if notify {
		n = 1
	}
	line := fmt.Sprintf(`{"turn":%d,"id":"%s","sent_at":%d,"tier":"%s","nonce":"%s","notify":%d}`, turn, id, time.Now().Unix(), tier, nonce, n)
	if err := appendLine(f, line); err != nil {
		return "", err
	}
	qdir := filepath.Join(sdir, name+".queue")
	if err := os.MkdirAll(qdir, 0755); err != nil {
		return "", err
	}
	nf := ""
	if notify {
		nf = " notify"
	}
	head := fmt.Sprintf("%s %s %s%s\n", id, tier, nonce, nf)
	return id, AtomicWrite(filepath.Join(qdir, fmt.Sprintf("%d.task", turn)), head+string(content))
}

// EnqueuePool drops a task on the shared pool queue (id pool-<seq>) — the
// auto path when every worker is busy at the cap. Any idle worker steals it.
func EnqueuePool(sdir, tier string, prompt []byte, notify bool) (string, error) {
	pooldir := filepath.Join(sdir, "queue")
	if err := os.MkdirAll(pooldir, 0755); err != nil {
		return "", err
	}
	seq, id, err := mintTaskID(sdir, "pool")
	if err != nil {
		return "", err
	}
	nonce := Nonce()
	promptFile := filepath.Join(sdir, fmt.Sprintf("pool.%d.txt", seq))
	content := append(append([]byte{}, prompt...), []byte(fmt.Sprintf("\nnonce: %s\n", nonce))...)
	if err := os.WriteFile(promptFile, content, 0644); err != nil {
		return "", err
	}
	nf := ""
	if notify {
		nf = " notify"
	}
	head := fmt.Sprintf("%s %s %s%s\n", id, tier, nonce, nf)
	return id, AtomicWrite(filepath.Join(pooldir, fmt.Sprintf("%d.task", seq)), head+string(content))
}

// WriteSteerFile writes the steer channel atomically — an immediate injection
// the extension delivers into the worker's current turn.
func WriteSteerFile(sdir, name, body string) error {
	return AtomicWrite(filepath.Join(sdir, name+".steer"), body)
}

// WriteResumeFile writes the resume trigger atomically (presence-only signal;
// content mirrors bash's epoch stamp).
func WriteResumeFile(sdir, name string) error {
	return AtomicWrite(filepath.Join(sdir, name+".resume"), fmt.Sprintf("%d", time.Now().Unix()))
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}
