//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package sshintegration

import (
	"runtime"
	"testing"
)

func startOpenSSH(t testing.TB) *Server {
	t.Helper()
	openSSHUnavailable(t, "local OpenSSH integration is unsupported on %s", runtime.GOOS)
	return nil
}
