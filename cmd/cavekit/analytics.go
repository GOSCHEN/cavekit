package main

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/JuliusBrussee/cavekit/internal/stats"
)

func runAnalytics(args []string) {
	cwd, _ := os.Getwd()
	if err := writeAnalytics(cwd, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = args
}

func writeAnalytics(projectRoot string, w io.Writer) error {
	logs := stats.CollectLoopLogs(projectRoot)
	if len(logs) == 0 {
		fmt.Fprintln(w, "No loop logs found. Run /ck:make first.")
		return nil
	}

	var totalIter, cycles, done, partial, blocked int
	tierCounts := map[string]int{}
	for _, p := range logs {
		m, err := stats.ParseLoopLog(p)
		if err != nil {
			return err
		}
		if len(m.Iterations) > 0 {
			cycles++
		}
		totalIter += len(m.Iterations)
		done += m.Done
		partial += m.Partial
		blocked += m.Blocked
		for k, v := range m.TierCounts {
			tierCounts[k] += v
		}
	}
	deadEnds := stats.CountDeadEnds(projectRoot)

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  ┌──────────────────────────┐")
	fmt.Fprintln(w, "  │  C A V E K I T           │")
	fmt.Fprintln(w, "  └──────────────────────────┘")
	fmt.Fprintln(w, "  Analytics")
	fmt.Fprintln(w, "────────────────────────────────────────────────────────────")
	fmt.Fprintln(w, "")

	fmt.Fprintln(w, "Overview")
	fmt.Fprintf(w, "  Cycles analyzed:      %d\n", cycles)
	fmt.Fprintf(w, "  Total iterations:     %d\n", totalIter)
	if cycles > 0 {
		fmt.Fprintf(w, "  Avg iterations/cycle: %d\n", totalIter/cycles)
	}
	fmt.Fprintln(w, "")

	total := done + partial + blocked
	fmt.Fprintln(w, "Task Outcomes")
	if total > 0 {
		fmt.Fprintf(w, "  Done     %4d  (%d%%)\n", done, done*100/total)
		fmt.Fprintf(w, "  Partial  %4d  (%d%%)\n", partial, partial*100/total)
		fmt.Fprintf(w, "  Blocked  %4d  (%d%%)\n", blocked, blocked*100/total)
	} else {
		fmt.Fprintln(w, "  No task outcomes recorded yet.")
	}
	fmt.Fprintln(w, "")

	fmt.Fprintln(w, "Failure Patterns")
	if deadEnds > 0 {
		fmt.Fprintf(w, "  Dead ends: %d\n", deadEnds)
	} else {
		fmt.Fprintln(w, "  No dead ends recorded.")
	}
	fmt.Fprintln(w, "")

	fmt.Fprintln(w, "Iterations by Tier")
	if len(tierCounts) > 0 {
		tiers := make([]string, 0, len(tierCounts))
		for k := range tierCounts {
			tiers = append(tiers, k)
		}
		sort.Strings(tiers)
		for _, t := range tiers {
			c := tierCounts[t]
			barLen := 1
			if totalIter > 0 {
				barLen = c * 30 / totalIter
				if barLen < 1 {
					barLen = 1
				}
			}
			bar := ""
			for i := 0; i < barLen; i++ {
				bar += "█"
			}
			fmt.Fprintf(w, "  Tier %s: %s %d\n", t, bar, c)
		}
	} else {
		fmt.Fprintln(w, "  No tier data recorded.")
	}
	fmt.Fprintln(w, "")

	fmt.Fprintln(w, "Completion Velocity")
	if totalIter > 0 && done > 0 {
		ratio := float64(done) / float64(totalIter)
		fmt.Fprintf(w, "  Tasks/iteration: %.2f\n", ratio)
		if total > 0 {
			fmt.Fprintf(w, "  Success rate:    %d%%\n", done*100/total)
		}
	} else {
		fmt.Fprintln(w, "  Not enough data yet.")
	}
	fmt.Fprintln(w, "")

	fmt.Fprintln(w, "Active Agent")
	state := stats.ReadRalphState(projectRoot)
	if state.Active {
		iter := state.Iteration
		if iter == "" {
			iter = "?"
		}
		fmt.Fprintf(w, "  ⟳ active — iteration %s\n", iter)
	} else {
		fmt.Fprintln(w, "  No active agent.")
	}
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "Data from %d log files across %d cycles.\n", len(logs), cycles)
	return nil
}
