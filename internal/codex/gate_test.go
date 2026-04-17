package codex

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/JuliusBrussee/cavekit/internal/config"
)

// seedFindings creates a FindingsStore with a scripted set of rows. Severities
// drive gate-mode tests.
func seedFindings(t *testing.T, sevs []string) *FindingsStore {
	t.Helper()
	s := &FindingsStore{
		Path: filepath.Join(t.TempDir(), "findings.md"),
		Now:  func() time.Time { return time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC) },
	}
	for i, sev := range sevs {
		_, err := s.Append(Finding{
			Severity:    sev,
			File:        "file.go",
			Description: "row-" + sev,
			Source:      "codex",
			Tier:        i + 1,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestGateSeverityMode(t *testing.T) {
	cfg := newTestConfig(t)
	findings := seedFindings(t, []string{"P0", "P1", "P2", "P3"})

	result, err := EvaluateGate(cfg, findings)
	if err != nil {
		t.Fatal(err)
	}
	if result.Proceed {
		t.Error("expected blocked")
	}
	if result.BlockingCount != 2 {
		t.Errorf("blocking=%d want 2", result.BlockingCount)
	}
	if result.DeferredCount != 2 {
		t.Errorf("deferred=%d want 2", result.DeferredCount)
	}
	if len(result.BlockingIDs) != 2 || result.BlockingIDs[0] != "F-001" || result.BlockingIDs[1] != "F-002" {
		t.Errorf("BlockingIDs=%v", result.BlockingIDs)
	}
}

func TestGateStrictBlocksAll(t *testing.T) {
	cfg := newTestConfig(t)
	_ = cfg.Set("tier_gate_mode", "strict", config.ScopeProject)
	findings := seedFindings(t, []string{"P3", "P2"})

	result, err := EvaluateGate(cfg, findings)
	if err != nil {
		t.Fatal(err)
	}
	if result.Proceed {
		t.Error("strict mode should block P2/P3")
	}
	if result.BlockingCount != 2 {
		t.Errorf("blocking=%d want 2", result.BlockingCount)
	}
}

func TestGatePermissiveAlwaysProceeds(t *testing.T) {
	cfg := newTestConfig(t)
	_ = cfg.Set("tier_gate_mode", "permissive", config.ScopeProject)
	findings := seedFindings(t, []string{"P0", "P1"})

	result, err := EvaluateGate(cfg, findings)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Proceed {
		t.Error("permissive should proceed")
	}
	if result.DeferredCount != 2 {
		t.Errorf("deferred=%d want 2", result.DeferredCount)
	}
}

func TestGateOffSkipsRead(t *testing.T) {
	cfg := newTestConfig(t)
	_ = cfg.Set("tier_gate_mode", "off", config.ScopeProject)
	findings := &FindingsStore{Path: filepath.Join(t.TempDir(), "missing.md")}

	result, err := EvaluateGate(cfg, findings)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Proceed {
		t.Error("off mode should proceed")
	}
}

func TestGateSkipsFixedRows(t *testing.T) {
	cfg := newTestConfig(t)
	findings := seedFindings(t, []string{"P0", "P0"})
	_ = findings.UpdateStatus("F-001", "FIXED")

	result, err := EvaluateGate(cfg, findings)
	if err != nil {
		t.Fatal(err)
	}
	if result.BlockingCount != 1 {
		t.Errorf("blocking=%d want 1", result.BlockingCount)
	}
	if result.BlockingIDs[0] != "F-002" {
		t.Errorf("BlockingIDs=%v want [F-002]", result.BlockingIDs)
	}
}

func TestGenerateFixTasks(t *testing.T) {
	cfg := newTestConfig(t)
	findings := seedFindings(t, []string{"P0", "P1", "P2"})

	tasks, err := GenerateFixTasks(cfg, findings)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d, want 2: %+v", len(tasks), tasks)
	}
	if tasks[0].ID != "FIX-F-001" || tasks[1].ID != "FIX-F-002" {
		t.Errorf("ids = %q,%q", tasks[0].ID, tasks[1].ID)
	}
	if tasks[0].Description != "row-P0" {
		t.Errorf("desc = %q want row-P0", tasks[0].Description)
	}
}
