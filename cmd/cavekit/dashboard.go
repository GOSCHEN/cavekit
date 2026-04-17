package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JuliusBrussee/cavekit/internal/stats"
)

const dashboardUsage = `Usage: cavekit dashboard {activity|progress} [--once]
  activity   Show recent iterations + git commits (loops until Ctrl-C)
  progress   Show frontier progress + tier breakdown (loops until Ctrl-C)
  --once     Render a single frame and exit
`

func runDashboard(args []string) {
	sub := "help"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	once := false
	for _, a := range args {
		if a == "--once" {
			once = true
		}
	}
	switch sub {
	case "activity":
		renderLoop(once, 3*time.Second, renderActivity)
	case "progress":
		renderLoop(once, 5*time.Second, renderProgress)
	case "help", "--help", "-h", "":
		fmt.Print(dashboardUsage)
	default:
		fmt.Fprintf(os.Stderr, "cavekit dashboard: unknown subcommand: %s\n", sub)
		os.Exit(1)
	}
}

func renderLoop(once bool, interval time.Duration, render func(io.Writer, string)) {
	cwd, _ := os.Getwd()
	if once {
		render(os.Stdout, cwd)
		return
	}
	fmt.Print("\033[?25l")             // hide cursor
	defer fmt.Print("\033[?25h")       // restore cursor
	for {
		fmt.Print("\033[2J\033[H")
		render(os.Stdout, cwd)
		time.Sleep(interval)
	}
}

func renderActivity(w io.Writer, projectRoot string) {
	fmt.Fprintln(w, " ACTIVITY")
	fmt.Fprintln(w, "────────────────────────────────────────────────")
	m, _ := stats.ParseLoopLog(filepath.Join(projectRoot, "context", "impl", "loop-log.md"))
	if len(m.Iterations) == 0 {
		fmt.Fprintln(w, "  Waiting for first iteration...")
	} else {
		max := 8
		start := len(m.Iterations) - max
		if start < 0 {
			start = 0
		}
		for _, iter := range m.Iterations[start:] {
			icon := "?"
			switch iter.Status {
			case "DONE":
				icon = "*"
			case "PARTIAL":
				icon = "~"
			case "BLOCKED":
				icon = "x"
			}
			fmt.Fprintf(w, "  %s #%d %s\n", icon, iter.Number, truncate80(iter.Task, 60))
		}
	}
	fmt.Fprintln(w, "")

	// Active loop hint
	state := stats.ReadRalphState(projectRoot)
	if !state.Active {
		fmt.Fprintln(w, "  Loop not active")
	}
	fmt.Fprintf(w, "%s\n", time.Now().Format("15:04:05"))
}

func renderProgress(w io.Writer, projectRoot string) {
	fmt.Fprintln(w, " CAVEKIT")
	fmt.Fprintln(w, "────────────────────────────────────────────────")
	state := stats.ReadRalphState(projectRoot)
	if state.Active {
		iter := state.Iteration
		if iter == "" {
			iter = "-"
		}
		maxI := state.MaxIterations
		if maxI == "" {
			maxI = "-"
		}
		fmt.Fprintf(w, "  ACTIVE  iter %s/%s\n", iter, maxI)
	} else {
		fmt.Fprintln(w, "  IDLE — no active loop")
	}
	fmt.Fprintln(w, "")

	frontier := stats.FindFrontier(projectRoot)
	if frontier == "" {
		fmt.Fprintln(w, "  No frontier found")
		fmt.Fprintf(w, "%s\n", time.Now().Format("15:04:05"))
		return
	}

	counts := stats.FrontierProgress(projectRoot, frontier)
	fmt.Fprintln(w, " Tasks")
	fmt.Fprintln(w, "────────────────────────────────────────────────")
	if counts.Total > 0 {
		bar := progressBar(counts.Done, counts.InProgress, counts.Total, 30)
		fmt.Fprintf(w, "  %s %d%%\n", bar, counts.Done*100/counts.Total)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "  Done        %d/%d\n", counts.Done, counts.Total)
	if counts.InProgress > 0 {
		fmt.Fprintf(w, "  In Progress %d\n", counts.InProgress)
	}
	if counts.Blocked > 0 {
		fmt.Fprintf(w, "  Blocked     %d\n", counts.Blocked)
	}
	fmt.Fprintf(w, "  Remaining   %d\n", counts.Remaining)
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "%s\n", time.Now().Format("15:04:05"))
}

func progressBar(done, wip, total, width int) string {
	if total <= 0 {
		return strings.Repeat(" ", width)
	}
	filled := done * width / total
	inProg := wip * width / total
	if wip > 0 && inProg < 1 {
		inProg = 1
	}
	empty := width - filled - inProg
	if empty < 0 {
		empty = 0
	}
	return strings.Repeat("█", filled) + strings.Repeat("▒", inProg) + strings.Repeat("·", empty)
}

func truncate80(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
