package codex

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginPresentNoClaudeDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	if pluginPresent() {
		t.Error("pluginPresent() = true with no HOME; want false")
	}
}

func TestPluginPresentByDirName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	plugDir := filepath.Join(home, ".claude", "plugins", "somehost", "codex")
	if err := os.MkdirAll(plugDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !pluginPresent() {
		t.Error("pluginPresent() = false with codex/ dir; want true")
	}
}

func TestPluginPresentByJSONManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	plugDir := filepath.Join(home, ".claude", "plugins", "local", "my-plugin")
	if err := os.MkdirAll(plugDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(plugDir, "plugin.json")
	if err := os.WriteFile(manifest, []byte(`{"name":"codex-companion"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !pluginPresent() {
		t.Error("pluginPresent() = false with JSON mentioning codex; want true")
	}
}

func TestPluginPresentByCodexHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !pluginPresent() {
		t.Error("pluginPresent() = false with ~/.codex; want true")
	}
}

func TestNudgeOnceWithoutBinary(t *testing.T) {
	project := t.TempDir()
	var buf bytes.Buffer
	a := Availability{BinaryAvailable: false, PluginPresent: false, Available: false}

	if got := Nudge(project, a, &buf); got == "" {
		t.Error("first Nudge returned empty; want message")
	}
	buf.Reset()
	if got := Nudge(project, a, &buf); got != "" {
		t.Errorf("second Nudge returned %q; want empty (marker should suppress)", got)
	}
}

func TestNudgeSkippedWhenAvailable(t *testing.T) {
	project := t.TempDir()
	var buf bytes.Buffer
	a := Availability{BinaryAvailable: true, PluginPresent: true, Available: true}
	if got := Nudge(project, a, &buf); got != "" {
		t.Errorf("Nudge returned %q when available; want empty", got)
	}
	if buf.Len() != 0 {
		t.Errorf("Nudge wrote %q when available; want nothing", buf.String())
	}
}

func TestNudgeBinaryButNoPlugin(t *testing.T) {
	project := t.TempDir()
	var buf bytes.Buffer
	// Not "available" per current rules, so the else branch needs a state
	// where Available is false but BinaryAvailable is true. The shell can't
	// produce this (binary alone sets available=true), but the logic path
	// exists. Force it:
	a := Availability{BinaryAvailable: true, PluginPresent: false, Available: false}
	got := Nudge(project, a, &buf)
	if got == "" {
		t.Fatal("expected setup nudge")
	}
	if !bytes.Contains(buf.Bytes(), []byte("codex setup")) {
		t.Errorf("nudge = %q; want mention of codex setup", got)
	}
}
