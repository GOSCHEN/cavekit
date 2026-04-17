package site

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// TaskStatus represents the status of a single task.
type TaskStatus int

const (
	TaskPending    TaskStatus = iota
	TaskDone
	TaskInProgress
	TaskPartial
	TaskBlocked
	TaskDeadEnd
)

func (s TaskStatus) String() string {
	switch s {
	case TaskDone:
		return "DONE"
	case TaskInProgress:
		return "IN PROGRESS"
	case TaskPartial:
		return "PARTIAL"
	case TaskBlocked:
		return "BLOCKED"
	case TaskDeadEnd:
		return "DEAD END"
	default:
		return "PENDING"
	}
}

// TaskStatusMap maps task IDs to their status.
type TaskStatusMap map[string]TaskStatus

// ProgressSummary holds aggregate progress data.
type ProgressSummary struct {
	Total      int
	Done       int
	InProgress int
	Blocked    int
	Remaining  int
}

// TrackStatus scans impl files in the given directories for task status markers.
// Uses word boundary matching to prevent prefix collisions (T-1 vs T-10).
func TrackStatus(implDirs ...string) (TaskStatusMap, error) {
	statuses := make(TaskStatusMap)

	for _, dir := range implDirs {
		matches, err := filepath.Glob(filepath.Join(dir, "impl-*.md"))
		if err != nil {
			continue
		}
		for _, path := range matches {
			if err := scanImplFile(path, statuses); err != nil {
				// Non-fatal: skip files that can't be read
				continue
			}
		}
	}

	return statuses, nil
}

// taskStatusLinePattern matches table rows with task ID and status.
// Uses word boundary (\b) around the task ID to prevent T-1 matching T-10.
var taskStatusLinePattern = regexp.MustCompile(`\|\s*(T-[A-Za-z0-9-]+)\s*\|\s*(DONE|IN PROGRESS|PARTIAL|BLOCKED|DEAD END)\s*\|`)

func scanImplFile(path string, statuses TaskStatusMap) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		matches := taskStatusLinePattern.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			if len(m) >= 3 {
				taskID := strings.TrimSpace(m[1])
				statusStr := strings.TrimSpace(m[2])
				statuses[taskID] = parseStatus(statusStr)
			}
		}
	}
	return scanner.Err()
}

func parseStatus(s string) TaskStatus {
	switch s {
	case "DONE":
		return TaskDone
	case "IN PROGRESS":
		return TaskInProgress
	case "PARTIAL":
		return TaskPartial
	case "BLOCKED":
		return TaskBlocked
	case "DEAD END":
		return TaskDeadEnd
	default:
		return TaskPending
	}
}

// ComputeProgress computes aggregate progress for a site given its task list and status map.
func ComputeProgress(s *Site, statuses TaskStatusMap) ProgressSummary {
	summary := ProgressSummary{
		Total: s.TotalTasks(),
	}

	for _, task := range s.Tasks {
		status, exists := statuses[task.ID]
		if !exists {
			summary.Remaining++
			continue
		}
		switch status {
		case TaskDone:
			summary.Done++
		case TaskInProgress:
			summary.InProgress++
		case TaskBlocked:
			summary.Blocked++
		default:
			summary.Remaining++
		}
	}

	return summary
}
