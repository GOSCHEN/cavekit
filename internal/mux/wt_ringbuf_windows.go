//go:build windows

package mux

import "sync"

// ringBuffer is a fixed-capacity byte queue. Once the capacity is exceeded
// older bytes are dropped. Safe for concurrent use.
//
// The wt multiplexer uses one per session: the daemon tees ConPTY output
// into the ring buffer so CapturePane and CaptureScrollback can return the
// most recent pane content without requiring the child to be re-rendered.
// A larger capacity means richer scrollback at the cost of daemon memory.
type ringBuffer struct {
	mu   sync.Mutex
	buf  []byte
	cap  int
}

// newRingBuffer creates a ring buffer that retains up to capacity bytes.
func newRingBuffer(capacity int) *ringBuffer {
	if capacity <= 0 {
		capacity = 64 * 1024
	}
	return &ringBuffer{cap: capacity}
}

// Write appends p to the buffer, evicting from the front when the cap is hit.
// Returns len(p), nil to satisfy io.Writer.
func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if overflow := len(r.buf) - r.cap; overflow > 0 {
		r.buf = r.buf[overflow:]
	}
	return len(p), nil
}

// Snapshot returns a copy of the current contents.
func (r *ringBuffer) Snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]byte, len(r.buf))
	copy(out, r.buf)
	return out
}

// Tail returns the last n bytes (or the whole buffer if shorter than n).
// Used for CapturePane to approximate the visible viewport.
func (r *ringBuffer) Tail(n int) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n <= 0 || n >= len(r.buf) {
		out := make([]byte, len(r.buf))
		copy(out, r.buf)
		return out
	}
	out := make([]byte, n)
	copy(out, r.buf[len(r.buf)-n:])
	return out
}
