//go:build !windows

package repository

import "os/exec"

func configureBackgroundCommand(*exec.Cmd) {}
