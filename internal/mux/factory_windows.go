//go:build windows

package mux

import (
	"github.com/JuliusBrussee/cavekit/internal/exec"
)

// New returns the default multiplexer for Windows — the wt.exe-backed
// implementation. Until T-005b fills in the daemon/ConPTY path, this
// returns a stub that fails cleanly on operations it cannot yet perform.
func New(executor exec.Executor) Multiplexer {
	return newWTMultiplexer(executor)
}
