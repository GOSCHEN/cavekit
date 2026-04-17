//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"github.com/JuliusBrussee/cavekit/internal/mux"
)

// runMuxDaemon is the entry point for `cavekit mux-daemon --name <n>
// --dir <d> --prog <p>`. It hosts one session until the child exits or a
// kill request arrives on the named pipe.
//
// Invoked by wtMultiplexer.CreateSession — end users do not call it
// directly. Errors are printed to stderr so diagnostics survive even when
// the daemon is detached from the parent's console.
func runMuxDaemon() {
	name, workDir, program := parseDaemonFlags(os.Args[2:])
	if name == "" || program == "" {
		fmt.Fprintln(os.Stderr, "usage: cavekit mux-daemon --name <n> --dir <d> --prog <p>")
		os.Exit(2)
	}
	if err := mux.RunDaemon(context.Background(), name, workDir, program); err != nil {
		fmt.Fprintf(os.Stderr, "cavekit mux-daemon: %v\n", err)
		os.Exit(1)
	}
}

func parseDaemonFlags(args []string) (name, dir, prog string) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			if i+1 < len(args) {
				name = args[i+1]
				i++
			}
		case "--dir":
			if i+1 < len(args) {
				dir = args[i+1]
				i++
			}
		case "--prog":
			if i+1 < len(args) {
				prog = args[i+1]
				i++
			}
		}
	}
	return
}

// runMuxAttach is the entry point for `cavekit mux-attach --name <n>`.
// Runs inside a wt.exe tab. Connects to the daemon's named pipe, enters
// attach mode, and pumps stdin/stdout between the tab's console and the
// daemon's ConPTY.
func runMuxAttach() {
	name := ""
	for i, a := range os.Args {
		if (a == "--name" || a == "-n") && i+1 < len(os.Args) {
			name = os.Args[i+1]
		}
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "usage: cavekit mux-attach --name <n>")
		os.Exit(2)
	}

	conn, err := winio.DialPipe(mux.PipeNameFor(name), durationPtr(5*time.Second))
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot connect to session %q: %v\n", name, err)
		os.Exit(1)
	}
	defer conn.Close()

	// Send opAttach header and drop into full-duplex mode.
	if err := mux.WriteAttachHeader(conn); err != nil {
		fmt.Fprintf(os.Stderr, "write attach header: %v\n", err)
		os.Exit(1)
	}

	// Switch our own console into raw/VT mode so escape sequences and
	// keystrokes pass through unfiltered.
	restore, err := enableRawConsole()
	if err != nil {
		fmt.Fprintf(os.Stderr, "raw console: %v\n", err)
	}
	defer restore()

	// Daemon → stdout.
	go func() { _, _ = io.Copy(os.Stdout, conn) }()

	// Poll console size and push resize events to the daemon.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pollAndSendResize(ctx, name)

	// Stdin → daemon.
	_, _ = io.Copy(conn, os.Stdin)
}

// pollAndSendResize watches the current console buffer size and forwards
// each change to the daemon via a short-lived opResize pipe call. Polling
// (500ms) is used instead of ReadConsoleInputW WINDOW_BUFFER_SIZE events
// because the attach client has already put stdin into raw VT mode for the
// child — re-reading input records would compete with that stream.
func pollAndSendResize(ctx context.Context, name string) {
	stdout, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return
	}

	var lastCols, lastRows int
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()

	send := func(cols, rows int) {
		if cols <= 0 || rows <= 0 {
			return
		}
		if cols == lastCols && rows == lastRows {
			return
		}
		lastCols, lastRows = cols, rows
		_ = mux.SendResize(ctx, name, cols, rows)
	}

	// Initial push so the daemon picks up the tab's actual dimensions
	// rather than the 120x30 default baked into the ConPTY creation.
	if cols, rows, ok := consoleSize(stdout); ok {
		send(cols, rows)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if cols, rows, ok := consoleSize(stdout); ok {
				send(cols, rows)
			}
		}
	}
}

func consoleSize(h windows.Handle) (int, int, bool) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(h, &info); err != nil {
		return 0, 0, false
	}
	cols := int(info.Window.Right - info.Window.Left + 1)
	rows := int(info.Window.Bottom - info.Window.Top + 1)
	return cols, rows, true
}

// durationPtr helps the winio.DialPipe timeout API, which takes *time.Duration.
func durationPtr(d time.Duration) *time.Duration { return &d }

// enableRawConsole switches stdin into VT input + raw mode and stdout into
// VT processing, returning a restore function callers must defer.
func enableRawConsole() (func(), error) {
	const (
		enableVTInput      uint32 = 0x0200
		enableVTProcessing uint32 = 0x0004
		enableProcessedOut uint32 = 0x0001
		enableLineInput    uint32 = 0x0002
		enableEchoInput    uint32 = 0x0004
		enableProcessedIn  uint32 = 0x0001
	)

	stdin := windows.Handle(os.Stdin.Fd())
	var inMode uint32
	if err := windows.GetConsoleMode(stdin, &inMode); err != nil {
		return func() {}, err
	}
	newIn := inMode
	newIn &^= enableLineInput | enableEchoInput | enableProcessedIn
	newIn |= enableVTInput
	_ = windows.SetConsoleMode(stdin, newIn)

	stdout, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	var outMode uint32
	if err == nil {
		_ = windows.GetConsoleMode(stdout, &outMode)
		_ = windows.SetConsoleMode(stdout, outMode|enableVTProcessing|enableProcessedOut)
	}

	return func() {
		_ = windows.SetConsoleMode(stdin, inMode)
		if err == nil {
			_ = windows.SetConsoleMode(stdout, outMode)
		}
	}, nil
}
