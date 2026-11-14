//go:build !windows

package repository

import "os/exec"

func configureBackgroundCommand(*exec.Cmd) {}

// ConfigureBackgroundCommand applies the OS-level flags used by every Git
// background execution so custom commands also stay invisible on Windows.
func ConfigureBackgroundCommand(command *exec.Cmd) {
	configureBackgroundCommand(command)
}
