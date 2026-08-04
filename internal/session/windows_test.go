package session

import (
	"encoding/json"
	"os/exec"
	"testing"
)

func TestDefaultClaudeCmdStartsFresh(t *testing.T) {
	if DefaultClaudeCmd != "claude --allow-dangerously-skip-permissions" {
		t.Fatalf("DefaultClaudeCmd = %q, want fresh Claude command", DefaultClaudeCmd)
	}
}

func TestNormalizeJQUsesFreshClaudeDefault(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to exercise window normalization")
	}

	out, err := runNormalizeJQ("{}")
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
	if got := wins[1].Panes[0].Cmd; got != DefaultClaudeCmd {
		t.Fatalf("default Claude pane command = %q, want %q", got, DefaultClaudeCmd)
	}
}

func TestNormalizeJQPreservesCustomClaudeCommand(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to exercise window normalization")
	}

	out, err := runNormalizeJQ(`{"claude":{"panes":[{"cmd":"pi"}]}}`)
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
}
