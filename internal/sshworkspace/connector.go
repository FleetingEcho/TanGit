package sshworkspace

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// HostKeyInfo describes an otherwise unknown server key presented during a
// handshake. Changed keys are never sent to HostKeyConfirmation.
type HostKeyInfo struct {
	Address           string
	FingerprintSHA256 string
	Key               ssh.PublicKey
}

// HostKeyConfirmation decides whether a new host key may be trusted and
// persisted. Returning false rejects the handshake.
type HostKeyConfirmation func(HostKeyInfo) (bool, error)

// Connector creates SSH/SFTP clients using one known_hosts policy and secret
// store. It is safe for concurrent use.
type Connector struct {
	secrets        SecretStore
	knownHostsPath string
	confirm        HostKeyConfirmation

	hostKeyMu sync.Mutex
}

// NewConnector constructs a connector. knownHostsPath is a local filesystem
// path. A nil confirmation callback rejects unknown hosts; known keys are
// accepted automatically, and changed keys are always rejected.
func NewConnector(secrets SecretStore, knownHostsPath string, confirm HostKeyConfirmation) (*Connector, error) {
	if knownHostsPath == "" {
		return nil, errors.New("known_hosts path is empty")
	}
	return &Connector{
		secrets:        secrets,
		knownHostsPath: knownHostsPath,
		confirm:        confirm,
	}, nil
}

// Connect establishes an SSH connection and its SFTP subsystem. ctx bounds the
// network connection and handshake only; canceling it later does not close the
// returned Client.
func (c *Connector) Connect(ctx context.Context, profile Profile) (*Client, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	if err := c.ensureKnownHosts(); err != nil {
		return nil, err
	}
	auth, closeAuth, err := c.authMethod(ctx, profile)
	if err != nil {
		return nil, err
	}
	if closeAuth != nil {
		defer closeAuth()
	}

	authMethods := []ssh.AuthMethod{auth}
	if profile.AuthMode == AuthPassword {
		// Many OpenSSH installations expose password authentication only as
		// keyboard-interactive. Answer every password prompt with the stored
		// credential without logging the challenges or answers.
		authMethods = append(authMethods, ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			password, err := loadSecret(c.secrets, PasswordSecretRef(profile.ID), "SSH password")
			if err != nil {
				return nil, err
			}
			answers := make([]string, len(questions))
			for index := range answers {
				answers[index] = password
			}
			return answers, nil
		}))
	}
	config := &ssh.ClientConfig{
		User:            profile.Username,
		Auth:            authMethods,
		HostKeyCallback: c.hostKeyCallback(),
		Timeout:         30 * time.Second,
	}
	address := profile.Address()
	networkConn, err := (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("dial SSH %s: %w", address, err)
	}
	stopCancellation := context.AfterFunc(ctx, func() { _ = networkConn.Close() })
	sshConn, channels, requests, err := ssh.NewClientConn(networkConn, address, config)
	if !stopCancellation() {
		_ = networkConn.Close()
	}
	if err != nil {
		_ = networkConn.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("SSH handshake with %s: %w", address, err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		_ = sshConn.Close()
		return nil, ctxErr
	}
	sshClient := ssh.NewClient(sshConn, channels, requests)
	stopSFTP := context.AfterFunc(ctx, func() { _ = sshClient.Close() })
	sftpClient, err := sftp.NewClient(sshClient)
	if !stopSFTP() {
		_ = sshClient.Close()
	}
	if err != nil {
		_ = sshClient.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("open SFTP subsystem: %w", err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		_ = sshClient.Close()
		_ = sftpClient.Close()
		return nil, ctxErr
	}
	return newClient(sshClient, sftpClient, profile), nil
}

func (c *Connector) authMethod(ctx context.Context, profile Profile) (ssh.AuthMethod, func() error, error) {
	switch profile.AuthMode {
	case AuthPassword:
		password, err := loadSecret(c.secrets, PasswordSecretRef(profile.ID), "SSH password")
		if err != nil {
			return nil, nil, err
		}
		return ssh.Password(password), nil, nil
	case AuthKey:
		keyPath, err := expandPrivateKeyPath(profile.PrivateKeyPath)
		if err != nil {
			return nil, nil, err
		}
		pemBytes, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read SSH private key: %w", err)
		}
		defer func() {
			for i := range pemBytes {
				pemBytes[i] = 0
			}
		}()
		signer, err := ssh.ParsePrivateKey(pemBytes)
		var passphraseErr *ssh.PassphraseMissingError
		if errors.As(err, &passphraseErr) {
			passphrase, secretErr := loadSecret(c.secrets, PassphraseSecretRef(profile.ID), "SSH private-key passphrase")
			if secretErr != nil {
				return nil, nil, secretErr
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(pemBytes, []byte(passphrase))
		}
		if err != nil {
			return nil, nil, fmt.Errorf("parse SSH private key: %w", err)
		}
		return ssh.PublicKeys(signer), nil, nil
	case AuthAgent:
		socket := os.Getenv("SSH_AUTH_SOCK")
		if socket == "" {
			return nil, nil, errors.New("SSH agent authentication requires SSH_AUTH_SOCK")
		}
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
		if err != nil {
			return nil, nil, fmt.Errorf("connect to SSH agent: %w", err)
		}
		agentClient := agent.NewClient(conn)
		return ssh.PublicKeysCallback(agentClient.Signers), conn.Close, nil
	default:
		return nil, nil, fmt.Errorf("unsupported SSH auth mode %q", profile.AuthMode)
	}
}

func expandPrivateKeyPath(name string) (string, error) {
	if name == "~" || strings.HasPrefix(name, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve local home for SSH private key: %w", err)
		}
		if name == "~" {
			return home, nil
		}
		return filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(name, "~/"))), nil
	}
	if strings.HasPrefix(name, "~") {
		return "", errors.New("SSH private-key path supports ~ and ~/ but not ~user expansion")
	}
	return name, nil
}

func (c *Connector) ensureKnownHosts() error {
	if err := os.MkdirAll(filepath.Dir(c.knownHostsPath), 0o700); err != nil {
		return fmt.Errorf("create known_hosts directory: %w", err)
	}
	file, err := os.OpenFile(c.knownHostsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open known_hosts: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close known_hosts: %w", err)
	}
	return nil
}

func (c *Connector) hostKeyCallback() ssh.HostKeyCallback {
	return func(address string, remote net.Addr, key ssh.PublicKey) error {
		c.hostKeyMu.Lock()
		defer c.hostKeyMu.Unlock()

		callback, err := knownhosts.New(c.knownHostsPath)
		if err != nil {
			return fmt.Errorf("load known_hosts: %w", err)
		}
		err = callback(address, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) {
			return fmt.Errorf("verify SSH host key: %w", err)
		}
		if len(keyErr.Want) != 0 {
			// This includes key changes and all other mismatches for an already
			// known address. Confirmation is deliberately never consulted.
			return fmt.Errorf("SSH host key changed for %s; refusing connection: %w", address, err)
		}
		if c.confirm == nil {
			return fmt.Errorf("unknown SSH host %s (%s)", address, ssh.FingerprintSHA256(key))
		}
		accepted, confirmErr := c.confirm(HostKeyInfo{
			Address:           address,
			FingerprintSHA256: ssh.FingerprintSHA256(key),
			Key:               key,
		})
		if confirmErr != nil {
			return fmt.Errorf("confirm SSH host key: %w", confirmErr)
		}
		if !accepted {
			return fmt.Errorf("SSH host key for %s was not accepted", address)
		}
		if err := c.appendKnownHost(address, key); err != nil {
			return err
		}
		return nil
	}
}

func (c *Connector) appendKnownHost(address string, key ssh.PublicKey) error {
	file, err := os.OpenFile(c.knownHostsPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open known_hosts for writing: %w", err)
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(address)}, key)
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	_, writeErr := file.WriteString(line)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("persist SSH host key: %w", err)
	}
	return nil
}
