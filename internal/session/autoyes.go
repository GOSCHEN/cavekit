package session

import (
	"context"

	"github.com/JuliusBrussee/cavekit/internal/mux"
)

// AutoYes monitors pane content and auto-approves permission prompts.
type AutoYes struct {
	mux      mux.Multiplexer
	detector *mux.StatusDetector
	enabled  bool
}

// NewAutoYes creates an auto-yes handler.
func NewAutoYes(m mux.Multiplexer, enabled bool) *AutoYes {
	return &AutoYes{
		mux:      m,
		detector: mux.NewStatusDetector(m),
		enabled:  enabled,
	}
}

// Check examines pane status and auto-approves if enabled.
// Returns true if an approval was sent.
func (a *AutoYes) Check(ctx context.Context, name string) bool {
	if !a.enabled {
		return false
	}

	status, err := a.detector.Detect(ctx, name)
	if err != nil {
		return false
	}

	switch status {
	case mux.PanePrompt, mux.PaneTrust:
		// Send Enter to approve the prompt / dismiss the trust dialog.
		a.mux.SendEnter(ctx, name)
		return true
	}

	return false
}

// SetEnabled toggles auto-yes mode.
func (a *AutoYes) SetEnabled(enabled bool) {
	a.enabled = enabled
}

// IsEnabled returns the current state.
func (a *AutoYes) IsEnabled() bool {
	return a.enabled
}
