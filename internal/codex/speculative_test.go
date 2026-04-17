package codex

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JuliusBrussee/cavekit/internal/config"
)

func newSpecStore(t *testing.T, now time.Time) *SpeculativeStore {
	t.Helper()
	return &SpeculativeStore{
		ProjectRoot: t.TempDir(),
		Now:         func() time.Time { return now },
	}
}

func TestRecordAndGetJob(t *testing.T) {
	s := newSpecStore(t, time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC))
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	j := Job{Tier: 2, PID: 1234, Output: "x", Done: "y", BaseRef: "main", Status: "running", StartUnix: 100}
	if err := s.RecordJob(j); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetJob(2)
	if err != nil || !ok {
		t.Fatalf("GetJob: err=%v ok=%v", err, ok)
	}
	if got != j {
		t.Errorf("got %+v want %+v", got, j)
	}
}

func TestSpeculativeEnabled(t *testing.T) {
	cfg := newTestConfig(t)
	cases := []struct {
		name   string
		set    map[string]string
		avail  Availability
		expect bool
	}{
		{"default with codex", nil, Availability{Available: true}, true},
		{"default no codex", nil, Availability{Available: false}, false},
		{"explicit on even if unavailable", map[string]string{"speculative_review": "on"}, Availability{Available: false}, true},
		{"explicit off even if available", map[string]string{"speculative_review": "off"}, Availability{Available: true}, false},
		{"gate off disables default", map[string]string{"tier_gate_mode": "off"}, Availability{Available: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fresh := newTestConfig(t)
			for k, v := range tc.set {
				if err := fresh.Set(k, v, config.ScopeProject); err != nil {
					t.Fatal(err)
				}
			}
			if got := SpeculativeEnabled(fresh, tc.avail); got != tc.expect {
				t.Errorf("got %v want %v", got, tc.expect)
			}
		})
	}
	_ = cfg
}

func TestStatusReclassifiesFromMarker(t *testing.T) {
	s := newSpecStore(t, time.Date(2026, 4, 17, 12, 0, 30, 0, time.UTC))
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	j := Job{Tier: 1, PID: 99, Output: s.OutputPath(1), Done: s.DonePath(1), Status: "running", StartUnix: s.Now().Unix() - 10}
	if err := s.RecordJob(j); err != nil {
		t.Fatal(err)
	}
	// No marker yet → running
	var buf bytes.Buffer
	if err := Status(s, &buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("RUNNING")) {
		t.Errorf("expected RUNNING; got:\n%s", buf.String())
	}

	// Write output + marker → complete
	if err := os.WriteFile(j.Output, []byte("findings data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(j.Done, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := Status(s, &buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("COMPLETE")) {
		t.Errorf("expected COMPLETE; got:\n%s", buf.String())
	}

	got, _, _ := s.GetJob(1)
	if got.Status != "complete" {
		t.Errorf("status = %q want complete", got.Status)
	}
}

func TestRetrieveAlreadyComplete(t *testing.T) {
	s := newSpecStore(t, time.Now())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	j := Job{Tier: 2, PID: 1, Output: s.OutputPath(2), Done: s.DonePath(2), Status: "running", StartUnix: time.Now().Unix()}
	_ = os.WriteFile(j.Output, []byte("NO_FINDINGS\n"), 0o644)
	_ = os.WriteFile(j.Done, []byte("1"), 0o644)
	_ = s.RecordJob(j)

	var buf bytes.Buffer
	outcome, err := RetrieveWithPoll(s, 2, time.Second, 10*time.Millisecond, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != RetrieveConsumed {
		t.Errorf("outcome = %v want Consumed", outcome)
	}
	if !bytes.Contains(buf.Bytes(), []byte("clean")) {
		t.Errorf("missing clean log:\n%s", buf.String())
	}
	got, _, _ := s.GetJob(2)
	if got.Status != "consumed" {
		t.Errorf("status = %q want consumed", got.Status)
	}
}

func TestRetrieveTimeout(t *testing.T) {
	s := newSpecStore(t, time.Now())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	j := Job{Tier: 2, PID: 1, Output: s.OutputPath(2), Done: s.DonePath(2), Status: "running", StartUnix: time.Now().Unix()}
	_ = s.RecordJob(j)

	var buf bytes.Buffer
	outcome, err := RetrieveWithPoll(s, 2, 100*time.Millisecond, 20*time.Millisecond, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != RetrieveTimeout {
		t.Errorf("outcome = %v want Timeout", outcome)
	}
	got, _, _ := s.GetJob(2)
	if got.Status != "timeout" {
		t.Errorf("status = %q want timeout", got.Status)
	}
}

func TestRetrieveNoJob(t *testing.T) {
	s := newSpecStore(t, time.Now())
	var buf bytes.Buffer
	outcome, err := RetrieveWithPoll(s, 1, time.Second, 10*time.Millisecond, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != RetrieveFallback {
		t.Errorf("outcome = %v want Fallback", outcome)
	}
}

func TestSpeculativeDirPath(t *testing.T) {
	s := &SpeculativeStore{ProjectRoot: "/tmp/x"}
	want := filepath.Join("/tmp/x", ".cavekit", ".speculative")
	if got := s.Dir(); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestSpeculativeTimeoutDefault(t *testing.T) {
	cfg := newTestConfig(t)
	if d := SpeculativeTimeout(cfg); d != 300*time.Second {
		t.Errorf("default = %v want 5m", d)
	}
	_ = cfg.Set("speculative_review_timeout", "60", config.ScopeProject)
	if d := SpeculativeTimeout(cfg); d != 60*time.Second {
		t.Errorf("override = %v want 1m", d)
	}
}
