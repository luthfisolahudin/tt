package worker

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ResultHeadFile reads a result file's head: "<id> <status>" (id defaults to
// "-"). Missing file -> ("", false).
func ResultHeadFile(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	id, status := "", ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "id: ") && id == "":
			id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "status: ") && status == "":
			status = strings.TrimPrefix(line, "status: ")
		}
		if id != "" && status != "" {
			break
		}
	}
	if id == "" {
		id = "-"
	}
	return id + " " + status, true
}

// ResultTextFile returns everything after the first "---" separator line —
// the same as the bash `sed -n '/^---$/,$p' | sed '1d'`.
func ResultTextFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if line == "---" {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	return ""
}

// ResultText reads the worker's latest-pointer body.
func ResultText(sdir, name string) string {
	return ResultTextFile(filepath.Join(sdir, name+".result"))
}

// ResultPathByID is the unified per-id result store path.
func ResultPathByID(sdir, id string) string {
	return filepath.Join(sdir, "results", id+".result")
}

// ResultHeadByID reads the per-id store head.
func ResultHeadByID(sdir, id string) (string, bool) {
	return ResultHeadFile(ResultPathByID(sdir, id))
}

// ResultTextByID reads the per-id store body.
func ResultTextByID(sdir, id string) string {
	return ResultTextFile(ResultPathByID(sdir, id))
}

// ResultTimesFile returns the head's started_at/ended_at epoch strings
// (empty when absent) — the timestamps the extension stamps.
func ResultTimesFile(path string) (string, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	sa, ea := "", ""
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "started_at: ") && sa == "":
			sa = strings.TrimPrefix(line, "started_at: ")
		case strings.HasPrefix(line, "ended_at: ") && ea == "":
			ea = strings.TrimPrefix(line, "ended_at: ")
		}
	}
	return sa, ea
}

// ResultDurationFile returns M:SS wall-clock of a recorded task, or "-" when
// both timestamps are not present.
func ResultDurationFile(path string) string {
	sa, ea := ResultTimesFile(path)
	if isDigits(sa) && isDigits(ea) {
		s, _ := strconv.Atoi(sa)
		e, _ := strconv.Atoi(ea)
		return FmtElapsed(e - s)
	}
	return "-"
}

// ResultField pulls the LAST "<field>: value" line from a result body — the
// WORKER_DONE/BLOCKED block is terminal, so the last occurrence wins.
func ResultField(text, field string) string {
	val := ""
	prefix := field + ": "
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			val = strings.TrimPrefix(line, prefix)
		}
	}
	return val
}

// ResultReasonHint is a short one-line hint for status interrupted/blocked
// rows: the blocked reason or done summary, else the first non-blank line,
// truncated to 80 chars.
func ResultReasonHint(text string) string {
	r := ResultField(text, "reason")
	if r == "" {
		r = ResultField(text, "summary")
	}
	if r == "" {
		for _, line := range strings.Split(text, "\n") {
			if strings.TrimSpace(line) != "" {
				r = line
				break
			}
		}
	}
	return truncateRunes(r, 80)
}

// JSONEscape escapes a string the way the bash json_escape does: backslash,
// double quote, and the three whitespace controls — nothing else, so the
// emitted envelopes match bash byte-for-byte.
func JSONEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// EmitResultJSON builds one task's --json result envelope, byte-identical to
// the bash emit_result_json (duration_s derived from the per-id store head).
func EmitResultJSON(sdir, id, status, text string) string {
	fc := ResultField(text, "files_changed")
	sm := ResultField(text, "summary")
	nt := ResultField(text, "notes")
	rs := ResultField(text, "reason")
	sa, ea, dur := "null", "null", "null"
	f := ResultPathByID(sdir, id)
	if FileExists(f) {
		saRaw, eaRaw := ResultTimesFile(f)
		if isDigits(saRaw) {
			sa = saRaw
		}
		if isDigits(eaRaw) {
			ea = eaRaw
		}
		if sa != "null" && ea != "null" {
			s, _ := strconv.Atoi(sa)
			e, _ := strconv.Atoi(ea)
			dur = strconv.Itoa(e - s)
		}
	}
	var b strings.Builder
	b.WriteString(`{"id":"` + JSONEscape(id) + `","status":"` + JSONEscape(status) +
		`","summary":"` + JSONEscape(sm) + `","files_changed":"` + JSONEscape(fc) +
		`","notes":"` + JSONEscape(nt) + `","reason":"` + JSONEscape(rs) +
		`","started_at":` + sa + `,"ended_at":` + ea + `,"duration_s":` + dur)
	if status == "other" || status == "error" {
		// bash's $(result_text_file) command substitution strips trailing
		// newlines before json_escape — match it for byte parity.
		b.WriteString(`,"text":"` + JSONEscape(strings.TrimRight(text, "\n")) + `"`)
	}
	b.WriteString("}")
	return b.String()
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
