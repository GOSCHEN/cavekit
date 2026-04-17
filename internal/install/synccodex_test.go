package install

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncCodexCreatesLayout(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	// Minimal repo layout expected by SyncCodex.
	if err := os.MkdirAll(filepath.Join(repo, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"map.md", "quick.md"} {
		if err := os.WriteFile(filepath.Join(repo, "commands", name), []byte("# "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Create ~/.codex so the legacy link branch is exercised.
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := SyncCodex(SyncCodexOptions{RepoRoot: repo, Home: home}, &buf); err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}

	pluginDir := filepath.Join(home, "plugins", "ck")
	if _, err := os.Lstat(pluginDir); err != nil {
		t.Errorf("plugin link missing: %v", err)
	}

	if _, err := os.Lstat(filepath.Join(home, ".codex", "cavekit")); err != nil {
		t.Errorf("legacy link missing: %v", err)
	}

	promptsDir := filepath.Join(home, ".codex", "prompts")
	for _, p := range []string{"ck-map.md", "bp-map.md", "ck-quick.md", "bp-quick.md"} {
		if _, err := os.Lstat(filepath.Join(promptsDir, p)); err != nil {
			t.Errorf("prompt %s missing: %v", p, err)
		}
	}

	mkData, err := os.ReadFile(filepath.Join(home, ".agents", "plugins", "marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(mkData, &parsed); err != nil {
		t.Fatal(err)
	}
	plugins, _ := parsed["plugins"].([]any)
	if len(plugins) == 0 {
		t.Errorf("marketplace missing plugins: %s", mkData)
	}
}

func TestSyncCodexCleansStalePrompts(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "commands", "current.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	promptsDir := filepath.Join(home, ".codex", "prompts")
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(promptsDir, "ck-gone.md")
	if err := os.WriteFile(stale, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := SyncCodex(SyncCodexOptions{RepoRoot: repo, Home: home}, &buf); err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale prompt not removed: err=%v", err)
	}
}

func TestUpsertMarketplaceIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	for i := 0; i < 2; i++ {
		if err := upsertMarketplace(path); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	data, _ := os.ReadFile(path)
	// Only a single entry should exist, even after two upserts.
	if strings.Count(string(data), `"name": "ck"`) != 1 {
		t.Errorf("expected exactly one ck entry, got:\n%s", data)
	}
}

func TestUpsertMarketplaceReplacesLegacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	// Seed with a legacy bp-named entry.
	seed := []byte(`{"name":"local-plugins","interface":{"displayName":"Local Plugins"},"plugins":[{"name":"bp","source":{"source":"local","path":"./plugins/bp"}}]}`)
	if err := os.WriteFile(path, seed, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := upsertMarketplace(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"name": "ck"`) {
		t.Errorf("ck not written: %s", data)
	}
	if strings.Contains(string(data), `"name": "bp"`) {
		t.Errorf("legacy bp entry not replaced: %s", data)
	}
}
