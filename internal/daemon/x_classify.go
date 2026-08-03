package daemon

import (
	"regexp"
	"strings"

	"github.com/luthfisolahudin/tt/internal/tmux"
)

// --- the safe-input classifier ----------------------------------------------
//
// The orchestrator is a live TUI with no file control channel, so delivery
// must wait for a safe input state: it rejects in-flight/interrupt states and
// a non-empty draft, and treats an empty prompt (and Claude Code's dim
// suggestion / queued-message banners) as safe.
//
// Three TUIs are supported, each with its own idle/busy tells — a single set
// of heuristics cannot cover them, which is why `x send` used to hang forever
// against anything but Claude Code:
//
//	claude    prompt char `❯`; busy shows "esc interrupt"/"Churning";
//	          dim-suggestion and queued-message states are also safe.
//	opencode  busy shows "esc interrupt"; an EMPTY input renders the
//	          placeholder "Ask anything..." (in splash and mid-conversation
//	          alike), which a draft replaces.
//	pi        busy shows "Working..."; the input line sits between the last
//	          two `───` dividers and is whitespace-only when empty.
//
// Detection prefers `pane_current_command`, falling back to content sniffing
// so a wrapper process (e.g. a shell exec'ing the TUI) still resolves.

var (
	reInterrupt = regexp.MustCompile(`(?i)esc interrupt|ctrl\+c cancel|ctrl\+c to cancel`)
	reQueued    = regexp.MustCompile(`(?i)queued messages?|paste again to expand`)
	reActive    = regexp.MustCompile(`(?i)Churning|Blanching`)
	reANSICSI   = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	// NBSP is a literal char in the source string (Go compiles \u00a0);
	// regexp itself cannot parse the \u escape, so feed it the character.
	reSpaceOnly  = regexp.MustCompile("^[[:space:]\u00a0]*$")
	reSuggestion = regexp.MustCompile("^[[:space:]\u00a0]*\x1b\\[2m")
	reCursorSugg = regexp.MustCompile("^[[:space:]\u00a0]*\x1b\\[7m.\x1b\\[0;2m")

	// opencode: placeholder shown only while the input is empty.
	reOpencodeEmpty = regexp.MustCompile(`(?i)Ask anything\.\.\.`)
	// opencode footer, used to recognize the TUI by content.
	reOpencodeMark = regexp.MustCompile(`(?i)tab agents|ctrl\+p commands|Ask anything\.\.\.`)
	// pi: the spinner line while a turn is in flight.
	rePiBusy = regexp.MustCompile(`(?i)Working\.\.\.`)
	// pi: a horizontal rule line (the input box borders), ANSI already stripped.
	rePiDivider = regexp.MustCompile(`^[[:space:]]*─{10,}[[:space:]]*$`)
)

// xClassification is the full classifier output — the bash X_CLASSIFIER /
// X_UNSAFE_MARKER / X_PROMPT_PLAIN / X_PROMPT_ESCAPED / X_STRIPPED_AFTER
// globals, returned as one value so the observe loop can record them all.
type xClassification struct {
	classifier    string
	unsafeMarker  string
	promptPlain   string
	promptEscaped string
	strippedAfter string
	tui           string
}

func lastLineWith(s, marker string) (string, bool) {
	line := ""
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, marker) {
			line = l
		}
	}
	return line, line != ""
}

// escapeESC is bash's `perl -pe 's/\e/\\e/g'`: ESC -> the two chars `\e`.
func escapeESC(s string) string {
	return strings.ReplaceAll(s, "\x1b", `\e`)
}

// detectTUI resolves which orchestrator TUI owns the target's claude pane.
// pane_current_command is authoritative when it names a known TUI; otherwise
// the pane content decides (a TUI launched via a wrapper reports the wrapper).
func detectTUI(target, plain string) string {
	if cmd, err := tmux.PaneCurrentCommand("=" + target + ":claude"); err == nil {
		switch strings.TrimSpace(cmd) {
		case "claude":
			return "claude"
		case "opencode":
			return "opencode"
		case "pi":
			return "pi"
		}
	}
	return detectTUIFromContent(plain)
}

// detectTUIFromContent is the pure fallback: which TUI does this pane look
// like? Used when pane_current_command names a wrapper rather than the TUI —
// notably pi, which runs as a node grandchild and reports "node".
func detectTUIFromContent(plain string) string {
	if reOpencodeMark.MatchString(plain) {
		return "opencode"
	}
	if strings.Contains(plain, "❯") {
		return "claude"
	}
	// An EMPTY pi prompt has no content to match on, so recognize the input
	// box structurally — count the dividers, not the text.
	if rePiBusy.MatchString(plain) || piDividerCount(plain) >= 2 {
		return "pi"
	}
	return "claude" // historical default
}

// piDividerCount counts pi's horizontal rule lines (the input box borders).
func piDividerCount(plain string) int {
	n := 0
	for _, l := range strings.Split(plain, "\n") {
		if rePiDivider.MatchString(reANSICSI.ReplaceAllString(l, "")) {
			n++
		}
	}
	return n
}

// piInputLine returns the raw content between the LAST two divider lines —
// pi's input box. Empty string when the layout is not recognized.
func piInputLine(plain string) string {
	lines := strings.Split(plain, "\n")
	var dividers []int
	for i, l := range lines {
		if rePiDivider.MatchString(reANSICSI.ReplaceAllString(l, "")) {
			dividers = append(dividers, i)
		}
	}
	if len(dividers) < 2 {
		return ""
	}
	top, bottom := dividers[len(dividers)-2], dividers[len(dividers)-1]
	if bottom-top < 1 {
		return ""
	}
	return strings.Join(lines[top+1:bottom], "\n")
}

// xClassifyClaudeInput classifies the target's orchestrator pane. (Name kept
// for the observe schema; it dispatches per TUI.)
func xClassifyClaudeInput(target string) xClassification {
	c := xClassification{}
	plain, err := tmux.CapturePanePlain(target, "claude", 12)
	if err != nil {
		c.classifier = "capture_error"
		return c
	}
	tui := detectTUI(target, plain)
	// Only the escaped capture distinguishes an empty input from a draft, so
	// fetch it lazily with the depth each TUI's layout needs.
	depth := 8
	if tui == "pi" {
		depth = 12
	}
	escaped, eerr := tmux.CapturePaneEscaped(target, "claude", depth)
	if eerr != nil {
		return xClassification{tui: tui, classifier: "capture_error"}
	}
	switch tui {
	case "opencode":
		return classifyOpencode(plain, escaped)
	case "pi":
		return classifyPi(plain, escaped)
	}
	return classifyClaude(plain, escaped)
}

// classifyOpencode: busy shows an interrupt hint; an empty input renders the
// "Ask anything..." placeholder, which any draft replaces.
func classifyOpencode(plain, escaped string) xClassification {
	c := xClassification{tui: "opencode"}
	if reInterrupt.MatchString(plain) {
		c.classifier = "wait_active"
		c.unsafeMarker = "interrupt/cancel"
		return c
	}
	if reOpencodeEmpty.MatchString(plain) {
		c.classifier = "safe_empty"
		return c
	}
	c.promptEscaped = escapeESC(escaped)
	c.classifier = "wait_real_input"
	c.unsafeMarker = "draft"
	return c
}

// classifyPi: busy shows the "Working..." spinner; the input box between the
// last two dividers is whitespace-only when empty.
func classifyPi(plain, escaped string) xClassification {
	c := xClassification{tui: "pi"}
	if rePiBusy.MatchString(plain) || reInterrupt.MatchString(plain) {
		c.classifier = "wait_active"
		c.unsafeMarker = "working"
		return c
	}
	// Prefer the escaped capture: pi renders the empty input as a lone
	// reverse-video cursor, which survives as whitespace once ANSI is stripped.
	if piDividerCount(escaped) < 2 && piDividerCount(plain) < 2 {
		c.classifier = "wait_no_prompt" // not pi's input box, or not rendered yet
		return c
	}
	input := piInputLine(escaped)
	if strings.TrimSpace(reANSICSI.ReplaceAllString(input, "")) == "" {
		// An empty box is the safe state; keep whichever capture showed it.
		if alt := piInputLine(plain); strings.TrimSpace(reANSICSI.ReplaceAllString(alt, "")) != "" {
			input = alt
		}
	}
	c.promptEscaped = escapeESC(input)
	c.strippedAfter = reANSICSI.ReplaceAllString(input, "")
	if reSpaceOnly.MatchString(c.strippedAfter) {
		c.classifier = "safe_empty"
		return c
	}
	c.classifier = "wait_real_input"
	c.unsafeMarker = "draft"
	return c
}

// classifyClaude is the original Claude Code heuristic set, unchanged.
func classifyClaude(plain, escaped string) xClassification {
	c := xClassification{tui: "claude"}
	if reInterrupt.MatchString(plain) {
		c.classifier = "wait_active"
		c.unsafeMarker = "interrupt/cancel"
		return c
	}
	if reQueued.MatchString(plain) {
		c.classifier = "safe_queued"
		c.unsafeMarker = "queued-message"
		return c
	}
	if reActive.MatchString(plain) {
		c.classifier = "wait_active"
		c.unsafeMarker = "active-status"
		return c
	}
	prompt, ok := lastLineWith(escaped, "❯")
	if !ok {
		c.classifier = "wait_no_prompt"
		return c
	}
	c.promptEscaped = escapeESC(prompt)
	if pp, ok2 := lastLineWith(plain, "❯"); ok2 {
		c.promptPlain = pp
	}
	after := prompt
	if i := strings.Index(after, "❯"); i >= 0 {
		// bash's `${prompt#*❯}` removes the whole character, not one byte.
		after = after[i+len("❯"):]
	}
	c.strippedAfter = reANSICSI.ReplaceAllString(after, "")
	if reSpaceOnly.MatchString(c.strippedAfter) {
		c.classifier = "safe_empty"
		return c
	}
	if reSuggestion.MatchString(after) || reCursorSugg.MatchString(after) {
		c.classifier = "safe_suggestion"
		return c
	}
	c.classifier = "wait_real_input"
	return c
}

func xClaudeAcceptsInput(target string) bool {
	switch xClassifyClaudeInput(target).classifier {
	case "safe_empty", "safe_suggestion", "safe_queued":
		return true
	}
	return false
}
