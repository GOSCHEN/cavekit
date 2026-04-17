package tui

import (
	"context"

	"github.com/JuliusBrussee/cavekit/internal/mux"
)

// PreviewTab captures and renders multiplexer pane content for the selected instance.
type PreviewTab struct {
	muxer      mux.Multiplexer
	content    string
	scrollMode bool
}

// NewPreviewTab creates a preview tab.
func NewPreviewTab(m mux.Multiplexer) *PreviewTab {
	return &PreviewTab{muxer: m}
}

// Capture refreshes the pane content for the given session.
func (p *PreviewTab) Capture(ctx context.Context, sessionName string) {
	if sessionName == "" {
		p.content = ""
		return
	}

	var content string
	var err error
	if p.scrollMode {
		content, err = p.muxer.CaptureScrollback(ctx, sessionName)
	} else {
		content, err = p.muxer.CapturePane(ctx, sessionName)
	}
	if err != nil {
		p.content = "Failed to capture: " + err.Error()
		return
	}
	p.content = content
}

// Content returns the current captured content.
func (p *PreviewTab) Content() string {
	if p.content == "" {
		return "No instance selected or instance is paused."
	}
	return p.content
}

// SetScrollMode toggles full scrollback capture.
func (p *PreviewTab) SetScrollMode(enabled bool) {
	p.scrollMode = enabled
}

// IsScrollMode returns true if in scroll mode.
func (p *PreviewTab) IsScrollMode() bool {
	return p.scrollMode
}
