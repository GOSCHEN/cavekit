//go:build windows

package tmux

import (
	"context"
	"errors"
)

// ErrAttachUnsupported is returned by Attacher.Attach on Windows until the
// Windows Terminal multiplexer (T-005) ships. tmux itself does not run on
// Windows natively; Windows users will route through wt.exe via the mux
// abstraction once it lands.
var ErrAttachUnsupported = errors.New("tmux attach not supported on windows; use the Windows Terminal multiplexer")

// Attacher handles full-screen attach/detach to tmux sessions.
// The Windows build is a stub — it satisfies the API surface so cross-OS
// callers compile, but Attach always returns ErrAttachUnsupported.
type Attacher struct {
	mgr *Manager
}

// NewAttacher creates an attacher for the given tmux manager.
func NewAttacher(mgr *Manager) *Attacher {
	return &Attacher{mgr: mgr}
}

// Attach is not implemented on Windows.
func (a *Attacher) Attach(_ context.Context, _ string) (<-chan struct{}, error) {
	done := make(chan struct{})
	close(done)
	return done, ErrAttachUnsupported
}
