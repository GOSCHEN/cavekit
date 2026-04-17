//go:build windows

package tui

import (
	osexec "os/exec"
)

// defaultShell picks the best available shell on Windows in order of
// preference: PowerShell 7 (pwsh.exe) → Windows PowerShell (powershell.exe)
// → cmd.exe. Falls back to cmd.exe so users with only the legacy shell still
// get a working terminal.
func defaultShell() string {
	for _, candidate := range []string{"pwsh.exe", "powershell.exe"} {
		if _, err := osexec.LookPath(candidate); err == nil {
			return candidate
		}
	}
	return "cmd.exe"
}
