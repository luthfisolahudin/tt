package cmd

import (
	"fmt"
	"os"
)

// die prints the bash-style "tt: <msg>" error and exits 1.
func die(msg string) {
	fmt.Fprintf(os.Stderr, "tt: %s\n", msg)
	osExit(1)
}

// note prints the bash-style "[tt] <msg>" info to stderr.
func note(msg string) {
	fmt.Fprintf(os.Stderr, "[tt] %s\n", msg)
}

// osExit is the process exit hook (kept separate so tests can intercept).
func osExit(code int) {
	os.Exit(code)
}
