//go:build linux

package launch

import (
	"os/exec"
	"syscall"
)

func ConfigureDetachedLaunch(command *exec.Cmd) {
	// A new session removes the child's controlling terminal, so shell job
	// control and later terminal signals do not take down the GUI.
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
