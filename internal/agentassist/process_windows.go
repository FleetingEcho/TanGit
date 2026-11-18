//go:build windows

package agentassist

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
)

// configureHelperCommand prevents a console window and terminates the complete
// helper tree on cancellation. taskkill takes a PID argument, never shell text.
func configureHelperCommand(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.HideWindow = true
	command.SysProcAttr.CreationFlags |= createNoWindow | createNewProcessGroup
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		killTree := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(command.Process.Pid))
		if err := killTree.Run(); err == nil {
			return nil
		}
		return command.Process.Kill()
	}
}
