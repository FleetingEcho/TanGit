//go:build linux || windows

package launch

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/egoist/mygo"
)

const DetachedLaunchEnvironment = "LAZYGITGO_DETACHED_LAUNCH"

// launchInBackground replaces a production launcher with an independent GUI
// process. The environment marker is private to that child, preventing another
// relaunch. Development builds stay attached so mygo dev owns their lifetime.
func LaunchInBackground() (bool, error) {
	if mygo.IsDev() || os.Getenv(DetachedLaunchEnvironment) == "1" {
		return false, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("locate lazygitGo executable: %w", err)
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return false, fmt.Errorf("open null device: %w", err)
	}
	defer null.Close()

	command := exec.Command(executable, os.Args[1:]...)
	command.Env = append(os.Environ(), DetachedLaunchEnvironment+"=1")
	command.Stdin = null
	command.Stdout = null
	command.Stderr = null
	ConfigureDetachedLaunch(command)
	if err := command.Start(); err != nil {
		return false, fmt.Errorf("start lazygitGo GUI: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		return true, fmt.Errorf("release lazygitGo launcher: %w", err)
	}
	return true, nil
}
