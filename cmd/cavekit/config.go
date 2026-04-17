package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/JuliusBrussee/cavekit/internal/config"
)

const configUsage = `Usage: cavekit config {init|get|set|list|path|source|source-path|effective-preset|model|show|summary|presets|caveman-active}
  init                              Create/backfill global and project config files
  get <key> [default]               Read an effective config value
  set <key> <val> [--global|--project]
                                    Write a config value (project by default)
  list [--global|--project]         Show raw config key=value pairs from one file
  path [--global|--project]         Print a config file path
  source <key>                      Print value source: project | global | default
  source-path <key>                 Print the path that supplied the value
  effective-preset                  Print the effective model preset
  model <task-type>                 Resolve model for reasoning | execution | exploration
  show                              Print effective preset, source, and resolved models
  summary                           Print a one-line preset summary
  presets                           Print the built-in preset table
  caveman-active <phase>            Check if caveman mode is active for a phase (build|inspect|draft|architect)
`

func runConfig(args []string) {
	sub := "help"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	store := config.NewStore()
	if err := dispatchConfig(store, sub, args, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dispatchConfig(s *config.Store, sub string, args []string, out io.Writer) error {
	switch sub {
	case "init":
		return s.Init()
	case "get":
		return cfgGet(s, args, out)
	case "set":
		return cfgSet(s, args)
	case "list":
		return cfgList(s, args, out)
	case "path":
		return cfgPath(s, args, out)
	case "source":
		return cfgSource(s, args, out)
	case "source-path":
		return cfgSourcePath(s, args, out)
	case "effective-preset":
		preset, err := s.EffectivePreset()
		if err != nil {
			return err
		}
		fmt.Fprintln(out, preset)
		return nil
	case "model":
		return cfgModel(s, args, out)
	case "show":
		return cfgShow(s, out)
	case "summary":
		line, err := s.SummaryLine()
		if err != nil {
			return err
		}
		fmt.Fprintln(out, line)
		return nil
	case "presets":
		fmt.Fprint(out, config.PresetTable())
		return nil
	case "caveman-active":
		return cfgCavemanActive(s, args, out)
	case "help", "--help", "-h":
		fmt.Fprint(out, configUsage)
		return nil
	default:
		return fmt.Errorf("cavekit config: unknown subcommand: %s", sub)
	}
}

func cfgGet(s *config.Store, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("cavekit config get: key required")
	}
	key := args[0]
	if len(args) >= 2 {
		fmt.Fprintln(out, s.GetWithDefault(key, args[1]))
		return nil
	}
	fmt.Fprintln(out, s.Get(key))
	return nil
}

func cfgSet(s *config.Store, args []string) error {
	scope := config.ScopeProject
	var key, value string
	seenKey, seenValue := false, false
	for _, a := range args {
		switch a {
		case "--global":
			scope = config.ScopeGlobal
		case "--project":
			scope = config.ScopeProject
		default:
			if !seenKey {
				key = a
				seenKey = true
			} else if !seenValue {
				value = a
				seenValue = true
			} else {
				return fmt.Errorf("cavekit config set: unexpected argument %q", a)
			}
		}
	}
	if !seenKey {
		return fmt.Errorf("cavekit config set: key required")
	}
	if !seenValue {
		return fmt.Errorf("cavekit config set: value required")
	}
	return s.Set(key, value, scope)
}

func cfgList(s *config.Store, args []string, out io.Writer) error {
	scope, err := parseScopeFlag(args)
	if err != nil {
		return err
	}
	lines, err := s.List(scope)
	if err != nil {
		return err
	}
	for _, line := range lines {
		fmt.Fprintln(out, line)
	}
	return nil
}

func cfgPath(s *config.Store, args []string, out io.Writer) error {
	scope, err := parseScopeFlag(args)
	if err != nil {
		return err
	}
	if scope == config.ScopeGlobal {
		fmt.Fprintln(out, s.GlobalPath)
	} else {
		fmt.Fprintln(out, s.ProjectPath)
	}
	return nil
}

func cfgSource(s *config.Store, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("cavekit config source: key required")
	}
	fmt.Fprintln(out, s.Source(args[0]))
	return nil
}

func cfgSourcePath(s *config.Store, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("cavekit config source-path: key required")
	}
	fmt.Fprintln(out, s.SourcePath(args[0]))
	return nil
}

func cfgModel(s *config.Store, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("cavekit config model: task type required (reasoning|execution|exploration)")
	}
	model, err := s.Model(config.TaskType(args[0]))
	if err != nil {
		return err
	}
	fmt.Fprintln(out, model)
	return nil
}

func cfgCavemanActive(s *config.Store, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("cavekit config caveman-active: phase required")
	}
	active, err := s.CavemanActive(args[0])
	if err != nil {
		return err
	}
	if active {
		fmt.Fprintln(out, "true")
	} else {
		fmt.Fprintln(out, "false")
	}
	return nil
}

func cfgShow(s *config.Store, out io.Writer) error {
	preset, err := s.EffectivePreset()
	if err != nil {
		return err
	}
	reasoning, err := s.Model(config.TaskReasoning)
	if err != nil {
		return err
	}
	execution, err := s.Model(config.TaskExecution)
	if err != nil {
		return err
	}
	exploration, err := s.Model(config.TaskExploration)
	if err != nil {
		return err
	}
	source := s.Source("bp_model_preset")
	sourcePath := s.SourcePath("bp_model_preset")
	cavemanMode := s.GetWithDefault("caveman_mode", "on")
	cavemanPhases := s.GetWithDefault("caveman_phases", "build,inspect")

	fmt.Fprintf(out, "bp_model_preset=%s\n", preset)
	fmt.Fprintf(out, "bp_model_preset_source=%s\n", source)
	fmt.Fprintf(out, "bp_model_preset_source_path=%s\n", sourcePath)
	fmt.Fprintf(out, "reasoning_model=%s\n", reasoning)
	fmt.Fprintf(out, "execution_model=%s\n", execution)
	fmt.Fprintf(out, "exploration_model=%s\n", exploration)
	fmt.Fprintf(out, "caveman_mode=%s\n", cavemanMode)
	fmt.Fprintf(out, "caveman_phases=%s\n", cavemanPhases)
	fmt.Fprintf(out, "project_config=%s\n", s.ProjectPath)
	fmt.Fprintf(out, "global_config=%s\n", s.GlobalPath)
	return nil
}

func parseScopeFlag(args []string) (config.Scope, error) {
	scope := config.ScopeProject
	for _, a := range args {
		switch a {
		case "--global":
			scope = config.ScopeGlobal
		case "--project":
			scope = config.ScopeProject
		default:
			if strings.HasPrefix(a, "--") {
				return "", fmt.Errorf("unknown flag %q", a)
			}
			return "", fmt.Errorf("unexpected argument %q", a)
		}
	}
	return scope, nil
}
