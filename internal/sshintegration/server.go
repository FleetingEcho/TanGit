package sshintegration

import (
	"sync"
	"testing"

	"lazygitapp/internal/sshworkspace"
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

// Start launches /usr/sbin/sshd without root privileges. It skips the calling
// test when OpenSSH, ssh-keygen, a supported POSIX platform, or required host
// privilege-separation facilities are unavailable. Configuration and key
// errors caused by this package are fatal rather than skipped.
func Start(t testing.TB) *Server {
	t.Helper()
	return startOpenSSH(t)
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
