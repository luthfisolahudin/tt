package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestParseUpArgs(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantAttach    bool
		wantContinued bool
		wantErr       string
	}{
		{
			name: "no flags",
		},
		{
			name:          "short continuation",
			args:          []string{"-c"},
			wantContinued: true,
		},
		{
			name:          "long continuation",
			args:          []string{"--continue"},
			wantContinued: true,
		},
		{
			name:          "attach then short continuation",
			args:          []string{"--attach", "-c"},
			wantAttach:    true,
			wantContinued: true,
		},
		{
			name:          "short continuation then attach",
			args:          []string{"-c", "--attach"},
			wantAttach:    true,
			wantContinued: true,
		},
		{
			name:          "attach then long continuation",
			args:          []string{"--attach", "--continue"},
			wantAttach:    true,
			wantContinued: true,
		},
		{
			name:          "long continuation then attach",
			args:          []string{"--continue", "--attach"},
			wantAttach:    true,
			wantContinued: true,
		},
		{
			name:    "unknown flag",
			args:    []string{"--unknown"},
			wantErr: "unknown flag for up: --unknown",
		},
		{
			name:    "unexpected positional",
			args:    []string{"project"},
			wantErr: "up: unexpected argument: project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attach, continued, err := parseUpArgs(tt.args)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("parseUpArgs(%q) error = %v, want %q", tt.args, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if attach != tt.wantAttach || continued != tt.wantContinued {
				t.Fatalf("parseUpArgs(%q) = (%t, %t), want (%t, %t)", tt.args, attach, continued, tt.wantAttach, tt.wantContinued)
			}
		})
	}
}

func TestContinuationFlagsAreRegistered(t *testing.T) {
	rootFlag := rootCmd.Flags().Lookup("continue")
	if rootFlag == nil || rootFlag.Shorthand != "c" {
		t.Fatalf("root continuation flag = %#v, want --continue with -c", rootFlag)
	}
	upFlag := upCmd.Flags().Lookup("continue")
	if upFlag == nil || upFlag.Shorthand != "c" {
		t.Fatalf("up continuation flag = %#v, want --continue with -c", upFlag)
	}
	if upCmd.Flags().Lookup("attach") == nil {
		t.Fatal("up attach flag is not registered")
	}
}

func TestRootContinuationFlagParses(t *testing.T) {
	flags := rootCmd.Flags()
	defer func() {
		if err := flags.Set("continue", "false"); err != nil {
			t.Fatal(err)
		}
	}()

	for _, arg := range []string{"-c", "--continue"} {
		if err := flags.Set("continue", "false"); err != nil {
			t.Fatal(err)
		}
		if err := flags.Parse([]string{arg}); err != nil {
			t.Fatalf("Parse(%q): %v", arg, err)
		}
		continued, err := flags.GetBool("continue")
		if err != nil {
			t.Fatal(err)
		}
		if !continued {
			t.Fatalf("Parse(%q) did not set --continue", arg)
		}
	}
}

func TestContinuationHelp(t *testing.T) {
	tests := []struct {
		name    string
		command *cobra.Command
		want    []string
	}{
		{
			name:    "root",
			command: rootCmd,
			want:    []string{"Usage:\n  tt [--continue] [flags]", "-c, --continue"},
		},
		{
			name:    "up",
			command: upCmd,
			want:    []string{"Usage:\n  tt up [--attach] [--continue] [flags]", "--attach", "-c, --continue"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			originalOutput := tt.command.OutOrStdout()
			tt.command.SetOut(&output)
			defer tt.command.SetOut(originalOutput)
			if err := tt.command.Help(); err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.want {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("help output missing %q:\n%s", want, output.String())
				}
			}
		})
	}
}
