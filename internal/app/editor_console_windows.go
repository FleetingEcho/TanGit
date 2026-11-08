package app

import (
	"os/exec"
	"syscall"
)

func editorNewConsole(cmd *exec.Cmd) {
	const createNewConsole = 0x00000010
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
}
