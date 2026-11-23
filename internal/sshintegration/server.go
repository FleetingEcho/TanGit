package sshintegration

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/FleetingEcho/lazygitGo/internal/sshworkspace"
)

// Server is an isolated OpenSSH server bound to an ephemeral loopback port.
// HostKeyPath is the generated private host key; HostPublicKeyPath is its
// corresponding public key. Profile uses unencrypted ed25519 client-key
// authentication, so SecretStore normally needs no entries.
//
// Start registers Close with testing cleanup. Close is also safe to call
// directly and more than once.
type Server struct {
	Profile           sshworkspace.Profile
	SecretStore       sshworkspace.SecretStore
	HostKeyPath       string
	HostPublicKeyPath string

	closeOnce sync.Once
	close     func()
}

const requireOpenSSHEnvironment = "LAZYGITGO_TEST_REQUIRE_OPENSSH"

// Start launches /usr/sbin/sshd without root privileges. It skips the calling
// test when OpenSSH, ssh-keygen, a supported POSIX platform, or required host
// privilege-separation facilities are unavailable. Setting
// LAZYGITGO_TEST_REQUIRE_OPENSSH=1 makes those environment limitations fatal so a
// required CI job cannot pass by skipping its OpenSSH coverage. Configuration
// and key errors caused by this package are fatal rather than skipped.
func Start(t testing.TB) *Server {
	t.Helper()
	return startOpenSSH(t)
}

type openSSHUnavailableReporter interface {
	Helper()
	Skip(args ...any)
	Fatalf(format string, args ...any)
}

func openSSHUnavailable(t openSSHUnavailableReporter, format string, args ...any) {
	t.Helper()
	reason := fmt.Sprintf(format, args...)
	if os.Getenv(requireOpenSSHEnvironment) == "1" {
		t.Fatalf("%s=1 requires local OpenSSH integration, but the environment is unsuitable: %s", requireOpenSSHEnvironment, reason)
		return
	}
	t.Skip(reason)
}

// Close terminates the server process group and waits for sshd to exit.
func (s *Server) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		if s.close != nil {
			s.close()
		}
	})
}
