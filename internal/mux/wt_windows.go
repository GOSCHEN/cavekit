//go:build windows

package mux

import (
	"context"
	"fmt"
	"net"
	"os"
	osexec "os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/Microsoft/go-winio"

	"github.com/JuliusBrussee/cavekit/internal/exec"
)

// wtMultiplexer is the Windows implementation of the Multiplexer interface.
// Sessions are owned by one cavekit-mux-daemon process per session; the
// daemon exposes a named pipe this struct talks to over short-lived
// connections (one frame-per-connection for control, long-lived for
// opAttach streaming).
//
// Windows Terminal (wt.exe) is only the display surface: each session's
// daemon is launched first, then a wt.exe tab is opened running
// `cavekit mux-attach --name <n>` so users see the ConPTY output.
type wtMultiplexer struct {
	exec exec.Executor

	// selfPath is the path to the currently running cavekit.exe; the daemon
	// and attach subcommands are spawned from this same binary. Resolved
	// once at construction and cached so repeated CreateSession calls do
	// not re-probe.
	selfPath string
}

func newWTMultiplexer(executor exec.Executor) *wtMultiplexer {
	self, _ := os.Executable()
	return &wtMultiplexer{exec: executor, selfPath: self}
}

// dialSession opens a fresh pipe connection to the session's daemon.
// Each control RPC uses its own connection so a stuck request never
// blocks others on the same pipe.
func (w *wtMultiplexer) dialSession(ctx context.Context, name string) (net.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := winio.DialPipeContext(dialCtx, pipeName(name))
	if err != nil {
		return nil, fmt.Errorf("dial pipe %s: %w", pipeName(name), err)
	}
	return conn, nil
}

// rpc sends one frame and reads one reply. Used by SendKeys, Capture, Kill,
// Ping — everything except opAttach.
func (w *wtMultiplexer) rpc(ctx context.Context, name string, op byte, payload []byte) (byte, []byte, error) {
	conn, err := w.dialSession(ctx, name)
	if err != nil {
		return 0, nil, err
	}
	defer conn.Close()
	if err := writeFrame(conn, op, payload); err != nil {
		return 0, nil, fmt.Errorf("write frame: %w", err)
	}
	respOp, respPayload, err := readFrame(conn)
	if err != nil {
		return 0, nil, fmt.Errorf("read frame: %w", err)
	}
	if respOp == opErr {
		return respOp, respPayload, fmt.Errorf("daemon error: %s", string(respPayload))
	}
	return respOp, respPayload, nil
}

// CreateSession spawns a detached cavekit-mux-daemon process and then a
// wt.exe tab attached to it. Returns once the daemon's pipe is answering
// pings, so callers know the session is live before SendCommand or SendKeys
// are invoked.
func (w *wtMultiplexer) CreateSession(ctx context.Context, name, workDir, program string) error {
	if w.selfPath == "" {
		return fmt.Errorf("cannot locate cavekit executable to spawn daemon")
	}
	if _, err := osexec.LookPath("wt.exe"); err != nil {
		return fmt.Errorf("Windows Terminal (wt.exe) not found in PATH")
	}

	// Launch the daemon fully detached so it survives after this process exits.
	daemon := osexec.Command(w.selfPath, "mux-daemon",
		"--name", name,
		"--dir", workDir,
		"--prog", program,
	)
	daemon.SysProcAttr = &syscall.SysProcAttr{
		// CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS — no console, no
		// parent bind, so the daemon outlives the launcher.
		CreationFlags: 0x00000200 | 0x00000008,
	}
	if err := daemon.Start(); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}
	// Intentionally not Wait()-ing — the daemon runs until killed.

	// Wait for the pipe to accept connections. Without this, the wt.exe
	// tab we spawn next races the daemon and shows a scary "pipe not
	// found" message.
	if err := w.waitForPipe(ctx, name, 5*time.Second); err != nil {
		return fmt.Errorf("daemon did not come up: %w", err)
	}

	// Launch a wt.exe tab attached to the daemon.
	//   wt.exe new-tab --title <n> --suppressApplicationTitle cavekit.exe mux-attach --name <n>
	tab := osexec.Command("wt.exe",
		"new-tab",
		"--title", strings.TrimPrefix(name, SessionPrefix),
		"--suppressApplicationTitle",
		w.selfPath, "mux-attach", "--name", name,
	)
	if err := tab.Start(); err != nil {
		// Non-fatal: daemon is still running; user can attach via
		// `cavekit mux-attach --name <n>` manually.
		fmt.Fprintf(os.Stderr, "warning: could not open wt.exe tab: %v\n", err)
	}

	return nil
}

// waitForPipe polls until Ping succeeds or the timeout elapses.
func (w *wtMultiplexer) waitForPipe(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for %s", pipeName(name))
		}
		conn, err := winio.DialPipe(pipeName(name), ptrDuration(200*time.Millisecond))
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func ptrDuration(d time.Duration) *time.Duration { return &d }

// Exists pings the session's pipe. Stale metadata files (daemon crashed
// without cleanup) are ignored automatically because Ping fails.
func (w *wtMultiplexer) Exists(ctx context.Context, name string) bool {
	_, _, err := w.rpc(ctx, name, opPing, nil)
	return err == nil
}

// Kill asks the daemon to terminate its child and exit. Best-effort: if the
// daemon has already crashed, we fall back to taskkill on its PID stored in
// the metadata file, then remove the stale metadata.
func (w *wtMultiplexer) Kill(ctx context.Context, name string) error {
	_, _, err := w.rpc(ctx, name, opKill, nil)
	if err == nil {
		return nil
	}
	// Daemon unreachable — check metadata for a stale PID we can taskkill.
	meta, metaErr := readSessionMeta(name)
	if metaErr == nil && meta.PID > 0 {
		_ = osexec.Command("taskkill.exe", "/T", "/F", "/PID", fmt.Sprintf("%d", meta.PID)).Run()
		removeSessionMeta(name)
	}
	return nil
}

// ListSessions returns every cavekit session whose metadata file is on disk
// AND whose pipe responds to Ping. Stale entries are pruned silently.
func (w *wtMultiplexer) ListSessions(ctx context.Context) ([]string, error) {
	metas, err := listSessionMetas()
	if err != nil {
		return nil, err
	}
	var live []string
	for _, m := range metas {
		if w.Exists(ctx, m.Name) {
			live = append(live, m.Name)
			continue
		}
		// Prune stale metadata so repeat ListSessions converges.
		removeSessionMeta(m.Name)
	}
	return live, nil
}

// SendKeys translates tmux-style tokens and forwards the bytes.
func (w *wtMultiplexer) SendKeys(ctx context.Context, name string, keys ...string) error {
	payload := translateKeys(keys)
	_, _, err := w.rpc(ctx, name, opSendBytes, payload)
	return err
}

// SendEnter is shorthand for SendKeys(name, "Enter").
func (w *wtMultiplexer) SendEnter(ctx context.Context, name string) error {
	return w.SendKeys(ctx, name, "Enter")
}

// SendText writes each line followed by Enter.
func (w *wtMultiplexer) SendText(ctx context.Context, name, text string) error {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if err := w.SendKeys(ctx, name, line, "Enter"); err != nil {
			return err
		}
	}
	return nil
}

// SendCommand types cmd then presses Enter.
func (w *wtMultiplexer) SendCommand(ctx context.Context, name, cmd string) error {
	return w.SendKeys(ctx, name, cmd, "Enter")
}

// Resize tells the daemon to reshape its ConPTY. Invoked by the attach
// client in response to WINDOW_BUFFER_SIZE events (or, on startup, to set
// the initial viewport from the current console dimensions).
func (w *wtMultiplexer) Resize(ctx context.Context, name string, cols, rows int) error {
	return SendResize(ctx, name, cols, rows)
}

// SendResize is a package-level helper so attach clients (which already have
// the session name in hand and don't need a full wtMultiplexer) can push
// dimension updates without building the whole adapter.
func SendResize(ctx context.Context, name string, cols, rows int) error {
	payload := make([]byte, 8)
	payload[0] = byte(cols >> 24)
	payload[1] = byte(cols >> 16)
	payload[2] = byte(cols >> 8)
	payload[3] = byte(cols)
	payload[4] = byte(rows >> 24)
	payload[5] = byte(rows >> 16)
	payload[6] = byte(rows >> 8)
	payload[7] = byte(rows)

	dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := winio.DialPipeContext(dialCtx, pipeName(name))
	if err != nil {
		return fmt.Errorf("dial pipe: %w", err)
	}
	defer conn.Close()
	if err := writeFrame(conn, opResize, payload); err != nil {
		return fmt.Errorf("write resize: %w", err)
	}
	op, body, err := readFrame(conn)
	if err != nil {
		return fmt.Errorf("read reply: %w", err)
	}
	if op == opErr {
		return fmt.Errorf("daemon error: %s", string(body))
	}
	return nil
}

// CapturePane returns the tail of the daemon's ring buffer (~ visible viewport).
func (w *wtMultiplexer) CapturePane(ctx context.Context, name string) (string, error) {
	_, payload, err := w.rpc(ctx, name, opCapturePane, nil)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

// CaptureScrollback returns the full ring buffer.
func (w *wtMultiplexer) CaptureScrollback(ctx context.Context, name string) (string, error) {
	_, payload, err := w.rpc(ctx, name, opCaptureFull, nil)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

