package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/JuliusBrussee/cavekit/internal/config"
	"github.com/JuliusBrussee/cavekit/internal/gate"
)

const commandGateUsage = `Usage: cavekit command-gate {hook|classify|normalize|codex|cache-clear}
  hook [tool cmd]   Run as PreToolUse hook (or reads JSON from stdin)
  classify <cmd>    Fast-path classify a command
  normalize <cmd>   Normalize command for caching
  codex <cmd>       Send to Codex for classification
  cache-clear       Clear the verdict cache
`

func runCommandGate(args []string) {
	sub := "hook"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	if err := dispatchCommandGate(context.Background(), sub, args, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dispatchCommandGate(ctx context.Context, sub string, args []string, in io.Reader, out io.Writer) error {
	cfgStore := config.NewStore()
	switch sub {
	case "hook":
		return cgHook(ctx, cfgStore, args, in, out)
	case "classify":
		if len(args) == 0 {
			return fmt.Errorf("classify: command required")
		}
		v := gate.FastClassify(cfgStore, args[0])
		if v.Reason != "" {
			fmt.Fprintf(out, "%s|%s\n", verdictLabel(v.Decision), v.Reason)
		} else {
			fmt.Fprintln(out, verdictLabel(v.Decision))
		}
		return nil
	case "normalize":
		if len(args) == 0 {
			return fmt.Errorf("normalize: command required")
		}
		fmt.Fprintln(out, gate.NormalizeCommand(args[0]))
		return nil
	case "codex":
		if len(args) == 0 {
			return fmt.Errorf("codex: command required")
		}
		v := gate.CodexClassify(ctx, cfgStore, args[0], gate.CodexClassifyOptions{})
		fmt.Fprintf(out, "%s|%s\n", verdictLabel(v.Decision), v.Reason)
		return nil
	case "cache-clear":
		return gate.NewCache().Clear()
	case "help", "--help", "-h", "":
		fmt.Fprint(out, commandGateUsage)
		return nil
	default:
		return fmt.Errorf("cavekit command-gate: unknown subcommand: %s", sub)
	}
}

func cgHook(ctx context.Context, cfgStore *config.Store, args []string, in io.Reader, out io.Writer) error {
	opts := gate.HookOptions{
		AlreadyAllowed: os.Getenv("BP_HOOK_ALREADY_ALLOWED") == "1",
		AlreadyBlocked: os.Getenv("BP_HOOK_ALREADY_BLOCKED") == "1",
	}
	if len(args) >= 2 {
		opts.ToolName = args[0]
		opts.Command = args[1]
	} else {
		parsed, err := gate.ParseHookStdin(in)
		if err != nil {
			return err
		}
		opts.ToolName = parsed.ToolName
		opts.Command = parsed.Input.Command
	}
	_, err := gate.RunHook(ctx, cfgStore, opts, out)
	return err
}

func verdictLabel(d string) string {
	switch d {
	case "approve":
		return "APPROVE"
	case "block":
		return "BLOCK"
	case "passthrough":
		return "PASSTHROUGH"
	case "unknown":
		return "UNKNOWN"
	}
	return d
}
