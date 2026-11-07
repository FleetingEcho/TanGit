//go:build windows

package repository

import (
	"os"
	"os/exec"
	"strconv"
)

const createNewProcessGroup = 0x00000200

// configureCloneCommand makes cancellation terminate Git's complete child
// tree, including SSH and transport helpers. taskkill is part of Windows and
// needs no shell; the direct-process fallback still applies if it fails.
func configureCloneCommand(command *exec.Cmd) {
	configureBackgroundCommand(command)
	command.SysProcAttr.CreationFlags |= createNewProcessGroup
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		killTree := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(command.Process.Pid))
		configureBackgroundCommand(killTree)
		if err := killTree.Run(); err == nil {
			return nil
		}
		return command.Process.Kill()
	}
}
