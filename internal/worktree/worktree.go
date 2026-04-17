// Package worktree manages git worktrees for isolated agent execution.
package worktree

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/JuliusBrussee/cavekit/internal/exec"
)

const branchPrefix = "cavekit/"

// Manager handles git worktree operations.
type Manager struct {
	exec exec.Executor
}

// NewManager creates a worktree manager.
func NewManager(executor exec.Executor) *Manager {
	return &Manager{exec: executor}
}

// WorktreePath returns the canonical worktree path for a given project root and site name.
// Format: {project_root}/../{project_name}-cavekit-{site_name}
func WorktreePath(projectRoot, siteName string) string {
	projectName := filepath.Base(projectRoot)
	return filepath.Join(filepath.Dir(projectRoot), projectName+"-cavekit-"+siteName)
}

// BranchName returns the canonical branch name for a site.
func BranchName(siteName string) string {
	return branchPrefix + siteName
}

// Create creates a git worktree at the canonical path on the canonical branch.
// If the branch doesn't exist, it creates it from HEAD.
// If the worktree already exists, it returns without error.
func (m *Manager) Create(ctx context.Context, projectRoot, siteName string) (string, error) {
	wtPath := WorktreePath(projectRoot, siteName)
	branch := BranchName(siteName)

	// Check if worktree already exists
	if m.Exists(ctx, projectRoot, wtPath) {
		return wtPath, nil
	}

	// Check if branch exists; if not, create from HEAD
	res, err := m.exec.RunDir(ctx, projectRoot, "git", "rev-parse", "--verify", branch)
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	if res.ExitCode != 0 {
		// Branch doesn't exist, create it
		res, err = m.exec.RunDir(ctx, projectRoot, "git", "branch", branch)
		if err != nil {
			return "", fmt.Errorf("git branch create: %w", err)
		}
		if res.ExitCode != 0 {
			return "", fmt.Errorf("git branch create failed: %s", res.Stderr)
		}
	}

	// Create worktree
	res, err = m.exec.RunDir(ctx, projectRoot, "git", "worktree", "add", wtPath, branch)
	if err != nil {
		return "", fmt.Errorf("git worktree add: %w", err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("git worktree add failed (exit %d): %s", res.ExitCode, res.Stderr)
	}

	return wtPath, nil
}

// Exists checks if a worktree path is registered with git.
// Normalizes path separators so Windows native backslashes match git's
// porcelain output (which uses forward slashes). On case-insensitive
// filesystems (Windows/macOS) the comparison is also case-folded so
// `C:\Foo` and `c:\foo` register as the same worktree.
func (m *Manager) Exists(ctx context.Context, projectRoot, wtPath string) bool {
	res, err := m.exec.RunDir(ctx, projectRoot, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	needle := filepath.ToSlash(wtPath)
	haystack := filepath.ToSlash(res.Stdout)
	if caseInsensitiveFS() {
		return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
	}
	return strings.Contains(haystack, needle)
}

// Remove removes a worktree and its branch.
func (m *Manager) Remove(ctx context.Context, projectRoot, siteName string) error {
	wtPath := WorktreePath(projectRoot, siteName)
	branch := BranchName(siteName)

	// Prune stale worktree entries first
	m.exec.RunDir(ctx, projectRoot, "git", "worktree", "prune")

	// Remove worktree
	res, err := m.exec.RunDir(ctx, projectRoot, "git", "worktree", "remove", "--force", wtPath)
	if err != nil {
		return fmt.Errorf("git worktree remove: %w", err)
	}
	if res.ExitCode != 0 {
		// Worktree might already be gone; continue to branch cleanup
	}

	// Delete branch
	res, err = m.exec.RunDir(ctx, projectRoot, "git", "branch", "-D", branch)
	if err != nil {
		return fmt.Errorf("git branch delete: %w", err)
	}
	if res.ExitCode != 0 {
		// Branch might already be gone; not an error
	}

	return nil
}

// ProjectRoot returns the git toplevel for a given path.
func (m *Manager) ProjectRoot(ctx context.Context, fromDir string) (string, error) {
	res, err := m.exec.RunDir(ctx, fromDir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("not a git repository: %s", res.Stderr)
	}
	return strings.TrimSpace(res.Stdout), nil
}
