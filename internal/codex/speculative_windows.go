//go:build windows

package codex

import (
	"os/exec"
	"syscall"
)

// applyDetachSysProcAttr detaches the child from the parent's console and
// places it in a new process group. DETACHED_PROCESS (0x00000008) + combined
// with CREATE_NEW_PROCESS_GROUP (0x00000200) means the child survives when
// the parent's cavekit process exits — required so speculative reviews can
// outlive the dispatching CLI invocation.
func applyDetachSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x00000008 | 0x00000200,
		HideWindow:    true,
	}
}
