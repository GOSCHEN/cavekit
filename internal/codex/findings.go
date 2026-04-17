package codex

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// findingsRelPath is the canonical location for the findings table, relative
// to the repository root.
const findingsRelPath = "context/impl/impl-review-findings.md"

// FindingsStore reads and writes the Markdown-table findings file.
//
// The table format is seven columns: Finding, Severity, File, Status, Source,
// Tier, Task. An older five-column format is migrated automatically the
// first time Init runs against it.
type FindingsStore struct {
	Path string
	// Now is injected so tests can assert on timestamps.
	Now func() time.Time
}

// NewFindingsStore resolves the findings path via `git rev-parse
// --show-toplevel`, falling back to the current directory when git is absent.
func NewFindingsStore() *FindingsStore {
	root := gitRoot()
	return &FindingsStore{
		Path: filepath.Join(root, findingsRelPath),
		Now:  time.Now,
	}
}

func gitRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "."
	}
	return strings.TrimSpace(string(out))
}

const headerRow = "| Finding | Severity | File | Status | Source | Tier | Task |"
const separatorRow = "|---------|----------|------|--------|--------|------|------|"

const legacyHeaderRow = "| Finding | Severity | File | Status | Task |"
const legacySeparatorRow = "|---------|----------|------|--------|------|"

// Init ensures the findings file exists with the current seven-column schema,
// migrating a legacy five-column file in place.
func (s *FindingsStore) Init() error {
	if _, err := os.Stat(s.Path); err == nil {
		return s.migrateIfLegacy()
	}

	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	now := s.Now().UTC().Format("2006-01-02T15:04:05Z")
	body := fmt.Sprintf("---\ncreated: %q\nlast_edited: %q\n---\n\n# Review Findings\n\n%s\n%s\n",
		now, now, headerRow, separatorRow)
	return os.WriteFile(s.Path, []byte(body), 0o644)
}

func (s *FindingsStore) migrateIfLegacy() error {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return err
	}
	content := string(data)
	if !strings.Contains(content, legacyHeaderRow) {
		return nil
	}
	content = strings.Replace(content, legacyHeaderRow, headerRow, 1)
	content = strings.Replace(content, legacySeparatorRow, separatorRow, 1)
	return os.WriteFile(s.Path, []byte(content), 0o644)
}

// NextID returns the next available finding ID (F-001, F-002, …). Scans the
// file for existing IDs; returns F-001 when the file is missing.
func (s *FindingsStore) NextID() (string, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return "F-001", nil
		}
		return "", err
	}

	re := regexp.MustCompile(`F-(\d+)`)
	matches := re.FindAllStringSubmatch(string(data), -1)
	seen := make(map[int]struct{}, len(matches))
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		seen[n] = struct{}{}
	}
	max := 0
	for n := range seen {
		if n > max {
			max = n
		}
	}
	return fmt.Sprintf("F-%03d", max+1), nil
}

// Finding is a row in the table.
type Finding struct {
	ID          string
	Description string
	Severity    string
	File        string
	Status      string
	Source      string
	Tier        int
	Task        string
}

// Append initializes the file if needed, allocates a new ID, and writes a
// row with status NEW. Returns the assigned ID.
func (s *FindingsStore) Append(f Finding) (string, error) {
	if f.Severity == "" {
		return "", fmt.Errorf("severity required (P0-P3)")
	}
	if f.File == "" {
		return "", fmt.Errorf("file required")
	}
	if f.Description == "" {
		return "", fmt.Errorf("description required")
	}
	if f.Source == "" {
		return "", fmt.Errorf("source required")
	}
	if f.Tier == 0 {
		return "", fmt.Errorf("tier required")
	}
	if err := s.Init(); err != nil {
		return "", err
	}
	id, err := s.NextID()
	if err != nil {
		return "", err
	}
	task := f.Task
	if task == "" {
		task = "—"
	}
	line := fmt.Sprintf("| %s: %s | %s | %s | NEW | %s | %d | %s |\n",
		id, f.Description, f.Severity, f.File, f.Source, f.Tier, task)

	file, err := os.OpenFile(s.Path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := file.WriteString(line); err != nil {
		return "", err
	}
	return id, nil
}

// UpdateStatus rewrites the status column for the row matching findingID.
func (s *FindingsStore) UpdateStatus(findingID, newStatus string) error {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("findings file not found")
		}
		return err
	}

	prefix := "| " + findingID + ":"
	lines := strings.Split(string(data), "\n")
	matched := false
	for i, line := range lines {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) < 5 {
			continue
		}
		cols[4] = " " + newStatus + " "
		lines[i] = strings.Join(cols, "|")
		matched = true
	}
	if !matched {
		return fmt.Errorf("finding %s not found", findingID)
	}
	return os.WriteFile(s.Path, []byte(strings.Join(lines, "\n")), 0o644)
}

// BlockingFinding is the compact representation emitted by ListBlocking.
type BlockingFinding struct {
	Finding  string
	Severity string
	File     string
}

// ListBlocking returns P0/P1 findings whose status is still NEW, in file
// order. Returns an empty slice when the file does not exist.
func (s *FindingsStore) ListBlocking() ([]BlockingFinding, error) {
	file, err := os.Open(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var out []BlockingFinding
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if !strings.HasPrefix(line, "|") {
			continue
		}
		if strings.HasPrefix(line, "| Finding") || strings.HasPrefix(line, "|-") {
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) < 6 {
			continue
		}
		severity := strings.TrimSpace(cols[2])
		status := strings.TrimSpace(cols[4])
		if (severity == "P0" || severity == "P1") && status == "NEW" {
			out = append(out, BlockingFinding{
				Finding:  strings.TrimSpace(cols[1]),
				Severity: severity,
				File:     strings.TrimSpace(cols[3]),
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	// Deterministic ordering by finding ID prefix so callers that slice the
	// slice (and stringify) produce stable output.
	sort.SliceStable(out, func(i, j int) bool {
		return extractID(out[i].Finding) < extractID(out[j].Finding)
	})
	return out, nil
}

func extractID(finding string) string {
	if i := strings.IndexByte(finding, ':'); i > 0 {
		return finding[:i]
	}
	return finding
}
