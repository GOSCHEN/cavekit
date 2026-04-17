// Package install orchestrates installers: Codex plugin sync, Claude Code
// marketplace junctions, settings merges.
package install

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/JuliusBrussee/cavekit/internal/paths"
)

// SyncCodexOptions controls SyncCodex.
type SyncCodexOptions struct {
	// RepoRoot points at the cavekit checkout to link into the Codex plugin
	// tree. Defaults to the current working directory when empty.
	RepoRoot string
	// Home overrides $HOME / %USERPROFILE% for testing.
	Home string
}

// SyncCodex links the Cavekit repo into the Codex plugin tree, registers
// slash commands as prompts (with both the primary ck- prefix and the
// deprecated bp- alias), and upserts the marketplace entry.
func SyncCodex(opts SyncCodexOptions, w io.Writer) error {
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		repoRoot = cwd
	}
	home := opts.Home
	if home == "" {
		home = paths.UserHome()
	}
	if home == "" {
		return fmt.Errorf("could not determine user home")
	}

	pluginDir := filepath.Join(home, "plugins", "ck")
	marketplaceFile := filepath.Join(home, ".agents", "plugins", "marketplace.json")
	legacyLink := filepath.Join(home, ".codex", "cavekit")
	promptsDir := filepath.Join(home, ".codex", "prompts")

	fmt.Fprintln(w, "▸ Syncing Cavekit into Codex local plugins...")

	if err := os.MkdirAll(filepath.Join(home, "plugins"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(marketplaceFile), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		return err
	}

	if err := linkDir(repoRoot, pluginDir); err != nil {
		return fmt.Errorf("link plugin: %w", err)
	}
	fmt.Fprintf(w, "■ Linked plugin at %s\n", pluginDir)

	codexDir := filepath.Join(home, ".codex")
	if info, err := os.Stat(codexDir); err == nil && info.IsDir() {
		if err := linkDir(repoRoot, legacyLink); err != nil {
			return fmt.Errorf("link legacy: %w", err)
		}
		fmt.Fprintf(w, "■ Updated legacy Codex shortcut at %s\n", legacyLink)
	} else {
		fmt.Fprintln(w, "! Skipping legacy ~/.codex symlink because ~/.codex does not exist")
	}

	cmdsDir := filepath.Join(repoRoot, "commands")
	commandFiles, err := listCommandMarkdown(cmdsDir)
	if err != nil {
		return fmt.Errorf("list commands: %w", err)
	}

	// Primary and deprecated alias links.
	for _, f := range commandFiles {
		name := strings.TrimSuffix(filepath.Base(f), ".md")
		for _, prefix := range []string{"ck", "bp"} {
			target := filepath.Join(promptsDir, prefix+"-"+name+".md")
			if err := linkFile(f, target); err != nil {
				return fmt.Errorf("link prompt %s: %w", target, err)
			}
		}
	}

	// Remove stale prompt links (command file disappeared upstream).
	valid := make(map[string]struct{}, len(commandFiles))
	for _, f := range commandFiles {
		valid[strings.TrimSuffix(filepath.Base(f), ".md")] = struct{}{}
	}
	if err := cleanStalePrompts(promptsDir, valid, []string{"ck", "bp"}); err != nil {
		return err
	}

	fmt.Fprintf(w, "■ Linked Codex prompts at %s\n", promptsDir)

	if err := upsertMarketplace(marketplaceFile); err != nil {
		return fmt.Errorf("marketplace: %w", err)
	}
	fmt.Fprintf(w, "■ Updated Codex marketplace at %s\n", marketplaceFile)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Codex sync complete.")
	fmt.Fprintln(w, "  Restart Codex if it is already running.")
	return nil
}

func listCommandMarkdown(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out, nil
}

func cleanStalePrompts(promptsDir string, valid map[string]struct{}, prefixes []string) error {
	entries, err := os.ReadDir(promptsDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		for _, prefix := range prefixes {
			if !strings.HasPrefix(name, prefix+"-") {
				continue
			}
			cmd := strings.TrimPrefix(name, prefix+"-")
			cmd = strings.TrimSuffix(cmd, ".md")
			if _, ok := valid[cmd]; ok {
				continue
			}
			_ = os.Remove(filepath.Join(promptsDir, name))
		}
	}
	return nil
}

// marketplaceEntry mirrors the Python dict in the bash script.
type marketplaceEntry struct {
	Name     string          `json:"name"`
	Source   marketplaceSrc  `json:"source"`
	Policy   marketplacePol  `json:"policy"`
	Category string          `json:"category"`
}

type marketplaceSrc struct {
	Source string `json:"source"`
	Path   string `json:"path"`
}

type marketplacePol struct {
	Installation   string `json:"installation"`
	Authentication string `json:"authentication"`
}

func defaultEntry() marketplaceEntry {
	return marketplaceEntry{
		Name: "ck",
		Source: marketplaceSrc{
			Source: "local",
			Path:   "./plugins/ck",
		},
		Policy: marketplacePol{
			Installation:   "AVAILABLE",
			Authentication: "ON_INSTALL",
		},
		Category: "Productivity",
	}
}

func upsertMarketplace(path string) error {
	raw := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	}

	if _, ok := raw["name"]; !ok {
		raw["name"] = "local-plugins"
	}
	iface, _ := raw["interface"].(map[string]any)
	if iface == nil {
		iface = map[string]any{}
	}
	if _, ok := iface["displayName"]; !ok {
		iface["displayName"] = "Local Plugins"
	}
	raw["interface"] = iface

	plugins, _ := raw["plugins"].([]any)

	entryData, err := json.Marshal(defaultEntry())
	if err != nil {
		return err
	}
	var entryMap map[string]any
	if err := json.Unmarshal(entryData, &entryMap); err != nil {
		return err
	}

	found := false
	for i, p := range plugins {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if m["name"] == "ck" || m["name"] == "bp" {
			plugins[i] = entryMap
			found = true
			break
		}
	}
	if !found {
		plugins = append(plugins, entryMap)
	}
	raw["plugins"] = plugins

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o644)
}
