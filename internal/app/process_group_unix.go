//go:build !windows

package app

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// configureCustomCancellation binds cancellation to the process group created
// for this command. Repository shutdown needs a hard stop of the entire group,
// not a graceful shutdown of only the shell. The Setpgid allocation guarantees
// this can never target the GUI application's own process group.
func configureCustomCancellation(cmd *exec.Cmd) {
	cmd.SysProcAttr = ensureUnixSysProcAttr(cmd.SysProcAttr)
	cmd.SysProcAttr.Setpgid = true
	cmd.WaitDelay = 3 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return nil
		}
		return err
	}
}

func ensureUnixSysProcAttr(attr *syscall.SysProcAttr) *syscall.SysProcAttr {
	if attr == nil {
		attr = &syscall.SysProcAttr{}
	}
	return attr
}
