//go:build !windows

package agentassist

import (
	"os"
	"os/exec"
	"syscall"
)

// configureHelperCommand places the shell and all helper children in their own
// process group. Cancellation cannot leave a child holding response pipes open.
func configureHelperCommand(command *exec.Cmd) {
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
