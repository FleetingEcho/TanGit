//go:build linux || windows

package app

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/egoist/mygo"
)

const detachedLaunchEnvironment = "TANGIT_DETACHED_LAUNCH"

// launchInBackground replaces a production launcher with an independent GUI
// process. The environment marker is private to that child, preventing another
// relaunch. Development builds stay attached so mygo dev owns their lifetime.
func launchInBackground() (bool, error) {
	if mygo.IsDev() || os.Getenv(detachedLaunchEnvironment) == "1" {
		return false, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("locate TanGit executable: %w", err)
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return false, fmt.Errorf("open null device: %w", err)
	}
	defer null.Close()

	command := exec.Command(executable, os.Args[1:]...)
	command.Env = append(os.Environ(), detachedLaunchEnvironment+"=1")
	command.Stdin = null
	command.Stdout = null
	command.Stderr = null
	configureDetachedLaunch(command)
	if err := command.Start(); err != nil {
		return false, fmt.Errorf("start TanGit GUI: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		return true, fmt.Errorf("release TanGit launcher: %w", err)
	}
	return true, nil
}
