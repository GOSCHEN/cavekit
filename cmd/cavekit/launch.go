package main

import (
	"context"
	"fmt"
	"os"

	"github.com/JuliusBrussee/cavekit/internal/launch"
)

const launchUsage = `Usage: cavekit launch [--expanded] [--program NAME] <frontier-path> [<frontier-path> ...]
  --expanded         One session per frontier plus dashboard panes (tmux only)
  --program NAME     Program to launch inside each session (default: claude)
`

func runLaunch(args []string) {
	opts := launch.Options{Program: "claude"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--expanded":
			opts.Expanded = true
		case "--program":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--program requires an argument")
				os.Exit(1)
			}
			opts.Program = args[i+1]
			i++
		case "--help", "-h":
			fmt.Print(launchUsage)
			return
		default:
			if len(args[i]) > 0 && args[i][0] == '-' {
				fmt.Fprintf(os.Stderr, "unknown flag: %s\n", args[i])
				os.Exit(1)
			}
			opts.Frontiers = append(opts.Frontiers, args[i])
		}
	}
	if len(opts.Frontiers) == 0 {
		fmt.Fprint(os.Stderr, launchUsage)
		os.Exit(1)
	}
	if _, err := launch.Run(context.Background(), opts, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
