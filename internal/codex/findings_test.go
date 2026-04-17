package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestFindings(t *testing.T) *FindingsStore {
	t.Helper()
	return &FindingsStore{
		Path: filepath.Join(t.TempDir(), "context", "impl", "impl-review-findings.md"),
		Now:  func() time.Time { return time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC) },
	}
}

func TestFindingsInitCreatesFile(t *testing.T) {
	s := newTestFindings(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{"# Review Findings", headerRow, separatorRow, "2026-04-17T12:00:00Z"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected file to contain %q\n%s", want, body)
		}
	}
}

func TestFindingsInitIdempotent(t *testing.T) {
	s := newTestFindings(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.Path)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(s.Path)
	if string(before) != string(after) {
		t.Error("Init rewrote existing file")
	}
}

func TestFindingsLegacyMigration(t *testing.T) {
	s := newTestFindings(t)
	legacy := "# Review Findings\n\n" + legacyHeaderRow + "\n" + legacySeparatorRow + "\n"
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(s.Path)
	if !strings.Contains(string(got), headerRow) {
		t.Errorf("missing new header after migration:\n%s", got)
	}
	if strings.Contains(string(got), legacyHeaderRow) {
		t.Errorf("legacy header still present:\n%s", got)
	}
}

func TestFindingsAppendAndNextID(t *testing.T) {
	s := newTestFindings(t)
	id, err := s.Append(Finding{
		Severity:    "P0",
		File:        "src/main.go:42",
		Description: "Nil pointer",
		Source:      "codex-tier-gate",
		Tier:        2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "F-001" {
		t.Errorf("first ID = %q, want F-001", id)
	}

	id2, err := s.Append(Finding{
		Severity:    "P1",
		File:        "x.go",
		Description: "Resource leak",
		Source:      "codex-tier-gate",
		Tier:        1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id2 != "F-002" {
		t.Errorf("second ID = %q, want F-002", id2)
	}

	data, _ := os.ReadFile(s.Path)
	if !strings.Contains(string(data), "F-001: Nil pointer") {
		t.Errorf("row missing:\n%s", data)
	}
}

func TestFindingsUpdateStatus(t *testing.T) {
	s := newTestFindings(t)
	id, err := s.Append(Finding{
		Severity: "P0", File: "x.go", Description: "bug", Source: "codex", Tier: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateStatus(id, "FIXED"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(s.Path)
	if !strings.Contains(string(data), "| FIXED |") {
		t.Errorf("status not updated:\n%s", data)
	}
	if strings.Contains(string(data), "| NEW |") {
		t.Errorf("old NEW status still present:\n%s", data)
	}
}

func TestFindingsUpdateStatusMissing(t *testing.T) {
	s := newTestFindings(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateStatus("F-999", "FIXED"); err == nil {
		t.Error("expected error for missing finding")
	}
}

func TestFindingsListBlocking(t *testing.T) {
	s := newTestFindings(t)
	_, _ = s.Append(Finding{Severity: "P0", File: "a.go", Description: "blocker", Source: "codex", Tier: 1})
	_, _ = s.Append(Finding{Severity: "P2", File: "b.go", Description: "minor", Source: "codex", Tier: 1})
	id3, _ := s.Append(Finding{Severity: "P1", File: "c.go", Description: "blocker2", Source: "codex", Tier: 1})
	_ = s.UpdateStatus(id3, "FIXED")
	id4, _ := s.Append(Finding{Severity: "P1", File: "d.go", Description: "blocker3", Source: "codex", Tier: 1})
	_ = id4

	blocking, err := s.ListBlocking()
	if err != nil {
		t.Fatal(err)
	}
	if len(blocking) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(blocking), blocking)
	}
	if blocking[0].Severity != "P0" || !strings.Contains(blocking[0].Finding, "F-001") {
		t.Errorf("blocking[0] = %+v", blocking[0])
	}
	if blocking[1].Severity != "P1" || !strings.Contains(blocking[1].Finding, "F-004") {
		t.Errorf("blocking[1] = %+v", blocking[1])
	}
}

func TestFindingsAppendValidation(t *testing.T) {
	s := newTestFindings(t)
	cases := []Finding{
		{},
		{Severity: "P0"},
		{Severity: "P0", File: "a"},
		{Severity: "P0", File: "a", Description: "d"},
		{Severity: "P0", File: "a", Description: "d", Source: "s"}, // tier missing
	}
	for i, f := range cases {
		if _, err := s.Append(f); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}
