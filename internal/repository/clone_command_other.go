//go:build !windows

package repository

import (
	"os"
	"os/exec"
	"syscall"
)

// configureCloneCommand isolates Git and its transport helpers in a process
// group. CommandContext invokes Cancel on cancellation, so SSH and filters
// cannot outlive the clone and keep its destination or output pipes active.
func configureCloneCommand(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.Setpgid = true
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
}
