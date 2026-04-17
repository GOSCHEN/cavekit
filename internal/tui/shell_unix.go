//go:build !windows

package tui

import "os"

// defaultShell returns the terminal tab's default interactive shell on Unix.
// Prefers $SHELL when set (honors user preference, e.g. bash on Linux), falls
// back to zsh which is the macOS default and widely available on Linux.
func defaultShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "zsh"
}
