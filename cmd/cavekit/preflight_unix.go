//go:build !windows

package main

import (
	"fmt"
	osexec "os/exec"
)

// preflightMultiplexer verifies the tmux binary is available on Unix targets.
func preflightMultiplexer() error {
	if _, err := osexec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux not installed")
	}
	return nil
}
