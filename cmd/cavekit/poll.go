package main

import (
	"fmt"
	"os"
	osexec "os/exec"
	"time"

	"github.com/JuliusBrussee/cavekit/internal/stats"
)

// runPoll updates the tmux status bar with frontier progress. Loops until
// the session disappears. On Windows this is effectively a no-op because
// wt.exe has no equivalent `set-option status-right`; the command exits
// cleanly so scripted invocations do not fail.
func runPoll(args []string) {
	sessionName := "cavekit"
	pollInterval := 5 * time.Second
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--session":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--session requires a name")
				os.Exit(1)
			}
			sessionName = args[i+1]
			i++
		case "--interval":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--interval requires seconds")
				os.Exit(1)
			}
			secs := 5
			fmt.Sscanf(args[i+1], "%d", &secs)
			pollInterval = time.Duration(secs) * time.Second
			i++
		}
	}

	if _, err := osexec.LookPath("tmux"); err != nil {
		fmt.Fprintln(os.Stderr, "cavekit poll: tmux not available on this platform; exiting")
		return
	}

	cwd, _ := os.Getwd()
	waitForSession(sessionName)

	for {
		if !sessionExists(sessionName) {
			return
		}
		frontier := stats.FindFrontier(cwd)
		counts := stats.FrontierProgress(cwd, frontier)
		icon := "○"
		switch {
		case counts.Total > 0 && counts.Done >= counts.Total:
			icon = "■"
		case stats.ReadRalphState(cwd).Active:
			icon = "⟳"
		}
		statusLine := fmt.Sprintf("%s %d/%d", icon, counts.Done, counts.Total)
		_ = osexec.Command("tmux", "set-option", "-t", sessionName, "status-right", statusLine+" ").Run()
		_ = osexec.Command("tmux", "set-option", "-t", sessionName, "status-right-length", "120").Run()
		time.Sleep(pollInterval)
	}
}

func waitForSession(name string) {
	for i := 0; i < 10; i++ {
		if sessionExists(name) {
			return
		}
		time.Sleep(time.Second)
	}
}

func sessionExists(name string) bool {
	return osexec.Command("tmux", "has-session", "-t", name).Run() == nil
}
