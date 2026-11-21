//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"lazygitapp/internal/repository"
)

// Cancelling stops the whole process tree, not only the shell: a background
// child the command left behind must not keep running.
func TestCancellingAShellCommandStopsItsChildren(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	s.tabs = []*tab{{app: h.app}}
	s.active = 0
	dir := t.TempDir()
	h.app.snapshot.Root = dir
	h.app.commandLog = repository.NewCommandLog()
	s.palette.open = true
	s.palette.query = "!sleep 60 & echo $! > child.pid; wait"
	if result := s.invokeCommand("commands/run-line"); !result.Accepted() {
		t.Fatalf("invoke = %+v", result)
	}
	var pid int
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && pid == 0 {
		if data, err := os.ReadFile(filepath.Join(dir, "child.pid")); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("the background child never started")
	}
	h.app.cancelCustomCommand()
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("child process %d survived cancelling the command", pid)
}
