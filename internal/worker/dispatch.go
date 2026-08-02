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

// EnqueueToWorker assigns id <name>-<turn>, persists the prompt (+nonce),
// logs the task, and appends it to the worker's own queue dir; returns the
// id. Each queue file is line 1 = `<task id> <tier> <nonce> [notify]`,
// remainder = prompt body — the format the tt-worker extension claims.
func EnqueueToWorker(sdir, name, tier string, prompt []byte, notify bool) (string, error) {
	f := filepath.Join(sdir, name+".tasks.jsonl")
	turn := 1
	if data, err := os.ReadFile(f); err == nil && len(data) > 0 {
		turn = strings.Count(string(data), "\n") + 1
	}
	id := fmt.Sprintf("%s-%d", name, turn)
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
	seqf := filepath.Join(sdir, "pool.seq")
	seq := 1
	if data, err := os.ReadFile(seqf); err == nil {
		if v, err2 := strconv.Atoi(strings.TrimSpace(string(data))); err2 == nil {
			seq = v + 1
		}
	}
	if err := os.WriteFile(seqf, []byte(strconv.Itoa(seq)), 0644); err != nil {
		return "", err
	}
	id := fmt.Sprintf("pool-%d", seq)
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
