package paths

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestUserHomeNonEmpty(t *testing.T) {
	if UserHome() == "" {
		t.Skip("no home dir in this environment")
	}
}

func TestClaudeDirEndsInClaude(t *testing.T) {
	got := ClaudeDir()
	if filepath.Base(got) != ".claude" {
		t.Errorf("ClaudeDir() = %q, want suffix .claude", got)
	}
}

func TestCavekitDirEndsInCavekit(t *testing.T) {
	if filepath.Base(CavekitDir()) != ".cavekit" {
		t.Errorf("CavekitDir() = %q, want suffix .cavekit", CavekitDir())
	}
}

func TestStateFile(t *testing.T) {
	got := StateFile()
	if !strings.HasSuffix(got, "state.json") {
		t.Errorf("StateFile() = %q, want suffix state.json", got)
	}
}

func TestProjectConfigFile(t *testing.T) {
	got := ProjectConfigFile("/proj")
	want := filepath.Join("/proj", ".cavekit", "config")
	if got != want {
		t.Errorf("ProjectConfigFile() = %q, want %q", got, want)
	}
}

func TestTempDirNonEmpty(t *testing.T) {
	if TempDir() == "" {
		t.Errorf("TempDir() returned empty")
	}
}

func TestTempDirEnvOverride(t *testing.T) {
	t.Setenv("CAVEKIT_TMPDIR", "/custom/tmp")
	if got := TempDir(); got != "/custom/tmp" {
		t.Errorf("TempDir() = %q, want /custom/tmp", got)
	}
}

func TestBinDirNonEmpty(t *testing.T) {
	if BinDir() == "" {
		t.Errorf("BinDir() returned empty")
	}
}

func TestMarketplaceDirSuffix(t *testing.T) {
	got := MarketplaceDir()
	if filepath.Base(got) != "cavekit-marketplace" {
		t.Errorf("MarketplaceDir() = %q, want suffix cavekit-marketplace", got)
	}
}
