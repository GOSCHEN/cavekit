package worktree

import (
	"os"
	"path/filepath"
)

// DiscoveredWorktree represents a found Cavekit worktree.
type DiscoveredWorktree struct {
	Path         string // Full worktree path
	Branch       string // Branch name (cavekit/xxx)
	SiteName string // Derived site name
	HasRalphLoop bool   // .claude/ralph-loop.local.md exists
}

// DiscoverAll finds all existing Cavekit worktrees for the given project.
// Scans {project_root}/../{project_name}-cavekit-* directories.
func DiscoverAll(projectRoot string) ([]DiscoveredWorktree, error) {
	projectName := filepath.Base(projectRoot)
	parentDir := filepath.Dir(projectRoot)
	pattern := projectName + "-cavekit-"

	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return nil, err
	}

	var results []DiscoveredWorktree
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !hasFoldedPrefix(name, pattern) {
			continue
		}

		siteName := trimFoldedPrefix(name, pattern)
		wtPath := filepath.Join(parentDir, name)

		// Check for active Ralph Loop
		ralphLoopPath := filepath.Join(wtPath, ".claude", "ralph-loop.local.md")
		hasRalphLoop := fileExists(ralphLoopPath)

		results = append(results, DiscoveredWorktree{
			Path:         wtPath,
			Branch:       BranchName(siteName),
			SiteName: siteName,
			HasRalphLoop: hasRalphLoop,
		})
	}

	return results, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
