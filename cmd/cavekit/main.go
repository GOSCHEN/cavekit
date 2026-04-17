package main

import (
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"

	"github.com/JuliusBrussee/cavekit/internal/exec"
	"github.com/JuliusBrussee/cavekit/internal/mux"
	"github.com/JuliusBrussee/cavekit/internal/session"
	"github.com/JuliusBrussee/cavekit/internal/site"
	"github.com/JuliusBrussee/cavekit/internal/tui"
	"github.com/JuliusBrussee/cavekit/internal/worktree"
)

const version = "v0.1.0"

func main() {
	cmd := "monitor"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	// Windows-only subcommands handle themselves by dispatching to helpers
	// defined under //go:build windows. dispatchPlatformCmd returns true iff
	// the command belonged to the platform layer.
	if dispatchPlatformCmd(cmd) {
		return
	}

	switch cmd {
	case "monitor", "":
		runMonitor()
	case "status":
		runStatus()
	case "kill":
		runKill()
	case "version":
		fmt.Println("cavekit", version)
	case "debug":
		runDebug()
	case "reset":
		runReset()
	case "config":
		runConfig(os.Args[2:])
	case "codex":
		runCodex(os.Args[2:])
	case "command-gate":
		runCommandGate(os.Args[2:])
	case "setup-build":
		runSetupBuild(os.Args[2:])
	case "install":
		runInstall(os.Args[2:])
	case "analytics":
		runAnalytics(os.Args[2:])
	case "dashboard":
		runDashboard(os.Args[2:])
	case "poll":
		runPoll(os.Args[2:])
	case "picker":
		runPicker(os.Args[2:])
	case "launch":
		runLaunch(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		fmt.Fprintln(os.Stderr, "usage: cavekit [monitor|status|kill|version|debug|reset|config|codex|command-gate|setup-build|install|analytics|dashboard|poll|picker|launch]")
		os.Exit(1)
	}
}

func runMonitor() {
	// Parse flags
	program := "claude"
	autoYes := false
	for i, arg := range os.Args {
		if (arg == "--program" || arg == "-p") && i+1 < len(os.Args) {
			program = os.Args[i+1]
		}
		if arg == "--autoyes" || arg == "-y" {
			autoYes = true
		}
	}

	// Preflight checks
	if err := preflight(program); err != nil {
		fmt.Fprintf(os.Stderr, "preflight failed: %s\n", err)
		os.Exit(1)
	}

	// Determine project root
	cwd, _ := os.Getwd()
	executor := exec.NewRealExecutor()
	wtMgr := worktree.NewManager(executor)
	ctx := context.Background()
	root, err := wtMgr.ProjectRoot(ctx, cwd)
	if err != nil {
		root = cwd
	}

	// Launch TUI
	if err := tui.Run(root, program, autoYes); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %s\n", err)
		os.Exit(1)
	}
}

func runStatus() {
	executor := exec.NewRealExecutor()
	wtMgr := worktree.NewManager(executor)
	ctx := context.Background()

	cwd, _ := os.Getwd()
	root, err := wtMgr.ProjectRoot(ctx, cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "not in a git repo: %s\n", err)
		os.Exit(1)
	}

	worktrees, err := worktree.DiscoverAll(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discover worktrees: %s\n", err)
		os.Exit(1)
	}

	if len(worktrees) == 0 {
		fmt.Println("No Cavekit worktrees found.")
		return
	}

	for _, wt := range worktrees {
		icon := "·"
		if wt.HasRalphLoop {
			icon = "⟳"
		}

		// Try to compute progress
		done, total := computeWorktreeProgress(wt.Path)
		if total > 0 {
			fmt.Printf("%s %s: %d/%d tasks done\n", icon, wt.SiteName, done, total)
		} else {
			fmt.Printf("%s %s: %s\n", icon, wt.SiteName, wt.Path)
		}
	}
}

// computeWorktreeProgress reads site and impl files to compute task progress.
func computeWorktreeProgress(wtPath string) (done, total int) {
	// Look for site files in worktree
	sitesDir := filepath.Join(wtPath, "context", "sites")
	sites, err := site.Discover(sitesDir)
	if err != nil || len(sites) == 0 {
		return 0, 0
	}

	// Parse first site
	f, err := site.Parse(sites[0].Path)
	if err != nil {
		return 0, 0
	}

	// Track status from impl files
	implDir := filepath.Join(wtPath, "context", "impl")
	statuses, err := site.TrackStatus(implDir)
	if err != nil {
		return 0, len(f.Tasks)
	}

	summary := site.ComputeProgress(f, statuses)
	return summary.Done, summary.Total
}

func runKill() {
	executor := exec.NewRealExecutor()
	muxer := mux.New(executor)
	wtMgr := worktree.NewManager(executor)
	ctx := context.Background()

	cwd, _ := os.Getwd()
	root, _ := wtMgr.ProjectRoot(ctx, cwd)

	// Kill multiplexer sessions
	sessions, _ := muxer.ListSessions(ctx)
	killed := 0
	for _, s := range sessions {
		muxer.Kill(ctx, s)
		killed++
	}

	// Remove worktrees and branches
	worktrees, _ := worktree.DiscoverAll(root)
	cleaned := 0
	for _, wt := range worktrees {
		wtMgr.Remove(ctx, root, wt.SiteName)
		cleaned++
	}

	// Clear persisted state
	store := session.NewStore("")
	os.Remove(store.Path())

	fmt.Printf("Killed %d sessions, cleaned %d worktrees.\n", killed, cleaned)
}

func runDebug() {
	store := session.NewStore("")
	fmt.Println("State file:", store.Path())
	fmt.Println("Version:", version)
}

func runReset() {
	store := session.NewStore("")
	os.Remove(store.Path())
	fmt.Println("State cleared.")
}

func preflight(program string) error {
	if err := preflightMultiplexer(); err != nil {
		return err
	}
	if _, err := osexec.LookPath("git"); err != nil {
		return fmt.Errorf("git not installed")
	}
	if _, err := osexec.LookPath(program); err != nil {
		return fmt.Errorf("%s not installed (use --program to override)", program)
	}
	return nil
}
