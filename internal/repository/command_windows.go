//go:build windows

package repository

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// configureBackgroundCommand keeps a child of the GUI executable from
// allocating and briefly displaying its own console window. Preserve fields
// such as CmdLine, which Lazygit's Windows shell builder may already have set.
func configureBackgroundCommand(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.HideWindow = true
	command.SysProcAttr.CreationFlags |= createNoWindow
}

// ConfigureBackgroundCommand exposes the same hidden-process behavior for
// custom user-invoked background commands.
func ConfigureBackgroundCommand(command *exec.Cmd) {
	configureBackgroundCommand(command)
}
