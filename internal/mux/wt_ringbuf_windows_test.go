//go:build windows

package mux

import "testing"

func TestRingBufferBelowCap(t *testing.T) {
	r := newRingBuffer(16)
	r.Write([]byte("hello"))
	if got := string(r.Snapshot()); got != "hello" {
		t.Errorf("snapshot = %q, want hello", got)
	}
}

func TestRingBufferEvictsOldest(t *testing.T) {
	r := newRingBuffer(4)
	r.Write([]byte("abcdef"))
	if got := string(r.Snapshot()); got != "cdef" {
		t.Errorf("snapshot = %q, want cdef", got)
	}
}

func TestRingBufferTail(t *testing.T) {
	r := newRingBuffer(64)
	r.Write([]byte("one-two-three-four-five"))
	if got := string(r.Tail(4)); got != "five" {
		t.Errorf("tail(4) = %q, want five", got)
	}
	if got := string(r.Tail(0)); got != "one-two-three-four-five" {
		t.Errorf("tail(0) should return everything: %q", got)
	}
}
