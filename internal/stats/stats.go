// Package stats extracts metrics from cavekit execution artefacts: loop
// logs, impl tracking files, and build-site task lists. Shared between
// `cavekit analytics`, `cavekit dashboard`, and `cavekit poll`.
package stats

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// TaskIDRe matches task identifiers like T-001 or T-UX-42.
var TaskIDRe = regexp.MustCompile(`T-(?:[A-Za-z0-9]+-)*[A-Za-z0-9]+`)

// IterationSummary is one entry parsed from a loop-log.
type IterationSummary struct {
	Number int
	Task   string
	Status string // DONE | PARTIAL | BLOCKED | ""
	Tier   string
	Lines  []string
}

// LoopLogMetrics summarizes a single loop-log.md file.
type LoopLogMetrics struct {
	Path         string
	Iterations   []IterationSummary
	Done         int
	Partial      int
	Blocked      int
	TierCounts   map[string]int
}

var (
	iterRe    = regexp.MustCompile(`^###\s+Iteration\s+(\d+)`)
	taskLineRe = regexp.MustCompile(`(?i)Task:\s*(.+)`)
	statusRe  = regexp.MustCompile(`(?i)Status:\s*([A-Z]+)`)
	tierRe    = regexp.MustCompile(`(?i)Tier:\s*([0-9]+)`)
)

// ParseLoopLog reads one loop-log.md and returns its iteration summaries +
// aggregate counts. Missing files return a zero-value struct without error.
func ParseLoopLog(path string) (LoopLogMetrics, error) {
	m := LoopLogMetrics{Path: path, TierCounts: map[string]int{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return m, err
	}

	var current *IterationSummary
	flush := func() {
		if current == nil {
			return
		}
		m.Iterations = append(m.Iterations, *current)
		switch strings.ToUpper(current.Status) {
		case "DONE":
			m.Done++
		case "PARTIAL":
			m.Partial++
		case "BLOCKED":
			m.Blocked++
		}
		if current.Tier != "" {
			m.TierCounts[current.Tier]++
		}
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if m := iterRe.FindStringSubmatch(line); len(m) == 2 {
			flush()
			n := 0
			_, _ = parseInt(m[1], &n)
			current = &IterationSummary{Number: n}
			continue
		}
		if current == nil {
			continue
		}
		current.Lines = append(current.Lines, line)
		if sm := statusRe.FindStringSubmatch(line); len(sm) == 2 && current.Status == "" {
			current.Status = strings.ToUpper(sm[1])
		}
		if tm := tierRe.FindStringSubmatch(line); len(tm) == 2 && current.Tier == "" {
			current.Tier = tm[1]
		}
		if tm := taskLineRe.FindStringSubmatch(line); len(tm) == 2 && current.Task == "" {
			current.Task = strings.TrimSpace(tm[1])
		}
	}
	flush()
	return m, scanner.Err()
}

func parseInt(s string, out *int) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	*out = n
	return n, nil
}

// CollectLoopLogs returns the current loop-log and every archived copy.
func CollectLoopLogs(projectRoot string) []string {
	var logs []string
	cur := filepath.Join(projectRoot, "context", "impl", "loop-log.md")
	if _, err := os.Stat(cur); err == nil {
		logs = append(logs, cur)
	}
	archiveGlob := filepath.Join(projectRoot, "context", "impl", "archive", "*", "loop-log.md")
	matches, _ := filepath.Glob(archiveGlob)
	sort.Strings(matches)
	logs = append(logs, matches...)
	return logs
}

// CountDeadEnds walks every impl-*.md under context/impl (plus archives)
// and counts occurrences of the case-insensitive substring "dead end" /
// "dead.end".
func CountDeadEnds(projectRoot string) int {
	return walkDeadEnds(filepath.Join(projectRoot, "context", "impl"))
}

var deadEndRe = regexp.MustCompile(`(?i)dead[.\s_-]?end`)

func walkDeadEnds(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	total := 0
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() {
			total += walkDeadEnds(path)
			continue
		}
		if !strings.HasPrefix(e.Name(), "impl-") || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		total += len(deadEndRe.FindAllIndex(data, -1))
	}
	return total
}

// FindFrontier returns the first build-site file in context/plans/sites,
// excluding archive paths. Returns "" when none exist.
func FindFrontier(projectRoot string) string {
	for _, rel := range []string{"context/plans", "context/sites"} {
		dir := filepath.Join(projectRoot, rel)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var names []string
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			name := strings.ToLower(e.Name())
			if !strings.Contains(name, "site") {
				continue
			}
			names = append(names, e.Name())
		}
		sort.Strings(names)
		if len(names) > 0 {
			return filepath.ToSlash(filepath.Join(rel, names[0]))
		}
	}
	return ""
}

// TaskStatus enumerates the per-task state known to the dashboard.
type TaskStatus string

const (
	StatusUnknown TaskStatus = ""
	StatusDone    TaskStatus = "DONE"
	StatusWIP     TaskStatus = "WIP"
	StatusBlocked TaskStatus = "BLOCKED"
)

// TaskCounts aggregates progress against a single build site.
type TaskCounts struct {
	Total      int
	Done       int
	InProgress int
	Blocked    int
	Remaining  int
}

// FrontierProgress reads frontierPath plus every context/impl/impl-*.md and
// returns per-state counts. Missing inputs return a zero-value struct.
func FrontierProgress(projectRoot, frontierPath string) TaskCounts {
	if frontierPath == "" {
		return TaskCounts{}
	}
	abs := frontierPath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(projectRoot, frontierPath)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return TaskCounts{}
	}

	taskIDs := map[string]struct{}{}
	for _, line := range strings.Split(string(data), "\n") {
		if !isFrontierTaskLine(line) {
			continue
		}
		if id := firstTaskID(line); id != "" {
			taskIDs[id] = struct{}{}
		}
	}

	statuses := implStatuses(projectRoot)
	counts := TaskCounts{Total: len(taskIDs)}
	for id := range taskIDs {
		switch statuses[id] {
		case StatusDone:
			counts.Done++
		case StatusWIP:
			counts.InProgress++
		case StatusBlocked:
			counts.Blocked++
		}
	}
	counts.Remaining = counts.Total - counts.Done - counts.InProgress - counts.Blocked
	if counts.Remaining < 0 {
		counts.Remaining = 0
	}
	return counts
}

func isFrontierTaskLine(line string) bool {
	trim := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trim, "|") {
		cols := strings.Split(trim, "|")
		if len(cols) >= 2 && TaskIDRe.MatchString(strings.TrimSpace(cols[1])) {
			return true
		}
	}
	if strings.HasPrefix(trim, "- ") {
		rest := strings.TrimLeft(trim[1:], " ")
		if TaskIDRe.MatchString(rest) {
			return true
		}
	}
	return false
}

func firstTaskID(s string) string {
	return TaskIDRe.FindString(s)
}

// implStatuses walks impl files and collapses the latest status per task ID.
func implStatuses(projectRoot string) map[string]TaskStatus {
	out := map[string]TaskStatus{}
	dir := filepath.Join(projectRoot, "context", "impl")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "impl-") || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			line := strings.TrimRight(scanner.Text(), "\r")
			id := firstTaskID(line)
			if id == "" {
				continue
			}
			up := strings.ToUpper(line)
			switch {
			case strings.Contains(up, "DONE"):
				out[id] = StatusDone
			case strings.Contains(up, "IN PROGRESS") || strings.Contains(up, "IN-PROGRESS") || strings.Contains(up, "PARTIAL"):
				if out[id] == "" {
					out[id] = StatusWIP
				}
			case strings.Contains(up, "BLOCKED") || strings.Contains(up, "DEAD END") || strings.Contains(up, "DEAD-END"):
				if out[id] == "" {
					out[id] = StatusBlocked
				}
			}
		}
	}
	return out
}

// RalphState summarizes .claude/ralph-loop.local.md.
type RalphState struct {
	Active        bool
	Iteration     string
	MaxIterations string
	StartedAt     string
}

var (
	ralphIterRe = regexp.MustCompile(`(?m)^iteration:\s*(\S+)`)
	ralphMaxRe  = regexp.MustCompile(`(?m)^max_iterations:\s*(\S+)`)
	ralphStartedRe = regexp.MustCompile(`(?m)^started_at:\s*"?([^"\n]+)"?`)
)

// ReadRalphState parses the Ralph Loop metadata file.
func ReadRalphState(projectRoot string) RalphState {
	path := filepath.Join(projectRoot, ".claude", "ralph-loop.local.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return RalphState{}
	}
	s := RalphState{Active: true}
	if m := ralphIterRe.FindSubmatch(data); len(m) == 2 {
		s.Iteration = string(m[1])
	}
	if m := ralphMaxRe.FindSubmatch(data); len(m) == 2 {
		s.MaxIterations = string(m[1])
	}
	if m := ralphStartedRe.FindSubmatch(data); len(m) == 2 {
		s.StartedAt = strings.TrimSpace(string(m[1]))
	}
	return s
}
