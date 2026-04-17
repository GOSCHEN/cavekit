// Package gate implements the PreToolUse command-safety gate: static
// allow/block lists, a pattern cache, and an optional Codex-backed
// classifier for ambiguous commands.
package gate

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/JuliusBrussee/cavekit/internal/codex"
	"github.com/JuliusBrussee/cavekit/internal/config"
	"github.com/JuliusBrussee/cavekit/internal/paths"
)

// AllowlistExecutables lists base executables that are always safe. Mirrors
// _BP_GATE_ALLOWLIST_EXECUTABLES in scripts/command-gate.sh.
var AllowlistExecutables = []string{
	"ls", "cat", "head", "tail", "less", "more", "wc", "file", "stat", "du", "df",
	"pwd", "whoami", "id", "uname", "hostname", "date",
	"echo", "printf", "true", "false",
	"grep", "rg", "ag", "ack", "find", "fd", "locate", "which", "where", "type",
	"git",
	"node", "npm", "npx", "yarn", "pnpm", "bun", "deno", "tsx", "ts-node",
	"python", "python3", "pip", "pip3", "uv",
	"go", "cargo", "rustc", "rustup",
	"make", "cmake",
	"docker",
	"kubectl", "helm",
	"jq", "yq", "sed", "awk", "sort", "uniq", "tr", "cut", "paste",
	"curl", "wget",
	"ssh", "scp",
	"tar", "gzip", "gunzip", "zip", "unzip",
	"diff", "patch",
	"test", "[", "[[",
	"tput", "clear", "reset",
	"codex",
}

// AllowlistGit lists git subcommands treated as safe regardless of arguments.
var AllowlistGit = []string{
	"status", "log", "diff", "show", "branch", "tag", "remote", "stash",
	"fetch", "pull", "add", "commit", "checkout", "switch",
	"rebase", "merge", "cherry-pick",
	"bisect", "blame", "annotate", "shortlog", "describe",
	"ls-files", "ls-tree", "rev-parse", "rev-list", "name-rev",
	"config",
}

// BlocklistPatterns are regexes that trigger an immediate BLOCK verdict.
var BlocklistPatterns = []string{
	`rm\s+(-[a-zA-Z]*r[a-zA-Z]*f|--recursive\b.*--force|-[a-zA-Z]*f[a-zA-Z]*r)\b.*(/|\*|\.\.|~)`,
	`git\s+push\s+.*--force\b.*\b(main|master)\b`,
	`git\s+push\s+.*-f\b.*\b(main|master)\b`,
	`git\s+reset\s+--hard`,
	`git\s+clean\s+-[a-zA-Z]*f`,
	`\b(DROP|TRUNCATE|DELETE\s+FROM)\b.*\b(TABLE|DATABASE)\b`,
	`curl\b.*\|\s*(bash|sh|zsh)\b`,
	`wget\b.*\|\s*(bash|sh|zsh)\b`,
	`chmod\s+777\b`,
	`chmod\s+-R\s+777\b`,
	`:\(\)\s*\{\s*:\|:\s*&\s*\}\s*;`,
	`mkfs\b`,
	`dd\s+.*of=/dev/`,
	`\bsudo\s+rm\b`,
}

// BlocklistGitPatterns are regex fragments that, appended to `git\s+`,
// trigger a BLOCK verdict for git-specific dangerous operations.
var BlocklistGitPatterns = []string{
	`push.*--force`,
	`push.*-f\b`,
	`reset\s+--hard`,
	`clean\s+-[a-zA-Z]*f`,
	`branch\s+-D`,
}

var (
	quotedDouble = regexp.MustCompile(`"[^"]*"`)
	quotedSingle = regexp.MustCompile(`'[^']*'`)
	subst        = regexp.MustCompile(`\$\([^)]*\)`)
	variable     = regexp.MustCompile(`\$\{[^}]*\}`)
	absPath      = regexp.MustCompile(`(/[a-zA-Z0-9_./-]+)`)
	relPath      = regexp.MustCompile(`\./[a-zA-Z0-9_./-]+`)
	hexHash      = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	multiSpace   = regexp.MustCompile(`\s+`)
)

// NormalizeCommand strips variable arguments (paths, quoted strings, hashes)
// while preserving command structure. Used as the cache key.
func NormalizeCommand(cmd string) string {
	out := quotedDouble.ReplaceAllString(cmd, "<STR>")
	out = quotedSingle.ReplaceAllString(out, "<STR>")
	out = subst.ReplaceAllString(out, "<SUBST>")
	out = variable.ReplaceAllString(out, "<VAR>")
	out = absPath.ReplaceAllString(out, "<PATH>")
	out = relPath.ReplaceAllString(out, "<PATH>")
	out = hexHash.ReplaceAllString(out, "<HASH>")
	out = multiSpace.ReplaceAllString(out, " ")
	return strings.TrimSpace(out)
}

// Verdict is the classifier output.
type Verdict struct {
	Decision string // "approve" | "block" | "passthrough"
	Reason   string
}

// FastClassify applies static rules without calling Codex. Returns Verdict.
// Decision values: "approve", "block", "unknown" (caller should fall through
// to the Codex classifier).
func FastClassify(cfgStore *config.Store, cmd string) Verdict {
	baseExec := baseExecutable(cmd)

	for _, pattern := range BlocklistPatterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		if re.MatchString(cmd) {
			return Verdict{Decision: "block", Reason: "Matches blocklist pattern: " + pattern}
		}
	}

	userBlocklist := strings.Fields(cfgStore.GetWithDefault("command_gate_blocklist", ""))
	for _, entry := range userBlocklist {
		if entry == "" {
			continue
		}
		re, err := regexp.Compile(entry)
		if err != nil {
			continue
		}
		if re.MatchString(cmd) {
			return Verdict{Decision: "block", Reason: "Matches user blocklist: " + entry}
		}
	}

	if baseExec == "git" {
		fields := strings.Fields(cmd)
		var gitSub string
		if len(fields) >= 2 {
			gitSub = fields[1]
		}

		for _, pattern := range BlocklistGitPatterns {
			re, err := regexp.Compile(`git\s+` + pattern)
			if err != nil {
				continue
			}
			if re.MatchString(cmd) {
				return Verdict{Decision: "block", Reason: "Dangerous git operation: " + pattern}
			}
		}
		for _, allowed := range AllowlistGit {
			if gitSub == allowed {
				return Verdict{Decision: "approve"}
			}
		}
	}

	for _, allowed := range AllowlistExecutables {
		if baseExec == allowed {
			return Verdict{Decision: "approve"}
		}
	}

	userAllowlist := strings.Fields(cfgStore.GetWithDefault("command_gate_allowlist", ""))
	for _, entry := range userAllowlist {
		if entry == "" {
			continue
		}
		if baseExec == entry {
			return Verdict{Decision: "approve"}
		}
		re, err := regexp.Compile(entry)
		if err != nil {
			continue
		}
		if re.MatchString(cmd) {
			return Verdict{Decision: "approve"}
		}
	}
	return Verdict{Decision: "unknown"}
}

func baseExecutable(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	base := fields[0]
	if idx := strings.LastIndexAny(base, "/\\"); idx >= 0 {
		base = base[idx+1:]
	}
	return base
}

// ── Cache ─────────────────────────────────────────────────────────────

// Cache is a line-delimited `normalized|verdict|reason` file. Matches the
// bash implementation so a Go port can share the same file with the legacy
// shell during the transition.
type Cache struct {
	Path string
}

// NewCache returns a Cache rooted at a per-CWD file under CavekitDir.
func NewCache() *Cache {
	return &Cache{Path: filepath.Join(paths.CavekitDir(), "command-gate-cache")}
}

// Get returns the cached verdict for key, or false when absent.
func (c *Cache) Get(key string) (Verdict, bool) {
	f, err := os.Open(c.Path)
	if err != nil {
		return Verdict{}, false
	}
	defer f.Close()

	prefix := key + "|"
	var last string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.HasPrefix(line, prefix) {
			last = strings.TrimPrefix(line, prefix)
		}
	}
	if last == "" {
		return Verdict{}, false
	}
	return parseCachedValue(last), true
}

func parseCachedValue(raw string) Verdict {
	parts := strings.SplitN(raw, "|", 2)
	v := Verdict{Decision: strings.ToLower(parts[0])}
	if len(parts) == 2 {
		v.Reason = parts[1]
	}
	return v
}

// Set stores a verdict, appending a fresh line. Older entries for the same
// key are shadowed — tail-reads pick the latest.
func (c *Cache) Set(key string, v Verdict) error {
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(c.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s|%s|%s\n", key, v.Decision, v.Reason)
	return err
}

// Clear removes the cache file.
func (c *Cache) Clear() error {
	err := os.Remove(c.Path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ── Codex classifier ──────────────────────────────────────────────────

// CodexPrompt is the instruction template given to the Codex classifier.
const CodexPrompt = `You are a command safety classifier. Given a bash command and its working directory context, classify whether it is safe to execute autonomously. Output exactly one JSON object with these fields:
- safe: boolean (true if the command is safe)
- reason: string (brief explanation)
- severity: "info" | "warn" | "block"

Rules:
- "info": safe to run silently
- "warn": probably safe but worth logging (e.g., writes to important files)
- "block": potentially destructive or dangerous (data loss, credential exposure, network exfiltration)

Be conservative: if unsure, classify as "warn" not "info".
Do NOT block standard development commands (test runners, build tools, linters, formatters).
DO block commands that delete data, expose secrets, or modify system configuration.

Command: `

// CodexClassifyOptions configures CodexClassify.
type CodexClassifyOptions struct {
	Runner    codex.CommandRunner
	Available codex.Availability
	WorkDir   string
}

// CodexClassify asks Codex to classify cmd. Falls back to passthrough when
// Codex is unavailable or the response does not parse.
func CodexClassify(ctx context.Context, cfgStore *config.Store, cmd string, opts CodexClassifyOptions) Verdict {
	if !opts.Available.Available {
		return Verdict{Decision: "passthrough", Reason: "Codex unavailable"}
	}
	runner := opts.Runner
	if runner == nil {
		runner = codex.ExecRunner{}
	}
	workdir := opts.WorkDir
	if workdir == "" {
		workdir, _ = os.Getwd()
	}

	timeoutMS, err := strconv.Atoi(cfgStore.GetWithDefault("command_gate_timeout", "3000"))
	if err != nil || timeoutMS <= 0 {
		timeoutMS = 3000
	}
	model := cfgStore.GetWithDefault("command_gate_model", "o4-mini")
	prompt := CodexPrompt + cmd + "\nWorking directory: " + workdir

	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()

	raw, runErr := runner.Run(cctx, "codex", []string{"--approval-mode", "full-auto", "--model", model, "--quiet", "-p", prompt}, "")
	if runErr != nil {
		return Verdict{Decision: "passthrough", Reason: "Codex classification failed"}
	}

	parsed, ok := parseCodexJSON(string(raw))
	if !ok {
		return Verdict{Decision: "passthrough", Reason: "Could not parse Codex response"}
	}
	switch parsed.Severity {
	case "info":
		return Verdict{Decision: "approve", Reason: parsed.Reason}
	case "warn":
		return Verdict{Decision: "approve", Reason: "WARNING: " + parsed.Reason}
	case "block":
		return Verdict{Decision: "block", Reason: parsed.Reason}
	}
	if parsed.Safe {
		return Verdict{Decision: "approve", Reason: parsed.Reason}
	}
	return Verdict{Decision: "block", Reason: parsed.Reason}
}

type codexReply struct {
	Safe     bool   `json:"safe"`
	Reason   string `json:"reason"`
	Severity string `json:"severity"`
}

// parseCodexJSON locates the first balanced JSON object in raw and decodes
// it. Tolerates trailing log output that surrounds the payload.
func parseCodexJSON(raw string) (codexReply, bool) {
	open := strings.IndexByte(raw, '{')
	if open < 0 {
		return codexReply{}, false
	}
	depth := 0
	for i := open; i < len(raw); i++ {
		switch raw[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				var r codexReply
				if err := json.Unmarshal([]byte(raw[open:i+1]), &r); err == nil {
					return r, true
				}
				return codexReply{}, false
			}
		}
	}
	return codexReply{}, false
}

// ── Hook protocol ─────────────────────────────────────────────────────

// HookInput is the JSON payload Claude Code PreToolUse hooks receive.
type HookInput struct {
	ToolName string `json:"tool_name"`
	Input    struct {
		Command string `json:"command"`
	} `json:"input"`
}

// HookDecision is the response the hook writes to stdout. An empty Decision
// means passthrough (no output).
type HookDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

// HookOptions tunes RunHook.
type HookOptions struct {
	ToolName string
	Command  string
	// AlreadyAllowed/AlreadyBlocked signal prior decisions from the Claude
	// Code permission layer; they short-circuit the gate.
	AlreadyAllowed bool
	AlreadyBlocked bool
	Runner         codex.CommandRunner
	// AvailabilityOverride bypasses Detect for tests.
	AvailabilityOverride *codex.Availability
	Cache                *Cache
}

// RunHook executes the full pipeline: Claude permission shortcut → fast
// classifier → cache → Codex. Writes the hook-protocol response to w. Returns
// the verdict for programmatic callers.
func RunHook(ctx context.Context, cfgStore *config.Store, opts HookOptions, w io.Writer) (Verdict, error) {
	if opts.ToolName != "Bash" && opts.ToolName != "bash" {
		return Verdict{Decision: "passthrough"}, nil
	}
	if opts.Command == "" {
		return Verdict{Decision: "passthrough"}, nil
	}
	if cfgStore.GetWithDefault("command_gate", "all") == "off" {
		return Verdict{Decision: "passthrough"}, nil
	}
	if opts.AlreadyAllowed || opts.AlreadyBlocked {
		return Verdict{Decision: "passthrough"}, nil
	}

	fast := FastClassify(cfgStore, opts.Command)
	if fast.Decision == "approve" {
		return fast, nil
	}
	if fast.Decision == "block" {
		writeBlock(w, fast.Reason)
		return fast, nil
	}

	cache := opts.Cache
	if cache == nil {
		cache = NewCache()
	}
	key := NormalizeCommand(opts.Command)
	if cached, ok := cache.Get(key); ok {
		switch cached.Decision {
		case "approve":
			return cached, nil
		case "block":
			writeBlock(w, cached.Reason)
			return cached, nil
		default:
			return cached, nil
		}
	}

	var avail codex.Availability
	if opts.AvailabilityOverride != nil {
		avail = *opts.AvailabilityOverride
	} else {
		avail = codex.Detect(ctx)
	}

	verdict := CodexClassify(ctx, cfgStore, opts.Command, CodexClassifyOptions{
		Runner:    opts.Runner,
		Available: avail,
	})
	_ = cache.Set(key, verdict)

	if verdict.Decision == "block" {
		writeBlock(w, verdict.Reason)
	}
	return verdict, nil
}

func writeBlock(w io.Writer, reason string) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(HookDecision{Decision: "block", Reason: reason})
}

// ParseHookStdin reads a PreToolUse JSON payload from r. Tolerant of empty
// input (returns zero value without error).
func ParseHookStdin(r io.Reader) (HookInput, error) {
	var in HookInput
	data, err := io.ReadAll(r)
	if err != nil {
		return in, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return in, nil
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return in, err
	}
	return in, nil
}
