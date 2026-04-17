//go:build windows

package mux

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JuliusBrussee/cavekit/internal/exec"
	"github.com/JuliusBrussee/cavekit/internal/paths"
)

// TestWTMultiplexer_EndToEnd exercises the real Windows daemon against the
// real ConPTY. It spawns a long-running child (cmd.exe) through the
// Multiplexer API, verifies Exists + ListSessions, then Kill.
//
// Skipped unless CAVEKIT_WINDOWS_E2E=1 so normal `go test ./...` does not
// spin up detached processes on developer machines.
func TestWTMultiplexer_EndToEnd(t *testing.T) {
	if os.Getenv("CAVEKIT_WINDOWS_E2E") != "1" {
		t.Skip("set CAVEKIT_WINDOWS_E2E=1 to run the wt multiplexer end-to-end test")
	}

	// Use a freshly-built cavekit.exe from the repo root. The daemon
	// subcommand lives in the main binary, so we need the binary on disk.
	binary, err := locateCavekitBinary(t)
	if err != nil {
		t.Fatalf("locate cavekit.exe: %v", err)
	}

	m := newWTMultiplexerWithSelf(exec.NewRealExecutor(), binary)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	name := "wt-e2e-" + time.Now().Format("150405")
	if err := m.CreateSession(ctx, name, "", "cmd.exe"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if !m.Exists(ctx, name) {
		t.Fatal("Exists returned false after CreateSession")
	}

	sessions, err := m.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	found := false
	for _, s := range sessions {
		if s == SanitizeName(name) {
			found = true
		}
	}
	if !found {
		t.Errorf("session %q not in ListSessions: %v", name, sessions)
	}

	// Send a harmless noop so we exercise SendKeys too.
	if err := m.SendKeys(ctx, name, "cls", "Enter"); err != nil {
		t.Errorf("SendKeys: %v", err)
	}

	// Give cmd.exe a tick to render before capturing.
	time.Sleep(300 * time.Millisecond)
	pane, err := m.CapturePane(ctx, name)
	if err != nil {
		t.Errorf("CapturePane: %v", err)
	}
	if len(pane) == 0 {
		t.Error("CapturePane returned empty buffer")
	}

	if err := m.Kill(ctx, name); err != nil {
		t.Errorf("Kill: %v", err)
	}
	// After Kill, metadata file should disappear.
	time.Sleep(300 * time.Millisecond)
	stateFile := filepath.Join(paths.SessionsDir(), SanitizeName(name)+".json")
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Errorf("session metadata not cleaned up: %v (state still at %s)", err, stateFile)
	}
}

// newWTMultiplexerWithSelf builds a wtMultiplexer that will spawn the given
// cavekit binary as its mux-daemon. Lets the test target a deterministic
// binary path instead of relying on os.Executable() (which returns the
// `go test` harness inside `go test ./...`).
func newWTMultiplexerWithSelf(executor exec.Executor, selfPath string) *wtMultiplexer {
	m := newWTMultiplexer(executor)
	m.selfPath = selfPath
	return m
}

// locateCavekitBinary returns the path to cavekit.exe the test should use.
// Expects the binary at repo root — builds once if missing.
func locateCavekitBinary(t *testing.T) (string, error) {
	t.Helper()
	repoRoot, err := findRepoRoot()
	if err != nil {
		return "", err
	}
	bin := filepath.Join(repoRoot, "cavekit.exe")
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	t.Logf("cavekit.exe not found at %s — run `go build -o cavekit.exe ./cmd/cavekit` before running E2E", bin)
	return "", os.ErrNotExist
}

// findRepoRoot walks up from CWD until it sees go.mod.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
