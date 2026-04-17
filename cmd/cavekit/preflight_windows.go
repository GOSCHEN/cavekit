//go:build windows

package main

import (
	"fmt"
	osexec "os/exec"
)

// preflightMultiplexer verifies Windows Terminal (wt.exe) is installed.
// Falls back to a friendly error pointing users at the Microsoft Store entry.
func preflightMultiplexer() error {
	if _, err := osexec.LookPath("wt.exe"); err != nil {
		return fmt.Errorf("Windows Terminal (wt.exe) not found in PATH — install from the Microsoft Store (minimum version 1.18)")
	}
	return nil
}
