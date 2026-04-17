package codex

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/JuliusBrussee/cavekit/internal/config"
)

// DesignPromptCaveman is the compressed prompt used when caveman mode is on
// for the draft phase.
const DesignPromptCaveman = `Senior architect. Adversarial design review of cavekit specs. CHALLENGE design, not rubber-stamp.

Review all kits as whole system. Design-level concerns only:

1. **Domain Decomposition** — boundaries right? Over/under-scoped? Better decomposition possible?
2. **Requirement Coverage** — missing reqs? Gaps between domains? Cross-refs cover all interactions?
3. **Ambiguity** — criteria vague? Checkbox reqs? Implementer know exactly what to build?
4. **Scope** — domains over/under-scoped? Implicit assumptions?
5. **Cross-Domain Coherence** — domains fit together? Contradictions? Hidden circular deps?

Rules: NO impl feedback. MUST propose alternative decomposition if better exists. Reference cavekit files + R-numbers.

Output: one row per finding in markdown table: Category, Severity, Cavekit, Requirement, Description
Category: decomposition|coverage|ambiguity|scope|assumption
Severity: critical|advisory
No issues = NO_ISSUES

## Kits to Review

`

// DesignPromptFull is the verbose default prompt for the design challenge.
const DesignPromptFull = `You are a senior software architect performing an adversarial design review of cavekit specifications. Your job is to CHALLENGE the design, not rubber-stamp it.

Review all kits as a whole system. Focus exclusively on design-level concerns:

1. **Domain Decomposition Quality**
   - Are domain boundaries drawn at the right places?
   - Is any domain doing too much (over-scoped) or too little (under-scoped)?
   - Would a different decomposition reduce coupling or improve cohesion?

2. **Requirement Coverage**
   - Are there missing requirements that the system clearly needs?
   - Are there gaps between domains where functionality falls through the cracks?
   - Do cross-references cover all domain interactions?

3. **Ambiguity in Acceptance Criteria**
   - Are any acceptance criteria vague enough to be interpreted two different ways?
   - Are any criteria technically testable but practically meaningless ("checkbox requirements")?
   - Would an implementer know exactly what to build from each requirement?

4. **Scope Assessment**
   - Is any domain over-scoped (trying to do too much for one implementation unit)?
   - Is any domain under-scoped (too thin to be worth its own domain)?
   - Are there implicit assumptions that should be made explicit?

5. **Cross-Domain Coherence**
   - Do the domains fit together as a coherent system?
   - Are there contradictions between domains?
   - Is the dependency graph sound (no hidden circular dependencies)?

## Rules
- Do NOT provide implementation-level feedback (no framework suggestions, no file path opinions, no API design)
- You MUST propose at least one alternative decomposition if you can identify a better one
- Focus on issues that would cause real problems during implementation
- Be specific: reference cavekit files and requirement numbers

## Output Format

For each finding, output exactly one row in a markdown table with columns:
  Category, Severity, Cavekit, Requirement, Description

Category must be one of: decomposition, coverage, ambiguity, scope, assumption
Severity must be one of: critical, advisory

If you find no issues at all, output exactly: NO_ISSUES

## Kits to Review

`

// DesignPrompt picks the right template for the current config.
func DesignPrompt(cfg *config.Store) string {
	active, _ := cfg.CavemanActive("draft")
	if active {
		return DesignPromptCaveman
	}
	return DesignPromptFull
}

// DesignFinding is one parsed challenge row.
type DesignFinding struct {
	Category    string
	Severity    string // "critical" | "advisory"
	Cavekit     string
	Requirement string
	Description string
}

// ParseDesignFindings extracts challenge rows from Codex output. Unknown
// severities default to advisory. Returns nil for NO_ISSUES output.
func ParseDesignFindings(raw string) []DesignFinding {
	if containsFold(raw, "NO_ISSUES") {
		return nil
	}

	catRe := regexp.MustCompile(`(?i)\|\s*` + "`?" + `(decomposition|coverage|ambiguity|scope|assumption)\b`)

	var out []DesignFinding
	for _, rawLine := range strings.Split(raw, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || strings.HasPrefix(trimmed, "|-") || strings.HasPrefix(trimmed, "| -") {
			continue
		}
		if strings.HasPrefix(trimmed, "| Category") {
			continue
		}
		if !catRe.MatchString(line) {
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) < 7 {
			continue
		}
		sev := strings.ToLower(cleanCell(cols[2]))
		if sev != "critical" && sev != "advisory" {
			sev = "advisory"
		}
		out = append(out, DesignFinding{
			Category:    cleanCell(cols[1]),
			Severity:    sev,
			Cavekit:     cleanCell(cols[3]),
			Requirement: cleanCell(cols[4]),
			Description: cleanCell(cols[5]),
		})
	}
	return out
}

// Split sorts findings into critical and advisory slices, preserving order.
func Split(findings []DesignFinding) (critical, advisory []DesignFinding) {
	for _, f := range findings {
		if f.Severity == "critical" {
			critical = append(critical, f)
		} else {
			advisory = append(advisory, f)
		}
	}
	return
}

// DesignOptions configure DesignChallenge.
type DesignOptions struct {
	KitsDir              string
	DryRun               bool
	Runner               CommandRunner
	AvailabilityOverride *Availability
	Clock                func() time.Time
}

// DesignResult captures the outcome of one DesignChallenge call.
type DesignResult struct {
	Skipped     bool // Codex disabled/unavailable or no kits found
	DryRun      bool
	Duration    time.Duration
	Findings    []DesignFinding
	RawOut      string
	FileCount   int
	HasCritical bool
}

// DesignChallenge runs one pass of the design challenge: load every
// context/kits/cavekit-*.md, submit them as the prompt suffix, parse the
// response into findings. Does not loop — use DesignChallengeCycle for that.
func DesignChallenge(ctx context.Context, opts DesignOptions, cfgStore *config.Store, w io.Writer) (DesignResult, error) {
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}

	if cfgStore.GetWithDefault("codex_review", "auto") == "off" {
		fmt.Fprintln(w, "[ck:design-challenge] Codex review disabled (codex_review=off). Skipping.")
		return DesignResult{Skipped: true}, nil
	}

	var avail Availability
	if opts.AvailabilityOverride != nil {
		avail = *opts.AvailabilityOverride
	} else {
		avail = Detect(ctx)
	}
	if !avail.Available {
		fmt.Fprintln(w, "[ck:design-challenge] Codex unavailable — skipping design challenge.")
		return DesignResult{Skipped: true}, nil
	}

	kitsDir := opts.KitsDir
	if kitsDir == "" {
		kitsDir = filepath.Join(DetectProjectRoot(), "context", "kits")
	}

	info, err := os.Stat(kitsDir)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(w, "[ck:design-challenge] No kits directory at %s\n", kitsDir)
		return DesignResult{Skipped: true}, nil
	}

	contents, count, err := loadKits(kitsDir)
	if err != nil {
		return DesignResult{}, err
	}
	if count == 0 {
		fmt.Fprintf(w, "[ck:design-challenge] No cavekit files found in %s\n", kitsDir)
		return DesignResult{Skipped: true}, nil
	}

	fmt.Fprintf(w, "[ck:design-challenge] Sending %d cavekit(s) to Codex for design challenge...\n", count)

	fullPrompt := DesignPrompt(cfgStore) + contents
	model := cfgStore.GetWithDefault("codex_model", "o4-mini")
	codexArgs := []string{"--approval-mode", "full-auto", "--model", model, "--quiet", "-p", fullPrompt}

	if opts.DryRun {
		fmt.Fprintf(w, "[ck:design-challenge] DRY RUN — would send %d kits to Codex\n", count)
		return DesignResult{DryRun: true, FileCount: count}, nil
	}

	start := clock()
	raw, runErr := runner.Run(ctx, "codex", codexArgs, "")
	duration := clock().Sub(start)
	if runErr != nil {
		fmt.Fprintln(w, "[ck:design-challenge] Codex invocation failed. Skipping design challenge.")
		fmt.Fprintf(w, "[ck:design-challenge] Error: %s\n", truncate(string(raw), 500))
		return DesignResult{Skipped: true, RawOut: string(raw), FileCount: count, Duration: duration}, nil
	}

	findings := ParseDesignFindings(string(raw))
	if len(findings) == 0 {
		fmt.Fprintf(w, "[ck:design-challenge] Codex found no design issues. Clean review. (%ds)\n", int(duration.Seconds()))
		return DesignResult{Findings: nil, RawOut: string(raw), FileCount: count, Duration: duration}, nil
	}

	critical, advisory := Split(findings)
	fmt.Fprintf(w, "[ck:design-challenge] Challenge complete in %ds: %d critical, %d advisory\n",
		int(duration.Seconds()), len(critical), len(advisory))

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "=== Design Challenge Findings ===")
	fmt.Fprintln(w, "| Category | Severity | Cavekit | Requirement | Description |")
	fmt.Fprintln(w, "|----------|----------|-----------|-------------|-------------|")
	for _, f := range findings {
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n", f.Category, f.Severity, f.Cavekit, f.Requirement, f.Description)
	}
	fmt.Fprintln(w, "=== End of Findings ===")

	return DesignResult{
		Findings:    findings,
		RawOut:      string(raw),
		FileCount:   count,
		Duration:    duration,
		HasCritical: len(critical) > 0,
	}, nil
}

func loadKits(dir string) (string, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "cavekit-") || !strings.HasSuffix(name, ".md") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", 0, err
		}
		b.WriteString("--- FILE: ")
		b.WriteString(name)
		b.WriteString(" ---\n")
		b.Write(data)
		b.WriteString("\n\n")
	}
	return b.String(), len(names), nil
}

// CycleOutcomeDesign enumerates the result of DesignChallengeCycle.
type CycleOutcomeDesign int

const (
	DesignCyclePass        CycleOutcomeDesign = iota // clean or advisory-only
	DesignCycleAwaitFixes                            // blocked; caller must fix and re-run
	DesignCycleExhausted                             // hit max cycles with criticals remaining
	DesignCycleSkipped                               // Codex unavailable / disabled
)

// DesignCycleOptions tunes DesignChallengeCycle.
type DesignCycleOptions struct {
	KitsDir              string
	MaxCycles            int
	Runner               CommandRunner
	AvailabilityOverride *Availability
	Clock                func() time.Time
}

// DesignCycleResult is returned alongside the outcome.
type DesignCycleResult struct {
	Outcome  CycleOutcomeDesign
	Findings []DesignFinding // last cycle's findings
	Duration time.Duration
}

// DesignChallengeCycle runs one iteration of the challenge and decides
// whether to proceed, await fixes, or exhaust. The caller is responsible for
// implementing critical fixes between calls when Outcome is AwaitFixes.
func DesignChallengeCycle(ctx context.Context, opts DesignCycleOptions, cfgStore *config.Store, w io.Writer) (DesignCycleResult, error) {
	maxCycles := opts.MaxCycles
	if maxCycles <= 0 {
		maxCycles = 2
	}

	fmt.Fprintf(w, "[ck:design-challenge] Challenge cycle 1/%d\n", maxCycles)
	res, err := DesignChallenge(ctx, DesignOptions{
		KitsDir:              opts.KitsDir,
		Runner:               opts.Runner,
		AvailabilityOverride: opts.AvailabilityOverride,
		Clock:                opts.Clock,
	}, cfgStore, w)
	if err != nil {
		return DesignCycleResult{}, err
	}
	if res.Skipped {
		return DesignCycleResult{Outcome: DesignCycleSkipped, Duration: res.Duration}, nil
	}

	critical, _ := Split(res.Findings)
	if len(critical) == 0 {
		fmt.Fprintln(w, "[ck:design-challenge] Cycle 1: No critical issues.")
		return DesignCycleResult{Outcome: DesignCyclePass, Findings: res.Findings, Duration: res.Duration}, nil
	}

	if maxCycles <= 1 {
		fmt.Fprintf(w, "[ck:design-challenge] WARNING: %d critical finding(s) remain after %d cycle.\n", len(critical), maxCycles)
		return DesignCycleResult{Outcome: DesignCycleExhausted, Findings: res.Findings, Duration: res.Duration}, nil
	}

	fmt.Fprintf(w, "[ck:design-challenge] %d critical finding(s) need fixes.\n", len(critical))
	fmt.Fprintln(w, "CRITICAL_FIXES:")
	for _, f := range critical {
		fmt.Fprintf(w, "%s|%s|%s|%s\n", f.Cavekit, f.Requirement, f.Category, f.Description)
	}
	fmt.Fprintln(w, "AWAITING_FIXES")
	return DesignCycleResult{Outcome: DesignCycleAwaitFixes, Findings: res.Findings, Duration: res.Duration}, nil
}

// FormatAdvisoryForUser renders the advisory findings as a markdown block
// suitable for insertion into a draft-flow user gate prompt.
func FormatAdvisoryForUser(findings []DesignFinding) string {
	_, advisory := Split(findings)
	if len(advisory) == 0 {
		return "No advisory findings from Codex design challenge."
	}
	var b strings.Builder
	b.WriteString("### Codex Design Challenge — Advisory Findings\n\n")
	b.WriteString("These findings are informational — Codex flagged them as worth considering but not blocking.\n\n")
	b.WriteString("| Category | Cavekit | Requirement | Finding |\n")
	b.WriteString("|----------|-----------|-------------|---------|\n")
	for _, f := range advisory {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", f.Category, f.Cavekit, f.Requirement, f.Description)
	}
	return b.String()
}

// FormatCriticalForFix emits the critical findings in the pipe-delimited
// format the draft-flow auto-fix step consumes.
func FormatCriticalForFix(findings []DesignFinding) string {
	critical, _ := Split(findings)
	if len(critical) == 0 {
		return ""
	}
	var b strings.Builder
	for _, f := range critical {
		fmt.Fprintf(&b, "%s|%s|%s|%s\n", f.Cavekit, f.Requirement, f.Category, f.Description)
	}
	return b.String()
}
