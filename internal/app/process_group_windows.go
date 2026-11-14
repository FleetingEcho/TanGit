//go:build windows

package app

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"lazygitapp/internal/repository"
)

// configureCustomCancellation uses Windows' supported process-tree terminate
// operation (taskkill /T). Killing cmd.exe alone would leave grandchildren
// alive after its repository was closed.
func configureCustomCancellation(cmd *exec.Cmd) {
	cmd.WaitDelay = 3 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		tree := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		repository.ConfigureBackgroundCommand(tree)
		if err := tree.Run(); err != nil {
			// The process may already have exited between /T assignment and
			// termination; the shell itself is otherwise a safe fallback.
			if killErr := cmd.Process.Kill(); killErr != nil && !strings.Contains(strings.ToLower(killErr.Error()), "finished") {
				return killErr
			}
		}
		return nil
	}
}
