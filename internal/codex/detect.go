// Package codex implements Codex binary + plugin detection and adversarial
// review orchestration. Ports scripts/codex-*.sh to a cross-platform Go API.
package codex

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/JuliusBrussee/cavekit/internal/paths"
)

// Availability reports the result of a detection pass.
//
// BinaryAvailable is true when `codex --version` succeeds.
// PluginPresent is true when a Claude Code Codex plugin directory or the
// Codex user config directory (~/.codex) exists.
// Available collapses both flags with the shell's rule: binary alone is
// enough for review workflows; the plugin is optional.
type Availability struct {
	BinaryAvailable bool
	PluginPresent   bool
	Available       bool
}

// Detect probes the environment and returns the result. Never errors —
// absence of the binary/plugin is a valid state, not a failure.
func Detect(ctx context.Context) Availability {
	a := Availability{
		BinaryAvailable: binaryAvailable(ctx),
		PluginPresent:   pluginPresent(),
	}
	a.Available = a.BinaryAvailable
	return a
}

func binaryAvailable(ctx context.Context) bool {
	if _, err := exec.LookPath("codex"); err != nil {
		return false
	}
	cmd := exec.CommandContext(ctx, "codex", "--version")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

func pluginPresent() bool {
	claudeDir := paths.ClaudeDir()
	if dirHasCodexPlugin(filepath.Join(claudeDir, "plugins")) {
		return true
	}
	if dirHasCodexPlugin(filepath.Join(claudeDir, "plugins", "local")) {
		return true
	}
	codexHome := filepath.Join(paths.UserHome(), ".codex")
	if info, err := os.Stat(codexHome); err == nil && info.IsDir() {
		return true
	}
	return false
}

// dirHasCodexPlugin scans children of root for a Codex plugin. A child
// matches when it contains a codex/ or openai-codex/ subdirectory, or when
// any .json manifest within two levels mentions "codex" (case-insensitive).
// Mirrors the two bash loops over plugins/*/ and plugins/local/*/.
func dirHasCodexPlugin(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		child := filepath.Join(root, e.Name())
		if hasCodexNamedSubdir(child) {
			return true
		}
		if jsonTreeMentionsCodex(child, 2) {
			return true
		}
	}
	return false
}

func hasCodexNamedSubdir(dir string) bool {
	for _, name := range []string{"codex", "openai-codex"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// jsonTreeMentionsCodex walks up to maxDepth levels under root and returns
// true if any .json file contains "codex" (case-insensitive).
func jsonTreeMentionsCodex(root string, maxDepth int) bool {
	return walkJSON(root, maxDepth)
}

func walkJSON(dir string, remaining int) bool {
	if remaining < 0 {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if walkJSON(path, remaining-1) {
				return true
			}
			continue
		}
		if jsonMentionsCodex(path) {
			return true
		}
	}
	return false
}

func jsonMentionsCodex(path string) bool {
	if !strings.HasSuffix(strings.ToLower(path), ".json") {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "codex")
}

// Nudge prints a one-time installation hint if Codex is missing, keyed on a
// marker file under the project root. Returns the nudge that was printed (or
// the empty string if none) for testability.
func Nudge(projectRoot string, a Availability, w io.Writer) string {
	if a.Available {
		return ""
	}
	markerDir := filepath.Join(projectRoot, ".cavekit")
	marker := filepath.Join(markerDir, ".codex-nudge-shown")
	if _, err := os.Stat(marker); err == nil {
		return ""
	}

	var msg string
	switch {
	case !a.BinaryAvailable:
		msg = "Tip: Install Codex for adversarial code review: npm install -g @openai/codex"
	case !a.PluginPresent:
		msg = "Tip: Codex binary found but plugin not detected. Run: codex setup"
	}
	if msg == "" {
		return ""
	}
	if err := os.MkdirAll(markerDir, 0o755); err == nil {
		_ = os.WriteFile(marker, nil, 0o644)
	}
	fmt.Fprintln(w, msg)
	return msg
}
