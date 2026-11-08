package sshworkspace

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestRunStreamingCancellationClosesLateSession(t *testing.T) {
	client, channels := newCancellationTestClient(t)

	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		result ExecResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := client.RunStreaming(ctx, ExecOptions{}, "true")
		done <- outcome{result: result, err: err}
	}()

	blocked := receiveNewChannel(t, channels)
	cancel()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("RunStreaming error = %v, want context.Canceled", got.err)
		}
		if got.result.ExitCode != -1 {
			t.Fatalf("RunStreaming exit code = %d, want -1", got.result.ExitCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunStreaming remained blocked after cancellation")
	}

	channel, requests := acceptChannel(t, blocked)
	go ssh.DiscardRequests(requests)
	waitForChannelClose(t, channel, "late command session")

	serverDone := make(chan error, 1)
	go func() {
		next := receiveNewChannel(t, channels)
		channel, requests, err := next.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		serverDone <- serveExec(channel, requests)
	}()
	result, err := client.Run(context.Background(), "", "true")
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("independent command after cancellation = %+v, %v", result, err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("serve independent command: %v", err)
	}
}

func TestOpenTerminalCancellationClosesLateChannel(t *testing.T) {
	client, channels := newCancellationTestClient(t)

	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		terminal *Terminal
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		terminal, err := client.OpenTerminal(ctx, TerminalOptions{})
		done <- outcome{terminal: terminal, err: err}
	}()

	blocked := receiveNewChannel(t, channels)
	cancel()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("OpenTerminal error = %v, want context.Canceled", got.err)
		}
		if got.terminal != nil {
			t.Fatal("OpenTerminal returned a terminal after cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OpenTerminal remained blocked after cancellation")
	}

	channel, requests := acceptChannel(t, blocked)
	go ssh.DiscardRequests(requests)
	waitForChannelClose(t, channel, "late terminal channel")

	terminalReady := make(chan error, 1)
	go func() {
		next := receiveNewChannel(t, channels)
		channel, requests, err := next.Accept()
		if err != nil {
			terminalReady <- err
			return
		}
		terminalReady <- serveTerminalSetup(channel, requests)
	}()
	terminal, err := client.OpenTerminal(context.Background(), TerminalOptions{})
	if err != nil {
		t.Fatalf("independent terminal after cancellation: %v", err)
	}
	if err := <-terminalReady; err != nil {
		_ = terminal.Close()
		t.Fatalf("serve independent terminal: %v", err)
	}
	if err := terminal.Close(); err != nil {
		t.Fatalf("close independent terminal: %v", err)
	}
}

func newCancellationTestClient(t *testing.T) (*Client, <-chan ssh.NewChannel) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := &ssh.ServerConfig{NoClientAuth: true}
	serverConfig.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	clientSide, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	serverSide, err := listener.Accept()
	_ = listener.Close()
	if err != nil {
		_ = clientSide.Close()
		t.Fatal(err)
	}
	type serverHandshake struct {
		connection *ssh.ServerConn
		channels   <-chan ssh.NewChannel
		requests   <-chan *ssh.Request
		err        error
	}
	serverReady := make(chan serverHandshake, 1)
	go func() {
		connection, channels, requests, err := ssh.NewServerConn(serverSide, serverConfig)
		serverReady <- serverHandshake{connection: connection, channels: channels, requests: requests, err: err}
	}()

	connection, channels, requests, err := ssh.NewClientConn(clientSide, "pipe", &ssh.ClientConfig{
		User:            "test",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		_ = clientSide.Close()
		_ = serverSide.Close()
		t.Fatalf("SSH client handshake: %v", err)
	}
	server := <-serverReady
	if server.err != nil {
		_ = connection.Close()
		t.Fatalf("SSH server handshake: %v", server.err)
	}
	go ssh.DiscardRequests(server.requests)
	sshClient := ssh.NewClient(connection, channels, requests)
	client := &Client{ssh: sshClient}
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.connection.Close()
	})
	return client, server.channels
}

func receiveNewChannel(t *testing.T, channels <-chan ssh.NewChannel) ssh.NewChannel {
	t.Helper()
	select {
	case channel, ok := <-channels:
		if !ok {
			t.Fatal("SSH channel stream closed")
		}
		return channel
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an SSH channel")
		return nil
	}
}

func acceptChannel(t *testing.T, newChannel ssh.NewChannel) (ssh.Channel, <-chan *ssh.Request) {
	t.Helper()
	channel, requests, err := newChannel.Accept()
	if err != nil {
		t.Fatalf("accept late SSH channel: %v", err)
	}
	return channel, requests
}

func waitForChannelClose(t *testing.T, channel ssh.Channel, description string) {
	t.Helper()
	closed := make(chan error, 1)
	go func() {
		_, err := channel.Read(make([]byte, 1))
		closed <- err
	}()
	select {
	case err := <-closed:
		if err == nil {
			t.Fatalf("%s returned data instead of closing", description)
		}
		_ = channel.Close()
	case <-time.After(5 * time.Second):
		_ = channel.Close()
		t.Fatalf("%s was not closed", description)
	}
}

func serveExec(channel ssh.Channel, requests <-chan *ssh.Request) error {
	defer channel.Close()
	for request := range requests {
		if request.Type != "exec" {
			_ = request.Reply(false, nil)
			return fmt.Errorf("request type = %q, want exec", request.Type)
		}
		if err := request.Reply(true, nil); err != nil {
			return err
		}
		_, err := channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{}))
		return err
	}
	return errors.New("exec request stream closed before exec")
}

func serveTerminalSetup(channel ssh.Channel, requests <-chan *ssh.Request) error {
	for _, expected := range []string{"pty-req", "shell"} {
		request, ok := <-requests
		if !ok {
			return fmt.Errorf("request stream closed before %s", expected)
		}
		if request.Type != expected {
			_ = request.Reply(false, nil)
			return fmt.Errorf("request type = %q, want %s", request.Type, expected)
		}
		if err := request.Reply(true, nil); err != nil {
			return err
		}
	}
	go ssh.DiscardRequests(requests)
	return nil
}
