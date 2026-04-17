package mux

import (
	"context"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"auth", "bp_auth"},
		{"build feature", "bp_build_feature"},
		{"with.dots", "bp_with_dots"},
		{"with:colons", "bp_with_colons"},
		{"bp_already_prefixed", "bp_already_prefixed"},
	}
	for _, tt := range tests {
		if got := SanitizeName(tt.in); got != tt.want {
			t.Errorf("SanitizeName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSanitizeNameIdempotent(t *testing.T) {
	once := SanitizeName("my site")
	twice := SanitizeName(once)
	if once != twice {
		t.Errorf("not idempotent: once=%q twice=%q", once, twice)
	}
}

func TestSessionNameAliasesSanitize(t *testing.T) {
	if SessionName("x") != SanitizeName("x") {
		t.Errorf("SessionName should alias SanitizeName")
	}
}

func TestPaneStatusString(t *testing.T) {
	cases := map[PaneStatus]string{
		PaneActive:  "active",
		PanePrompt:  "prompt",
		PaneTrust:   "trust",
		PaneIdle:    "idle",
		PaneUnknown: "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("PaneStatus(%d).String() = %q, want %q", s, got, want)
		}
	}
}

// fakeMux lets StatusDetector tests run without tmux or wt present.
type fakeMux struct {
	capture func(name string) string
}

func (f *fakeMux) CreateSession(context.Context, string, string, string) error { return nil }
func (f *fakeMux) Exists(context.Context, string) bool                         { return true }
func (f *fakeMux) Kill(context.Context, string) error                          { return nil }
func (f *fakeMux) ListSessions(context.Context) ([]string, error)              { return nil, nil }
func (f *fakeMux) SendKeys(context.Context, string, ...string) error           { return nil }
func (f *fakeMux) SendEnter(context.Context, string) error                     { return nil }
func (f *fakeMux) SendText(context.Context, string, string) error              { return nil }
func (f *fakeMux) SendCommand(context.Context, string, string) error           { return nil }
func (f *fakeMux) CapturePane(_ context.Context, name string) (string, error) {
	return f.capture(name), nil
}
func (f *fakeMux) CaptureScrollback(ctx context.Context, name string) (string, error) {
	return f.CapturePane(ctx, name)
}

func TestStatusDetectorClassifies(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    PaneStatus
	}{
		{"trust", "Do you trust the files in this folder?", PaneTrust},
		{"prompt", "Allow once\nAllow always", PanePrompt},
		{"active", "something else", PaneActive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &fakeMux{capture: func(string) string { return tt.output }}
			d := NewStatusDetector(m)
			got, err := d.Detect(ctx(), "x")
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatusDetectorDetectsIdle(t *testing.T) {
	m := &fakeMux{capture: func(string) string { return "stable output" }}
	d := NewStatusDetector(m)
	// First detect = active (no prior sample).
	if s, _ := d.Detect(ctx(), "x"); s != PaneActive {
		t.Errorf("first detect = %v, want active", s)
	}
	// Second detect with identical content = idle.
	if s, _ := d.Detect(ctx(), "x"); s != PaneIdle {
		t.Errorf("second detect = %v, want idle", s)
	}
}
