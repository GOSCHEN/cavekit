//go:build !windows

package tmux

import "testing"

func TestDetachKey(t *testing.T) {
	// Ctrl+Q is ASCII 17.
	if DetachKey != 17 {
		t.Errorf("DetachKey should be 17 (Ctrl+Q), got %d", DetachKey)
	}
}
