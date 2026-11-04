//go:build !windows

package app

import "os/exec"

// Only used when exercising Windows command construction on another host.
func editorNewConsole(cmd *exec.Cmd) {}
