package main

import (
	"fmt"
	"os"

	"github.com/JuliusBrussee/cavekit/internal/build"
	"github.com/JuliusBrussee/cavekit/internal/config"
)

const setupBuildUsage = `Usage: cavekit setup-build [FILE] [OPTIONS]
  FILE                           Path to build site file (optional; strips @ prefix)

OPTIONS:
  --filter <pattern>             Scope to kits/build site matching pattern
  --peer-review                  Add Codex peer review
  --codex-model <model>          Codex model (default: gpt-5.4)
  --review-interval <n>          Review every Nth iteration (default: 2)
  --max-iterations <n>           Max iterations (default: 20)
  --completion-promise '<text>'  Completion phrase (default: CAVEKIT COMPLETE)
  -h, --help                     Show this help
`

func runSetupBuild(args []string) {
	var opts build.Options
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			fmt.Print(setupBuildUsage)
			return
		case "--filter":
			if i+1 >= len(args) {
				die("--filter requires a pattern")
			}
			opts.Filter = args[i+1]
			i++
		case "--peer-review":
			opts.PeerReview = true
		case "--codex-model":
			if i+1 >= len(args) {
				die("--codex-model requires a model name")
			}
			opts.CodexModel = args[i+1]
			i++
		case "--review-interval":
			if i+1 >= len(args) {
				die("--review-interval requires a number")
			}
			if _, err := fmt.Sscanf(args[i+1], "%d", &opts.ReviewInterval); err != nil {
				die("--review-interval must be an integer")
			}
			i++
		case "--max-iterations":
			if i+1 >= len(args) {
				die("--max-iterations requires a number")
			}
			if _, err := fmt.Sscanf(args[i+1], "%d", &opts.MaxIterations); err != nil {
				die("--max-iterations must be a positive integer")
			}
			i++
		case "--completion-promise":
			if i+1 >= len(args) {
				die("--completion-promise requires text")
			}
			opts.CompletionPromise = args[i+1]
			i++
		default:
			if len(args[i]) > 0 && args[i][0] == '-' {
				die("unknown option: " + args[i])
			}
			arg := args[i]
			if len(arg) > 0 && arg[0] == '@' {
				arg = arg[1:]
			}
			if opts.ExplicitFile != "" {
				die("unexpected argument: " + args[i])
			}
			if _, err := os.Stat(arg); err != nil {
				die("file not found: " + arg)
			}
			opts.ExplicitFile = arg
		}
	}

	cwd, _ := os.Getwd()
	cfg := config.NewStore()
	res, err := build.Run(cwd, opts, cfg, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if res.SelectionRequired {
		os.Exit(0)
	}
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, "❌ "+msg)
	os.Exit(1)
}
