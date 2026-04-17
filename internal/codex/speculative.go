package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/JuliusBrussee/cavekit/internal/config"
)

// SpeculativeStore persists background-review job metadata under the project
// root, surviving across CLI invocations within the same working tree.
//
// Layout (all under ProjectRoot/.cavekit/.speculative/):
//
//	jobs.json                — map tier → Job
//	review-tier-<N>.out      — captured stdout/stderr of each review
//	review-tier-<N>.done     — marker file written when review exits
//
// Now is injected for testability.
type SpeculativeStore struct {
	ProjectRoot string
	Now         func() time.Time
}

// NewSpeculativeStore constructs a store rooted at projectRoot. Use
// DetectProjectRoot() when the caller does not already have one.
func NewSpeculativeStore(projectRoot string) *SpeculativeStore {
	return &SpeculativeStore{ProjectRoot: projectRoot, Now: time.Now}
}

// Dir returns .cavekit/.speculative under the project root.
func (s *SpeculativeStore) Dir() string {
	return filepath.Join(s.ProjectRoot, ".cavekit", ".speculative")
}

// JobsPath returns the path to jobs.json.
func (s *SpeculativeStore) JobsPath() string { return filepath.Join(s.Dir(), "jobs.json") }

// OutputPath returns the captured-output path for tier.
func (s *SpeculativeStore) OutputPath(tier int) string {
	return filepath.Join(s.Dir(), fmt.Sprintf("review-tier-%d.out", tier))
}

// DonePath returns the completion-marker path for tier.
func (s *SpeculativeStore) DonePath(tier int) string {
	return filepath.Join(s.Dir(), fmt.Sprintf("review-tier-%d.done", tier))
}

// Job is one tracked background review.
type Job struct {
	Tier      int    `json:"tier"`
	PID       int    `json:"pid"`
	Output    string `json:"output"`
	Done      string `json:"done"`
	BaseRef   string `json:"base_ref"`
	Status    string `json:"status"` // running|complete|failed|consumed|timeout
	StartUnix int64  `json:"start_unix"`
}

type jobMap map[int]Job

// load reads jobs.json, returning an empty map on first use.
func (s *SpeculativeStore) load() (jobMap, error) {
	data, err := os.ReadFile(s.JobsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return jobMap{}, nil
		}
		return nil, err
	}
	var jobs jobMap
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, err
	}
	if jobs == nil {
		jobs = jobMap{}
	}
	return jobs, nil
}

func (s *SpeculativeStore) save(jobs jobMap) error {
	if err := os.MkdirAll(s.Dir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.JobsPath(), data, 0o644)
}

// Init creates the state directory. No-op when it already exists.
func (s *SpeculativeStore) Init() error {
	return os.MkdirAll(s.Dir(), 0o755)
}

// RecordJob upserts a job keyed by tier.
func (s *SpeculativeStore) RecordJob(j Job) error {
	jobs, err := s.load()
	if err != nil {
		return err
	}
	jobs[j.Tier] = j
	return s.save(jobs)
}

// GetJob returns the job for tier, if any.
func (s *SpeculativeStore) GetJob(tier int) (Job, bool, error) {
	jobs, err := s.load()
	if err != nil {
		return Job{}, false, err
	}
	j, ok := jobs[tier]
	return j, ok, nil
}

// UpdateStatus mutates the status field for tier. No-op when the tier is
// untracked.
func (s *SpeculativeStore) UpdateStatus(tier int, status string) error {
	jobs, err := s.load()
	if err != nil {
		return err
	}
	j, ok := jobs[tier]
	if !ok {
		return nil
	}
	j.Status = status
	jobs[tier] = j
	return s.save(jobs)
}

// ListJobs returns all tracked jobs. Ordering is not guaranteed.
func (s *SpeculativeStore) ListJobs() ([]Job, error) {
	jobs, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j)
	}
	return out, nil
}

// Cleanup removes the state directory.
func (s *SpeculativeStore) Cleanup() error {
	return os.RemoveAll(s.Dir())
}

// ── Config ────────────────────────────────────────────────────────────

// SpeculativeEnabled reports whether speculative review should run.
// Matches the bash default: on when Codex is available and tier_gate_mode is
// not off, unless explicitly overridden via the speculative_review config.
func SpeculativeEnabled(cfg *config.Store, avail Availability) bool {
	val := cfg.GetWithDefault("speculative_review", "")
	if val == "on" {
		return true
	}
	if val == "off" {
		return false
	}
	gateMode := cfg.GetWithDefault("tier_gate_mode", "severity")
	return avail.Available && gateMode != "off"
}

// SpeculativeTimeout returns the retrieve timeout, defaulting to 300 seconds.
func SpeculativeTimeout(cfg *config.Store) time.Duration {
	raw := cfg.GetWithDefault("speculative_review_timeout", "300")
	secs := 300
	fmt.Sscanf(raw, "%d", &secs)
	return time.Duration(secs) * time.Second
}

// ── Dispatch / Status / Retrieve ──────────────────────────────────────

// DispatchOptions tunes Dispatch.
type DispatchOptions struct {
	Tier    int
	BaseRef string
	// Self is the path to the cavekit binary. Defaults to os.Args[0].
	Self string
	// AvailabilityOverride allows tests and pre-computed states to skip
	// detection.
	AvailabilityOverride *Availability
}

// Dispatch launches a background Codex review of tier-1's work, writing
// output to OutputPath(tier) and a .done marker on exit. It records the
// spawned PID via RecordJob and returns immediately. A nil error indicates
// the job was tracked (or skipped with a reason); dispatch failures surface
// as errors from the Start call.
func Dispatch(ctx context.Context, store *SpeculativeStore, cfg *config.Store, opts DispatchOptions, w io.Writer) error {
	var avail Availability
	if opts.AvailabilityOverride != nil {
		avail = *opts.AvailabilityOverride
	} else {
		avail = Detect(ctx)
	}

	if !SpeculativeEnabled(cfg, avail) {
		fmt.Fprintln(w, "[ck:speculative] Speculative review disabled. Skipping.")
		return nil
	}
	if !avail.Available {
		fmt.Fprintln(w, "[ck:speculative] Codex unavailable. Skipping speculative dispatch.")
		return nil
	}
	if opts.Tier == 0 {
		fmt.Fprintln(w, "[ck:speculative] Tier 0 — no previous tier to review speculatively.")
		return nil
	}

	if err := store.Init(); err != nil {
		return err
	}

	outPath := store.OutputPath(opts.Tier)
	donePath := store.DonePath(opts.Tier)
	// Clear stale markers so a re-dispatch of the same tier starts fresh.
	_ = os.Remove(donePath)

	self := opts.Self
	if self == "" {
		self = os.Args[0]
	}

	outFile, err := os.Create(outPath)
	if err != nil {
		return err
	}

	cmd := exec.Command(self, "codex", "speculative", "_run",
		"--tier", fmt.Sprintf("%d", opts.Tier),
		"--base", opts.BaseRef,
		"--done", donePath,
	)
	cmd.Stdout = outFile
	cmd.Stderr = outFile
	applyDetachSysProcAttr(cmd)

	fmt.Fprintf(w, "[ck:speculative] Dispatching background review of tier %d (diff from %s)...\n", opts.Tier-1, opts.BaseRef)

	if err := cmd.Start(); err != nil {
		_ = outFile.Close()
		return fmt.Errorf("dispatch: %w", err)
	}
	// Parent does not Wait — orphan by design.
	_ = outFile.Close()

	job := Job{
		Tier:      opts.Tier,
		PID:       cmd.Process.Pid,
		Output:    outPath,
		Done:      donePath,
		BaseRef:   opts.BaseRef,
		Status:    "running",
		StartUnix: store.Now().Unix(),
	}
	if err := store.RecordJob(job); err != nil {
		return err
	}
	fmt.Fprintf(w, "[ck:speculative] Background job PID=%d dispatched for tier %d review.\n", job.PID, opts.Tier-1)
	return nil
}

// RunInBackground is the subcommand body executed by the spawned child. It
// runs a full Review using the given config+findings stores, then writes the
// .done marker. Errors are swallowed intentionally — the output file captures
// the diagnostic context that callers inspect after the fact.
func RunInBackground(ctx context.Context, tier int, baseRef, donePath string, cfg *config.Store, findings *FindingsStore, w io.Writer) {
	defer func() {
		_ = os.WriteFile(donePath, []byte("1\n"), 0o644)
	}()
	_, _ = Review(ctx, ReviewOptions{BaseRef: baseRef}, cfg, findings, w)
}

// Status prints a human-friendly summary of tracked jobs, refreshing their
// status in the store based on marker existence.
func Status(store *SpeculativeStore, w io.Writer) error {
	jobs, err := store.ListJobs()
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		fmt.Fprintln(w, "[ck:speculative] No speculative reviews tracked.")
		return nil
	}

	fmt.Fprintln(w, "[ck:speculative] Pipeline status:")
	for _, j := range jobs {
		status, elapsed := classifyJob(store, j)
		if status != j.Status {
			_ = store.UpdateStatus(j.Tier, status)
		}
		fmt.Fprintf(w, "  Tier %d review → %s (%ds)\n", j.Tier-1, strings.ToUpper(status), elapsed)
	}
	return nil
}

// classifyJob reconciles a stored status against the on-disk marker file.
func classifyJob(store *SpeculativeStore, j Job) (status string, elapsedSecs int) {
	elapsed := int(store.Now().Unix() - j.StartUnix)
	if j.Status == "consumed" || j.Status == "failed" || j.Status == "timeout" {
		return j.Status, elapsed
	}
	if _, err := os.Stat(j.Done); err == nil {
		if info, err := os.Stat(j.Output); err == nil && info.Size() > 0 {
			return "complete", elapsed
		}
		return "failed", elapsed
	}
	return "running", elapsed
}

// RetrieveOutcome enumerates the result of Retrieve.
type RetrieveOutcome int

const (
	RetrieveConsumed RetrieveOutcome = iota // fetched speculative results
	RetrieveFallback                        // caller must run synchronous review
	RetrieveTimeout                         // timed out waiting; fall back
)

// Retrieve blocks up to timeout waiting for the tier's review to finish.
// Polls at pollInterval granularity; the default is 2s to match bash.
func Retrieve(store *SpeculativeStore, tier int, timeout time.Duration, w io.Writer) (RetrieveOutcome, error) {
	return RetrieveWithPoll(store, tier, timeout, 2*time.Second, w)
}

// RetrieveWithPoll is Retrieve with a custom poll interval; tests use a
// fraction of a second to keep them fast.
func RetrieveWithPoll(store *SpeculativeStore, tier int, timeout, poll time.Duration, w io.Writer) (RetrieveOutcome, error) {
	j, ok, err := store.GetJob(tier)
	if err != nil {
		return RetrieveFallback, err
	}
	if !ok {
		fmt.Fprintf(w, "[ck:speculative] No speculative job for tier %d. Falling back to synchronous.\n", tier)
		return RetrieveFallback, nil
	}
	switch j.Status {
	case "consumed":
		fmt.Fprintf(w, "[ck:speculative] Tier %d review already consumed.\n", tier-1)
		return RetrieveConsumed, nil
	case "failed":
		fmt.Fprintf(w, "[ck:speculative] Tier %d review failed. Falling back to synchronous.\n", tier-1)
		return RetrieveFallback, nil
	}

	if _, err := os.Stat(j.Done); err == nil {
		return consume(store, j, w)
	}

	fmt.Fprintf(w, "[ck:speculative] Tier %d review still running. Waiting up to %ds...\n", tier-1, int(timeout.Seconds()))
	// Use the real clock here; store.Now is reserved for timestamping state.
	// A frozen test clock would otherwise spin forever.
	start := time.Now()
	deadline := start.Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(j.Done); err == nil {
			waited := int(time.Since(start).Seconds())
			fmt.Fprintf(w, "[ck:speculative] Review completed after %ds wait.\n", waited)
			return consume(store, j, w)
		}
		time.Sleep(poll)
	}

	fmt.Fprintf(w, "[ck:speculative] Timed out after %ds. Falling back to synchronous review.\n", int(timeout.Seconds()))
	_ = store.UpdateStatus(tier, "timeout")
	return RetrieveTimeout, nil
}

func consume(store *SpeculativeStore, j Job, w io.Writer) (RetrieveOutcome, error) {
	data, err := os.ReadFile(j.Output)
	if err != nil {
		return RetrieveFallback, err
	}
	content := string(data)
	clean := strings.Contains(content, "NO_FINDINGS") ||
		strings.Contains(content, "no issues") ||
		strings.Contains(content, "Clean review")
	if clean {
		fmt.Fprintf(w, "[ck:speculative] Tier %d speculative review: clean.\n", j.Tier-1)
	} else {
		fmt.Fprintf(w, "[ck:speculative] Tier %d speculative review found issues.\n", j.Tier-1)
		fmt.Fprintln(w, content)
	}
	_ = store.UpdateStatus(j.Tier, "consumed")
	return RetrieveConsumed, nil
}

// DetectProjectRoot walks up from cwd looking for a .git directory. Falls
// back to cwd when none is found. Used by the CLI to locate the state dir
// when the caller does not supply one.
func DetectProjectRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := cwd
	for i := 0; i < 32; i++ {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return cwd
}
