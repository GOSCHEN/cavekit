package codex

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JuliusBrussee/cavekit/internal/config"
)

// fakeRunner scripts responses for specific (cmd, args) tuples. Arguments are
// compared with a strings.Join fallback so tests read naturally.
type fakeRunner struct {
	responses map[string]response
}

type response struct {
	out []byte
	err error
}

func (f fakeRunner) Run(ctx context.Context, name string, args []string, stdin string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	r, ok := f.responses[key]
	if !ok {
		return nil, ctxError{msg: "unexpected command: " + key}
	}
	return r.out, r.err
}

type ctxError struct{ msg string }

func (e ctxError) Error() string { return e.msg }

func TestParseFindings(t *testing.T) {
	raw := strings.Join([]string{
		"some preamble",
		"| Severity | File | Line | Description |",
		"|----------|------|------|-------------|",
		"| P0 | src/a.go | 42 | Null pointer deref |",
		"| `P1` | `src/b.go` | `12` | `Race condition` |",
		"| P2 | src/c.go | - | Missing doc |",
		"| P3 | src/d.go | n/a | Typo |",
		"| other | src/x | 1 | ignored |",
		"trailing noise",
	}, "\n")

	got := ParseFindings(raw)
	if len(got) != 4 {
		t.Fatalf("got %d findings, want 4: %+v", len(got), got)
	}
	wantFiles := []string{"src/a.go:L42", "src/b.go:L12", "src/c.go", "src/d.go"}
	for i, want := range wantFiles {
		if got[i].File != want {
			t.Errorf("finding %d file = %q, want %q", i, got[i].File, want)
		}
	}
	if got[0].Severity != "P0" || got[0].Description != "Null pointer deref" {
		t.Errorf("row 0 mangled: %+v", got[0])
	}
	if got[1].Severity != "P1" {
		t.Errorf("backticks leaked: %+v", got[1])
	}
}

func TestReviewDisabled(t *testing.T) {
	store := newTestConfig(t)
	_ = store.Set("codex_review", "off", config.ScopeProject)
	findings := &FindingsStore{Path: filepath.Join(t.TempDir(), "findings.md"), Now: time.Now}

	var buf bytes.Buffer
	res, err := Review(context.Background(), ReviewOptions{Runner: fakeRunner{}}, store, findings, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped {
		t.Error("expected skipped")
	}
	if !strings.Contains(buf.String(), "disabled") {
		t.Errorf("missing disabled message: %s", buf.String())
	}
}

func TestReviewDryRun(t *testing.T) {
	store := newTestConfig(t)
	findings := &FindingsStore{Path: filepath.Join(t.TempDir(), "findings.md"), Now: time.Now}

	runner := fakeRunner{responses: map[string]response{
		"git rev-parse --abbrev-ref @{upstream}": {err: ctxError{msg: "no upstream"}},
		"git rev-parse --verify main":            {out: []byte("abc\n")},
		"git diff main...HEAD":                   {out: []byte("diff --git a/x b/x\n+new line\n")},
	}}

	var buf bytes.Buffer
	res, err := Review(context.Background(), ReviewOptions{
		Runner:               runner,
		DryRun:               true,
		AvailabilityOverride: &Availability{BinaryAvailable: true, PluginPresent: true, Available: true},
	}, store, findings, &buf)
	if err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	if !res.DryRun {
		t.Errorf("expected dry-run: %+v\n%s", res, buf.String())
	}
	if !strings.Contains(buf.String(), "DRY RUN") {
		t.Errorf("expected DRY RUN log; got:\n%s", buf.String())
	}
}

func TestReviewNoDiff(t *testing.T) {
	store := newTestConfig(t)
	findings := &FindingsStore{Path: filepath.Join(t.TempDir(), "findings.md"), Now: time.Now}

	runner := fakeRunner{responses: map[string]response{
		"git rev-parse --abbrev-ref @{upstream}": {err: ctxError{msg: "no upstream"}},
		"git rev-parse --verify main":            {out: []byte("abc\n")},
		"git diff main...HEAD":                   {out: []byte("")},
		"git diff main HEAD":                     {out: []byte("")},
	}}

	var buf bytes.Buffer
	res, err := Review(context.Background(), ReviewOptions{
		Runner:               runner,
		AvailabilityOverride: &Availability{Available: true, BinaryAvailable: true, PluginPresent: true},
	}, store, findings, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoDiff {
		t.Errorf("expected NoDiff, got %+v", res)
	}
}

func TestReviewParsesAndAppends(t *testing.T) {
	store := newTestConfig(t)
	_ = store.Set("caveman_mode", "off", config.ScopeProject)
	findings := &FindingsStore{Path: filepath.Join(t.TempDir(), "findings.md"), Now: time.Now}

	codexOut := strings.Join([]string{
		"| Severity | File | Line | Description |",
		"|----------|------|------|-------------|",
		"| P0 | src/a.go | 42 | Null pointer |",
		"| P1 | src/b.go | 12 | Race |",
	}, "\n")

	runner := fakeRunner{responses: map[string]response{
		"git rev-parse --abbrev-ref @{upstream}":                                         {err: ctxError{msg: "no upstream"}},
		"git rev-parse --verify main":                                                    {out: []byte("abc\n")},
		"git diff main...HEAD":                                                           {out: []byte("diff --git a/x b/x\n+new\n")},
		"codex --approval-mode full-auto --model o4-mini --quiet -p " + ReviewPromptFull: {out: []byte(codexOut)},
	}}

	var buf bytes.Buffer
	res, err := Review(context.Background(), ReviewOptions{
		Runner:               runner,
		AvailabilityOverride: &Availability{Available: true, BinaryAvailable: true, PluginPresent: true},
	}, store, findings, &buf)
	if err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	if len(res.Findings) != 2 {
		t.Fatalf("len findings = %d, want 2\nlog:\n%s\nraw:%q", len(res.Findings), buf.String(), res.RawOut)
	}
	if res.Findings[0].ID != "F-001" || res.Findings[1].ID != "F-002" {
		t.Errorf("IDs = %q,%q want F-001,F-002", res.Findings[0].ID, res.Findings[1].ID)
	}
}

func TestReviewCodexFailsFallsBack(t *testing.T) {
	store := newTestConfig(t)
	_ = store.Set("caveman_mode", "off", config.ScopeProject)
	findings := &FindingsStore{Path: filepath.Join(t.TempDir(), "findings.md"), Now: time.Now}

	runner := fakeRunner{responses: map[string]response{
		"git rev-parse --abbrev-ref @{upstream}":                                         {err: ctxError{msg: "no upstream"}},
		"git rev-parse --verify main":                                                    {out: []byte("abc\n")},
		"git diff main...HEAD":                                                           {out: []byte("diff\n")},
		"codex --approval-mode full-auto --model o4-mini --quiet -p " + ReviewPromptFull: {out: []byte("network error"), err: ctxError{msg: "exit 1"}},
	}}

	var buf bytes.Buffer
	res, err := Review(context.Background(), ReviewOptions{
		Runner:               runner,
		AvailabilityOverride: &Availability{Available: true, BinaryAvailable: true, PluginPresent: true},
	}, store, findings, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped {
		t.Errorf("expected skipped after codex failure, got %+v", res)
	}
	if !strings.Contains(buf.String(), "Falling back") {
		t.Errorf("missing fallback message:\n%s", buf.String())
	}
}

func newTestConfig(t *testing.T) *config.Store {
	t.Helper()
	dir := t.TempDir()
	return &config.Store{
		GlobalPath:  filepath.Join(dir, "g", "config"),
		ProjectPath: filepath.Join(dir, "p", "config"),
	}
}
