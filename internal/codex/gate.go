package codex

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/JuliusBrussee/cavekit/internal/config"
)

// GateMode names the tier-gate policy. Mirrors tier_gate_mode in config.
type GateMode string

const (
	GateModeSeverity   GateMode = "severity"
	GateModeStrict     GateMode = "strict"
	GateModePermissive GateMode = "permissive"
	GateModeOff        GateMode = "off"
)

// GateResult captures the outcome of a single gate evaluation.
//
// Proceed is true when no findings block progress. BlockingIDs lists the
// finding IDs the gate considers blocking (under the severity mode: P0/P1
// with status NEW; under strict mode: every NEW finding).
type GateResult struct {
	Proceed        bool
	BlockingCount  int
	DeferredCount  int
	BlockingIDs    []string
}

// EvaluateGate loads findings, applies the configured tier_gate_mode, and
// returns the verdict.
func EvaluateGate(cfgStore *config.Store, findings *FindingsStore) (GateResult, error) {
	mode := GateMode(cfgStore.GetWithDefault("tier_gate_mode", string(GateModeSeverity)))

	if mode == GateModeOff {
		return GateResult{Proceed: true}, nil
	}

	rows, err := readNewFindings(findings.Path)
	if err != nil {
		return GateResult{}, err
	}

	result := GateResult{Proceed: true}
	for _, row := range rows {
		switch mode {
		case GateModeSeverity:
			if row.Severity == "P0" || row.Severity == "P1" {
				result.BlockingIDs = append(result.BlockingIDs, row.ID)
				result.BlockingCount++
			} else {
				result.DeferredCount++
			}
		case GateModeStrict:
			result.BlockingIDs = append(result.BlockingIDs, row.ID)
			result.BlockingCount++
		case GateModePermissive:
			result.DeferredCount++
		}
	}
	result.Proceed = result.BlockingCount == 0
	return result, nil
}

// FixTask is a row emitted by GenerateFixTasks.
type FixTask struct {
	ID          string // e.g. FIX-F-001
	Severity    string
	File        string
	Description string
}

// GenerateFixTasks returns one FixTask per blocking finding. A finding is
// blocking under the severity mode (P0/P1) or the strict mode (any).
// Returns nil in off/permissive modes.
func GenerateFixTasks(cfgStore *config.Store, findings *FindingsStore) ([]FixTask, error) {
	mode := GateMode(cfgStore.GetWithDefault("tier_gate_mode", string(GateModeSeverity)))
	if mode == GateModeOff || mode == GateModePermissive {
		return nil, nil
	}

	rows, err := readNewFindings(findings.Path)
	if err != nil {
		return nil, err
	}

	var tasks []FixTask
	for _, row := range rows {
		isBlocking := false
		switch mode {
		case GateModeSeverity:
			isBlocking = row.Severity == "P0" || row.Severity == "P1"
		case GateModeStrict:
			isBlocking = true
		}
		if !isBlocking {
			continue
		}
		tasks = append(tasks, FixTask{
			ID:          "FIX-" + row.ID,
			Severity:    row.Severity,
			File:        row.File,
			Description: row.Description,
		})
	}
	return tasks, nil
}

// newRow is an internal, pre-parsed finding row.
type newRow struct {
	ID          string
	Description string
	Severity    string
	File        string
}

var findingIDRe = regexp.MustCompile(`F-\d+`)

// readNewFindings reads the findings markdown table and returns only rows
// whose status is NEW. Header, separator, and malformed rows are skipped.
func readNewFindings(path string) ([]newRow, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var rows []newRow
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if !strings.HasPrefix(line, "|") {
			continue
		}
		if strings.HasPrefix(line, "| Finding") || strings.HasPrefix(line, "|-") {
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) < 6 {
			continue
		}
		finding := strings.TrimSpace(cols[1])
		severity := strings.TrimSpace(cols[2])
		file := strings.TrimSpace(cols[3])
		status := strings.TrimSpace(cols[4])
		if status != "NEW" {
			continue
		}
		id := findingIDRe.FindString(finding)
		if id == "" {
			continue
		}
		desc := strings.TrimPrefix(finding, id+":")
		desc = strings.TrimSpace(desc)

		rows = append(rows, newRow{
			ID:          id,
			Description: desc,
			Severity:    severity,
			File:        file,
		})
	}
	return rows, scanner.Err()
}

// CycleOutcome enumerates the result of ReviewFixCycle.
type CycleOutcome int

const (
	// CycleProceed — all clear, no blocking findings after review.
	CycleProceed CycleOutcome = iota
	// CycleAwaitingFixes — blocked this cycle; caller must implement fixes
	// and call ReviewFixCycle again (which re-reviews).
	CycleAwaitingFixes
	// CycleExhausted — hit max cycles with blocking findings still present.
	CycleExhausted
)

// CycleOptions tunes ReviewFixCycle.
type CycleOptions struct {
	BaseRef   string
	MaxCycles int
	Runner    CommandRunner
	// AvailabilityOverride lets tests inject Codex availability.
	AvailabilityOverride *Availability
}

// ReviewFixCycle runs the review, evaluates the gate, and either returns
// Proceed (happy path), AwaitingFixes (caller must implement fixes), or
// Exhausted (max cycles hit with blockers remaining). One iteration — the
// caller drives the retry loop because fix implementation happens outside
// this function.
func ReviewFixCycle(ctx context.Context, opts CycleOptions, cfgStore *config.Store, findings *FindingsStore, w io.Writer) (CycleOutcome, error) {
	if opts.BaseRef == "" {
		return 0, fmt.Errorf("base ref required")
	}
	maxCycles := opts.MaxCycles
	if maxCycles <= 0 {
		maxCycles = 2
	}

	fmt.Fprintf(w, "[ck:tier-gate] Review-fix cycle 1/%d\n", maxCycles)

	reviewOpts := ReviewOptions{
		BaseRef:              opts.BaseRef,
		Runner:               opts.Runner,
		AvailabilityOverride: opts.AvailabilityOverride,
	}
	if _, err := Review(ctx, reviewOpts, cfgStore, findings, w); err != nil {
		return 0, err
	}

	result, err := EvaluateGate(cfgStore, findings)
	if err != nil {
		return 0, err
	}
	writeGate(w, result)

	if result.Proceed {
		fmt.Fprintln(w, "[ck:tier-gate] Gate: PROCEED (no blocking findings)")
		return CycleProceed, nil
	}

	if maxCycles <= 1 {
		remaining, _ := GenerateFixTasks(cfgStore, findings)
		fmt.Fprintf(w, "[ck:tier-gate] WARNING: Advancing after %d review-fix cycle(s) with %d unresolved blocking findings\n", maxCycles, len(remaining))
		return CycleExhausted, nil
	}

	fmt.Fprintf(w, "[ck:tier-gate] Gate: BLOCKED — %d finding(s) need fixes\n", result.BlockingCount)
	fmt.Fprintln(w, "[ck:tier-gate] Fix tasks for this cycle:")
	tasks, _ := GenerateFixTasks(cfgStore, findings)
	for _, task := range tasks {
		fmt.Fprintf(w, "%s|%s|%s|%s\n", task.ID, task.Severity, task.File, task.Description)
	}
	fmt.Fprintln(w, "[ck:tier-gate] AWAITING_FIXES")
	return CycleAwaitingFixes, nil
}

func writeGate(w io.Writer, r GateResult) {
	verdict := "proceed"
	if !r.Proceed {
		verdict = "blocked"
	}
	fmt.Fprintf(w, "GATE_RESULT=%s\n", verdict)
	fmt.Fprintf(w, "BLOCKING_COUNT=%d\n", r.BlockingCount)
	fmt.Fprintf(w, "DEFERRED_COUNT=%d\n", r.DeferredCount)
	if len(r.BlockingIDs) > 0 {
		fmt.Fprintf(w, "BLOCKING_FINDINGS=%s\n", strings.Join(r.BlockingIDs, ","))
	}
}
