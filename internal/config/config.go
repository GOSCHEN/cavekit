// Package config implements Cavekit's two-tier configuration store.
//
// Lookup order: project (.cavekit/config under repo root) then global
// (~/.cavekit/config) then built-in default. The first non-empty value wins.
// File format is a tiny key=value grammar compatible with scripts/bp-config.sh
// so both the Go port and legacy shell script can read the same files.
package config

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/JuliusBrussee/cavekit/internal/paths"
)

// Scope identifies which config file a Set or List targets.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
)

// Source identifies where an effective value came from.
type Source string

const (
	SourceProject Source = "project"
	SourceGlobal  Source = "global"
	SourceDefault Source = "default"
)

// Keys lists every well-known config key. Order matches the bash script so
// init-emitted files diff cleanly against the legacy implementation.
var Keys = []string{
	"bp_model_preset",
	"codex_review",
	"codex_model",
	"codex_effort",
	"tier_gate_mode",
	"command_gate",
	"command_gate_model",
	"command_gate_timeout",
	"command_gate_allowlist",
	"command_gate_blocklist",
	"speculative_review",
	"speculative_review_timeout",
	"caveman_mode",
	"caveman_phases",
}

var defaults = map[string]string{
	"bp_model_preset":            "quality",
	"codex_review":               "auto",
	"codex_model":                "",
	"codex_effort":               "",
	"tier_gate_mode":             "severity",
	"command_gate":               "all",
	"command_gate_model":         "o4-mini",
	"command_gate_timeout":       "3000",
	"command_gate_allowlist":     "",
	"command_gate_blocklist":     "",
	"speculative_review":         "",
	"speculative_review_timeout": "",
	"caveman_mode":               "on",
	"caveman_phases":             "build,inspect",
}

// Default returns the built-in default for key, or the empty string for an
// unknown key (mirrors bash default case).
func Default(key string) string {
	return defaults[key]
}

// Store resolves and mutates the two config files.
//
// Paths are computed once at construction so tests can inject overrides via
// NewStoreFromEnv without racing against directory changes.
type Store struct {
	GlobalPath  string
	ProjectPath string
}

// NewStore returns a Store using the process environment (BP_GLOBAL_CONFIG_PATH,
// BP_PROJECT_CONFIG_PATH, BP_PROJECT_ROOT) with git-root and $HOME fallbacks.
func NewStore() *Store {
	return &Store{
		GlobalPath:  resolveGlobalPath(),
		ProjectPath: resolveProjectPath(),
	}
}

func resolveGlobalPath() string {
	if v := os.Getenv("BP_GLOBAL_CONFIG_PATH"); v != "" {
		return v
	}
	return paths.UserConfigFile()
}

func resolveProjectPath() string {
	if v := os.Getenv("BP_PROJECT_CONFIG_PATH"); v != "" {
		return v
	}
	root := os.Getenv("BP_PROJECT_ROOT")
	if root == "" {
		root = gitToplevel()
	}
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}
		root = cwd
	}
	return paths.ProjectConfigFile(root)
}

func gitToplevel() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Get returns the effective value for key, falling back to the built-in
// default when both config files are silent.
func (s *Store) Get(key string) string {
	if v := readValue(s.ProjectPath, key); v != "" {
		return v
	}
	if v := readValue(s.GlobalPath, key); v != "" {
		return v
	}
	return Default(key)
}

// GetWithDefault behaves like Get but substitutes fallback for the final
// default tier. Mirrors `bp_config_get key fallback` from the bash script.
func (s *Store) GetWithDefault(key, fallback string) string {
	if v := readValue(s.ProjectPath, key); v != "" {
		return v
	}
	if v := readValue(s.GlobalPath, key); v != "" {
		return v
	}
	return fallback
}

// Source reports which tier supplied the effective value for key.
func (s *Store) Source(key string) Source {
	if v := readValue(s.ProjectPath, key); v != "" {
		return SourceProject
	}
	if v := readValue(s.GlobalPath, key); v != "" {
		return SourceGlobal
	}
	return SourceDefault
}

// SourcePath returns the file that supplied the effective value, or the
// placeholder "(built-in default)" when neither file matches.
func (s *Store) SourcePath(key string) string {
	switch s.Source(key) {
	case SourceProject:
		return s.ProjectPath
	case SourceGlobal:
		return s.GlobalPath
	default:
		return "(built-in default)"
	}
}

// Set validates value, then writes it to the global or project file, creating
// parent directories as needed. An existing key= line is replaced in place;
// missing keys are appended.
func (s *Store) Set(key, value string, scope Scope) error {
	if err := Validate(key, value); err != nil {
		return err
	}
	target := s.ProjectPath
	if scope == ScopeGlobal {
		target = s.GlobalPath
	}
	return writeKey(target, key, value)
}

// Init creates the global file (seeded with every default) and project file
// (empty, header only), backfilling any keys missing from an existing global
// file. Matches `bp_config_init`.
func (s *Store) Init() error {
	if err := initGlobal(s.GlobalPath); err != nil {
		return err
	}
	return initProject(s.ProjectPath)
}

// List returns the raw key=value lines from a single tier's file.
func (s *Store) List(scope Scope) ([]string, error) {
	target := s.ProjectPath
	if scope == ScopeGlobal {
		target = s.GlobalPath
	}
	f, err := os.Open(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if looksLikeConfigLine(line) {
			out = append(out, line)
		}
	}
	return out, scanner.Err()
}

func looksLikeConfigLine(line string) bool {
	eq := strings.IndexByte(line, '=')
	if eq <= 0 {
		return false
	}
	for i := 0; i < eq; i++ {
		c := line[i]
		if !(c == '_' || (c >= 'a' && c <= 'z')) {
			return false
		}
	}
	return true
}

// readValue reads the last occurrence of key in file, returning an empty
// string when the file or key is absent. Matches bash `grep | tail -1 | cut`.
func readValue(file, key string) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()

	prefix := key + "="
	last := ""
	scanner := bufio.NewScanner(f)
	// CRLF tolerance: Scanner strips "\n"; strip trailing "\r" ourselves so
	// files edited on Windows with notepad don't produce trailing carriage
	// returns in Go-read values.
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.HasPrefix(line, prefix) {
			last = strings.TrimPrefix(line, prefix)
		}
	}
	return last
}

func writeKey(file, key, value string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	prefix := key + "="
	data, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	lines := splitLines(data)
	replaced := false
	for i, line := range lines {
		stripped := strings.TrimRight(line, "\r")
		if strings.HasPrefix(stripped, prefix) {
			lines[i] = prefix + value
			replaced = true
		}
	}
	if !replaced {
		lines = append(lines, prefix+value)
	}
	return os.WriteFile(file, joinLines(lines), 0o644)
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	trimmed := bytes.TrimRight(data, "\n")
	return strings.Split(string(trimmed), "\n")
}

func joinLines(lines []string) []byte {
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func initGlobal(file string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(file); os.IsNotExist(err) {
		var buf bytes.Buffer
		buf.WriteString("# Cavekit configuration\n")
		buf.WriteString("# User-level defaults\n")
		buf.WriteString("# See: scripts/bp-config.sh for documentation\n\n")
		for _, key := range Keys {
			fmt.Fprintf(&buf, "%s=%s\n", key, Default(key))
		}
		return os.WriteFile(file, buf.Bytes(), 0o644)
	} else if err != nil {
		return err
	}

	existing, err := existingKeys(file)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, key := range Keys {
		if existing[key] {
			continue
		}
		if _, err := fmt.Fprintf(f, "%s=%s\n", key, Default(key)); err != nil {
			return err
		}
	}
	return nil
}

func existingKeys(file string) (map[string]bool, error) {
	out := make(map[string]bool)
	f, err := os.Open(file)
	if err != nil {
		return out, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		out[line[:eq]] = true
	}
	return out, scanner.Err()
}

func initProject(file string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		return err
	}
	header := "# Cavekit configuration\n" +
		"# Project-level overrides\n" +
		"# Add only the keys you want this repo to override.\n" +
		"# See: scripts/bp-config.sh for documentation\n"
	return os.WriteFile(file, []byte(header), 0o644)
}

// Validate returns nil if value is acceptable for key. Rules mirror
// `_bp_config_validate` in scripts/bp-config.sh.
func Validate(key, value string) error {
	switch key {
	case "bp_model_preset":
		return oneOf(key, value, "expensive", "quality", "balanced", "fast")
	case "codex_review":
		return oneOf(key, value, "auto", "off")
	case "tier_gate_mode":
		return oneOf(key, value, "severity", "strict", "permissive", "off")
	case "command_gate":
		return oneOf(key, value, "all", "interactive", "off")
	case "command_gate_timeout", "speculative_review_timeout":
		if value == "" {
			return invalid(key, value, "positive integer")
		}
		if _, err := strconv.Atoi(value); err != nil {
			return invalid(key, value, "positive integer")
		}
		return nil
	case "speculative_review":
		return oneOf(key, value, "on", "off")
	case "caveman_mode":
		return oneOf(key, value, "on", "off")
	case "caveman_phases":
		for _, phase := range splitPhases(value) {
			if !isKnownPhase(phase) {
				return fmt.Errorf("invalid phase %q in %q (allowed: build,inspect,draft,architect)", phase, key)
			}
		}
		return nil
	}
	return nil
}

func splitPhases(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func isKnownPhase(p string) bool {
	switch p {
	case "build", "inspect", "draft", "architect":
		return true
	}
	return false
}

func oneOf(key, value string, allowed ...string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return invalid(key, value, strings.Join(allowed, " "))
}

func invalid(key, value, allowed string) error {
	return fmt.Errorf("invalid value %q for %q (allowed: %s)", value, key, allowed)
}

// SortedKeys returns Keys sorted lexicographically. Helper for callers that
// need stable output order independent of insertion order.
func SortedKeys() []string {
	out := make([]string, len(Keys))
	copy(out, Keys)
	sort.Strings(out)
	return out
}
