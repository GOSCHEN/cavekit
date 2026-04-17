package launch

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDeriveName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"context/plans/build-site-coherence.md", "coherence"},
		{"context/plans/plan-auth.md", "auth"},
		{"context/plans/feature-frontier-ingest.md", "ingest"},
		{"random.md", "random"},
	}
	for _, tc := range cases {
		if got := DeriveName(tc.in); got != tc.want {
			t.Errorf("DeriveName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestShouldResumeRalphState(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "ralph-loop.local.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !shouldResume(root) {
		t.Error("expected resume via ralph state")
	}
}

func TestShouldResumeImplFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "context", "impl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "context", "impl", "impl-a.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !shouldResume(root) {
		t.Error("expected resume via impl files")
	}
}

// stubMuxer records calls for assertions.
type stubMuxer struct {
	mu       sync.Mutex
	created  []string
	keys     []struct{ Name, Text string }
	sessions []string
}

func (s *stubMuxer) CreateSession(ctx context.Context, name, workDir, program string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.created = append(s.created, name)
	return nil
}
func (s *stubMuxer) Exists(ctx context.Context, name string) bool { return false }
func (s *stubMuxer) Kill(ctx context.Context, name string) error  { return nil }
func (s *stubMuxer) ListSessions(ctx context.Context) ([]string, error) {
	return s.sessions, nil
}
func (s *stubMuxer) SendKeys(ctx context.Context, name string, keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		s.keys = append(s.keys, struct{ Name, Text string }{name, k})
	}
	return nil
}
func (s *stubMuxer) SendEnter(ctx context.Context, name string) error {
	return s.SendKeys(ctx, name, "Enter")
}
func (s *stubMuxer) SendText(ctx context.Context, name, text string) error {
	return s.SendKeys(ctx, name, text)
}
func (s *stubMuxer) SendCommand(ctx context.Context, name, cmd string) error {
	return s.SendKeys(ctx, name, cmd, "Enter")
}
func (s *stubMuxer) CapturePane(ctx context.Context, name string) (string, error) {
	return "", nil
}
func (s *stubMuxer) CaptureScrollback(ctx context.Context, name string) (string, error) {
	return "", nil
}

func TestRunCreatesSessionsAndDispatches(t *testing.T) {
	// stubMuxer implements mux.Multiplexer via duck-typing (interface).
	st := &stubMuxer{}
	root := t.TempDir()

	var buf bytes.Buffer
	res, err := Run(context.Background(), Options{
		Frontiers:    []string{"context/plans/build-site-a.md", "context/plans/build-site-b.md"},
		Muxer:        st,
		ProjectRoot:  root,
		Program:      chooseLocalProgram(t),
		StaggerDelay: 10 * time.Millisecond,
	}, &buf)
	if err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	if len(res.Sessions) != 2 {
		t.Errorf("sessions = %d want 2", len(res.Sessions))
	}
	// Wait for the goroutine to finish dispatching /ck:make.
	time.Sleep(4 * time.Second)

	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.created) != 2 {
		t.Errorf("created = %d want 2", len(st.created))
	}
	foundMake := 0
	for _, k := range st.keys {
		if k.Text == "/ck:make --filter a" || k.Text == "/ck:make --filter b" {
			foundMake++
		}
	}
	if foundMake != 2 {
		t.Errorf("expected 2 /ck:make sends, got %d: %+v", foundMake, st.keys)
	}
}

// chooseLocalProgram picks a binary guaranteed to be on PATH so the
// LookPath preflight does not fail the test. `go` is available because
// Go itself is running the test.
func chooseLocalProgram(t *testing.T) string {
	t.Helper()
	return "go"
}
