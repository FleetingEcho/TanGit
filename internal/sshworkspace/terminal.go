package sshworkspace

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// TerminalOptions configures an independent interactive shell channel.
type TerminalOptions struct {
	Term        string
	Columns     uint32
	Rows        uint32
	PixelWidth  uint32
	PixelHeight uint32
}

// Terminal is one bidirectional SSH shell channel. It implements net.Conn;
// network addresses describe the underlying SSH transport. Deadline methods
// return ErrDeadlineUnsupported because ssh.Channel has no deadline facility.
type Terminal struct {
	channel ssh.Channel
	local   net.Addr
	remote  net.Addr

	closeOnce sync.Once
	closeErr  error
}

// ErrDeadlineUnsupported is returned by terminal deadline setters.
var ErrDeadlineUnsupported = errors.New("SSH terminal deadlines are not supported")

var _ net.Conn = (*Terminal)(nil)

// OpenTerminal opens a PTY and starts a shell on a new channel. Canceling ctx
// during setup closes only the new channel. The caller owns the Terminal and
// should close it independently of other terminals and commands.
func (c *Client) OpenTerminal(ctx context.Context, options TerminalOptions) (*Terminal, error) {
	if c == nil || c.ssh == nil {
		return nil, errors.New("SSH client is nil or uninitialized")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type openedChannel struct {
		channel  ssh.Channel
		requests <-chan *ssh.Request
	}
	opened, err := openCancelable(ctx, func() (openedChannel, error) {
		channel, requests, err := c.ssh.OpenChannel("session", nil)
		return openedChannel{channel: channel, requests: requests}, err
	}, func(opened openedChannel) {
		if opened.channel != nil {
			_ = opened.channel.Close()
		}
	})
	channel, requests := opened.channel, opened.requests
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if channel != nil {
			_ = channel.Close()
		}
		return nil, fmt.Errorf("open terminal channel: %w", err)
	}
	go ssh.DiscardRequests(requests)

	setupDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = channel.Close()
		case <-setupDone:
		}
	}()
	defer close(setupDone)

	term := options.Term
	if term == "" {
		term = "xterm-256color"
	}
	ptyRequest := struct {
		Term        string
		Columns     uint32
		Rows        uint32
		PixelWidth  uint32
		PixelHeight uint32
		Modes       string
	}{term, options.Columns, options.Rows, options.PixelWidth, options.PixelHeight, ""}
	accepted, err := channel.SendRequest("pty-req", true, ssh.Marshal(&ptyRequest))
	if err != nil || !accepted {
		_ = channel.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if err == nil {
			err = errors.New("server rejected PTY request")
		}
		return nil, fmt.Errorf("request terminal PTY: %w", err)
	}
	accepted, err = channel.SendRequest("shell", true, nil)
	if err != nil || !accepted {
		_ = channel.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if err == nil {
			err = errors.New("server rejected shell request")
		}
		return nil, fmt.Errorf("start terminal shell: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = channel.Close()
		return nil, err
	}
	return &Terminal{channel: channel, local: c.ssh.LocalAddr(), remote: c.ssh.RemoteAddr()}, nil
}

func (t *Terminal) Read(p []byte) (int, error)  { return t.channel.Read(p) }
func (t *Terminal) Write(p []byte) (int, error) { return t.channel.Write(p) }

// Close closes only this shell channel.
func (t *Terminal) Close() error {
	if t == nil {
		return nil
	}
	t.closeOnce.Do(func() { t.closeErr = t.channel.Close() })
	return t.closeErr
}

// CloseWrite sends EOF to the shell's standard input while keeping output
// readable.
func (t *Terminal) CloseWrite() error { return t.channel.CloseWrite() }

// Resize updates the remote PTY dimensions without affecting other channels.
func (t *Terminal) Resize(columns, rows uint32) error {
	if t == nil || t.channel == nil {
		return errors.New("terminal is closed or uninitialized")
	}
	request := struct {
		Columns     uint32
		Rows        uint32
		PixelWidth  uint32
		PixelHeight uint32
	}{Columns: columns, Rows: rows}
	_, err := t.channel.SendRequest("window-change", false, ssh.Marshal(&request))
	if err != nil {
		return fmt.Errorf("resize terminal: %w", err)
	}
	return nil
}

func (t *Terminal) LocalAddr() net.Addr              { return t.local }
func (t *Terminal) RemoteAddr() net.Addr             { return t.remote }
func (t *Terminal) SetDeadline(time.Time) error      { return ErrDeadlineUnsupported }
func (t *Terminal) SetReadDeadline(time.Time) error  { return ErrDeadlineUnsupported }
func (t *Terminal) SetWriteDeadline(time.Time) error { return ErrDeadlineUnsupported }
