package codex

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JuliusBrussee/cavekit/internal/config"
)

func TestParseDesignFindings(t *testing.T) {
	raw := strings.Join([]string{
		"preamble",
		"| Category | Severity | Cavekit | Requirement | Description |",
		"|----------|----------|---------|-------------|-------------|",
		"| decomposition | critical | kit-session.md | R-3 | Session kit is too broad |",
		"| ambiguity | advisory | `kit-build.md` | `R-7` | `Vague acceptance criteria` |",
		"| scope | WEIRD | kit-x.md | R-9 | Invalid severity defaults advisory |",
		"| other | critical | kit-y.md | R-1 | Unknown category ignored |",
	}, "\n")

	got := ParseDesignFindings(raw)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %+v", len(got), got)
	}
	if got[0].Category != "decomposition" || got[0].Severity != "critical" {
		t.Errorf("row 0 = %+v", got[0])
	}
	if got[1].Severity != "advisory" || got[1].Requirement != "R-7" {
		t.Errorf("row 1 backticks/severity = %+v", got[1])
	}
	if got[2].Severity != "advisory" {
		t.Errorf("row 2 fallback = %+v", got[2])
	}
}

func TestParseDesignFindingsNoIssues(t *testing.T) {
	if got := ParseDesignFindings("NO_ISSUES"); got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if got := ParseDesignFindings("no_issues here"); got != nil {
		t.Errorf("got %v, want nil (case-insensitive)", got)
	}
}

func TestSplit(t *testing.T) {
	findings := []DesignFinding{
		{Severity: "critical"}, {Severity: "advisory"}, {Severity: "critical"},
	}
	crit, adv := Split(findings)
	if len(crit) != 2 || len(adv) != 1 {
		t.Errorf("crit=%d adv=%d", len(crit), len(adv))
	}
}

func TestLoadKits(t *testing.T) {
	dir := t.TempDir()
	for i, name := range []string{"cavekit-a.md", "cavekit-b.md", "notes.txt", "cavekit-c.md"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("content "+string(rune('0'+i))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	contents, count, err := loadKits(dir)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	// Ensure the non-matching file was skipped and contents are ordered.
	if strings.Contains(contents, "notes.txt") {
		t.Error("notes.txt leaked into contents")
	}
	if !strings.Contains(contents, "--- FILE: cavekit-a.md ---") {
		t.Errorf("missing cavekit-a marker:\n%s", contents)
	}
}

func TestDesignChallengeHappyPath(t *testing.T) {
	cfg := newTestConfig(t)
	_ = cfg.Set("caveman_mode", "off", config.ScopeProject)

	kits := t.TempDir()
	if err := os.WriteFile(filepath.Join(kits, "cavekit-demo.md"), []byte("# demo kit\nR-1: do a thing"), 0o644); err != nil {
		t.Fatal(err)
	}

	codexOut := strings.Join([]string{
		"| Category | Severity | Cavekit | Requirement | Description |",
		"|----------|----------|---------|-------------|-------------|",
		"| scope | critical | cavekit-demo.md | R-1 | Too vague |",
	}, "\n")

	fullPrompt := DesignPromptFull // caveman off
	contents, _, err := loadKits(kits)
	if err != nil {
		t.Fatal(err)
	}
	key := "codex --approval-mode full-auto --model o4-mini --quiet -p " + fullPrompt + contents

	runner := fakeRunner{responses: map[string]response{
		key: {out: []byte(codexOut)},
	}}

	frozen := time.Unix(1_700_000_000, 0)
	clockCall := 0
	clock := func() time.Time {
		clockCall++
		return frozen.Add(time.Duration(clockCall) * time.Second)
	}

	var buf bytes.Buffer
	res, err := DesignChallenge(context.Background(), DesignOptions{
		KitsDir:              kits,
		Runner:               runner,
		AvailabilityOverride: &Availability{Available: true, BinaryAvailable: true, PluginPresent: true},
		Clock:                clock,
	}, cfg, &buf)
	if err != nil {
		t.Fatalf("err=%v\n%s", err, buf.String())
	}
	if len(res.Findings) != 1 {
		t.Fatalf("got %d findings: %+v", len(res.Findings), res.Findings)
	}
	if !res.HasCritical {
		t.Error("expected HasCritical")
	}
}

func TestDesignChallengeDryRun(t *testing.T) {
	cfg := newTestConfig(t)
	_ = cfg.Set("caveman_mode", "off", config.ScopeProject)
	kits := t.TempDir()
	_ = os.WriteFile(filepath.Join(kits, "cavekit-x.md"), []byte("x"), 0o644)

	var buf bytes.Buffer
	res, err := DesignChallenge(context.Background(), DesignOptions{
		KitsDir:              kits,
		DryRun:               true,
		AvailabilityOverride: &Availability{Available: true, BinaryAvailable: true, PluginPresent: true},
	}, cfg, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun {
		t.Errorf("got %+v", res)
	}
}

func TestDesignChallengeSkippedCodexOff(t *testing.T) {
	cfg := newTestConfig(t)
	_ = cfg.Set("codex_review", "off", config.ScopeProject)

	var buf bytes.Buffer
	res, err := DesignChallenge(context.Background(), DesignOptions{
		AvailabilityOverride: &Availability{Available: true},
	}, cfg, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped {
		t.Errorf("want skipped")
	}
}

func TestDesignChallengeCycleAwaitFixes(t *testing.T) {
	cfg := newTestConfig(t)
	_ = cfg.Set("caveman_mode", "off", config.ScopeProject)
	kits := t.TempDir()
	_ = os.WriteFile(filepath.Join(kits, "cavekit-a.md"), []byte("R-1"), 0o644)

	contents, _, _ := loadKits(kits)
	codexOut := "| Category | Severity | Cavekit | Requirement | Description |\n" +
		"|-|-|-|-|-|\n" +
		"| scope | critical | cavekit-a.md | R-1 | bad |\n"

	runner := fakeRunner{responses: map[string]response{
		"codex --approval-mode full-auto --model o4-mini --quiet -p " + DesignPromptFull + contents: {out: []byte(codexOut)},
	}}

	var buf bytes.Buffer
	res, err := DesignChallengeCycle(context.Background(), DesignCycleOptions{
		KitsDir:              kits,
		MaxCycles:            2,
		Runner:               runner,
		AvailabilityOverride: &Availability{Available: true, BinaryAvailable: true, PluginPresent: true},
	}, cfg, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != DesignCycleAwaitFixes {
		t.Errorf("outcome=%v want AwaitFixes\n%s", res.Outcome, buf.String())
	}
	if !strings.Contains(buf.String(), "AWAITING_FIXES") {
		t.Errorf("missing AWAITING_FIXES:\n%s", buf.String())
	}
}

func TestFormatAdvisoryForUser(t *testing.T) {
	findings := []DesignFinding{
		{Category: "scope", Severity: "advisory", Cavekit: "a.md", Requirement: "R-1", Description: "foo"},
	}
	out := FormatAdvisoryForUser(findings)
	if !strings.Contains(out, "| scope | a.md | R-1 | foo |") {
		t.Errorf("missing row:\n%s", out)
	}
}

func TestFormatCriticalForFix(t *testing.T) {
	findings := []DesignFinding{
		{Category: "scope", Severity: "critical", Cavekit: "a.md", Requirement: "R-1", Description: "foo"},
		{Category: "scope", Severity: "advisory", Cavekit: "b.md", Requirement: "R-2", Description: "bar"},
	}
	out := FormatCriticalForFix(findings)
	if strings.Contains(out, "b.md") {
		t.Errorf("advisory leaked:\n%s", out)
	}
	if !strings.Contains(out, "a.md|R-1|scope|foo") {
		t.Errorf("missing critical:\n%s", out)
	}
}
