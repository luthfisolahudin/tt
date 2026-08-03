package daemon

import "testing"

// Fixtures are real `tmux capture-pane` output, taken 2026-08-03 from live
// TUIs (see docs/STATUS.md). They are the only way to test the safe-input
// gate without a live orchestrator, so keep them verbatim — re-capture rather
// than hand-edit if a TUI's rendering changes.

// opencode: input empty. The "Ask anything..." placeholder renders only while
// the input is empty, in both the splash and mid-conversation layouts.
const opencodeIdle = `
                    ┃
                    ┃  Ask anything... "Fix a TODO in the codebase"
                    ┃
                    ┃  Build · Claude Opus 5 Anthropic · high
                    ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
                    tab agents  ctrl+p commands
  /tmp
`

// opencode: a draft replaces the placeholder.
const opencodeDraft = `
                    ┃
                    ┃  draft text here
                    ┃
                    ┃  Build · Claude Opus 5 Anthropic · high
                    ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
                    tab agents  ctrl+p commands
  /tmp
`

// opencode: mid-turn. The footer swaps to an interrupt hint.
const opencodeBusy = `
     ~ Writing command...

     ▣  Build · Claude Opus 5

  ┃  Build · Claude Opus 5 Anthropic · high
  ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
   ■ ■ ■ ▬▬  esc interrupt
`

// pi: empty input — a lone reverse-video cursor between the two dividers.
const piIdleEscaped = "\x1b[38;2;255;95;255m────────────────────────────\n" +
	"\x1b[7m\x1b[39m \x1b[0m\n" +
	"\x1b[38;2;255;95;255m────────────────────────────\n" +
	"\x1b[39m \x1b[38;2;138;190;183mpi-lens\x1b[39m\n" +
	"\x1b[38;2;102;102;102m~/code/tt (main)\x1b[39m\n" +
	"LSP Inactive\x1b[39m\n"

// pi: a draft sits before the cursor on the same input line.
const piDraftEscaped = "\x1b[38;2;255;95;255m────────────────────────────\n" +
	"\x1b[39mmy draft here\x1b[7m \x1b[0m\n" +
	"\x1b[38;2;255;95;255m────────────────────────────\n" +
	"\x1b[39m \x1b[38;2;138;190;183mpi-lens\x1b[39m\n" +
	"LSP Inactive\x1b[39m\n"

// pi: mid-turn spinner.
const piBusyPlain = `
 ⠇ Working...

────────────────────────────

────────────────────────────
 pi-lens
`

func TestClassifyOpencode(t *testing.T) {
	tests := []struct {
		name  string
		plain string
		want  string
	}{
		{"idle placeholder is safe", opencodeIdle, "safe_empty"},
		{"draft is not safe", opencodeDraft, "wait_real_input"},
		{"mid-turn is not safe", opencodeBusy, "wait_active"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyOpencode(tc.plain, tc.plain)
			if got.classifier != tc.want {
				t.Fatalf("classifier = %q, want %q", got.classifier, tc.want)
			}
			if got.tui != "opencode" {
				t.Fatalf("tui = %q, want opencode", got.tui)
			}
		})
	}
}

func TestClassifyPi(t *testing.T) {
	tests := []struct {
		name    string
		plain   string
		escaped string
		want    string
	}{
		{"empty input box is safe", piIdleEscaped, piIdleEscaped, "safe_empty"},
		{"draft is not safe", piDraftEscaped, piDraftEscaped, "wait_real_input"},
		{"spinner is not safe", piBusyPlain, piBusyPlain, "wait_active"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyPi(tc.plain, tc.escaped)
			if got.classifier != tc.want {
				t.Fatalf("classifier = %q, want %q", got.classifier, tc.want)
			}
		})
	}
}

// The Claude Code path must not drift while the other TUIs are added — these
// mirror the states the bash classifier was tuned against.
func TestClassifyClaudeUnchanged(t *testing.T) {
	tests := []struct {
		name    string
		plain   string
		escaped string
		want    string
	}{
		{"empty prompt is safe", "\n❯ \n", "\n❯ \n", "safe_empty"},
		{"dim suggestion is safe", "\n❯ try this\n", "\n❯ \x1b[2mtry this\x1b[0m\n", "safe_suggestion"},
		{"queued banner is safe", "\n2 queued messages\n❯ \n", "\n❯ \n", "safe_queued"},
		{"interrupt hint is not safe", "\nesc interrupt\n❯ \n", "\n❯ \n", "wait_active"},
		{"active status is not safe", "\nChurning…\n❯ \n", "\n❯ \n", "wait_active"},
		{"real draft is not safe", "\n❯ hello\n", "\n❯ hello\n", "wait_real_input"},
		{"no prompt at all", "\nnothing here\n", "\nnothing here\n", "wait_no_prompt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyClaude(tc.plain, tc.escaped)
			if got.classifier != tc.want {
				t.Fatalf("classifier = %q, want %q", got.classifier, tc.want)
			}
		})
	}
}

// piInputLine locates the box between the LAST two dividers; without two
// dividers there is no input box to read.
func TestPiInputLine(t *testing.T) {
	if got := piInputLine("no dividers here"); got != "" {
		t.Fatalf("expected empty for missing dividers, got %q", got)
	}
	if got := piInputLine(piDraftEscaped); got == "" {
		t.Fatal("expected the draft line between dividers")
	}
}

// Only these three classifications may open the delivery gate.
func TestAcceptsInputGate(t *testing.T) {
	safe := map[string]bool{"safe_empty": true, "safe_suggestion": true, "safe_queued": true}
	for _, c := range []string{"wait_active", "wait_real_input", "wait_no_prompt", "capture_error"} {
		if safe[c] {
			t.Fatalf("%q must not be treated as safe", c)
		}
	}
}

// An empty pi box must classify safe even when the input line carries no
// visible text at all (the pane_current_command is "node", so detection and
// classification both have to work off the box structure).
func TestClassifyPiEmptyBoxNoCursor(t *testing.T) {
	pane := "────────────────────────────\n\n────────────────────────────\n/tmp\nLSP Inactive\n"
	if got := classifyPi(pane, pane); got.classifier != "safe_empty" {
		t.Fatalf("classifier = %q, want safe_empty", got.classifier)
	}
	if n := piDividerCount(pane); n != 2 {
		t.Fatalf("piDividerCount = %d, want 2", n)
	}
	if got := detectTUIFromContent(pane); got != "pi" {
		t.Fatalf("detectTUIFromContent = %q, want pi", got)
	}
}

func TestDetectTUIFromContent(t *testing.T) {
	cases := []struct{ name, pane, want string }{
		{"opencode by placeholder", opencodeIdle, "opencode"},
		{"opencode by footer", "\ntab agents  ctrl+p commands\n", "opencode"},
		{"claude by prompt char", "\n❯ \n", "claude"},
		{"pi by divider box", piIdleEscaped, "pi"},
		{"pi by spinner", piBusyPlain, "pi"},
		{"unknown falls back to claude", "just some text\n", "claude"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectTUIFromContent(tc.pane); got != tc.want {
				t.Fatalf("detectTUIFromContent = %q, want %q", got, tc.want)
			}
		})
	}
}
