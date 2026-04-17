// Package mux defines a cross-platform multiplexer abstraction for running
// detached long-lived processes (Claude Code instances, auxiliary shells)
// and interacting with their pseudo-terminals.
//
// Unix targets use tmux under the hood. Windows uses a cavekit-owned daemon
// that fronts ConPTY sessions and is displayed through Windows Terminal
// (wt.exe) tabs. Both back-ends satisfy the Multiplexer interface so upper
// layers stay OS-agnostic.
package mux

import (
	"context"
)

// Multiplexer manages detached terminal sessions.
//
// All methods take a session name — an opaque identifier the multiplexer
// sanitizes per its own rules. Callers should round-trip names through
// SanitizeName before comparing.
type Multiplexer interface {
	// CreateSession starts a new detached session named `name`, spawning
	// `program` in `workDir`. Returns without waiting for the program.
	CreateSession(ctx context.Context, name, workDir, program string) error

	// Exists reports whether a session with the given name is currently live.
	Exists(ctx context.Context, name string) bool

	// Kill terminates a session. Not-found is not an error-worthy case but
	// implementations may still return the underlying error for diagnostics.
	Kill(ctx context.Context, name string) error

	// ListSessions returns the names of all cavekit-managed sessions.
	ListSessions(ctx context.Context) ([]string, error)

	// SendKeys forwards tmux-style key tokens ("Enter", "C-c", "BSpace",
	// "Tab", arbitrary text, ...). Windows back-ends translate these to the
	// appropriate VT input sequences.
	SendKeys(ctx context.Context, name string, keys ...string) error

	// SendEnter is shorthand for SendKeys(name, "Enter").
	SendEnter(ctx context.Context, name string) error

	// SendText sends a block of text followed by a single Enter per line.
	SendText(ctx context.Context, name, text string) error

	// SendCommand types a command line and presses Enter.
	SendCommand(ctx context.Context, name, cmd string) error

	// CapturePane returns the currently visible pane content with ANSI
	// escape sequences preserved.
	CapturePane(ctx context.Context, name string) (string, error)

	// CaptureScrollback returns the full scrollback buffer (not just the
	// visible viewport) with ANSI escapes preserved.
	CaptureScrollback(ctx context.Context, name string) (string, error)
}
