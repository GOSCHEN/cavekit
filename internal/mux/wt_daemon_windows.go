//go:build windows

package mux

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	osexec "os/exec"
	"sync"

	"github.com/Microsoft/go-winio"
	"github.com/UserExistsError/conpty"
)

// Daemon hosts a single cavekit session on Windows. It owns the ConPTY, the
// child process running inside it, the ring buffer mirroring pane output,
// and the named pipe that parent and wt.exe clients connect to.
//
// Lifecycle:
//  1. RunDaemon (invoked from the `cavekit mux-daemon` subcommand) writes
//     session metadata, opens the pipe listener, starts the child inside a
//     ConPTY, and blocks serving pipe connections.
//  2. Each accepted connection reads framed requests and writes framed
//     replies. An opAttach connection enters a full-duplex stream until one
//     side hangs up.
//  3. Exit happens when the child exits or an opKill arrives. Either way
//     the metadata file is removed before returning.
type Daemon struct {
	name    string // sanitized name (with SessionPrefix)
	program string
	workDir string

	pty *conpty.ConPty

	ringBuf *ringBuffer
	ln      net.Listener

	// subscribers receive a copy of every byte the child emits. Used by
	// active Attach streams so multiple wt.exe tabs could theoretically
	// mirror the same session.
	subMu       sync.Mutex
	subscribers map[chan []byte]struct{}

	// killOnce guards the shutdown path so both "child exited" and "kill
	// received over pipe" converge safely.
	killOnce sync.Once
	done     chan struct{}
}

// RunDaemon spawns the child, opens the pipe, and blocks until the child
// exits or a kill request arrives. Intended to be called from the
// `cavekit mux-daemon` subcommand — not from production library code.
func RunDaemon(ctx context.Context, rawName, workDir, program string) error {
	name := SanitizeName(rawName)

	if !conpty.IsConPtyAvailable() {
		return fmt.Errorf("ConPTY unavailable on this Windows build — minimum Windows 10 1809")
	}

	var opts []conpty.ConPtyOption
	if workDir != "" {
		opts = append(opts, conpty.ConPtyWorkDir(workDir))
	}
	// Default dimensions — wt.exe will resize via the attach protocol once
	// a client connects. 120x30 is a reasonable starting viewport.
	opts = append(opts, conpty.ConPtyDimensions(120, 30))

	pty, err := conpty.Start(program, opts...)
	if err != nil {
		return fmt.Errorf("conpty.Start: %w", err)
	}

	// Open the named pipe listener.
	pipePath := pipeName(rawName)
	pipeCfg := &winio.PipeConfig{
		InputBufferSize:  65536,
		OutputBufferSize: 65536,
	}
	ln, err := winio.ListenPipe(pipePath, pipeCfg)
	if err != nil {
		_ = pty.Close()
		return fmt.Errorf("winio.ListenPipe: %w", err)
	}

	d := &Daemon{
		name:        name,
		program:     program,
		workDir:     workDir,
		pty:         pty,
		ringBuf:     newRingBuffer(256 * 1024), // ~256 KB scrollback
		ln:          ln,
		subscribers: make(map[chan []byte]struct{}),
		done:        make(chan struct{}),
	}

	// Persist metadata so parents can discover this session.
	meta := sessionMeta{
		Name:     name,
		PID:      os.Getpid(),
		PipeName: pipePath,
		Program:  program,
		WorkDir:  workDir,
	}
	if err := writeSessionMeta(meta); err != nil {
		fmt.Fprintf(os.Stderr, "cavekit mux-daemon: write session meta: %v\n", err)
	}
	defer removeSessionMeta(name)

	// Tee ConPTY output into the ring buffer and all subscribers.
	go d.pumpOutput()

	// Reap the child in a goroutine so we exit once it does.
	go func() {
		_, _ = pty.Wait(ctx)
		d.shutdown()
	}()

	// Accept pipe connections until shutdown.
	go d.acceptLoop()

	<-d.done
	_ = ln.Close()
	_ = pty.Close()
	return nil
}

// pumpOutput reads the ConPTY, copies to the ring buffer and every live
// subscriber. Exits when the PTY closes.
func (d *Daemon) pumpOutput() {
	buf := make([]byte, 4096)
	for {
		n, err := d.pty.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			_, _ = d.ringBuf.Write(chunk)
			d.broadcast(chunk)
		}
		if err != nil {
			d.shutdown()
			return
		}
	}
}

func (d *Daemon) broadcast(chunk []byte) {
	d.subMu.Lock()
	defer d.subMu.Unlock()
	for ch := range d.subscribers {
		// Non-blocking send so a slow client never stalls the PTY pump.
		select {
		case ch <- chunk:
		default:
		}
	}
}

func (d *Daemon) subscribe() chan []byte {
	ch := make(chan []byte, 64)
	d.subMu.Lock()
	d.subscribers[ch] = struct{}{}
	d.subMu.Unlock()
	return ch
}

func (d *Daemon) unsubscribe(ch chan []byte) {
	d.subMu.Lock()
	delete(d.subscribers, ch)
	d.subMu.Unlock()
	close(ch)
}

func (d *Daemon) acceptLoop() {
	for {
		conn, err := d.ln.Accept()
		if err != nil {
			return
		}
		go d.handleConn(conn)
	}
}

func (d *Daemon) handleConn(conn io.ReadWriteCloser) {
	defer conn.Close()
	op, payload, err := readFrame(conn)
	if err != nil {
		return
	}
	switch op {
	case opPing:
		_ = writeFrame(conn, opOK, nil)
	case opSendBytes:
		if _, err := d.pty.Write(payload); err != nil {
			_ = writeFrame(conn, opErr, []byte(err.Error()))
			return
		}
		_ = writeFrame(conn, opOK, nil)
	case opCapturePane:
		tail := d.ringBuf.Tail(8 * 1024)
		_ = writeFrame(conn, opOK, tail)
	case opCaptureFull:
		_ = writeFrame(conn, opOK, d.ringBuf.Snapshot())
	case opResize:
		if len(payload) != 8 {
			_ = writeFrame(conn, opErr, []byte("opResize payload must be 8 bytes"))
			return
		}
		cols := int(binary.BigEndian.Uint32(payload[:4]))
		rows := int(binary.BigEndian.Uint32(payload[4:]))
		if cols <= 0 || rows <= 0 {
			_ = writeFrame(conn, opErr, []byte("opResize dimensions must be positive"))
			return
		}
		if err := d.pty.Resize(cols, rows); err != nil {
			_ = writeFrame(conn, opErr, []byte(err.Error()))
			return
		}
		_ = writeFrame(conn, opOK, nil)
	case opKill:
		_ = writeFrame(conn, opOK, nil)
		d.shutdown()
	case opAttach:
		d.runAttach(conn)
	default:
		_ = writeFrame(conn, opErr, []byte("unknown op"))
	}
}

// runAttach enters full-duplex streaming mode. The client (typically a
// wt.exe tab running `cavekit mux-attach`) sends stdin bytes; the daemon
// writes them to the ConPTY. In parallel, every PTY output chunk is
// forwarded to the client. Exits when either side closes.
func (d *Daemon) runAttach(conn io.ReadWriteCloser) {
	sub := d.subscribe()
	defer d.unsubscribe(sub)

	// On first attach, replay the existing scrollback so the tab lands on
	// a populated screen rather than an empty one.
	if existing := d.ringBuf.Snapshot(); len(existing) > 0 {
		_, _ = conn.Write(existing)
	}

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for chunk := range sub {
			if _, err := conn.Write(chunk); err != nil {
				return
			}
		}
	}()

	_, _ = io.Copy(&ptyWriter{d.pty}, conn)
	<-writerDone
}

// ptyWriter is a minimal io.Writer adapter around *conpty.ConPty. Used by
// io.Copy inside runAttach so stdin bytes flowing from the wt.exe tab land
// on the pseudo-console. A separate type avoids exporting *ConPty into
// function signatures elsewhere.
type ptyWriter struct{ p *conpty.ConPty }

func (w *ptyWriter) Write(p []byte) (int, error) { return w.p.Write(p) }

func (d *Daemon) shutdown() {
	d.killOnce.Do(func() {
		// Escalate to the whole process tree. The ConPTY child may have
		// spawned grandchildren (claude → node → codex …) that survive a
		// plain pty.Close. taskkill /T /F walks the tree.
		if d.pty != nil {
			pid := d.pty.Pid()
			if pid > 0 {
				_ = osexec.Command("taskkill.exe", "/T", "/F", "/PID", fmt.Sprintf("%d", pid)).Run()
			}
		}
		close(d.done)
	})
}

