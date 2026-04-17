package mux

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
)

// PaneStatus indicates what the multiplexer pane is doing.
type PaneStatus int

const (
	PaneUnknown PaneStatus = iota
	// PaneActive means the pane content is changing — the agent is working.
	PaneActive
	// PanePrompt means Claude is waiting for the user to approve an action.
	PanePrompt
	// PaneTrust means Claude is showing its one-time trust prompt.
	PaneTrust
	// PaneIdle means content has not changed since the previous sample.
	PaneIdle
)

func (s PaneStatus) String() string {
	switch s {
	case PaneActive:
		return "active"
	case PanePrompt:
		return "prompt"
	case PaneTrust:
		return "trust"
	case PaneIdle:
		return "idle"
	default:
		return "unknown"
	}
}

// Markers are shared across tmux and wt back-ends because they come from
// Claude Code's output, not the multiplexer.
var permissionPromptMarkers = []string{
	"No, and tell Claude what to do differently",
	"Allow once",
	"Allow always",
	"(Y)es",
}

var trustPromptMarkers = []string{
	"Do you trust the files in this folder?",
	"Trust this project",
}

// StatusDetector samples a pane and classifies what the agent is doing.
// Works with any Multiplexer — holds per-session content hashes to detect
// idle vs. active transitions.
type StatusDetector struct {
	mux      Multiplexer
	lastHash map[string]string
}

// NewStatusDetector wires a detector to a multiplexer.
func NewStatusDetector(m Multiplexer) *StatusDetector {
	return &StatusDetector{
		mux:      m,
		lastHash: make(map[string]string),
	}
}

// Detect captures pane content and classifies the current pane status.
func (d *StatusDetector) Detect(ctx context.Context, name string) (PaneStatus, error) {
	content, err := d.mux.CapturePane(ctx, name)
	if err != nil {
		return PaneUnknown, err
	}

	if containsAny(content, trustPromptMarkers) {
		return PaneTrust, nil
	}
	if containsAny(content, permissionPromptMarkers) {
		return PanePrompt, nil
	}

	hash := hashContent(content)
	session := SanitizeName(name)
	prev, seen := d.lastHash[session]
	d.lastHash[session] = hash

	if !seen {
		return PaneActive, nil
	}
	if hash != prev {
		return PaneActive, nil
	}
	return PaneIdle, nil
}

func hashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h[:8])
}

func containsAny(content string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
}
