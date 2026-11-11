package sshworkspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Client owns one SSH connection and its SFTP client. SSH commands, SFTP
// operations, and terminals each use independent channels and may run
// concurrently. Close closes all channels and is idempotent.
type Client struct {
	ssh  *ssh.Client
	sftp *sftp.Client

	profile   Profile
	closeOnce sync.Once
	closeErr  error
}

func newClient(sshClient *ssh.Client, sftpClient *sftp.Client, profile Profile) *Client {
	return &Client{ssh: sshClient, sftp: sftpClient, profile: profile}
}

// Close closes SFTP and SSH once. Concurrent and repeated calls return the
// result of the first close.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		var sftpErr error
		if c.sftp != nil {
			sftpErr = c.sftp.Close()
		}
		var sshErr error
		if c.ssh != nil {
			sshErr = c.ssh.Close()
		}
		c.closeErr = errors.Join(sftpErr, sshErr)
	})
	return c.closeErr
}

// ExecResult describes command output and process completion. Output is filled
// by Run and nil for RunStreaming. ExitCode is zero on success, the remote exit
// status after *ssh.ExitError, and -1 when no exit status was received. As with
// os/exec, a nonzero exit returns both a result and an error.
type ExecResult struct {
	Output   []byte
	ExitCode int
}

// ExecOptions controls a streaming command. Dir is a POSIX remote directory;
// an empty value leaves the command in the SSH server's initial directory.
// Nil streams are discarded by the SSH session.
type ExecOptions struct {
	Dir    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Run executes a quoted command and returns interleaved stdout/stderr. The
// process, including its working-directory change, runs in a fresh session.
func (c *Client) Run(ctx context.Context, dir, name string, args ...string) (ExecResult, error) {
	return c.RunWithEnv(ctx, dir, nil, name, args...)
}

// RunWithEnv executes a command with additional environment variables. It uses
// the SSH environment protocol rather than a shell assignment, keeping name and
// args as the actual remote executable and its quoted argument list.
func (c *Client) RunWithEnv(ctx context.Context, dir string, environment map[string]string, name string, args ...string) (ExecResult, error) {
	session, err := c.newSession(ctx)
	if err != nil {
		return ExecResult{ExitCode: -1}, err
	}
	defer session.Close()
	command, err := commandInDirectory(dir, name, args...)
	if err != nil {
		return ExecResult{ExitCode: -1}, err
	}
	for _, key := range slices.Sorted(maps.Keys(environment)) {
		if err := session.Setenv(key, environment[key]); err != nil {
			return ExecResult{ExitCode: -1}, fmt.Errorf("set remote environment %s: %w", key, err)
		}
	}
	var output lockedBuffer
	session.Stdout = &output
	session.Stderr = &output
	return runSession(ctx, session, command, true)
}

// RunStreaming executes a quoted command with independent stdin, stdout, and
// stderr streams. Canceling ctx closes only this command's SSH session.
func (c *Client) RunStreaming(ctx context.Context, options ExecOptions, name string, args ...string) (ExecResult, error) {
	session, err := c.newSession(ctx)
	if err != nil {
		return ExecResult{ExitCode: -1}, err
	}
	defer session.Close()
	command, err := commandInDirectory(options.Dir, name, args...)
	if err != nil {
		return ExecResult{ExitCode: -1}, err
	}
	session.Stdin = options.Stdin
	session.Stdout = options.Stdout
	session.Stderr = options.Stderr
	return runSession(ctx, session, command, false)
}

func (c *Client) newSession(ctx context.Context) (*ssh.Session, error) {
	if c == nil || c.ssh == nil {
		return nil, errors.New("SSH client is nil or uninitialized")
	}
	session, err := openCancelable(ctx, c.ssh.NewSession, func(session *ssh.Session) {
		if session != nil {
			_ = session.Close()
		}
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("open SSH session: %w", err)
	}
	return session, nil
}

// openCancelable gives an SSH channel-opening call a cancellation path without
// closing the shared transport. Its goroutine owns a late result: if the caller
// has stopped waiting, the channel is closed before that goroutine exits.
func openCancelable[T any](ctx context.Context, open func() (T, error), closeValue func(T)) (T, error) {
	type result struct {
		value T
		err   error
	}
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	results := make(chan result)
	go func() {
		value, err := open()
		select {
		case results <- result{value: value, err: err}:
		case <-ctx.Done():
			closeValue(value)
		}
	}()

	select {
	case opened := <-results:
		if err := ctx.Err(); err != nil {
			closeValue(opened.value)
			return zero, err
		}
		return opened.value, opened.err
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

func runSession(ctx context.Context, session *ssh.Session, command string, combined bool) (ExecResult, error) {
	result := ExecResult{ExitCode: -1}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	cancelDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Signal(ssh.SIGKILL)
			_ = session.Close()
		case <-cancelDone:
		}
	}()
	err := session.Run(command)
	close(cancelDone)

	if exitErr := new(ssh.ExitError); errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitStatus()
	} else if err == nil {
		result.ExitCode = 0
	}
	if combined {
		if output, ok := session.Stdout.(*lockedBuffer); ok {
			result.Output = append([]byte(nil), output.Bytes()...)
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	return result, err
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Bytes()
}
