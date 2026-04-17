package picker

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

func TestDeriveName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"build-site-coherence.md", "coherence"},
		{"plan-auth-v2.md", "auth-v2"},
		{"feature-frontier-ingest.md", "ingest"},
		{"random.md", "random"},
	}
	for _, tc := range cases {
		if got := deriveName(tc.in); got != tc.want {
			t.Errorf("deriveName(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestDiscoverFrontiersGroupsByStatus(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"context/plans/build-site-a.md": "| T-001 | X |\n| T-002 | Y |",
		"context/plans/build-site-b.md": "| T-100 | X |",
		"context/plans/archive/build-site-old.md": "| T-900 | X |",
		"context/impl/impl-a.md": "T-001 DONE\nT-002 DONE",
	})

	frontiers := DiscoverFrontiers(root)
	var a, b, old *Frontier
	for i := range frontiers {
		switch frontiers[i].Name {
		case "a":
			a = &frontiers[i]
		case "b":
			b = &frontiers[i]
		case "old":
			old = &frontiers[i]
		}
	}
	if a == nil || a.Status != StatusDone {
		t.Errorf("a.Status = %v, want done: %+v", a, frontiers)
	}
	if b == nil || b.Status != StatusAvailable {
		t.Errorf("b.Status = %v, want available", b)
	}
	if old == nil || old.Status != StatusDone {
		t.Errorf("old.Status = %v, want done (archived)", old)
	}
}

func TestCountDoneForFrontier(t *testing.T) {
	content := "| T-001 |\n| T-002 |\n| T-003 |"
	doneSet := map[string]struct{}{"T-001": {}, "T-003": {}, "T-999": {}}
	if got := countDoneForFrontier(content, doneSet); got != 2 {
		t.Errorf("got %d want 2", got)
	}
}

func TestDetectStatusInProgressViaWorktree(t *testing.T) {
	parent := t.TempDir()
	project := filepath.Join(parent, "myproj")
	worktree := filepath.Join(parent, "myproj-cavekit-v1")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(worktree, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".claude", "ralph-loop.local.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := detectStatus(project, "v1", 10, 2); got != StatusInProgress {
		t.Errorf("got %v want in-progress", got)
	}
}

func TestWriteSelectionToOutfile(t *testing.T) {
	outfile := filepath.Join(t.TempDir(), "sel.txt")
	t.Setenv("CAVEKIT_PICKER_OUTFILE", outfile)
	if err := WriteSelection([]string{"/a", "/b"}, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outfile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "/a") || !strings.Contains(string(data), "/b") {
		t.Errorf("unexpected contents: %q", data)
	}
}
