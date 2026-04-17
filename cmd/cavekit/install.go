package main

import (
	"fmt"
	"io"
	"os"

	"github.com/JuliusBrussee/cavekit/internal/install"
)

const installUsage = `Usage: cavekit install {plugin|sync-codex|all}
  plugin        Link Cavekit into Claude Code marketplace + merge settings.json
  sync-codex    Link Cavekit into Codex local plugins + prompts + marketplace
  all           Run both plugin and sync-codex
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
	case "plugin":
		return install.InstallPlugin(install.PluginOptions{}, out)
	case "sync-codex":
		return install.SyncCodex(install.SyncCodexOptions{}, out)
	case "all":
		if err := install.InstallPlugin(install.PluginOptions{}, out); err != nil {
			return err
		}
		fmt.Fprintln(out, "")
		return install.SyncCodex(install.SyncCodexOptions{}, out)
	case "help", "--help", "-h", "":
		fmt.Fprint(out, installUsage)
		return nil
	default:
		return fmt.Errorf("cavekit install: unknown subcommand: %s", sub)
	}
}
