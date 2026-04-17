//go:build !windows

package codex

import (
	"os/exec"
	"syscall"
)

// applyDetachSysProcAttr places the child in its own process group so it
// survives the parent's exit and is not bound to the parent's controlling
// terminal.
func applyDetachSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
