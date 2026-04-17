package main

import (
	"fmt"
	"io"
	"os"

	"github.com/JuliusBrussee/cavekit/internal/install"
)

const installUsage = `Usage: cavekit install {sync-codex}
  sync-codex    Link Cavekit into Codex local plugins + prompts + marketplace
`

func runInstall(args []string) {
	sub := "help"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	if err := dispatchInstall(sub, args, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dispatchInstall(sub string, args []string, out io.Writer) error {
	switch sub {
	case "sync-codex":
		return install.SyncCodex(install.SyncCodexOptions{}, out)
	case "help", "--help", "-h", "":
		fmt.Fprint(out, installUsage)
		return nil
	default:
		return fmt.Errorf("cavekit install: unknown subcommand: %s", sub)
	}
}
