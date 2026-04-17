//go:build !windows

package tmux

import (
	"os/exec"

	"golang.org/x/term"
)

// rawState wraps the opaque term.State so callers can hold onto it across
// attach/detach without importing golang.org/x/term directly.
type rawState = term.State

// makeRaw puts the terminal attached to fd into raw mode and returns the
// previous state so restoreTerminal can undo it. Works on Linux, macOS and
// other Unix targets via golang.org/x/term.
func makeRaw(fd uintptr) (*rawState, error) {
	return term.MakeRaw(int(fd))
}

// restoreTerminal returns the terminal at fd to the state captured by makeRaw.
func restoreTerminal(fd uintptr, state *rawState) {
	if state == nil {
		return
	}
	_ = term.Restore(int(fd), state)
}

// buildCommand creates an exec.Cmd without using the executor (attach needs a
// raw process whose stdio we can splice ourselves).
func buildCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
