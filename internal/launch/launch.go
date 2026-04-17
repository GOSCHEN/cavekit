// Package launch creates one multiplexer session per frontier and sends a
// `/ck:make --filter <name>` kick-off to each pane after a staggered delay.
// Replaces scripts/cavekit-launch-session.sh.
package launch

import (
	"context"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/JuliusBrussee/cavekit/internal/exec"
	"github.com/JuliusBrussee/cavekit/internal/mux"
)

// Options tunes Run.
type Options struct {
	Frontiers    []string
	Expanded     bool
	SessionName  string
	Program      string
	StaggerDelay time.Duration
	// ProjectRoot overrides auto-detection (git toplevel → cwd).
	ProjectRoot string
	// Muxer is the multiplexer to drive. nil → mux.New(exec.NewRealExecutor()).
	Muxer mux.Multiplexer
	// Attach specifies whether to attach to the session after dispatch.
	// Ignored on Windows (wt.exe already opened tabs for each session).
	Attach bool
}

// Result captures what Run did.
type Result struct {
	Sessions []SessionInfo
}

// SessionInfo describes one launched session.
type SessionInfo struct {
	Name       string
	Frontier   string
	Resuming   bool
}

// Run launches every frontier, returning the session info set.
func Run(ctx context.Context, opts Options, w io.Writer) (Result, error) {
	if len(opts.Frontiers) == 0 {
		return Result{}, fmt.Errorf("no frontiers provided")
	}
	if opts.SessionName == "" {
		opts.SessionName = "cavekit"
	}
	if opts.Program == "" {
		opts.Program = "claude"
	}
	if opts.StaggerDelay == 0 {
		opts.StaggerDelay = 5 * time.Second
	}

	projectRoot := opts.ProjectRoot
	if projectRoot == "" {
		projectRoot = detectProjectRoot()
	}

	if _, err := osexec.LookPath(opts.Program); err != nil {
		return Result{}, fmt.Errorf("%s not installed", opts.Program)
	}

	muxer := opts.Muxer
	if muxer == nil {
		muxer = mux.New(exec.NewRealExecutor())
	}

	// Tear down any leftover cavekit-managed sessions with the names we're about
	// to reuse so fresh launches aren't silently no-ops when a prior run
	// orphaned sessions.
	existing, _ := muxer.ListSessions(ctx)
	existingSet := make(map[string]struct{}, len(existing))
	for _, name := range existing {
		existingSet[name] = struct{}{}
	}

	var sessions []SessionInfo
	for _, f := range opts.Frontiers {
		name := DeriveName(f)
		sessions = append(sessions, SessionInfo{
			Name:     name,
			Frontier: f,
			Resuming: shouldResume(projectRoot),
		})
	}

	for _, s := range sessions {
		if _, ok := existingSet[s.Name]; ok {
			_ = muxer.Kill(ctx, s.Name)
		}
		command := launchCommand(opts.Program, s.Resuming, projectRoot, s.Frontier, s.Name)
		if err := muxer.CreateSession(ctx, s.Name, projectRoot, command); err != nil {
			return Result{Sessions: sessions}, fmt.Errorf("create session %s: %w", s.Name, err)
		}
		mode := "NEW"
		if s.Resuming {
			mode = "RESUME"
		}
		fmt.Fprintf(w, "  %s [%s]\n", s.Name, mode)
	}

	// Stagger /ck:make sends — resumed sessions already have their loop, skip.
	go dispatchKickoffs(ctx, muxer, sessions, opts.StaggerDelay, w)

	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "Launched %d Cavekit agents in %s:\n", len(sessions), projectRoot)
	for _, s := range sessions {
		fmt.Fprintf(w, "  %s\n", s.Name)
	}
	return Result{Sessions: sessions}, nil
}

func dispatchKickoffs(ctx context.Context, muxer mux.Multiplexer, sessions []SessionInfo, delay time.Duration, w io.Writer) {
	time.Sleep(3 * time.Second)
	for i, s := range sessions {
		if s.Resuming {
			continue
		}
		_ = muxer.SendKeys(ctx, s.Name, fmt.Sprintf("/ck:make --filter %s", s.Name))
		_ = muxer.SendKeys(ctx, s.Name, "Enter")
		if i < len(sessions)-1 {
			time.Sleep(delay)
		}
	}
}

// launchCommand builds the shell command that Multiplexer.CreateSession
// runs inside each session. We wrap the invocation in `sh -c ...` so it can
// print the header lines before handing control to Claude.
func launchCommand(program string, resuming bool, workDir, frontier, name string) string {
	claude := program
	modeLabel := "NEW"
	if resuming {
		claude = program + " --resume"
		modeLabel = "RESUME"
	}
	banner := fmt.Sprintf(
		"echo \"Cavekit Agent: %s [%s]\"; echo \"Directory: %s\"; echo \"Frontier: %s\"; echo; ",
		name, modeLabel, workDir, filepath.Base(frontier),
	)
	return banner + claude
}

// DeriveName strips the usual prefixes/suffixes from a frontier filename to
// produce a short session-friendly slug. Matches scripts/cavekit-launch-
// session.sh's derive_name helper.
func DeriveName(frontier string) string {
	base := filepath.Base(frontier)
	base = strings.TrimSuffix(base, ".md")
	for _, prefix := range []string{"plan-", "feature-frontier-", "feature-", "build-site-"} {
		base = strings.TrimPrefix(base, prefix)
	}
	base = strings.TrimSuffix(base, "-frontier")
	return strings.Trim(base, "-")
}

func shouldResume(projectRoot string) bool {
	if _, err := os.Stat(filepath.Join(projectRoot, ".claude", "ralph-loop.local.md")); err == nil {
		return true
	}
	implDir := filepath.Join(projectRoot, "context", "impl")
	entries, err := os.ReadDir(implDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "impl-") && strings.HasSuffix(e.Name(), ".md") {
			return true
		}
	}
	return false
}

func detectProjectRoot() string {
	out, err := osexec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	cwd, _ := os.Getwd()
	return cwd
}
