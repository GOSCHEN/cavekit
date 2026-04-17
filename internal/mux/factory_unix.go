//go:build !windows

package mux

import (
	"github.com/JuliusBrussee/cavekit/internal/exec"
)

// New returns the default multiplexer for this OS. On Unix that means
// tmux — the `tmux` binary must be installed and discoverable in PATH.
func New(executor exec.Executor) Multiplexer {
	return NewTmuxAdapter(executor)
}
