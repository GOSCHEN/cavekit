//go:build windows

package main

// dispatchPlatformCmd routes Windows-only subcommands (`mux-daemon`,
// `mux-attach`) to their handlers. Returns true if the command was handled
// so the generic switch in main.go can skip it.
func dispatchPlatformCmd(cmd string) bool {
	switch cmd {
	case "mux-daemon":
		runMuxDaemon()
		return true
	case "mux-attach":
		runMuxAttach()
		return true
	}
	return false
}
