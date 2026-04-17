package install

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"

	"github.com/JuliusBrussee/cavekit/internal/paths"
)

// PluginOptions tunes InstallPlugin.
type PluginOptions struct {
	// RepoRoot is the cavekit checkout to link into the marketplace. Defaults
	// to the current working directory.
	RepoRoot string
	// ClaudeDir overrides ~/.claude for tests.
	ClaudeDir string
}

// InstallPlugin sets up the Cavekit marketplace under $CLAUDE_DIR/plugins/
// local/cavekit-marketplace, links the repo in under both ck and bp, and
// merges plugin registration into settings.json. Idempotent.
func InstallPlugin(opts PluginOptions, w io.Writer) error {
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		repoRoot = cwd
	}
	claudeDir := opts.ClaudeDir
	if claudeDir == "" {
		claudeDir = paths.ClaudeDir()
	}

	marketplaceName := "cavekit-local"
	marketplaceDir := filepath.Join(claudeDir, "plugins", "local", "cavekit-marketplace")
	settingsFile := filepath.Join(claudeDir, "settings.json")

	fmt.Fprintf(w, "▸ Setting up Cavekit marketplace at %s\n", marketplaceDir)
	if err := os.MkdirAll(filepath.Join(marketplaceDir, ".claude-plugin"), 0o755); err != nil {
		return err
	}

	for _, slug := range []string{"ck", "bp"} {
		target := filepath.Join(marketplaceDir, slug)
		if err := linkDir(repoRoot, target); err != nil {
			return fmt.Errorf("link %s: %w", slug, err)
		}
	}

	owner := currentUserName()
	if err := writeMarketplaceJSON(marketplaceDir, marketplaceName, owner); err != nil {
		return err
	}
	if err := writeMarketplacePluginJSON(marketplaceDir); err != nil {
		return err
	}
	fmt.Fprintf(w, "■ Marketplace linked (ck + bp → %s)\n", repoRoot)

	fmt.Fprintf(w, "▸ Updating %s\n", settingsFile)
	if err := mergeClaudeSettings(settingsFile, marketplaceName, marketplaceDir); err != nil {
		return err
	}
	fmt.Fprintln(w, "■ Claude Code settings updated")
	return nil
}

func currentUserName() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "cavekit"
}

func writeMarketplaceJSON(marketplaceDir, name, owner string) error {
	doc := map[string]any{
		"name":  name,
		"owner": map[string]any{"name": owner},
		"metadata": map[string]any{
			"description": "Local Cavekit plugin marketplace",
			"version":     "2.0.0",
		},
		"plugins": []any{
			map[string]any{
				"name":        "ck",
				"description": "Cavekit framework with skills, commands, agents, and references",
				"version":     "2.0.0",
				"source":      "./ck",
				"author":      map[string]any{"name": owner},
			},
			map[string]any{
				"name":        "bp",
				"description": "[DEPRECATED — use /ck:* instead] Cavekit framework (legacy alias)",
				"version":     "2.0.0",
				"source":      "./bp",
				"author":      map[string]any{"name": owner},
			},
		},
	}
	return writeJSON(filepath.Join(marketplaceDir, ".claude-plugin", "marketplace.json"), doc)
}

func writeMarketplacePluginJSON(marketplaceDir string) error {
	doc := map[string]any{
		"name":        "cavekit-marketplace",
		"description": "Local Cavekit plugin marketplace",
		"version":     "2.0.0",
		"plugins":     []any{"ck", "bp"},
	}
	return writeJSON(filepath.Join(marketplaceDir, ".claude-plugin", "plugin.json"), doc)
}

func writeJSON(path string, doc any) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// mergeClaudeSettings ensures the marketplace and enabled-plugin entries exist
// in settings.json. Preserves unrelated keys.
func mergeClaudeSettings(path, marketplaceName, marketplaceDir string) error {
	existing := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	}

	extra, _ := existing["extraKnownMarketplaces"].(map[string]any)
	if extra == nil {
		extra = map[string]any{}
	}
	extra[marketplaceName] = map[string]any{
		"source": map[string]any{
			"source": "directory",
			"path":   marketplaceDir,
		},
	}
	existing["extraKnownMarketplaces"] = extra

	enabled, _ := existing["enabledPlugins"].(map[string]any)
	if enabled == nil {
		enabled = map[string]any{}
	}
	enabled["ck@"+marketplaceName] = true
	enabled["bp@"+marketplaceName] = true
	existing["enabledPlugins"] = enabled

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
