package tui

import (
	"context"

	"github.com/JuliusBrussee/cavekit/internal/mux"
)

// TerminalTab manages a separate multiplexer session for shell access in
// the worktree.
type TerminalTab struct {
	muxer    mux.Multiplexer
	sessions map[string]string // instance title → terminal session name
	content  string
}

// NewTerminalTab creates a terminal tab.
func NewTerminalTab(m mux.Multiplexer) *TerminalTab {
	return &TerminalTab{
		muxer:    m,
		sessions: make(map[string]string),
	}
}

// EnsureSession creates a terminal session for the instance if it doesn't exist.
// Uses the platform default shell (zsh on Unix, pwsh/cmd on Windows).
func (t *TerminalTab) EnsureSession(ctx context.Context, instanceTitle, worktreePath string) string {
	sessionName := "bp_term_" + instanceTitle

	if _, exists := t.sessions[instanceTitle]; !exists {
		err := t.muxer.CreateSession(ctx, "term_"+instanceTitle, worktreePath, defaultShell())
		if err == nil {
			t.sessions[instanceTitle] = sessionName
		}
	}

	return sessionName
}

// Capture updates the terminal pane content.
func (t *TerminalTab) Capture(ctx context.Context, instanceTitle string) {
	sessionName, exists := t.sessions[instanceTitle]
	if !exists {
		t.content = "Press Enter to open terminal."
		return
	}

	content, err := t.muxer.CapturePane(ctx, sessionName)
	if err != nil {
		t.content = "Terminal session error: " + err.Error()
		return
	}
	t.content = content
}

// Content returns the current terminal content.
func (t *TerminalTab) Content() string {
	if t.content == "" {
		return "Press Enter to open terminal."
	}
	return t.content
}

// HasSession returns true if a terminal session exists for the instance.
func (t *TerminalTab) HasSession(instanceTitle string) bool {
	_, exists := t.sessions[instanceTitle]
	return exists
}

// SessionName returns the multiplexer session name for the instance's terminal.
func (t *TerminalTab) SessionName(instanceTitle string) string {
	return t.sessions[instanceTitle]
}
