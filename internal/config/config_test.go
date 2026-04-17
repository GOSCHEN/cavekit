package config

import (
	"os"
	"path/filepath"
	"testing"
)

// newTestStore creates a Store pointing at temp files under t.TempDir so each
// subtest is hermetic. Mirrors scripts/test-bp-config.sh's BP_PROJECT_ROOT +
// BP_GLOBAL_CONFIG_PATH environment dance.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return &Store{
		GlobalPath:  filepath.Join(dir, "home", ".cavekit", "config"),
		ProjectPath: filepath.Join(dir, "project", ".cavekit", "config"),
	}
}

func TestDefaults(t *testing.T) {
	s := newTestStore(t)

	preset, err := s.EffectivePreset()
	if err != nil {
		t.Fatalf("EffectivePreset: %v", err)
	}
	if preset != PresetQuality {
		t.Errorf("default preset = %q, want quality", preset)
	}

	cases := map[TaskType]string{
		TaskReasoning:   "opus",
		TaskExecution:   "opus",
		TaskExploration: "sonnet",
	}
	for task, want := range cases {
		got, err := s.Model(task)
		if err != nil {
			t.Fatalf("Model(%s): %v", task, err)
		}
		if got != want {
			t.Errorf("Model(%s) = %q, want %q", task, got, want)
		}
	}

	if src := s.Source("bp_model_preset"); src != SourceDefault {
		t.Errorf("Source = %q, want default", src)
	}
}

func TestGlobalSetAndLookup(t *testing.T) {
	s := newTestStore(t)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Set("bp_model_preset", "fast", ScopeGlobal); err != nil {
		t.Fatalf("Set global: %v", err)
	}

	preset, err := s.EffectivePreset()
	if err != nil {
		t.Fatal(err)
	}
	if preset != PresetFast {
		t.Errorf("preset = %q, want fast", preset)
	}
	if src := s.Source("bp_model_preset"); src != SourceGlobal {
		t.Errorf("Source = %q, want global", src)
	}

	if m, _ := s.Model(TaskReasoning); m != "sonnet" {
		t.Errorf("fast reasoning = %q, want sonnet", m)
	}
	if m, _ := s.Model(TaskExploration); m != "haiku" {
		t.Errorf("fast exploration = %q, want haiku", m)
	}
}

func TestProjectOverridesGlobal(t *testing.T) {
	s := newTestStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("bp_model_preset", "fast", ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("bp_model_preset", "balanced", ScopeProject); err != nil {
		t.Fatal(err)
	}

	preset, err := s.EffectivePreset()
	if err != nil {
		t.Fatal(err)
	}
	if preset != PresetBalanced {
		t.Errorf("preset = %q, want balanced", preset)
	}
	if src := s.Source("bp_model_preset"); src != SourceProject {
		t.Errorf("Source = %q, want project", src)
	}

	cases := map[TaskType]string{
		TaskReasoning:   "opus",
		TaskExecution:   "sonnet",
		TaskExploration: "haiku",
	}
	for task, want := range cases {
		got, _ := s.Model(task)
		if got != want {
			t.Errorf("balanced %s = %q, want %q", task, got, want)
		}
	}
}

func TestInvalidPresetRejected(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("bp_model_preset", "invalid", ScopeProject); err == nil {
		t.Error("expected Set to reject invalid preset")
	}
}

func TestRoundTripFreeFormKey(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("codex_model", "gpt-5.4-mini", ScopeProject); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("codex_model"); got != "gpt-5.4-mini" {
		t.Errorf("codex_model = %q, want gpt-5.4-mini", got)
	}
}

func TestInitPreservesExistingProjectValue(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("codex_model", "gpt-5.4-mini", ScopeProject); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("codex_model"); got != "gpt-5.4-mini" {
		t.Errorf("Init wiped project value, got %q", got)
	}
}

func TestInitBackfillsMissingGlobalKeys(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(filepath.Dir(s.GlobalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.GlobalPath, []byte("bp_model_preset=fast\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	for _, key := range Keys {
		if got := readValue(s.GlobalPath, key); got != Default(key) && key != "bp_model_preset" {
			t.Errorf("after Init, %s = %q, want default %q", key, got, Default(key))
		}
	}
	if got := readValue(s.GlobalPath, "bp_model_preset"); got != "fast" {
		t.Errorf("Init overwrote user value: %s", got)
	}
}

func TestCavemanActive(t *testing.T) {
	s := newTestStore(t)
	active, err := s.CavemanActive("build")
	if err != nil || !active {
		t.Errorf("default caveman build: active=%v err=%v, want true/nil", active, err)
	}
	active, err = s.CavemanActive("draft")
	if err != nil || active {
		t.Errorf("default caveman draft: active=%v err=%v, want false/nil", active, err)
	}

	if err := s.Set("caveman_mode", "off", ScopeProject); err != nil {
		t.Fatal(err)
	}
	active, err = s.CavemanActive("build")
	if err != nil || active {
		t.Errorf("caveman_mode=off build: active=%v err=%v, want false/nil", active, err)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		key   string
		value string
		ok    bool
	}{
		{"bp_model_preset", "quality", true},
		{"bp_model_preset", "balanced", true},
		{"bp_model_preset", "turbo", false},
		{"codex_review", "auto", true},
		{"codex_review", "no", false},
		{"tier_gate_mode", "severity", true},
		{"command_gate", "interactive", true},
		{"command_gate_timeout", "3000", true},
		{"command_gate_timeout", "abc", false},
		{"command_gate_timeout", "", false},
		{"speculative_review", "on", true},
		{"caveman_mode", "off", true},
		{"caveman_phases", "build,inspect", true},
		{"caveman_phases", "build,ouch", false},
		{"caveman_phases", "", true},
		{"codex_model", "anything", true},
	}
	for _, tc := range cases {
		err := Validate(tc.key, tc.value)
		if tc.ok && err != nil {
			t.Errorf("Validate(%s=%q) unexpected error: %v", tc.key, tc.value, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("Validate(%s=%q) expected error", tc.key, tc.value)
		}
	}
}

func TestList(t *testing.T) {
	s := newTestStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("bp_model_preset", "balanced", ScopeProject); err != nil {
		t.Fatal(err)
	}
	lines, err := s.List(ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "bp_model_preset=balanced" {
		t.Errorf("List project = %v, want [bp_model_preset=balanced]", lines)
	}

	lines, err = s.List(ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) < len(Keys) {
		t.Errorf("global list has %d entries, want >= %d", len(lines), len(Keys))
	}
}

func TestSetReplacesInPlace(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("bp_model_preset", "fast", ScopeProject); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("bp_model_preset", "quality", ScopeProject); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if countOccurrences(content, "bp_model_preset=") != 1 {
		t.Errorf("expected exactly one bp_model_preset= line, got:\n%s", content)
	}
}

func TestCRLFTolerance(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(filepath.Dir(s.ProjectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.ProjectPath, []byte("bp_model_preset=balanced\r\ncodex_model=foo\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("bp_model_preset"); got != "balanced" {
		t.Errorf("CRLF: bp_model_preset = %q, want balanced", got)
	}
	if got := s.Get("codex_model"); got != "foo" {
		t.Errorf("CRLF: codex_model = %q, want foo", got)
	}
}

func countOccurrences(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}
