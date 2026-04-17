//go:build !windows

package main

// dispatchPlatformCmd is a no-op on Unix — the mux daemon/attach
// subcommands only exist on Windows where tmux is not available.
func dispatchPlatformCmd(_ string) bool {
	return false
}
