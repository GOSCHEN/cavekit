package codex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"

	"github.com/JuliusBrussee/cavekit/internal/config"
)

// ReviewPromptCaveman is emitted when caveman mode is active.
const ReviewPromptCaveman = "Senior engineer. Adversarial code review. Check diff for bugs, security holes, logic errors, spec violations. Each finding = one row in markdown table: Severity, File, Line, Description. Severity: P0 (critical) | P1 (high) | P2 (medium) | P3 (low). No issues found = output NO_FINDINGS alone."

// ReviewPromptFull is the default prompt.
const ReviewPromptFull = "You are a senior engineer performing adversarial code review. Review the following diff for bugs, security issues, logic errors, and spec violations. For each finding output exactly one row in a markdown table with columns: Severity, File, Line, Description. Severity must be one of P0 (critical), P1 (high), P2 (medium), P3 (low). If no issues found, output exactly the word NO_FINDINGS on its own line and nothing else."

// ReviewPrompt returns the correct prompt for the current config state.
func ReviewPrompt(s *config.Store) string {
	active, _ := s.CavemanActive("build")
	if active {
		return ReviewPromptCaveman
	}
	return ReviewPromptFull
}

// ReviewOptions bundles the knobs exposed by `cavekit codex review`.
type ReviewOptions struct {
	BaseRef string
	DryRun  bool
	// Runner executes the git+codex commands. Replaced in tests.
	Runner CommandRunner
	// AvailabilityOverride lets tests (and other callers that have already
	// probed the environment) skip the Detect() call inside Review.
	AvailabilityOverride *Availability
}

// CommandRunner abstracts execution for tests. Input is sent on stdin; stdout
// and stderr are merged on return.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, stdin string) ([]byte, error)
}

// ExecRunner is the production runner. It shells out via os/exec.
type ExecRunner struct{}

// Run executes cmd with args, feeding stdin and returning combined output.
func (ExecRunner) Run(ctx context.Context, name string, args []string, stdin string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

// ReviewResult describes the outcome of a Review call.
type ReviewResult struct {
	Skipped  bool   // Codex disabled or unavailable
	DryRun   bool   // Dry-run; no Codex call made
	NoDiff   bool   // Diff was empty
	Findings []Finding
	RawOut   string // Raw Codex output (empty on skip/dry-run)
	DiffSize int
}

// Review runs the adversarial review end-to-end.
func Review(ctx context.Context, opts ReviewOptions, cfgStore *config.Store, findings *FindingsStore, w io.Writer) (ReviewResult, error) {
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}

	if cfgStore.GetWithDefault("codex_review", "auto") == "off" {
		fmt.Fprintln(w, "[ck:review] Codex review is disabled (codex_review=off). Skipping.")
		return ReviewResult{Skipped: true}, nil
	}

	var avail Availability
	if opts.AvailabilityOverride != nil {
		avail = *opts.AvailabilityOverride
	} else {
		avail = Detect(ctx)
	}
	if !avail.Available {
		fmt.Fprintln(w, "[ck:review] Codex is not available. Falling back to inspector-only review.")
		return ReviewResult{Skipped: true}, nil
	}

	baseRef := opts.BaseRef
	if baseRef == "" {
		baseRef = detectBaseRef(ctx, runner)
	}

	fmt.Fprintf(w, "[ck:review] Computing diff %s...HEAD\n", baseRef)
	diff, err := computeDiff(ctx, runner, baseRef)
	if err != nil {
		return ReviewResult{}, err
	}
	if diff == "" {
		fmt.Fprintln(w, "[ck:review] No diff found. Nothing to review.")
		return ReviewResult{NoDiff: true}, nil
	}
	diffLines := strings.Count(diff, "\n") + 1
	fmt.Fprintf(w, "[ck:review] Diff is %d lines. Sending to Codex...\n", diffLines)

	model := cfgStore.GetWithDefault("codex_model", "o4-mini")
	prompt := ReviewPrompt(cfgStore)
	codexArgs := []string{"--approval-mode", "full-auto", "--model", model, "--quiet", "-p", prompt}

	if opts.DryRun {
		fmt.Fprintf(w, "[ck:review] DRY RUN — would execute: codex %s <<< <diff>\n", strings.Join(codexArgs, " "))
		return ReviewResult{DryRun: true, DiffSize: diffLines}, nil
	}

	raw, runErr := runner.Run(ctx, "codex", codexArgs, diff)
	rawStr := string(raw)
	if runErr != nil {
		fmt.Fprintln(w, "[ck:review] Codex invocation failed. Falling back to inspector-only review.")
		fmt.Fprintf(w, "[ck:review] Error: %s\n", truncate(rawStr, 500))
		return ReviewResult{Skipped: true, RawOut: rawStr}, nil
	}

	if containsFold(rawStr, "NO_FINDINGS") {
		fmt.Fprintln(w, "[ck:review] Codex found no issues. Clean review.")
		return ReviewResult{RawOut: rawStr}, nil
	}

	fmt.Fprintln(w, "[ck:review] Parsing Codex findings...")
	parsed := ParseFindings(rawStr)
	if len(parsed) == 0 {
		fmt.Fprintln(w, "[ck:review] Could not parse findings from Codex output.")
		fmt.Fprintf(w, "[ck:review] Raw (first 1000 chars): %s\n", truncate(rawStr, 1000))
		return ReviewResult{RawOut: rawStr}, nil
	}

	appended := make([]Finding, 0, len(parsed))
	for _, f := range parsed {
		f.Source = "codex"
		f.Tier = 0
		id, err := findings.Append(Finding{
			Severity:    f.Severity,
			File:        f.File,
			Description: f.Description,
			Source:      "codex",
			Tier:        tierFromSeverity(f.Severity),
		})
		if err != nil {
			return ReviewResult{RawOut: rawStr, Findings: appended}, err
		}
		f.ID = id
		appended = append(appended, f)
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "[ck:review] === Codex Adversarial Review Findings ===")
	for _, f := range appended {
		fmt.Fprintf(w, "| %s: %s (source: codex) | %s | %s | NEW | — |\n", f.ID, f.Description, f.Severity, f.File)
	}
	fmt.Fprintln(w, "[ck:review] === End of Findings ===")
	fmt.Fprintf(w, "[ck:review] Findings appended to %s\n", findings.Path)

	return ReviewResult{Findings: appended, RawOut: rawStr, DiffSize: diffLines}, nil
}

func tierFromSeverity(sev string) int {
	switch sev {
	case "P0":
		return 1
	case "P1":
		return 2
	case "P2":
		return 3
	case "P3":
		return 4
	}
	return 0
}

// ParseFindings extracts rows of the form `| Severity | File | Line |
// Description |` from free-form Codex output. Header and separator rows are
// ignored; backticks are stripped from cell values to tolerate inline code
// spans produced by some models.
func ParseFindings(raw string) []Finding {
	var out []Finding
	sevRe := regexp.MustCompile(`(?m)\|\s*` + "`?" + `P[0-3]`)
	for _, rawLine := range strings.Split(raw, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		if line == "" {
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "|-") || strings.HasPrefix(trimmed, "| -") {
			continue
		}
		if strings.HasPrefix(trimmed, "| Severity") {
			continue
		}
		if !sevRe.MatchString(line) {
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) < 6 {
			continue
		}
		severity := cleanCell(cols[1])
		file := cleanCell(cols[2])
		lineno := cleanCell(cols[3])
		description := cleanCell(cols[4])

		fileRef := file
		if lineno != "" && lineno != "-" && !strings.EqualFold(lineno, "n/a") {
			fileRef = fmt.Sprintf("%s:L%s", file, lineno)
		}
		out = append(out, Finding{
			Severity:    severity,
			File:        fileRef,
			Description: description,
		})
	}
	return out
}

func cleanCell(s string) string {
	s = strings.ReplaceAll(s, "`", "")
	return strings.TrimSpace(s)
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// detectBaseRef probes the git repo for a sensible diff base.
func detectBaseRef(ctx context.Context, runner CommandRunner) string {
	if out, err := runner.Run(ctx, "git", []string{"rev-parse", "--abbrev-ref", "@{upstream}"}, ""); err == nil {
		ref := strings.TrimSpace(string(out))
		if ref != "" {
			return ref
		}
	}
	for _, cand := range []string{"main", "master", "develop"} {
		if _, err := runner.Run(ctx, "git", []string{"rev-parse", "--verify", cand}, ""); err == nil {
			return cand
		}
	}
	return "HEAD~10"
}

var errEmptyDiff = errors.New("empty diff")

func computeDiff(ctx context.Context, runner CommandRunner, base string) (string, error) {
	if out, err := runner.Run(ctx, "git", []string{"diff", base + "...HEAD"}, ""); err == nil && len(bytes.TrimSpace(out)) > 0 {
		return string(out), nil
	}
	if out, err := runner.Run(ctx, "git", []string{"diff", base, "HEAD"}, ""); err == nil && len(bytes.TrimSpace(out)) > 0 {
		return string(out), nil
	}
	return "", nil
}
