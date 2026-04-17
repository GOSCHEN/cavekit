package install

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallPluginFresh(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	claudeDir := t.TempDir()

	var buf bytes.Buffer
	if err := InstallPlugin(PluginOptions{RepoRoot: repo, ClaudeDir: claudeDir}, &buf); err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}

	marketplaceDir := filepath.Join(claudeDir, "plugins", "local", "cavekit-marketplace")
	for _, slug := range []string{"ck", "bp"} {
		if _, err := os.Lstat(filepath.Join(marketplaceDir, slug)); err != nil {
			t.Errorf("%s link missing: %v", slug, err)
		}
	}

	for _, rel := range []string{
		filepath.Join(".claude-plugin", "marketplace.json"),
		filepath.Join(".claude-plugin", "plugin.json"),
	} {
		if _, err := os.Stat(filepath.Join(marketplaceDir, rel)); err != nil {
			t.Errorf("%s missing: %v", rel, err)
		}
	}

	settings, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(settings, &parsed); err != nil {
		t.Fatal(err)
	}
	extras, _ := parsed["extraKnownMarketplaces"].(map[string]any)
	if _, ok := extras["cavekit-local"]; !ok {
		t.Errorf("missing marketplace entry: %+v", parsed)
	}
	enabled, _ := parsed["enabledPlugins"].(map[string]any)
	if enabled["ck@cavekit-local"] != true {
		t.Errorf("ck not enabled: %+v", enabled)
	}
}

func TestInstallPluginPreservesUnrelatedSettings(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	claudeDir := t.TempDir()
	seed := []byte(`{"model":"opus","theme":"dark","enabledPlugins":{"other@marketplace":true}}`)
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), seed, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallPlugin(PluginOptions{RepoRoot: repo, ClaudeDir: claudeDir}, new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["model"] != "opus" || parsed["theme"] != "dark" {
		t.Errorf("unrelated keys lost: %+v", parsed)
	}
	enabled := parsed["enabledPlugins"].(map[string]any)
	if enabled["other@marketplace"] != true {
		t.Errorf("prior enabled plugin dropped: %+v", enabled)
	}
	if enabled["ck@cavekit-local"] != true {
		t.Errorf("ck not added: %+v", enabled)
	}
}

func TestInstallPluginIdempotent(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	claudeDir := t.TempDir()

	for i := 0; i < 2; i++ {
		if err := InstallPlugin(PluginOptions{RepoRoot: repo, ClaudeDir: claudeDir}, new(bytes.Buffer)); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)
	enabled := parsed["enabledPlugins"].(map[string]any)
	if len(enabled) != 2 {
		t.Errorf("enabled should have exactly ck+bp, got %+v", enabled)
	}
}
