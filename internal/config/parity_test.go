package config

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestShellParity runs representative bp-config.sh invocations and compares
// their stdout to the Go port. Skipped when bash is unavailable or when the
// legacy shell script is removed (which will happen once M4 retires it).
func TestShellParity(t *testing.T) {
	if runtime.GOOS == "windows" && os.Getenv("CAVEKIT_PARITY") == "" {
		t.Skip("set CAVEKIT_PARITY=1 to run shell parity on Windows")
	}
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found; skipping parity test")
	}

	script := resolveBashScript(t)

	tmp := t.TempDir()
	globalPath := filepath.Join(tmp, "home", ".cavekit", "config")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	shell := func(args ...string) string {
		cmd := exec.Command(bashPath, append([]string{script}, args...)...)
		cmd.Env = append(os.Environ(),
			"BP_GLOBAL_CONFIG_PATH="+globalPath,
			"BP_PROJECT_ROOT="+projectRoot,
		)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("bash %v failed: %v\n%s", args, err, out.String())
		}
		return strings.TrimRight(out.String(), "\r\n")
	}

	store := &Store{
		GlobalPath:  globalPath,
		ProjectPath: filepath.Join(projectRoot, ".cavekit", "config"),
	}

	cases := []struct {
		name    string
		shell   func() string
		goValue func() string
	}{
		{
			name:    "default preset",
			shell:   func() string { return shell("effective-preset") },
			goValue: func() string { p, _ := store.EffectivePreset(); return string(p) },
		},
		{
			name:    "default source",
			shell:   func() string { return shell("source", "bp_model_preset") },
			goValue: func() string { return string(store.Source("bp_model_preset")) },
		},
		{
			name:    "reasoning model",
			shell:   func() string { return shell("model", "reasoning") },
			goValue: func() string { m, _ := store.Model(TaskReasoning); return m },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.goValue()
			want := tc.shell()
			if got != want {
				t.Errorf("parity: go=%q shell=%q", got, want)
			}
		})
	}

	// After mutations — use shell to mutate then ensure Go sees the same state.
	shell("init")
	shell("set", "bp_model_preset", "fast", "--global")
	if got, want := string(store.Source("bp_model_preset")), shell("source", "bp_model_preset"); got != want {
		t.Errorf("post-set: go=%q shell=%q", got, want)
	}
	if gp, _ := store.EffectivePreset(); string(gp) != shell("effective-preset") {
		t.Errorf("post-set preset: go=%q shell=%q", gp, shell("effective-preset"))
	}

	shell("set", "bp_model_preset", "balanced", "--project")
	if gp, _ := store.Model(TaskExecution); gp != shell("model", "execution") {
		t.Errorf("balanced execution: go=%q shell=%q", gp, shell("model", "execution"))
	}
}

func resolveBashScript(t *testing.T) string {
	t.Helper()
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := here
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(root, "scripts", "bp-config.sh")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	t.Skip("scripts/bp-config.sh not found; skipping parity test")
	return ""
}
