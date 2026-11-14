package app

import (
	"fmt"
	"os/exec"
	"runtime"
)

// launchWorktreeTerminal opens a visible external terminal rooted at path.
// Arguments are passed directly, so paths with spaces or shell metacharacters
// never pass through a shell.
var launchWorktreeTerminal = func(path string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		program, err := exec.LookPath("wt.exe")
		if err != nil {
			return fmt.Errorf("Windows Terminal was not found")
		}
		command = exec.Command(program, "-d", path)
	case "darwin":
		command = exec.Command("/usr/bin/open", "-a", "Terminal", path)
	default:
		candidates := []struct {
			name string
			args []string
		}{
			{"x-terminal-emulator", []string{"--working-directory", path}},
			{"gnome-terminal", []string{"--working-directory=" + path}},
			{"konsole", []string{"--workdir", path}},
			{"xfce4-terminal", []string{"--working-directory", path}},
		}
		for _, candidate := range candidates {
			if program, err := exec.LookPath(candidate.name); err == nil {
				command = exec.Command(program, candidate.args...)
				break
			}
		}
		if command == nil {
			return fmt.Errorf("no supported terminal emulator was found")
		}
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
