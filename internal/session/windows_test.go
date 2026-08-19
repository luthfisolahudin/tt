package session

import (
	"encoding/json"
	"os/exec"
	"testing"
)

func TestClaudeCommand(t *testing.T) {
	tests := []struct {
		name      string
		continued bool
		want      string
	}{
		{
			name: "fresh",
			want: "claude --allow-dangerously-skip-permissions",
		},
		{
			name:      "continued falls back to fresh Claude",
			continued: true,
			want:      "claude --continue --allow-dangerously-skip-permissions || claude --allow-dangerously-skip-permissions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := claudeCommand(tt.continued); got != tt.want {
				t.Fatalf("claudeCommand(%t) = %q, want %q", tt.continued, got, tt.want)
			}
		})
	}
}

func TestNormalizeJQUsesSelectedClaudeDefault(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to exercise window normalization")
	}

	tests := []struct {
		name      string
		continued bool
	}{
		{name: "fresh"},
		{name: "continued", continued: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claudeCmd := claudeCommand(tt.continued)
			out, err := runNormalizeJQ("{}", claudeCmd)
			if err != nil {
				t.Fatal(err)
			}
			var wins []Window
			if err := json.Unmarshal([]byte(out), &wins); err != nil {
				t.Fatal(err)
			}
			if len(wins) != 2 || wins[1].Role != "claude" {
				t.Fatalf("normalized windows = %#v, want dev and claude roles", wins)
			}
			if got := wins[1].Panes[0].Cmd; got != claudeCmd {
				t.Fatalf("default Claude pane command = %q, want %q", got, claudeCmd)
			}
		})
	}
}

func TestNormalizeJQPreservesCustomClaudeCommand(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to exercise window normalization")
	}

	tests := []struct {
		name      string
		continued bool
	}{
		{name: "fresh"},
		{name: "continued", continued: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runNormalizeJQ(`{"claude":{"panes":[{"cmd":"pi"}]}}`, claudeCommand(tt.continued))
			if err != nil {
				t.Fatal(err)
			}
			var wins []Window
			if err := json.Unmarshal([]byte(out), &wins); err != nil {
				t.Fatal(err)
			}
			if got := wins[1].Panes[0].Cmd; got != "pi" {
				t.Fatalf("custom Claude pane command = %q, want pi", got)
			}
		})
	}
}

func TestLegacyLaunchClaudeCommandSelection(t *testing.T) {
	for _, tt := range []struct {
		name        string
		paneCommand string
		continued   bool
		want        string
		wantOK      bool
	}{
		{
			name:        "fresh shell",
			paneCommand: "bash",
			want:        DefaultClaudeCmd,
			wantOK:      true,
		},
		{
			name:        "continued shell",
			paneCommand: "zsh",
			continued:   true,
			want:        ContinuedClaudeCmd,
			wantOK:      true,
		},
		{
			name:        "running Claude is not restarted",
			paneCommand: "claude",
			continued:   true,
			wantOK:      false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := claudeCommandForBarePane(tt.paneCommand, claudeCommand(tt.continued))
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("legacy Claude command for %q = (%q, %t), want (%q, %t)", tt.paneCommand, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
