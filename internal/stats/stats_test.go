package stats

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParseLoopLog(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"loop-log.md": strings.Join([]string{
			"### Iteration 1 — 2026-04-17",
			"- Task: T-001 — do X",
			"- Tier: 1",
			"- Status: DONE",
			"",
			"### Iteration 2 — 2026-04-17",
			"- Task: T-002 — do Y",
			"- Tier: 2",
			"- Status: PARTIAL",
			"",
			"### Iteration 3",
			"- Task: T-003",
			"- Tier: 1",
			"- Status: BLOCKED",
		}, "\n"),
	})

	m, err := ParseLoopLog(filepath.Join(root, "loop-log.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Iterations) != 3 {
		t.Fatalf("iterations = %d want 3", len(m.Iterations))
	}
	if m.Done != 1 || m.Partial != 1 || m.Blocked != 1 {
		t.Errorf("counts = done=%d partial=%d blocked=%d", m.Done, m.Partial, m.Blocked)
	}
	if m.TierCounts["1"] != 2 || m.TierCounts["2"] != 1 {
		t.Errorf("tier counts = %+v", m.TierCounts)
	}
	if m.Iterations[0].Task != "T-001 — do X" {
		t.Errorf("task parse = %q", m.Iterations[0].Task)
	}
}

func TestCountDeadEnds(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"context/impl/impl-a.md":                  "dead end here\nDEAD-END again",
		"context/impl/impl-b.md":                  "nothing",
		"context/impl/archive/20260101/impl-c.md": "one dead.end line",
	})
	if got := CountDeadEnds(root); got != 3 {
		t.Errorf("got %d want 3", got)
	}
}

func TestFindFrontier(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"context/plans/build-site.md": "x",
		"context/sites/old-site.md":   "y",
	})
	if got := FindFrontier(root); got != "context/plans/build-site.md" {
		t.Errorf("got %q", got)
	}
}

func TestFrontierProgress(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"context/plans/build-site.md": strings.Join([]string{
			"## Tier 1",
			"| T-001 | do X |",
			"| T-002 | do Y |",
			"- T-003 — free-form",
		}, "\n"),
		"context/impl/impl-a.md": strings.Join([]string{
			"| T-001 | DONE |",
			"| T-002 | IN PROGRESS |",
		}, "\n"),
	})
	counts := FrontierProgress(root, "context/plans/build-site.md")
	if counts.Total != 3 {
		t.Errorf("total = %d want 3", counts.Total)
	}
	if counts.Done != 1 {
		t.Errorf("done = %d want 1", counts.Done)
	}
	if counts.InProgress != 1 {
		t.Errorf("wip = %d want 1", counts.InProgress)
	}
	if counts.Remaining != 1 {
		t.Errorf("remaining = %d want 1", counts.Remaining)
	}
}

func TestReadRalphState(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".claude/ralph-loop.local.md": strings.Join([]string{
			"---",
			"active: true",
			"iteration: 3",
			"max_iterations: 20",
			`started_at: "2026-04-17T12:00:00Z"`,
			"---",
		}, "\n"),
	})
	s := ReadRalphState(root)
	if !s.Active {
		t.Error("expected active")
	}
	if s.Iteration != "3" || s.MaxIterations != "20" {
		t.Errorf("got %+v", s)
	}
	if s.StartedAt != "2026-04-17T12:00:00Z" {
		t.Errorf("started = %q", s.StartedAt)
	}
}

func TestCollectLoopLogsInOrder(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"context/impl/loop-log.md":                              "current",
		"context/impl/archive/20260101-120000/loop-log.md":      "old",
		"context/impl/archive/20260101-130000/loop-log.md":      "newer",
	})
	got := CollectLoopLogs(root)
	if len(got) != 3 {
		t.Fatalf("got %d: %v", len(got), got)
	}
	if !strings.HasSuffix(got[0], "context/impl/loop-log.md") && !strings.HasSuffix(got[0], "context\\impl\\loop-log.md") {
		t.Errorf("first should be current: %v", got)
	}
}
