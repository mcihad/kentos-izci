//go:build unix

package ai

import (
	"os/exec"
	"syscall"
)

// setProcessGroup starts cmd in its own process group and makes
// cancellation terminate the whole group (opencode starts helpers).
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
}

// killProcessGroup removes what is left of the group after cmd exited.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
