//go:build windows

package repository

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestBackgroundCommandDoesNotAllocateAConsole(t *testing.T) {
	command := exec.Command("git", "status")
	command.SysProcAttr = &syscall.SysProcAttr{CmdLine: `git status`, CreationFlags: 0x00000004}

	configureBackgroundCommand(command)

	if command.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatal("the Git child would allocate a visible console window")
	}
	if !command.SysProcAttr.HideWindow {
		t.Fatal("the Git child's startup window is not hidden")
	}
	if command.SysProcAttr.CreationFlags&0x00000004 == 0 {
		t.Fatal("configuring the background command replaced existing creation flags")
	}
	if command.SysProcAttr.CmdLine != `git status` {
		t.Fatalf("configuring the background command replaced CmdLine with %q", command.SysProcAttr.CmdLine)
	}
}
