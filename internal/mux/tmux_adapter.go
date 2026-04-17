package mux

import (
	"context"

	"github.com/JuliusBrussee/cavekit/internal/exec"
	"github.com/JuliusBrussee/cavekit/internal/tmux"
)

// NewTmuxAdapter returns a Multiplexer backed by internal/tmux.Manager.
// Exposed so tests (on any OS) can wire the tmux adapter against a mock
// executor without going through the OS-specific factory. Production code
// on Windows uses the wt.exe multiplexer via New(); this constructor is
// intended for explicit, test-scoped wiring.
func NewTmuxAdapter(executor exec.Executor) Multiplexer {
	return &tmuxAdapter{mgr: tmux.NewManager(executor)}
}

// tmuxAdapter bridges internal/tmux.Manager to the Multiplexer interface.
// It is a thin pass-through; the tmux package keeps its existing behaviour
// untouched so regressions on Linux and macOS are impossible by design.
type tmuxAdapter struct {
	mgr *tmux.Manager
}

func (a *tmuxAdapter) CreateSession(ctx context.Context, name, workDir, program string) error {
	return a.mgr.CreateSession(ctx, name, workDir, program)
}

func (a *tmuxAdapter) Exists(ctx context.Context, name string) bool {
	return a.mgr.Exists(ctx, name)
}

func (a *tmuxAdapter) Kill(ctx context.Context, name string) error {
	return a.mgr.Kill(ctx, name)
}

func (a *tmuxAdapter) ListSessions(ctx context.Context) ([]string, error) {
	return a.mgr.ListSessions(ctx)
}

func (a *tmuxAdapter) SendKeys(ctx context.Context, name string, keys ...string) error {
	return a.mgr.SendKeys(ctx, name, keys...)
}

func (a *tmuxAdapter) SendEnter(ctx context.Context, name string) error {
	return a.mgr.SendEnter(ctx, name)
}

func (a *tmuxAdapter) SendText(ctx context.Context, name, text string) error {
	return a.mgr.SendText(ctx, name, text)
}

func (a *tmuxAdapter) SendCommand(ctx context.Context, name, cmd string) error {
	return a.mgr.SendCommand(ctx, name, cmd)
}

func (a *tmuxAdapter) CapturePane(ctx context.Context, name string) (string, error) {
	return a.mgr.CapturePane(ctx, name)
}

func (a *tmuxAdapter) CaptureScrollback(ctx context.Context, name string) (string, error) {
	return a.mgr.CaptureScrollback(ctx, name)
}
