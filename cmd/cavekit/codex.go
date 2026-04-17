package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/JuliusBrussee/cavekit/internal/codex"
	"github.com/JuliusBrussee/cavekit/internal/config"
)

const codexUsage = `Usage: cavekit codex {detect|review|gate|speculative|design|findings}
  detect                             Print CODEX_BINARY_AVAILABLE, CODEX_PLUGIN_PRESENT, codex_available
  review [--base <ref>] [--dry-run]  Adversarial review via Codex CLI
  gate {evaluate|fix-tasks|cycle}    Tier-gate evaluation + fix-task generation
  speculative {dispatch|status|retrieve|cleanup|_run}
                                     Speculative pre-build review pipeline
  design [--kits-dir <dir>] [--cycle [--max-cycles N]] [--dry-run]
                                     Adversarial design challenge over cavekit specs
  findings {init|next-id|append|update|blocking|path}
                                     Manage the review-findings table
`

func runCodex(args []string) {
	sub := "help"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	if err := dispatchCodex(context.Background(), sub, args, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dispatchCodex(ctx context.Context, sub string, args []string, out io.Writer) error {
	switch sub {
	case "detect":
		a := codex.Detect(ctx)
		fmt.Fprintf(out, "CODEX_BINARY_AVAILABLE=%s\n", boolString(a.BinaryAvailable))
		fmt.Fprintf(out, "CODEX_PLUGIN_PRESENT=%s\n", boolString(a.PluginPresent))
		fmt.Fprintf(out, "codex_available=%s\n", boolString(a.Available))
		return nil
	case "review":
		return dispatchReview(ctx, args, out)
	case "gate":
		return dispatchGate(ctx, args, out)
	case "speculative":
		return dispatchSpeculative(ctx, args, out)
	case "design":
		return dispatchDesign(ctx, args, out)
	case "findings":
		return dispatchFindings(args, out)
	case "help", "--help", "-h", "":
		fmt.Fprint(out, codexUsage)
		return nil
	default:
		return fmt.Errorf("cavekit codex: unknown subcommand: %s", sub)
	}
}

func dispatchFindings(args []string, out io.Writer) error {
	sub := "help"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	s := codex.NewFindingsStore()
	switch sub {
	case "init":
		return s.Init()
	case "next-id":
		id, err := s.NextID()
		if err != nil {
			return err
		}
		fmt.Fprintln(out, id)
		return nil
	case "append":
		return findingsAppend(s, args, out)
	case "update":
		if len(args) < 2 {
			return fmt.Errorf("findings update: ID and status required")
		}
		return s.UpdateStatus(args[0], args[1])
	case "blocking":
		list, err := s.ListBlocking()
		if err != nil {
			return err
		}
		for _, b := range list {
			fmt.Fprintf(out, "%s|%s|%s\n", b.Finding, b.Severity, b.File)
		}
		return nil
	case "path":
		fmt.Fprintln(out, s.Path)
		return nil
	case "help", "--help", "-h", "":
		fmt.Fprintln(out, "Usage: cavekit codex findings {init|next-id|append|update|blocking|path}")
		return nil
	default:
		return fmt.Errorf("cavekit codex findings: unknown subcommand: %s", sub)
	}
}

func dispatchReview(ctx context.Context, args []string, out io.Writer) error {
	opts := codex.ReviewOptions{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--base":
			if i+1 >= len(args) {
				return fmt.Errorf("--base requires an argument")
			}
			opts.BaseRef = args[i+1]
			i++
		case "--dry-run":
			opts.DryRun = true
		case "--help", "-h":
			fmt.Fprintln(out, "Usage: cavekit codex review [--base <ref>] [--dry-run]")
			return nil
		default:
			return fmt.Errorf("unknown argument: %s", args[i])
		}
	}
	cfgStore := config.NewStore()
	findings := codex.NewFindingsStore()
	_, err := codex.Review(ctx, opts, cfgStore, findings, out)
	return err
}

func dispatchGate(ctx context.Context, args []string, out io.Writer) error {
	sub := "evaluate"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	cfgStore := config.NewStore()
	findings := codex.NewFindingsStore()

	switch sub {
	case "evaluate", "gate":
		result, err := codex.EvaluateGate(cfgStore, findings)
		if err != nil {
			return err
		}
		verdict := "proceed"
		if !result.Proceed {
			verdict = "blocked"
		}
		fmt.Fprintf(out, "GATE_RESULT=%s\n", verdict)
		fmt.Fprintf(out, "BLOCKING_COUNT=%d\n", result.BlockingCount)
		fmt.Fprintf(out, "DEFERRED_COUNT=%d\n", result.DeferredCount)
		if len(result.BlockingIDs) > 0 {
			fmt.Fprintf(out, "BLOCKING_FINDINGS=%s\n", joinComma(result.BlockingIDs))
		}
		if !result.Proceed {
			os.Exit(1)
		}
		return nil
	case "fix-tasks":
		tasks, err := codex.GenerateFixTasks(cfgStore, findings)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			fmt.Fprintf(out, "%s|%s|%s|%s\n", t.ID, t.Severity, t.File, t.Description)
		}
		return nil
	case "cycle":
		return dispatchGateCycle(ctx, args, cfgStore, findings, out)
	case "help", "--help", "-h":
		fmt.Fprintln(out, "Usage: cavekit codex gate {evaluate|fix-tasks|cycle}")
		return nil
	default:
		return fmt.Errorf("cavekit codex gate: unknown subcommand: %s", sub)
	}
}

func dispatchDesign(ctx context.Context, args []string, out io.Writer) error {
	var kitsDir string
	cycle := false
	maxCycles := 2
	dryRun := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--kits-dir":
			if i+1 >= len(args) {
				return fmt.Errorf("--kits-dir requires argument")
			}
			kitsDir = args[i+1]
			i++
		case "--cycle":
			cycle = true
		case "--max-cycles":
			if i+1 >= len(args) {
				return fmt.Errorf("--max-cycles requires argument")
			}
			if _, err := fmt.Sscanf(args[i+1], "%d", &maxCycles); err != nil {
				return err
			}
			i++
		case "--dry-run":
			dryRun = true
		case "--help", "-h":
			fmt.Fprintln(out, "Usage: cavekit codex design [--kits-dir <dir>] [--cycle [--max-cycles N]] [--dry-run]")
			return nil
		default:
			return fmt.Errorf("unknown arg: %s", args[i])
		}
	}
	cfgStore := config.NewStore()

	if cycle {
		res, err := codex.DesignChallengeCycle(ctx, codex.DesignCycleOptions{
			KitsDir:   kitsDir,
			MaxCycles: maxCycles,
		}, cfgStore, out)
		if err != nil {
			return err
		}
		switch res.Outcome {
		case codex.DesignCyclePass:
			return nil
		case codex.DesignCycleSkipped:
			os.Exit(2)
		case codex.DesignCycleAwaitFixes:
			os.Exit(3)
		case codex.DesignCycleExhausted:
			os.Exit(1)
		}
		return nil
	}

	res, err := codex.DesignChallenge(ctx, codex.DesignOptions{
		KitsDir: kitsDir,
		DryRun:  dryRun,
	}, cfgStore, out)
	if err != nil {
		return err
	}
	if res.Skipped {
		os.Exit(2)
	}
	if res.HasCritical {
		os.Exit(1)
	}
	return nil
}

func dispatchSpeculative(ctx context.Context, args []string, out io.Writer) error {
	sub := "help"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	store := codex.NewSpeculativeStore(codex.DetectProjectRoot())
	cfgStore := config.NewStore()
	findings := codex.NewFindingsStore()

	switch sub {
	case "dispatch":
		return specDispatch(ctx, store, cfgStore, args, out)
	case "status":
		return codex.Status(store, out)
	case "retrieve":
		return specRetrieve(store, cfgStore, args, out)
	case "cleanup":
		return store.Cleanup()
	case "_run":
		return specRun(ctx, cfgStore, findings, args, out)
	case "help", "--help", "-h", "":
		fmt.Fprintln(out, "Usage: cavekit codex speculative {dispatch <tier> <base-ref>|status|retrieve <tier>|cleanup}")
		return nil
	default:
		return fmt.Errorf("cavekit codex speculative: unknown subcommand: %s", sub)
	}
}

func specDispatch(ctx context.Context, store *codex.SpeculativeStore, cfgStore *config.Store, args []string, out io.Writer) error {
	if len(args) < 2 {
		return fmt.Errorf("dispatch: tier and base-ref required")
	}
	var tier int
	if _, err := fmt.Sscanf(args[0], "%d", &tier); err != nil {
		return fmt.Errorf("tier must be an integer")
	}
	return codex.Dispatch(ctx, store, cfgStore, codex.DispatchOptions{
		Tier:    tier,
		BaseRef: args[1],
	}, out)
}

func specRetrieve(store *codex.SpeculativeStore, cfgStore *config.Store, args []string, out io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("retrieve: tier required")
	}
	var tier int
	if _, err := fmt.Sscanf(args[0], "%d", &tier); err != nil {
		return fmt.Errorf("tier must be an integer")
	}
	timeout := codex.SpeculativeTimeout(cfgStore)
	outcome, err := codex.Retrieve(store, tier, timeout, out)
	if err != nil {
		return err
	}
	switch outcome {
	case codex.RetrieveConsumed:
		return nil
	case codex.RetrieveFallback:
		os.Exit(1)
	case codex.RetrieveTimeout:
		os.Exit(2)
	}
	return nil
}

func specRun(ctx context.Context, cfgStore *config.Store, findings *codex.FindingsStore, args []string, out io.Writer) error {
	var tier int
	baseRef, donePath := "", ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--tier":
			if i+1 >= len(args) {
				return fmt.Errorf("--tier requires argument")
			}
			if _, err := fmt.Sscanf(args[i+1], "%d", &tier); err != nil {
				return err
			}
			i++
		case "--base":
			if i+1 >= len(args) {
				return fmt.Errorf("--base requires argument")
			}
			baseRef = args[i+1]
			i++
		case "--done":
			if i+1 >= len(args) {
				return fmt.Errorf("--done requires argument")
			}
			donePath = args[i+1]
			i++
		default:
			return fmt.Errorf("unknown arg: %s", args[i])
		}
	}
	if donePath == "" {
		return fmt.Errorf("--done required")
	}
	codex.RunInBackground(ctx, tier, baseRef, donePath, cfgStore, findings, out)
	return nil
}

func dispatchGateCycle(ctx context.Context, args []string, cfgStore *config.Store, findings *codex.FindingsStore, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("cycle: base ref required")
	}
	opts := codex.CycleOptions{BaseRef: args[0], MaxCycles: 2}
	if len(args) >= 2 {
		if _, err := fmt.Sscanf(args[1], "%d", &opts.MaxCycles); err != nil {
			return fmt.Errorf("max cycles must be an integer, got %q", args[1])
		}
	}
	outcome, err := codex.ReviewFixCycle(ctx, opts, cfgStore, findings, out)
	if err != nil {
		return err
	}
	switch outcome {
	case codex.CycleProceed:
		return nil
	case codex.CycleAwaitingFixes:
		os.Exit(2)
	case codex.CycleExhausted:
		os.Exit(1)
	}
	return nil
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}

func findingsAppend(s *codex.FindingsStore, args []string, out io.Writer) error {
	if len(args) < 5 {
		return fmt.Errorf("findings append: severity file description source tier [task]")
	}
	var tier int
	if _, err := fmt.Sscanf(args[4], "%d", &tier); err != nil {
		return fmt.Errorf("tier must be an integer, got %q", args[4])
	}
	task := ""
	if len(args) >= 6 {
		task = args[5]
	}
	id, err := s.Append(codex.Finding{
		Severity:    args[0],
		File:        args[1],
		Description: args[2],
		Source:      args[3],
		Tier:        tier,
		Task:        task,
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(out, id)
	return nil
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
