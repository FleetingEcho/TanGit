package sshworkspace_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lazygitapp/internal/sshintegration"
	"lazygitapp/internal/sshworkspace"

	"golang.org/x/crypto/ssh"
)

func TestLocalOpenSSHWorkspace(t *testing.T) {
	server := sshintegration.Start(t)
	hostPublicKey := readOpenSSHPublicKey(t, server.HostPublicKeyPath)
	knownHostsPath := filepath.Join(t.TempDir(), "state", "known_hosts")
	confirmations := 0
	connector, err := sshworkspace.NewConnector(server.SecretStore, knownHostsPath, func(info sshworkspace.HostKeyInfo) (bool, error) {
		confirmations++
		if info.Address != server.Profile.Address() {
			t.Errorf("host-key confirmation address = %q, want %q", info.Address, server.Profile.Address())
		}
		if info.FingerprintSHA256 != ssh.FingerprintSHA256(hostPublicKey) {
			t.Errorf("host-key fingerprint = %q, want %q", info.FingerprintSHA256, ssh.FingerprintSHA256(hostPublicKey))
		}
		if !bytes.Equal(info.Key.Marshal(), hostPublicKey.Marshal()) {
			t.Error("confirmed host key differs from the generated OpenSSH host key")
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	client, err := connector.Connect(ctx, server.Profile)
	if err != nil {
		t.Fatalf("Connect with generated ed25519 key: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if confirmations != 1 {
		t.Fatalf("unknown-host confirmation calls = %d, want 1", confirmations)
	}
	persisted, err := os.ReadFile(knownHostsPath)
	if err != nil {
		t.Fatalf("read persisted known_hosts: %v", err)
	}
	publicLine := strings.Fields(string(mustReadOpenSSHFile(t, server.HostPublicKeyPath)))
	if len(publicLine) < 2 || !strings.Contains(string(persisted), publicLine[1]) {
		t.Fatalf("known_hosts does not contain the generated host key: %q", persisted)
	}

	// A separate connector with no confirmation callback proves that the first
	// handshake persisted a usable trust record rather than merely accepting it.
	trustedConnector, err := sshworkspace.NewConnector(server.SecretStore, knownHostsPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	trustedClient, err := trustedConnector.Connect(ctx, server.Profile)
	if err != nil {
		t.Fatalf("Connect using persisted host key: %v", err)
	}
	if err := trustedClient.Close(); err != nil {
		t.Fatalf("close second trusted connection: %v", err)
	}
	if confirmations != 1 {
		t.Fatalf("known host was confirmed again: %d calls", confirmations)
	}

	result, err := client.Run(ctx, "", "printf", "%s", "key-auth-ok")
	if err != nil || result.ExitCode != 0 || string(result.Output) != "key-auth-ok" {
		t.Fatalf("key-auth command = %+v, %v", result, err)
	}
	remoteRoot := makeOpenSSHRemoteRoot(t, ctx, client)
	t.Cleanup(func() { _ = client.RemoveAll(remoteRoot) })

	t.Run("command streams and exit status", func(t *testing.T) {
		testOpenSSHCommandStreams(t, ctx, client, remoteRoot)
	})
	t.Run("POSIX SFTP and atomic replacement", func(t *testing.T) {
		testOpenSSHFilesystem(t, ctx, client, remoteRoot)
	})
	t.Run("independent concurrent commands", func(t *testing.T) {
		testOpenSSHConcurrentCommands(t, ctx, client)
	})
	t.Run("cancellation is channel local", func(t *testing.T) {
		testOpenSSHLocalCancellation(t, ctx, client)
	})
	t.Run("recursive transfers and ZIP downloads", func(t *testing.T) {
		testOpenSSHTransfers(t, ctx, client, remoteRoot)
	})
	t.Run("terminal input output and resize", func(t *testing.T) {
		testOpenSSHTerminal(t, ctx, client)
	})
}

func testOpenSSHCommandStreams(t *testing.T, ctx context.Context, client *sshworkspace.Client, remoteRoot string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	result, err := client.RunStreaming(ctx, sshworkspace.ExecOptions{
		Dir:    remoteRoot,
		Stdin:  strings.NewReader("payload with spaces\n"),
		Stdout: &stdout,
		Stderr: &stderr,
	}, "sh", "-c", `printf 'dir:%s\n' "$PWD"; IFS= read -r line; printf 'stdout:%s\n' "$line"; printf 'stderr:%s\n' "$line" >&2; exit 37`)
	var exitErr *ssh.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("streaming command error = %v, want ssh.ExitError", err)
	}
	if result.ExitCode != 37 || exitErr.ExitStatus() != 37 {
		t.Fatalf("streaming command exit = %+v, ssh error = %+v", result, exitErr)
	}
	wantStdout := "dir:" + remoteRoot + "\nstdout:payload with spaces\n"
	if stdout.String() != wantStdout {
		t.Fatalf("stdout = %q, want %q", stdout.String(), wantStdout)
	}
	if stderr.String() != "stderr:payload with spaces\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func testOpenSSHFilesystem(t *testing.T, ctx context.Context, client *sshworkspace.Client, parent string) {
	t.Helper()
	root := path.Join(parent, "filesystem cases")
	if err := client.MkdirAll(path.Join(root, "nested", "directory")); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	plainFile := path.Join(root, "plain file.txt")
	writer, err := client.OpenWriter(plainFile, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := io.WriteString(writer, "through SFTP writer")
	closeErr := writer.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		t.Fatalf("write remote file: %v", err)
	}
	if contents, err := client.ReadFile(plainFile); err != nil || string(contents) != "through SFTP writer" {
		t.Fatalf("ReadFile = %q, %v", contents, err)
	}
	if info, err := client.Stat(plainFile); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("plain file metadata = %v, %v", info, err)
	}

	atomicFile := path.Join(root, "atomic.txt")
	if err := client.WriteFileAtomic(atomicFile, []byte("first"), 0o600); err != nil {
		t.Fatalf("initial atomic write: %v", err)
	}
	if err := client.WriteFileAtomic(atomicFile, []byte("second"), 0); err != nil {
		t.Fatalf("replacement atomic write: %v", err)
	}
	if contents, err := client.ReadFile(atomicFile); err != nil || string(contents) != "second" {
		t.Fatalf("atomic contents = %q, %v", contents, err)
	}
	if info, err := client.Stat(atomicFile); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("atomic mode was not preserved: info=%v err=%v", info, err)
	}

	link := path.Join(root, "atomic-link")
	if result, err := client.Run(ctx, "", "ln", "-s", atomicFile, link); err != nil {
		t.Fatalf("create symbolic link: result=%+v err=%v", result, err)
	}
	linkInfo, err := client.Lstat(link)
	if err != nil {
		t.Fatalf("Lstat symbolic link: %v", err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Lstat followed the symbolic link: mode=%v", linkInfo.Mode())
	}
	if targetInfo, err := client.Stat(link); err != nil || !targetInfo.Mode().IsRegular() {
		t.Fatalf("Stat symbolic-link target: info=%v err=%v", targetInfo, err)
	}

	replacement := path.Join(root, "replacement.txt")
	if err := client.WriteFileAtomic(replacement, []byte("atomic rename wins"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := client.RenameAtomically(replacement, atomicFile); err != nil {
		t.Fatalf("RenameAtomically overwrite: %v", err)
	}
	if contents, err := client.ReadFile(atomicFile); err != nil || string(contents) != "atomic rename wins" {
		t.Fatalf("atomically renamed contents = %q, %v", contents, err)
	}
	if exists, err := client.Exists(replacement); err != nil || exists {
		t.Fatalf("replacement source still exists: exists=%v err=%v", exists, err)
	}
	entries, err := client.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	names := make(map[string]bool, len(entries))
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	for _, name := range []string{"nested", "plain file.txt", "atomic.txt", "atomic-link"} {
		if !names[name] {
			t.Errorf("ReadDir omitted %q from %v", name, names)
		}
	}
	if err := client.Remove(plainFile); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if exists, err := client.Exists(plainFile); err != nil || exists {
		t.Fatalf("removed file exists: exists=%v err=%v", exists, err)
	}
}

func testOpenSSHConcurrentCommands(t *testing.T, ctx context.Context, client *sshworkspace.Client) {
	t.Helper()
	first := startControlledOpenSSHCommand(ctx, client, "first", "printf 'first-ready\\n'; IFS= read -r value; printf 'first:%s\\n' \"$value\"")
	defer first.close()
	second := startControlledOpenSSHCommand(ctx, client, "second", "printf 'second-ready\\n'; IFS= read -r value; printf 'second:%s\\n' \"$value\"")
	defer second.close()
	first.waitReady(t, ctx)
	second.waitReady(t, ctx)

	result, err := client.Run(ctx, "", "printf", "%s", "third-independent")
	if err != nil || result.ExitCode != 0 || string(result.Output) != "third-independent" {
		t.Fatalf("third command while two channels are blocked = %+v, %v", result, err)
	}
	second.release(t, ctx, "released-second\n")
	secondOutcome := second.wait(t, ctx)
	if secondOutcome.err != nil || secondOutcome.result.ExitCode != 0 || !strings.Contains(second.output(), "second:released-second\n") {
		t.Fatalf("second command outcome = %+v, output=%q", secondOutcome, second.output())
	}
	select {
	case outcome := <-first.done:
		t.Fatalf("first command ended when its peer was released: %+v", outcome)
	default:
	}
	first.release(t, ctx, "released-first\n")
	firstOutcome := first.wait(t, ctx)
	if firstOutcome.err != nil || firstOutcome.result.ExitCode != 0 || !strings.Contains(first.output(), "first:released-first\n") {
		t.Fatalf("first command outcome = %+v, output=%q", firstOutcome, first.output())
	}
}

func testOpenSSHLocalCancellation(t *testing.T, ctx context.Context, client *sshworkspace.Client) {
	t.Helper()
	blocked := startControlledOpenSSHCommand(ctx, client, "blocked", "printf 'blocked-ready\\n'; IFS= read -r value; printf 'blocked:%s\\n' \"$value\"")
	defer blocked.close()
	survivor := startControlledOpenSSHCommand(ctx, client, "survivor", "printf 'survivor-ready\\n'; IFS= read -r value; printf 'survivor:%s\\n' \"$value\"")
	defer survivor.close()
	blocked.waitReady(t, ctx)
	survivor.waitReady(t, ctx)

	blocked.cancel()
	outcome := blocked.wait(t, ctx)
	if !errors.Is(outcome.err, context.Canceled) {
		t.Fatalf("canceled command outcome = %+v, want context.Canceled", outcome)
	}
	// session.Close races with the SIGKILL it sends first: OpenSSH may report
	// the remote 128+SIGKILL status before the channel closes, or no status at
	// all (-1). Both represent a canceled command and neither is success.
	if outcome.result.ExitCode == 0 {
		t.Fatalf("canceled command unexpectedly succeeded: %+v", outcome.result)
	}
	survivor.release(t, ctx, "still-alive\n")
	survivorOutcome := survivor.wait(t, ctx)
	if survivorOutcome.err != nil || survivorOutcome.result.ExitCode != 0 || !strings.Contains(survivor.output(), "survivor:still-alive\n") {
		t.Fatalf("surviving concurrent command = %+v, output=%q", survivorOutcome, survivor.output())
	}
	result, err := client.Run(ctx, "", "printf", "%s", "connection-alive")
	if err != nil || result.ExitCode != 0 || string(result.Output) != "connection-alive" {
		t.Fatalf("new command after peer cancellation = %+v, %v", result, err)
	}
}

func testOpenSSHTransfers(t *testing.T, ctx context.Context, client *sshworkspace.Client, parent string) {
	t.Helper()
	localRoot := filepath.Join(t.TempDir(), "upload tree")
	if err := os.MkdirAll(filepath.Join(localRoot, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(localRoot, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	localFiles := map[string]struct {
		contents string
		mode     os.FileMode
	}{
		"root.txt":        {contents: "root contents", mode: 0o600},
		"nested/file.txt": {contents: "nested contents", mode: 0o640},
	}
	for name, file := range localFiles {
		if err := os.WriteFile(filepath.Join(localRoot, filepath.FromSlash(name)), []byte(file.contents), file.mode); err != nil {
			t.Fatal(err)
		}
	}
	remoteRoot := path.Join(parent, "recursive upload")
	if err := client.UploadFolder(ctx, localRoot, remoteRoot, nil); err != nil {
		t.Fatalf("UploadFolder: %v", err)
	}
	for name, file := range localFiles {
		contents, err := client.ReadFile(path.Join(remoteRoot, name))
		if err != nil || string(contents) != file.contents {
			t.Fatalf("uploaded %s = %q, %v", name, contents, err)
		}
	}

	localDownload := filepath.Join(t.TempDir(), "downloaded.txt")
	if err := client.DownloadFile(ctx, path.Join(remoteRoot, "nested/file.txt"), localDownload, 0, nil); err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	contents, err := os.ReadFile(localDownload)
	if err != nil || string(contents) != "nested contents" {
		t.Fatalf("downloaded file = %q, %v", contents, err)
	}
	if info, err := os.Stat(localDownload); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("downloaded mode was not preserved: info=%v err=%v", info, err)
	}

	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	if err := client.DownloadFolderZIP(ctx, remoteRoot, zipWriter, nil); err != nil {
		_ = zipWriter.Close()
		t.Fatalf("DownloadFolderZIP: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	zipReader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	entries := make(map[string]string, len(zipReader.File))
	for _, entry := range zipReader.File {
		if strings.HasSuffix(entry.Name, "/") {
			entries[entry.Name] = ""
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			t.Fatal(err)
		}
		entries[entry.Name] = string(data)
	}
	wantEntries := map[string]string{"root.txt": "root contents", "nested/file.txt": "nested contents", "empty/": ""}
	if len(entries) != len(wantEntries) {
		t.Fatalf("ZIP entries = %v, want %v", entries, wantEntries)
	}
	for name, contents := range wantEntries {
		if entries[name] != contents {
			t.Errorf("ZIP entry %q = %q, want %q", name, entries[name], contents)
		}
	}
}

func testOpenSSHTerminal(t *testing.T, ctx context.Context, client *sshworkspace.Client) {
	t.Helper()
	terminal, err := client.OpenTerminal(ctx, sshworkspace.TerminalOptions{Term: "xterm", Columns: 80, Rows: 24})
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	output := newOpenSSHWatchBuffer()
	readDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(output, terminal)
		close(readDone)
	}()
	t.Cleanup(func() {
		_ = terminal.Close()
		select {
		case <-readDone:
		case <-ctx.Done():
		}
	})
	writeTerminalOpenSSH(t, terminal, "stty -echo; printf 'TTY_%s\\n' READY\n")
	output.waitContains(t, ctx, "TTY_READY")
	writeTerminalOpenSSH(t, terminal, "printf 'INITIAL_%s\\n' \"$(stty size)\"\n")
	output.waitContains(t, ctx, "INITIAL_24 80")
	if err := terminal.Resize(83, 37); err != nil {
		t.Fatalf("Resize terminal: %v", err)
	}
	writeTerminalOpenSSH(t, terminal, "printf 'RESIZED_%s\\n' \"$(stty size)\"\n")
	output.waitContains(t, ctx, "RESIZED_37 83")
	writeTerminalOpenSSH(t, terminal, "printf 'PTY_%s\\n' OK\n")
	output.waitContains(t, ctx, "PTY_OK")
}

func readOpenSSHPublicKey(t *testing.T, name string) ssh.PublicKey {
	t.Helper()
	contents := mustReadOpenSSHFile(t, name)
	key, _, _, _, err := ssh.ParseAuthorizedKey(contents)
	if err != nil {
		t.Fatalf("parse generated host public key: %v", err)
	}
	return key
}

func mustReadOpenSSHFile(t *testing.T, name string) []byte {
	t.Helper()
	contents, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func makeOpenSSHRemoteRoot(t *testing.T, ctx context.Context, client *sshworkspace.Client) string {
	t.Helper()
	result, err := client.Run(ctx, "", "mktemp", "-d", "/tmp/tangit-openssh-workspace-XXXXXXXXXX")
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("create remote test root: result=%+v err=%v", result, err)
	}
	root := strings.TrimSpace(string(result.Output))
	if !path.IsAbs(root) || root == "/tmp" || root == "/" {
		t.Fatalf("mktemp returned unsafe remote root %q", root)
	}
	return root
}

type controlledOpenSSHCommand struct {
	name         string
	stdin        *io.PipeWriter
	outputBuffer *openSSHOutput
	done         chan openSSHCommandOutcome
	cancel       context.CancelFunc

	closeOnce sync.Once
}

type openSSHCommandOutcome struct {
	result sshworkspace.ExecResult
	err    error
}

func startControlledOpenSSHCommand(ctx context.Context, client *sshworkspace.Client, name, script string) *controlledOpenSSHCommand {
	commandCtx, cancel := context.WithCancel(ctx)
	stdin, input := io.Pipe()
	output := newOpenSSHOutput(name + "-ready")
	command := &controlledOpenSSHCommand{
		name: name, stdin: input, outputBuffer: output,
		done: make(chan openSSHCommandOutcome, 1), cancel: cancel,
	}
	go func() {
		result, err := client.RunStreaming(commandCtx, sshworkspace.ExecOptions{
			Stdin: stdin, Stdout: output, Stderr: output,
		}, "sh", "-c", script)
		_ = stdin.Close()
		command.done <- openSSHCommandOutcome{result: result, err: err}
	}()
	return command
}

func (c *controlledOpenSSHCommand) waitReady(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-c.outputBuffer.ready:
	case <-ctx.Done():
		t.Fatalf("%s did not become ready: %v; output=%q", c.name, ctx.Err(), c.output())
	}
}

func (c *controlledOpenSSHCommand) release(t *testing.T, ctx context.Context, input string) {
	t.Helper()
	written := make(chan error, 1)
	go func() {
		_, err := io.WriteString(c.stdin, input)
		closeErr := c.stdin.Close()
		written <- errors.Join(err, closeErr)
	}()
	select {
	case err := <-written:
		if err != nil {
			t.Fatalf("release %s: %v", c.name, err)
		}
	case <-ctx.Done():
		t.Fatalf("release %s: %v", c.name, ctx.Err())
	}
}

func (c *controlledOpenSSHCommand) wait(t *testing.T, ctx context.Context) openSSHCommandOutcome {
	t.Helper()
	select {
	case outcome := <-c.done:
		return outcome
	case <-ctx.Done():
		t.Fatalf("wait for %s: %v; output=%q", c.name, ctx.Err(), c.output())
		return openSSHCommandOutcome{}
	}
}

func (c *controlledOpenSSHCommand) output() string {
	return c.outputBuffer.String()
}

func (c *controlledOpenSSHCommand) close() {
	c.closeOnce.Do(func() {
		c.cancel()
		_ = c.stdin.Close()
	})
}

type openSSHOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	marker string
	ready  chan struct{}
	once   sync.Once
}

func newOpenSSHOutput(marker string) *openSSHOutput {
	return &openSSHOutput{marker: marker, ready: make(chan struct{})}
}

func (b *openSSHOutput) Write(data []byte) (int, error) {
	b.mu.Lock()
	n, err := b.buffer.Write(data)
	containsMarker := strings.Contains(b.buffer.String(), b.marker)
	b.mu.Unlock()
	if containsMarker {
		b.once.Do(func() { close(b.ready) })
	}
	return n, err
}

func (b *openSSHOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

type openSSHWatchBuffer struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	changed chan struct{}
}

func newOpenSSHWatchBuffer() *openSSHWatchBuffer {
	return &openSSHWatchBuffer{changed: make(chan struct{})}
}

func (b *openSSHWatchBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buffer.Write(data)
	close(b.changed)
	b.changed = make(chan struct{})
	return n, err
}

func (b *openSSHWatchBuffer) waitContains(t *testing.T, ctx context.Context, expected string) {
	t.Helper()
	for {
		b.mu.Lock()
		contains := strings.Contains(b.buffer.String(), expected)
		changed := b.changed
		output := b.buffer.String()
		b.mu.Unlock()
		if contains {
			return
		}
		select {
		case <-changed:
		case <-ctx.Done():
			t.Fatalf("terminal output did not contain %q: %v\noutput:\n%s", expected, ctx.Err(), output)
		}
	}
}

func writeTerminalOpenSSH(t *testing.T, terminal *sshworkspace.Terminal, input string) {
	t.Helper()
	if _, err := io.WriteString(terminal, input); err != nil {
		t.Fatalf("write terminal input %q: %v", input, err)
	}
}
