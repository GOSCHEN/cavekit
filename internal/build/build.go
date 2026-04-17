// Package build implements the `cavekit build` setup: build-site discovery,
// prior-cycle archival, Codex peer-review wiring, and Ralph Loop activation.
package build

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/JuliusBrussee/cavekit/internal/codex"
	"github.com/JuliusBrussee/cavekit/internal/config"
)

// Options enumerates the flags cavekit build accepts. Zero values default
// to the bash script's behavior.
type Options struct {
	Filter            string
	PeerReview        bool
	CodexModel        string
	ReviewInterval    int
	MaxIterations     int
	CompletionPromise string
	ExplicitFile      string
	// AvailabilityOverride skips Codex detection during tests.
	AvailabilityOverride *codex.Availability
	// Now is injected for testability.
	Now func() time.Time
}

// defaults fills in zero-valued fields.
func (o *Options) defaults() {
	if o.CodexModel == "" {
		o.CodexModel = "gpt-5.4"
	}
	if o.ReviewInterval == 0 {
		o.ReviewInterval = 2
	}
	if o.MaxIterations == 0 {
		o.MaxIterations = 20
	}
	if o.CompletionPromise == "" {
		o.CompletionPromise = "CAVEKIT COMPLETE"
	}
	if o.Now == nil {
		o.Now = time.Now
	}
}

// SiteCandidate describes one discovered build site file.
type SiteCandidate struct {
	Path       string
	TotalTasks int
	DoneTasks  int
}

// Result summarizes one invocation of Run.
type Result struct {
	FrontierFile          string
	SiteCandidates        []SiteCandidate // populated only when selection required
	SelectionRequired     bool
	SpecFiles             []string
	ArchiveCount          int
	ArchivedDir           string
	PeerReviewCLI         bool
	PeerReviewMCPUpdated  bool
}

// Run drives the full setup flow, writing progress to w. When multiple
// candidate sites exist and no ExplicitFile is supplied, returns with
// SelectionRequired=true and does not archive or write Ralph state.
func Run(projectRoot string, opts Options, cfg *config.Store, w io.Writer) (Result, error) {
	opts.defaults()

	summary, err := cfg.SummaryLine()
	if err == nil {
		fmt.Fprintln(w, summary)
	}

	ralphLocal := filepath.Join(projectRoot, ".claude", "ralph-loop.local.md")
	_ = os.Remove(ralphLocal)

	var res Result

	if opts.ExplicitFile != "" {
		if _, err := os.Stat(opts.ExplicitFile); err != nil {
			return res, fmt.Errorf("file not found: %s", opts.ExplicitFile)
		}
		res.FrontierFile = opts.ExplicitFile
		fmt.Fprintf(w, "📋 Build site: %s\n", res.FrontierFile)
	} else {
		candidates, err := DiscoverSites(projectRoot, opts.Filter, w)
		if err != nil {
			return res, err
		}
		if len(candidates) == 0 {
			fmt.Fprintln(w, "❌ No build site or plan found in context/plans/ or context/sites/")
			fmt.Fprintln(w, "   Run /ck:map first to generate one.")
			return res, fmt.Errorf("no build site found")
		}
		if len(candidates) > 1 {
			loaded, err := enrichCandidates(projectRoot, candidates)
			if err != nil {
				return res, err
			}
			printCandidateMenu(w, loaded)
			res.SelectionRequired = true
			res.SiteCandidates = loaded
			return res, nil
		}
		res.FrontierFile = candidates[0].Path
		fmt.Fprintf(w, "📋 Build site: %s\n", res.FrontierFile)
	}

	archCount, archDir, err := MaybeArchivePriorCycle(projectRoot, res.FrontierFile, opts.Now(), w)
	if err != nil {
		return res, err
	}
	res.ArchiveCount = archCount
	res.ArchivedDir = archDir

	res.SpecFiles = DiscoverSpecs(projectRoot, opts.Filter)

	peerCLI, mcpUpdated, err := configurePeerReview(projectRoot, opts, cfg, w)
	if err != nil {
		return res, err
	}
	res.PeerReviewCLI = peerCLI
	res.PeerReviewMCPUpdated = mcpUpdated

	prompt := BuildRalphPrompt(res.FrontierFile, res.SpecFiles, opts, peerCLI)
	if err := WriteRalphState(ralphLocal, prompt, opts); err != nil {
		return res, err
	}

	printSummary(w, res, opts, prompt)
	return res, nil
}

var (
	taskIDRe = regexp.MustCompile(`T-(?:[A-Za-z0-9]+-)*[0-9]+`)
)

// DiscoverSites searches context/plans then context/sites for buildable
// markdown files, skipping archives and overview/index files. If filter is
// non-empty, only filenames containing the substring are returned (with a
// fallback to all candidates when the filter matches nothing).
func DiscoverSites(projectRoot, filter string, w io.Writer) ([]SiteCandidate, error) {
	var all []string
	for _, rel := range []string{"context/plans", "context/sites"} {
		dir := filepath.Join(projectRoot, rel)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var names []string
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			name := e.Name()
			lower := strings.ToLower(name)
			if !strings.Contains(lower, "site") && !strings.Contains(lower, "plan") && !strings.Contains(lower, "frontier") {
				continue
			}
			if strings.Contains(lower, "overview") {
				continue
			}
			names = append(names, name)
		}
		sort.Strings(names)
		for _, n := range names {
			all = append(all, filepath.Join(dir, n))
		}
	}

	// Exclude archive subpaths.
	filtered := all[:0]
	for _, p := range all {
		if strings.Contains(filepath.ToSlash(p), "/archive/") {
			continue
		}
		filtered = append(filtered, p)
	}
	all = filtered

	if filter != "" {
		var match []string
		for _, p := range all {
			if strings.Contains(filepath.Base(p), filter) {
				match = append(match, p)
			}
		}
		if len(match) == 0 {
			fmt.Fprintf(w, "⚠️  Filter %q matched no build sites, searching all\n", filter)
		} else {
			all = match
		}
	}

	cands := make([]SiteCandidate, 0, len(all))
	for _, p := range all {
		cands = append(cands, SiteCandidate{Path: p})
	}
	return cands, nil
}

// enrichCandidates fills in task counts for each candidate. Used when the
// user must choose between multiple candidates.
func enrichCandidates(projectRoot string, cands []SiteCandidate) ([]SiteCandidate, error) {
	for i, c := range cands {
		total := countTasks(c.Path)
		done, err := countDoneTasks(projectRoot, c.Path)
		if err != nil {
			return nil, err
		}
		cands[i].TotalTasks = total
		cands[i].DoneTasks = min(done, total)
	}
	return cands, nil
}

func printCandidateMenu(w io.Writer, cands []SiteCandidate) {
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "📋 Multiple build sites found:")
	fmt.Fprintln(w, "")
	for i, c := range cands {
		fmt.Fprintf(w, "  %d. %s — %d/%d tasks done\n", i+1, filepath.Base(c.Path), c.DoneTasks, c.TotalTasks)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "CAVEKIT_SITE_SELECTION_REQUIRED=true")
	paths := make([]string, len(cands))
	for i, c := range cands {
		paths[i] = c.Path
	}
	fmt.Fprintf(w, "CAVEKIT_SITE_CANDIDATES=%s\n", strings.Join(paths, " "))
}

func countTasks(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	ids := map[string]struct{}{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		// A task row is any markdown table line whose first cell matches
		// T-<id>. grep -cE counts lines — we follow the same interpretation.
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cols := strings.Split(trimmed, "|")
		if len(cols) < 2 {
			continue
		}
		first := strings.TrimSpace(cols[1])
		if taskIDRe.MatchString(first) {
			ids[first] = struct{}{}
		}
	}
	return len(ids)
}

// countDoneTasks counts tasks from sitePath that are marked DONE in any
// impl file scoped to the site. Falls back to all impl files when no scoped
// file exists.
func countDoneTasks(projectRoot, sitePath string) (int, error) {
	implDir := filepath.Join(projectRoot, "context", "impl")
	info, err := os.Stat(implDir)
	if err != nil || !info.IsDir() {
		return 0, nil
	}
	entries, err := os.ReadDir(implDir)
	if err != nil {
		return 0, err
	}
	var scoped, unscoped []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "impl-") || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(implDir, e.Name())
		decl := declaredSite(p)
		if decl != "" {
			if sameSite(decl, sitePath) {
				scoped = append(scoped, p)
			}
		} else {
			unscoped = append(unscoped, p)
		}
	}
	search := scoped
	if len(search) == 0 {
		search = unscoped
	}
	if len(search) == 0 {
		return 0, nil
	}

	taskIDs := extractUniqueTaskIDs(sitePath)
	done := 0
	for _, id := range taskIDs {
		for _, impl := range search {
			if taskIsDone(impl, id) {
				done++
				break
			}
		}
	}
	return done, nil
}

var declaredSiteRe = regexp.MustCompile(`(?m)^Build site:\s*(\S+)`)

func declaredSite(implPath string) string {
	data, err := os.ReadFile(implPath)
	if err != nil {
		return ""
	}
	m := declaredSiteRe.FindSubmatch(data)
	if len(m) != 2 {
		return ""
	}
	return string(m[1])
}

func sameSite(a, b string) bool {
	if a == b {
		return true
	}
	return filepath.Base(a) == filepath.Base(b)
}

func extractUniqueTaskIDs(sitePath string) []string {
	data, err := os.ReadFile(sitePath)
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, m := range taskIDRe.FindAllString(string(data), -1) {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

func taskIsDone(implPath, taskID string) bool {
	data, err := os.ReadFile(implPath)
	if err != nil {
		return false
	}
	idBoundary := regexp.MustCompile(`\b` + regexp.QuoteMeta(taskID) + `\b`)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if !strings.Contains(line, "DONE") {
			continue
		}
		if idBoundary.MatchString(line) {
			return true
		}
	}
	return false
}

// MaybeArchivePriorCycle moves prior-cycle impl files to a timestamped
// archive dir iff every task in sitePath is already DONE. Returns the
// count of archived files and the archive directory.
func MaybeArchivePriorCycle(projectRoot, sitePath string, now time.Time, w io.Writer) (int, string, error) {
	implDir := filepath.Join(projectRoot, "context", "impl")
	info, err := os.Stat(implDir)
	if err != nil || !info.IsDir() {
		return 0, "", nil
	}

	hasOld := fileExists(filepath.Join(implDir, "loop-log.md"))
	if !hasOld {
		entries, _ := os.ReadDir(implDir)
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), "impl-") && strings.HasSuffix(e.Name(), ".md") {
				hasOld = true
				break
			}
		}
	}
	if !hasOld {
		return 0, "", nil
	}

	shouldArchive := true
	if sitePath != "" && fileExists(sitePath) {
		scoped, unscoped := partitionImplsBySite(implDir, sitePath)
		check := scoped
		if len(check) == 0 {
			check = unscoped
		}
		for _, id := range extractUniqueTaskIDs(sitePath) {
			found := false
			for _, impl := range check {
				if taskIsDone(impl, id) {
					found = true
					break
				}
			}
			if !found {
				shouldArchive = false
				break
			}
		}
	}
	if !shouldArchive {
		fmt.Fprintln(w, "♻️  Incomplete tasks found — keeping impl tracking from previous cycle")
		return 0, "", nil
	}

	archiveDir := filepath.Join(implDir, "archive", now.UTC().Format("20060102-150405"))
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return 0, "", err
	}
	count := 0
	for _, rel := range []string{
		filepath.Join(implDir, "loop-log.md"),
		filepath.Join(implDir, "peer-review-findings.md"),
		filepath.Join(projectRoot, "context", "peer-review-findings.md"),
	} {
		if !fileExists(rel) {
			continue
		}
		dst := filepath.Join(archiveDir, filepath.Base(rel))
		if err := os.Rename(rel, dst); err == nil {
			count++
		}
	}
	entries, _ := os.ReadDir(implDir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "impl-") || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if e.Name() == "CLAUDE.md" {
			continue
		}
		src := filepath.Join(implDir, e.Name())
		dst := filepath.Join(archiveDir, e.Name())
		if err := os.Rename(src, dst); err == nil {
			count++
		}
	}
	if count > 0 {
		fmt.Fprintf(w, "📦 Archived %d files from previous cycle → %s/\n", count, archiveDir)
	}
	return count, archiveDir, nil
}

func partitionImplsBySite(implDir, sitePath string) (scoped, unscoped []string) {
	entries, err := os.ReadDir(implDir)
	if err != nil {
		return nil, nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "impl-") || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(implDir, e.Name())
		decl := declaredSite(p)
		if decl != "" {
			if sameSite(decl, sitePath) {
				scoped = append(scoped, p)
			}
		} else {
			unscoped = append(unscoped, p)
		}
	}
	return scoped, unscoped
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DiscoverSpecs returns every markdown file under context/kits (recursive)
// except CLAUDE.md, optionally filtered by substring.
func DiscoverSpecs(projectRoot, filter string) []string {
	kitsDir := filepath.Join(projectRoot, "context", "kits")
	info, err := os.Stat(kitsDir)
	if err != nil || !info.IsDir() {
		return nil
	}
	var out []string
	_ = filepath.Walk(kitsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		if filepath.Base(path) == "CLAUDE.md" {
			return nil
		}
		if filter != "" && !strings.Contains(path, filter) {
			return nil
		}
		rel, err := filepath.Rel(projectRoot, path)
		if err != nil {
			rel = path
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out
}

// ── Peer review ───────────────────────────────────────────────────────

func configurePeerReview(projectRoot string, opts Options, cfg *config.Store, w io.Writer) (peerCLI, mcpUpdated bool, err error) {
	if !opts.PeerReview {
		return false, false, nil
	}
	avail := codex.Availability{}
	if opts.AvailabilityOverride != nil {
		avail = *opts.AvailabilityOverride
	} else {
		avail = codex.Detect(context.Background())
	}
	if avail.Available {
		fmt.Fprintf(w, "📡 Codex CLI review enabled (model: %s)\n",
			cfg.GetWithDefault("codex_model", "o4-mini"))
		return true, false, nil
	}

	updated, err := ensureMCPConfig(projectRoot, opts.CodexModel)
	if err != nil {
		return false, false, err
	}
	if updated {
		fmt.Fprintf(w, "📡 Configured Codex (%s) as MCP peer reviewer (legacy fallback)\n", opts.CodexModel)
	}
	return false, updated, nil
}

func ensureMCPConfig(projectRoot, codexModel string) (bool, error) {
	mcpPath := filepath.Join(projectRoot, ".mcp.json")
	existing := map[string]any{}
	if data, err := os.ReadFile(mcpPath); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return false, fmt.Errorf("parse %s: %w", mcpPath, err)
		}
	}

	servers, _ := existing["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	if _, ok := servers["codex-reviewer"]; ok {
		return false, nil
	}
	servers["codex-reviewer"] = map[string]any{
		"command": "codex",
		"args":    []string{"mcp-server", "-c", fmt.Sprintf("model=%q", codexModel)},
	}
	existing["mcpServers"] = servers

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(mcpPath, data, 0o644)
}

// ── Prompt + Ralph state ──────────────────────────────────────────────

// BuildRalphPrompt composes the per-iteration instructions fed to the Ralph
// Loop. Mirrors the template from scripts/setup-build.sh.
func BuildRalphPrompt(frontierFile string, specs []string, opts Options, peerCLI bool) string {
	var specList strings.Builder
	for _, s := range specs {
		fmt.Fprintf(&specList, "\n- `%s`", s)
	}

	peerSection := ""
	if opts.PeerReview {
		if peerCLI {
			peerSection = fmt.Sprintf(`
## Peer Review (every %[1]dth iteration)

Check the iteration number from the Ralph system message.
If iteration %% %[1]d == 0, this is a REVIEW iteration:

1. Run the Codex CLI review:
   `+"```bash\n"+`   cavekit codex review --base main
   `+"```"+`
   This sends the diff to Codex for adversarial review and writes parsed findings
   to `+"`context/impl/impl-review-findings.md`"+` automatically.

2. Read the findings output and fix all P0 (critical) and P1 (high) findings immediately
3. Mark fixed findings as FIXED in the findings file

Completion requires: no P0/P1 findings remain unfixed.`, opts.ReviewInterval)
		} else {
			peerSection = fmt.Sprintf(`
## Peer Review (every %[1]dth iteration) — MCP Legacy

Check the iteration number from the Ralph system message.
If iteration %% %[1]d == 0, this is a REVIEW iteration:

1. Run `+"`git diff main...HEAD`"+` to get all changes
2. Call the `+"`codex-reviewer`"+` MCP server with this prompt:

   > You are a senior engineer performing peer review code review.
   > Your job is to find what the builder MISSED — not to agree.
   > SPEC REQUIREMENTS: [include relevant spec content]
   > CODE CHANGES: [include diff]
   > For each issue: Severity (CRITICAL/HIGH/MEDIUM/LOW), File, Issue, Suggestion.
   > If you find zero issues, explain what you checked and why it's correct.

3. Write findings to `+"`context/impl/peer-review-findings.md`"+`
4. Fix all CRITICAL and HIGH findings immediately
5. Mark fixed findings as FIXED

Completion requires: no CRITICAL/HIGH findings remain unfixed.`, opts.ReviewInterval)
		}
	}

	return fmt.Sprintf(`# Cavekit Build

## Your Role
You are implementing tasks from a build site. Each iteration: find the next
unblocked task, read its cavekit, implement it, validate, commit.

## Read These First (every iteration)
1. `+"`context/impl/loop-log.md`"+` — your iteration history (if exists)
2. `+"`%[1]s`"+` — the task dependency graph
3. Impl tracking files in `+"`context/impl/`"+` — but ONLY files that are scoped to this build site.
   An impl file is scoped if it contains `+"`Build site: %[1]s`"+` (or the matching basename).
   Ignore impl files that declare a different build site. If no scoped files exist, read all impl files.

## Kits (read when implementing a specific requirement)
%[2]s
%[3]s
## Each Iteration

### 1. Orient
- Read loop-log.md and impl tracking to know what's done
- Read the build site to find the lowest tier with incomplete tasks

### 2. Pick Task
- Find the next unblocked task (all blockedBy tasks are DONE)
- Among equals, pick the one that unblocks the most downstream work

### 3. Implement
- Read the task's cavekit requirement and acceptance criteria
- Implement it, following existing codebase patterns
- One task per iteration

### 4. Validate
1. **Build** — must compile/pass
2. **Tests** — on changed files, must pass
3. **Acceptance criteria** — each criterion from the spec must be met

If stuck 2+ attempts → document as dead end, move on.

### 5. Track
Update `+"`context/impl/impl-{domain}.md`"+` (create if missing):

`+"```markdown\n"+`---
created: "{CURRENT_DATE_UTC}"
last_edited: "{CURRENT_DATE_UTC}"
---
# Implementation Tracking: {domain}

Build site: %[1]s

| Task | Status | Notes |
|------|--------|-------|
| T-001 | DONE | what was done |
`+"```\n\n"+`The `+"`Build site:`"+` line is REQUIRED — it scopes this impl file to the correct build site
so task IDs don't collide across different build sites.

Append to `+"`context/impl/loop-log.md`"+` (create if missing):

`+"```markdown\n"+`### Iteration N — {timestamp}
- **Task:** T-{id} — {title}
- **Tier:** {n}
- **Status:** DONE / PARTIAL / BLOCKED
- **Files:** {changed files}
- **Validation:** Build {P/F}, Tests {P/F}, Acceptance {n/n}
- **Next:** T-{id} — {next task}
`+"```\n\n"+`### 6. Commit
Descriptive message with task ID and cavekit requirement. Do NOT push.

### 7. Done?
All tasks across all tiers DONE + build passes + tests pass?
→ output: <promise>%[4]s</promise>

Otherwise → next iteration.

## CRITICAL: Do NOT falsely mark tasks as DONE

**NEVER mark a task DONE because 'existing code already handles this'.**
A task is DONE only when you have:
1. Written or modified code specifically for this task's acceptance criteria
2. Verified EACH acceptance criterion individually (not 'it looks like it works')
3. Written or run tests that prove the criteria are met

If existing code partially covers a requirement, implement the MISSING parts.
If it fully covers every criterion, write a test proving it and document exactly
which existing code satisfies which criterion — with file paths and line numbers.

## Rules
1. NEVER output completion promise unless ALL tasks are genuinely DONE
2. ONE task per iteration
3. Stuck 2+ iterations → dead end, move on
4. Re-read build site and tracking every iteration
5. Commit after each task
6. NEVER skip implementation because code 'looks related'`,
		frontierFile, specList.String(), peerSection, opts.CompletionPromise)
}

// WriteRalphState serializes the prompt plus YAML frontmatter to path.
func WriteRalphState(path, prompt string, opts Options) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	sessionID := os.Getenv("CLAUDE_CODE_SESSION_ID")
	if sessionID == "" {
		sessionID = newSessionID(opts.Now())
	}
	started := opts.Now().UTC().Format("2006-01-02T15:04:05Z")
	body := fmt.Sprintf(`---
active: true
iteration: 1
session_id: %s
max_iterations: %d
completion_promise: %q
started_at: %q
---

%s
`, sessionID, opts.MaxIterations, opts.CompletionPromise, started, prompt)
	return os.WriteFile(path, []byte(body), 0o644)
}

func newSessionID(now time.Time) string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("cavekit-%d", now.Unix())
	}
	return fmt.Sprintf("cavekit-%d-%s", now.Unix(), hex.EncodeToString(buf[:]))
}

func printSummary(w io.Writer, res Result, opts Options, prompt string) {
	fmt.Fprintln(w, "🔄 Cavekit Build — Loop activated!")
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "Build site: %s\n", res.FrontierFile)
	fmt.Fprintf(w, "Specs: %d found\n", len(res.SpecFiles))
	if opts.Filter != "" {
		fmt.Fprintf(w, "Filter: %s\n", opts.Filter)
	}
	if opts.PeerReview {
		fmt.Fprintf(w, "Peer reviewer: Codex (%s) every %d iterations\n", opts.CodexModel, opts.ReviewInterval)
	}
	if res.ArchiveCount > 0 {
		fmt.Fprintf(w, "Archived: %d files from previous cycle\n", res.ArchiveCount)
	}
	fmt.Fprintf(w, "Max iterations: %d\n", opts.MaxIterations)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Each iteration: build site → cavekit → implement → validate → commit")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "═══════════════════════════════════════════════════════════════════════")
	fmt.Fprintf(w, "COMPLETION: <promise>%s</promise>\n", opts.CompletionPromise)
	fmt.Fprintln(w, "Only when ALL tasks are done.")
	fmt.Fprintln(w, "═══════════════════════════════════════════════════════════════════════")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, prompt)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
