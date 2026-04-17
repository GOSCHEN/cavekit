//go:build windows

package mux

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/JuliusBrussee/cavekit/internal/paths"
)

// PipeName builds the canonical Windows named-pipe path for a session.
// Mirrors SessionPrefix so ListSessions can enumerate cavekit pipes without
// picking up unrelated servers on the host.
func pipeName(session string) string {
	return `\\.\pipe\cavekit_` + SanitizeName(session)
}

// PipeNameFor is the exported form used by cmd/cavekit mux-attach so the
// attach subcommand can build the pipe path without duplicating logic.
func PipeNameFor(session string) string {
	return pipeName(session)
}

// WriteAttachHeader sends the opAttach framing message so the daemon knows
// to enter full-duplex mode. Exported for use by the mux-attach subcommand.
func WriteAttachHeader(w io.Writer) error {
	return writeFrame(w, opAttach, nil)
}

// sessionStateDir holds one JSON file per live daemon (PID + pipe name).
// Parent processes use it for discovery and cleanup. ListSessions scans it,
// Kill looks up the PID there, and daemons delete their entry on exit.
func sessionStateDir() string {
	return paths.SessionsDir()
}

// Protocol messages exchanged over the pipe.
// Format (big-endian): [1 byte op][4 byte payload length][payload bytes].
const (
	opPing         byte = 0x01 // client → daemon: health check; daemon → "ok"
	opSendBytes    byte = 0x02 // client → daemon: write raw bytes to child stdin
	opCapturePane  byte = 0x03 // client → daemon: return recent tail bytes
	opCaptureFull  byte = 0x04 // client → daemon: return full ring buffer
	opKill         byte = 0x05 // client → daemon: terminate child and exit
	opAttach       byte = 0x06 // client → daemon: bidirectional stream for wt.exe tab
	opOK           byte = 0x10 // daemon → client: success (payload optional)
	opErr          byte = 0x11 // daemon → client: error, payload is message text
)

// errFramedShortRead indicates the pipe was closed mid-message.
var errFramedShortRead = errors.New("pipe: short read on framed message")

// writeFrame encodes a single op+payload message.
func writeFrame(w io.Writer, op byte, payload []byte) error {
	hdr := make([]byte, 5)
	hdr[0] = op
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(payload)))
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
}

// readFrame decodes a single op+payload message.
func readFrame(r io.Reader) (byte, []byte, error) {
	hdr := make([]byte, 5)
	if _, err := io.ReadFull(r, hdr); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return 0, nil, errFramedShortRead
		}
		return 0, nil, err
	}
	op := hdr[0]
	n := binary.BigEndian.Uint32(hdr[1:])
	if n == 0 {
		return op, nil, nil
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return op, nil, fmt.Errorf("read payload: %w", err)
	}
	return op, payload, nil
}
