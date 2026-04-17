package build

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JuliusBrussee/cavekit/internal/codex"
	"github.com/JuliusBrussee/cavekit/internal/config"
)

func newCfg(t *testing.T) *config.Store {
	t.Helper()
	dir := t.TempDir()
	return &config.Store{
		GlobalPath:  filepath.Join(dir, "g", "config"),
		ProjectPath: filepath.Join(dir, "p", "config"),
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverSitesPlansFirst(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "context", "plans", "build-site.md"), "| T-001 |")
	writeFile(t, filepath.Join(root, "context", "sites", "build-site-legacy.md"), "| T-002 |")
	writeFile(t, filepath.Join(root, "context", "plans", "overview.md"), "ignored")

	var buf bytes.Buffer
	got, err := DiscoverSites(root, "", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d candidates: %+v", len(got), got)
	}
	// Plans come first in the discovery order.
	if !strings.Contains(got[0].Path, "plans") {
		t.Errorf("plans should sort first: %+v", got)
	}
	// Overview skipped
	for _, c := range got {
		if strings.Contains(c.Path, "overview") {
			t.Errorf("overview leaked: %+v", c)
		}
	}
}

func TestDiscoverSitesFilter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "context", "plans", "build-site-v1.md"), "x")
	writeFile(t, filepath.Join(root, "context", "plans", "build-site-v2.md"), "y")

	var buf bytes.Buffer
	got, err := DiscoverSites(root, "v2", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0].Path, "v2") {
		t.Errorf("filter failed: %+v", got)
	}

	buf.Reset()
	got, err = DiscoverSites(root, "nomatch", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("fallback failed: %+v", got)
	}
	if !strings.Contains(buf.String(), "matched no build sites") {
		t.Errorf("expected fallback warning: %s", buf.String())
	}
}

func TestCountTasksAndDone(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "context", "plans", "build-site.md")
	writeFile(t, site, strings.Join([]string{
		"| Task | Desc |",
		"|------|------|",
		"| T-001 | thing |",
		"| T-002 | thing |",
		"| T-003 | thing |",
	}, "\n"))

	if got := countTasks(site); got != 3 {
		t.Errorf("total = %d, want 3", got)
	}

	impl := filepath.Join(root, "context", "impl", "impl-x.md")
	writeFile(t, impl, strings.Join([]string{
		"Build site: " + site,
		"",
		"| T-001 | DONE | ok |",
		"| T-002 | DONE | ok |",
	}, "\n"))

	done, err := countDoneTasks(root, site)
	if err != nil {
		t.Fatal(err)
	}
	if done != 2 {
		t.Errorf("done = %d, want 2", done)
	}
}

func TestMaybeArchiveSkipsWhenIncomplete(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "context", "plans", "build-site.md")
	writeFile(t, site, "| T-001 |\n| T-002 |")
	impl := filepath.Join(root, "context", "impl", "impl-x.md")
	writeFile(t, impl, "Build site: "+site+"\n| T-001 | DONE |\n")

	var buf bytes.Buffer
	count, _, err := MaybeArchivePriorCycle(root, site, time.Now(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("archived = %d, want 0 (incomplete tasks)", count)
	}
	if !strings.Contains(buf.String(), "Incomplete") {
		t.Errorf("missing incomplete msg: %s", buf.String())
	}
	if !fileExists(impl) {
		t.Error("impl file was moved prematurely")
	}
}

func TestMaybeArchiveWhenAllDone(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "context", "plans", "build-site.md")
	writeFile(t, site, "| T-001 |\n| T-002 |")
	impl := filepath.Join(root, "context", "impl", "impl-x.md")
	writeFile(t, impl, "Build site: "+site+"\n| T-001 | DONE |\n| T-002 | DONE |\n")

	var buf bytes.Buffer
	count, dir, err := MaybeArchivePriorCycle(root, site, time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("archived = %d, want 1", count)
	}
	if fileExists(impl) {
		t.Error("impl file should have been moved")
	}
	if !strings.Contains(dir, "20260417-120000") {
		t.Errorf("archive dir = %q", dir)
	}
}

func TestBuildRalphPromptEmbedsFrontierAndCompletion(t *testing.T) {
	opts := Options{
		CompletionPromise: "ALL DONE",
		ReviewInterval:    3,
	}
	prompt := BuildRalphPrompt("context/plans/foo.md", []string{"context/kits/a.md"}, opts, false)
	if !strings.Contains(prompt, "context/plans/foo.md") {
		t.Error("frontier missing")
	}
	if !strings.Contains(prompt, "ALL DONE") {
		t.Error("completion missing")
	}
	if !strings.Contains(prompt, "context/kits/a.md") {
		t.Error("spec listing missing")
	}
}

func TestBuildRalphPromptPeerReviewCLI(t *testing.T) {
	opts := Options{PeerReview: true, ReviewInterval: 2, CompletionPromise: "x"}
	prompt := BuildRalphPrompt("s.md", nil, opts, true)
	if !strings.Contains(prompt, "cavekit codex review --base main") {
		t.Errorf("CLI review block missing:\n%s", prompt)
	}
}

func TestBuildRalphPromptPeerReviewMCP(t *testing.T) {
	opts := Options{PeerReview: true, ReviewInterval: 2, CompletionPromise: "x"}
	prompt := BuildRalphPrompt("s.md", nil, opts, false)
	if !strings.Contains(prompt, "MCP Legacy") {
		t.Errorf("MCP review block missing:\n%s", prompt)
	}
}

func TestRunSingleCandidate(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "context", "plans", "build-site.md")
	writeFile(t, site, "| T-001 | Do thing |")
	writeFile(t, filepath.Join(root, "context", "kits", "cavekit-demo.md"), "demo")

	var buf bytes.Buffer
	res, err := Run(root, Options{
		AvailabilityOverride: &codex.Availability{Available: false},
		Now:                  func() time.Time { return time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC) },
	}, newCfg(t), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if res.FrontierFile != site {
		t.Errorf("frontier = %q want %q", res.FrontierFile, site)
	}
	if !fileExists(filepath.Join(root, ".claude", "ralph-loop.local.md")) {
		t.Error("ralph state not written")
	}
}

func TestRunMultipleCandidatesRequiresSelection(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "context", "plans", "build-site-a.md"), "| T-001 |")
	writeFile(t, filepath.Join(root, "context", "plans", "build-site-b.md"), "| T-002 |")

	var buf bytes.Buffer
	res, err := Run(root, Options{}, newCfg(t), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !res.SelectionRequired {
		t.Error("expected selection required")
	}
	if !strings.Contains(buf.String(), "CAVEKIT_SITE_SELECTION_REQUIRED=true") {
		t.Errorf("missing selection marker:\n%s", buf.String())
	}
}

func TestEnsureMCPConfig(t *testing.T) {
	root := t.TempDir()
	added, err := ensureMCPConfig(root, "gpt-5.4")
	if err != nil {
		t.Fatal(err)
	}
	if !added {
		t.Error("expected mcp config to be added")
	}

	data, _ := os.ReadFile(filepath.Join(root, ".mcp.json"))
	if !strings.Contains(string(data), "codex-reviewer") {
		t.Errorf("mcp json missing codex-reviewer: %s", data)
	}

	// Second call should be idempotent.
	added, err = ensureMCPConfig(root, "gpt-5.4")
	if err != nil {
		t.Fatal(err)
	}
	if added {
		t.Error("second call should not add (idempotent)")
	}
}
