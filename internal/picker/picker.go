// Package picker renders an interactive multi-select over the project's
// build-site / plan files. Replaces scripts/cavekit-picker.ts with a pure
// Go implementation so the plugin does not depend on Node/tsx.
package picker

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FrontierStatus enumerates the lifecycle states the picker renders.
type FrontierStatus string

const (
	StatusAvailable  FrontierStatus = "available"
	StatusInProgress FrontierStatus = "in-progress"
	StatusDone       FrontierStatus = "done"
)

// Frontier is one discovered build-site / plan file.
type Frontier struct {
	Path       string
	Name       string
	Total      int
	Done       int
	Status     FrontierStatus
}

var (
	taskCellRe = regexp.MustCompile(`\|\s*T-(?:[A-Za-z0-9]+-)*\d+\s*\|`)
	taskIDRe   = regexp.MustCompile(`T-(?:[A-Za-z0-9]+-)*\d+`)
	doneRe     = regexp.MustCompile(`(?i)\b(T-(?:[A-Za-z0-9]+-)*\d+)\b.*?\bDONE\b`)
)

// DiscoverFrontiers returns every eligible build-site or plan file in the
// current project root, including archived ones (flagged as Done).
func DiscoverFrontiers(projectRoot string) []Frontier {
	dir := firstExisting(
		filepath.Join(projectRoot, "context", "plans"),
		filepath.Join(projectRoot, "context", "sites"),
	)
	if dir == "" {
		return nil
	}

	doneSet := map[string]struct{}{}
	scanImplFiles(filepath.Join(projectRoot, "context", "impl"), doneSet)
	scanSiblingWorktreeImpls(projectRoot, doneSet)

	var out []Frontier
	appendFrontiers(dir, false, doneSet, &out)

	archiveDir := filepath.Join(dir, "archive")
	if fi, err := os.Stat(archiveDir); err == nil && fi.IsDir() {
		appendFrontiers(archiveDir, true, doneSet, &out)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Status != out[j].Status {
			return statusOrder(out[i].Status) < statusOrder(out[j].Status)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func statusOrder(s FrontierStatus) int {
	switch s {
	case StatusAvailable:
		return 0
	case StatusInProgress:
		return 1
	case StatusDone:
		return 2
	}
	return 3
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
	}
	return ""
}

func appendFrontiers(dir string, archived bool, doneSet map[string]struct{}, out *[]Frontier) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		content := string(data)
		total := len(taskCellRe.FindAllString(content, -1))
		f := Frontier{
			Path:  full,
			Name:  deriveName(e.Name()),
			Total: total,
		}
		if archived {
			f.Done = total
			f.Status = StatusDone
		} else {
			f.Done = countDoneForFrontier(content, doneSet)
			f.Status = detectStatus(filepath.Dir(filepath.Dir(dir)), f.Name, f.Total, f.Done)
		}
		*out = append(*out, f)
	}
}

func deriveName(filename string) string {
	base := strings.TrimSuffix(filename, ".md")
	for _, prefix := range []string{"plan-", "feature-frontier-", "feature-", "build-site-"} {
		base = strings.TrimPrefix(base, prefix)
	}
	// Collapse "frontier" substrings that sometimes linger after the primary
	// prefix has been stripped.
	base = strings.ReplaceAll(base, "-frontier-", "-")
	base = strings.TrimPrefix(base, "frontier-")
	base = strings.TrimSuffix(base, "-frontier")
	return strings.Trim(base, "-")
}

// scanImplFiles walks dir (recursively) for impl-*.md files and records
// every task ID associated with the word DONE.
func scanImplFiles(dir string, doneSet map[string]struct{}) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			scanImplFiles(full, doneSet)
			continue
		}
		if !strings.HasPrefix(e.Name(), "impl-") || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		for _, m := range doneRe.FindAllStringSubmatch(string(data), -1) {
			doneSet[m[1]] = struct{}{}
		}
	}
}

func scanSiblingWorktreeImpls(projectRoot string, doneSet map[string]struct{}) {
	parent := filepath.Dir(projectRoot)
	name := filepath.Base(projectRoot)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	prefix := name + "-cavekit-"
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		scanImplFiles(filepath.Join(parent, e.Name(), "context", "impl"), doneSet)
	}
}

func countDoneForFrontier(content string, doneSet map[string]struct{}) int {
	ids := taskIDRe.FindAllString(content, -1)
	seen := map[string]struct{}{}
	count := 0
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if _, ok := doneSet[id]; ok {
			count++
		}
	}
	return count
}

func detectStatus(projectRoot, frontierName string, total, done int) FrontierStatus {
	if total > 0 && done >= total {
		return StatusDone
	}
	projectName := filepath.Base(projectRoot)
	worktree := filepath.Join(filepath.Dir(projectRoot), fmt.Sprintf("%s-cavekit-%s", projectName, frontierName))
	if fi, err := os.Stat(worktree); err == nil && fi.IsDir() {
		if _, err := os.Stat(filepath.Join(worktree, ".claude", "ralph-loop.local.md")); err == nil {
			return StatusInProgress
		}
		if done > 0 {
			return StatusInProgress
		}
	}
	return StatusAvailable
}

// ── Bubbletea model ───────────────────────────────────────────────────

type item struct {
	frontier Frontier
	sep      bool
	header   string
}

type model struct {
	items    []item
	cursor   int
	selected map[int]struct{}
	done     bool
	canceled bool
}

func newModel(frontiers []Frontier) model {
	var items []item
	addGroup := func(status FrontierStatus, header string, defaultSelected bool) {
		var group []Frontier
		for _, f := range frontiers {
			if f.Status == status {
				group = append(group, f)
			}
		}
		if len(group) == 0 {
			return
		}
		items = append(items, item{sep: true, header: header})
		for _, f := range group {
			items = append(items, item{frontier: f})
		}
		_ = defaultSelected
	}
	addGroup(StatusAvailable, "── Available ──", true)
	addGroup(StatusInProgress, "── In Progress (select to resume) ──", false)
	addGroup(StatusDone, "── Done ──", false)

	sel := map[int]struct{}{}
	for i, it := range items {
		if !it.sep && it.frontier.Status == StatusAvailable {
			sel[i] = struct{}{}
		}
	}

	// Place cursor on the first non-separator item.
	cursor := 0
	for i, it := range items {
		if !it.sep {
			cursor = i
			break
		}
	}

	return model{items: items, cursor: cursor, selected: sel}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.canceled = true
			return m, tea.Quit
		case "enter":
			m.done = true
			return m, tea.Quit
		case "down", "j":
			m.cursor = nextSelectable(m.items, m.cursor, 1)
		case "up", "k":
			m.cursor = nextSelectable(m.items, m.cursor, -1)
		case " ":
			if !m.items[m.cursor].sep && m.items[m.cursor].frontier.Status != StatusDone {
				if _, ok := m.selected[m.cursor]; ok {
					delete(m.selected, m.cursor)
				} else {
					m.selected[m.cursor] = struct{}{}
				}
			}
		case "a":
			// Toggle all non-Done rows between selected/unselected in one pass.
			allSelected := true
			for i, it := range m.items {
				if it.sep || it.frontier.Status == StatusDone {
					continue
				}
				if _, ok := m.selected[i]; !ok {
					allSelected = false
					break
				}
			}
			for i, it := range m.items {
				if it.sep || it.frontier.Status == StatusDone {
					continue
				}
				if allSelected {
					delete(m.selected, i)
				} else {
					m.selected[i] = struct{}{}
				}
			}
		}
	}
	return m, nil
}

func nextSelectable(items []item, cursor, delta int) int {
	for i := 0; i < len(items); i++ {
		cursor += delta
		if cursor < 0 {
			cursor = len(items) - 1
		}
		if cursor >= len(items) {
			cursor = 0
		}
		if !items[cursor].sep {
			return cursor
		}
	}
	return cursor
}

func (m model) View() string {
	var b strings.Builder
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	strike := lipgloss.NewStyle().Strikethrough(true).Foreground(lipgloss.Color("240"))

	b.WriteString("Select build sites to launch\n\n")

	for i, it := range m.items {
		if it.sep {
			b.WriteString("  ")
			b.WriteString(dim.Render(it.header))
			b.WriteString("\n")
			continue
		}
		marker := "[ ]"
		if it.frontier.Status == StatusDone {
			marker = "[-]"
		} else if _, ok := m.selected[i]; ok {
			marker = "[x]"
		}
		cursor := " "
		if i == m.cursor {
			cursor = ">"
		}

		var label string
		progress := "?"
		if it.frontier.Total > 0 {
			progress = fmt.Sprintf("%d/%d", it.frontier.Done, it.frontier.Total)
		}

		switch it.frontier.Status {
		case StatusDone:
			label = strike.Render(it.frontier.Name) + " " + dim.Render("("+progress+" done)")
		case StatusInProgress:
			label = yellow.Render("⟳") + " " + it.frontier.Name + " " + dim.Render("("+progress+")")
		case StatusAvailable:
			label = it.frontier.Name + " " + dim.Render("("+progress+" tasks)")
		}
		fmt.Fprintf(&b, " %s %s %s\n", cursor, marker, label)
	}
	b.WriteString("\n")
	b.WriteString(dim.Render("[space] toggle  [a] toggle all  [enter] launch  [q/ctrl+c] quit"))
	b.WriteString("\n")
	return b.String()
}

// ── Entry point ───────────────────────────────────────────────────────

// Run launches the interactive picker. Returns the selected paths, or nil
// when the user cancelled.
func Run(projectRoot string, output io.Writer) ([]string, error) {
	frontiers := DiscoverFrontiers(projectRoot)
	if len(frontiers) == 0 {
		return nil, fmt.Errorf("no frontiers found in context/plans/ or context/sites/\nRun /ck:map first to generate one")
	}

	p := tea.NewProgram(newModel(frontiers))
	result, err := p.Run()
	if err != nil {
		return nil, err
	}
	m := result.(model)
	if m.canceled || !m.done {
		return nil, fmt.Errorf("no frontiers selected")
	}
	var selected []string
	for i := range m.selected {
		selected = append(selected, m.items[i].frontier.Path)
	}
	sort.Strings(selected)
	return selected, nil
}

// WriteSelection routes selected paths to CAVEKIT_PICKER_OUTFILE when set,
// otherwise writes to out (one path per line, trailing newline).
func WriteSelection(selected []string, out io.Writer) error {
	payload := strings.Join(selected, "\n") + "\n"
	if outfile := os.Getenv("CAVEKIT_PICKER_OUTFILE"); outfile != "" {
		return os.WriteFile(outfile, []byte(payload), 0o644)
	}
	w := bufio.NewWriter(out)
	defer w.Flush()
	_, err := w.WriteString(payload)
	return err
}

// ProjectRoot returns the git top level, falling back to cwd.
func ProjectRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		cwd, _ := os.Getwd()
		return cwd
	}
	return strings.TrimSpace(string(out))
}
