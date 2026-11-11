//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package sshintegration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"lazygitapp/internal/sshworkspace"
)

const (
	openSSHDPath    = "/usr/sbin/sshd"
	startupTimeout  = 15 * time.Second
	keygenTimeout   = 20 * time.Second
	shutdownTimeout = 5 * time.Second
)

func startOpenSSH(t testing.TB) *Server {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("local OpenSSH integration requires an unprivileged user")
	}
	if !executableFile(openSSHDPath) {
		t.Skipf("local OpenSSH integration requires executable %s", openSSHDPath)
	}
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skipf("local OpenSSH integration requires ssh-keygen: %v", err)
	}
	current, err := user.Current()
	if err != nil {
		t.Skipf("local OpenSSH integration cannot identify the current user: %v", err)
	}
	if err := validOpenSSHUsername(current.Username); err != nil {
		t.Skipf("local OpenSSH integration does not support this test account: %v", err)
	}
	lockedReason := lockedAccountReason(current.Username)
	usePAM := "no"
	if lockedReason != "" {
		// OpenSSH's non-PAM path rejects a locked shadow entry before trying
		// public keys. PAM performs account/session checks without treating a
		// locked password as a locked login on platforms such as Linux. All
		// password-based SSH authentication methods remain disabled below.
		if runtime.GOOS != "linux" {
			t.Skipf("local OpenSSH integration cannot use this locked account without PAM: %s", lockedReason)
		}
		usePAM = "yes"
	}

	stateDir := secureStateDir(t)
	hostKey := filepath.Join(stateDir, "ssh_host_ed25519_key")
	clientKey := filepath.Join(stateDir, "id_ed25519")
	generateKey(t, keygen, hostKey)
	generateKey(t, keygen, clientKey)

	authorizedKeys := filepath.Join(stateDir, "authorized_keys")
	publicKey, err := os.ReadFile(clientKey + ".pub")
	if err != nil {
		t.Fatalf("read generated OpenSSH client public key: %v", err)
	}
	if err := os.WriteFile(authorizedKeys, publicKey, 0o600); err != nil {
		t.Fatalf("write OpenSSH authorized_keys: %v", err)
	}

	port := reserveLoopbackPort(t)
	configPath := filepath.Join(stateDir, "sshd_config")
	config := strings.Join([]string{
		"Port " + strconv.Itoa(port),
		"ListenAddress 127.0.0.1",
		"Protocol 2",
		"HostKey " + hostKey,
		"HostKeyAlgorithms ssh-ed25519",
		"PidFile " + filepath.Join(stateDir, "sshd.pid"),
		"AuthorizedKeysFile " + authorizedKeys,
		"PubkeyAuthentication yes",
		"PubkeyAcceptedAlgorithms ssh-ed25519",
		"AuthenticationMethods publickey",
		"PasswordAuthentication no",
		"KbdInteractiveAuthentication no",
		"ChallengeResponseAuthentication no",
		"UsePAM " + usePAM,
		"StrictModes yes",
		"PermitRootLogin no",
		"AllowUsers " + current.Username,
		"AllowTcpForwarding no",
		"AllowAgentForwarding no",
		"X11Forwarding no",
		"PermitTunnel no",
		"PermitTTY yes",
		"PrintMotd no",
		"UseDNS no",
		"Subsystem sftp internal-sftp",
		"LogLevel VERBOSE",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write OpenSSH server configuration: %v", err)
	}

	var log bytes.Buffer
	logPath := filepath.Join(stateDir, "sshd.log")
	command := exec.Command(openSSHDPath, "-D", "-e", "-E", logPath, "-f", configPath)
	command.Stdout = &log
	command.Stderr = &log
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Skipf("cannot start unprivileged local OpenSSH server: %v", err)
	}
	process := &openSSHProcess{command: command, done: make(chan struct{})}
	go func() {
		process.err = command.Wait()
		close(process.done)
	}()

	server := &Server{
		Profile: sshworkspace.Profile{
			ID:             "local-openssh",
			Name:           "Local OpenSSH",
			Host:           "127.0.0.1",
			Username:       current.Username,
			Port:           port,
			AuthMode:       sshworkspace.AuthKey,
			PrivateKeyPath: clientKey,
			DefaultRoot:    "/tmp",
		},
		SecretStore:       sshworkspace.NewMemorySecretStore(),
		HostKeyPath:       hostKey,
		HostPublicKeyPath: hostKey + ".pub",
	}
	server.close = func() {
		select {
		case <-process.done:
			return
		default:
		}
		// sshd forks per-connection children. Killing the process group prevents
		// blocked test commands from surviving the test's temporary state.
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			_ = command.Process.Kill()
		}
		select {
		case <-process.done:
		case <-time.After(shutdownTimeout):
			t.Errorf("local OpenSSH server did not exit after its process group was killed")
		}
	}
	t.Cleanup(server.Close)

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	waitForListener(t, address, process, &log)
	return server
}

type openSSHProcess struct {
	command *exec.Cmd
	done    chan struct{}
	err     error
}

func validOpenSSHUsername(username string) error {
	if username == "" {
		return errors.New("the current username is empty")
	}
	if strings.ContainsAny(username, " \t\r\n:") {
		return fmt.Errorf("username %q cannot be represented safely in sshd_config", username)
	}
	return nil
}

func executableFile(name string) bool {
	info, err := os.Stat(name)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func secureStateDir(t testing.TB) string {
	t.Helper()
	// StrictModes checks every parent of AuthorizedKeysFile. Even though
	// /tmp/Test... is private, the world-writable /tmp ancestor makes OpenSSH
	// reject the key. A disposable directory inside the checkout keeps all test
	// writes local while retaining StrictModes yes; callers can enforce the
	// usual secure source-tree ownership just as OpenSSH does.
	workingDir, err := os.Getwd()
	if err != nil {
		t.Skipf("local OpenSSH integration cannot locate a secure state parent: %v", err)
	}
	stateDir, err := os.MkdirTemp(workingDir, ".openssh-state-")
	if err != nil {
		t.Skipf("local OpenSSH integration cannot create isolated server state inside the working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(stateDir); err != nil {
			t.Errorf("remove local OpenSSH state: %v", err)
		}
	})
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatalf("secure OpenSSH state directory: %v", err)
	}
	return stateDir
}

func lockedAccountReason(username string) string {
	// passwd -S works for the calling user even when the shadow database is not
	// readable. Only a confirmed locked status is a skip; unavailable status
	// tools must never hide an actual helper or authentication failure.
	passwd, err := exec.LookPath("passwd")
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		output, commandErr := exec.CommandContext(ctx, passwd, "-S", username).CombinedOutput()
		cancel()
		if commandErr == nil && passwdStatusLocked(string(output)) {
			return "the current account is locked (passwd -S), and unprivileged sshd refuses locked accounts before public-key authentication"
		}
	}
	getent, err := exec.LookPath("getent")
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		output, commandErr := exec.CommandContext(ctx, getent, "shadow", username).Output()
		cancel()
		if commandErr == nil && shadowEntryLocked(string(output)) {
			return "the current account is locked in the shadow database, and unprivileged sshd refuses locked accounts before public-key authentication"
		}
	}
	return ""
}

func passwdStatusLocked(output string) bool {
	fields := strings.Fields(output)
	return len(fields) >= 2 && (fields[1] == "L" || fields[1] == "LK")
}

func shadowEntryLocked(output string) bool {
	line, _, _ := strings.Cut(strings.TrimSpace(output), "\n")
	fields := strings.Split(line, ":")
	if len(fields) < 2 {
		return false
	}
	password := fields[1]
	return strings.HasPrefix(password, "!") || strings.HasPrefix(password, "*")
}

func generateKey(t testing.TB, keygen, destination string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), keygenTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, keygen, "-q", "-t", "ed25519", "-N", "", "-f", destination)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("ssh-keygen timed out for %s: %v\n%s", destination, ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("ssh-keygen failed for %s: %v\n%s", destination, err, output)
	}
}

func reserveLoopbackPort(t testing.TB) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local OpenSSH integration cannot reserve a loopback port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close temporary loopback listener: %v", err)
	}
	return port
}

func waitForListener(t testing.TB, address string, process *openSSHProcess, log *bytes.Buffer) {
	t.Helper()
	deadline := time.NewTimer(startupTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		connection, err := net.DialTimeout("tcp4", address, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		select {
		case <-process.done:
			startupFailed(t, process.err, log.String())
		case <-deadline.C:
			_ = syscall.Kill(-process.command.Process.Pid, syscall.SIGKILL)
			<-process.done
			t.Fatalf("local OpenSSH server did not listen on %s within %s; last dial error: %v\nsshd output:\n%s", address, startupTimeout, err, log.String())
		case <-ticker.C:
		}
	}
}

func startupFailed(t testing.TB, waitErr error, output string) {
	t.Helper()
	if constraint := environmentConstraint(output); constraint != "" {
		t.Skipf("local OpenSSH server cannot run in this environment: %s\nsshd output:\n%s", constraint, output)
	}
	t.Fatalf("local OpenSSH server exited before accepting connections: %v\nsshd output:\n%s", waitErr, output)
}

func environmentConstraint(output string) string {
	switch {
	case strings.Contains(output, "Missing privilege separation directory"):
		return "the system sshd privilege-separation directory is missing"
	case strings.Contains(output, "Privilege separation user") && strings.Contains(output, "does not exist"):
		return "the system sshd privilege-separation account is missing"
	case strings.Contains(output, "setgroups:") && strings.Contains(output, "Operation not permitted"),
		strings.Contains(output, "setgid:") && strings.Contains(output, "Operation not permitted"),
		strings.Contains(output, "setuid:") && strings.Contains(output, "Operation not permitted"):
		return "the environment denies unprivileged user/group setup"
	default:
		return ""
	}
}
