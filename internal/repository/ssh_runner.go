package repository

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	goerrors "github.com/go-errors/errors"
	"github.com/jesseduffield/lazygit/pkg/commands/oscommands"
	"github.com/jesseduffield/lazygit/pkg/utils"
	"github.com/sirupsen/logrus"

	"lazygitapp/internal/sshworkspace"
)

// sshCommandExecutor is the one SSH operation needed by the Lazygit runner.
// The small interface also makes argv, environment and stream behavior unit
// testable without an SSH server.
type sshCommandExecutor interface {
	RunStreaming(context.Context, sshworkspace.ExecOptions, string, ...string) (sshworkspace.ExecResult, error)
}

type sshCmdObjRunner struct {
	client                sshCommandExecutor
	log                   *logrus.Entry
	logCmd                func(string, bool)
	newWriter             func() io.Writer
	repositoryEnvironment map[string]string
}

var _ oscommands.ICmdObjRunner = (*sshCmdObjRunner)(nil)

// remoteExitError retains the transport error while exposing the remote
// process status to Git helpers which treat diff's status 1 as a normal result.
type remoteExitError struct {
	result sshworkspace.ExecResult
	err    error
}

func (e *remoteExitError) Error() string { return e.err.Error() }
func (e *remoteExitError) Unwrap() error { return e.err }
func (e *remoteExitError) ExitCode() int { return e.result.ExitCode }

func commandExitCode(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) {
		return exit.ExitCode(), true
	}
	return 0, false
}

func (r *sshCmdObjRunner) prepare(command *oscommands.CmdObj) {
	if command.ShouldLog() {
		r.logCmd(command.ToString(), true)
	}
	r.log.WithField("command", command.ToString()).Debug("RunCommand")
	if command.GetCredentialStrategy() != oscommands.NONE {
		// Lazygit's terminal runner can answer prompts through its GUI IO. This
		// repository API is unattended, so make every credential-aware command
		// fail rather than leaving a remote Git process waiting forever.
		command.AddEnvVars("LANG=C", "LC_ALL=C", "LC_MESSAGES=C", "GIT_TERMINAL_PROMPT=0")
	}
}

func (r *sshCmdObjRunner) environment(command *oscommands.CmdObj) ([]string, error) {
	var values []string
	for _, entry := range command.GetEnvVars() {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || !safeRemoteEnvironment(name) {
			continue
		}
		if name == "GIT_DIR" || name == "GIT_WORK_TREE" {
			allowed, discovered := r.repositoryEnvironment[name]
			if !discovered || value != allowed {
				continue
			}
		}
		if strings.ContainsRune(value, '\x00') {
			return nil, fmt.Errorf("remote environment variable %s contains NUL", name)
		}
		values = append(values, name+"="+value)
	}
	return values, nil
}

// CmdObjBuilder starts with the GUI process's complete local environment. Do
// not disclose it to the SSH server. Only variables which describe Git or the
// command's locale/terminal are forwarded; all remote account and PATH setup is
// left to the server. AddEnvVars uses the same names for deliberate per-command
// overrides (for example GIT_INDEX_FILE and GIT_SEQUENCE_EDITOR).
func safeRemoteEnvironment(name string) bool {
	if name == "" {
		return false
	}
	if name == "LANG" || name == "TERM" || name == "COLORTERM" || name == "TZ" ||
		strings.HasPrefix(name, "LC_") || strings.HasPrefix(name, "GIT_") {
		for i, r := range name {
			if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9') {
				return false
			}
		}
		return name != ""
	}
	return false
}

func (r *sshCmdObjRunner) execute(ctx context.Context, command *oscommands.CmdObj, stdin io.Reader, stdout, stderr io.Writer) error {
	args := command.Args()
	if len(args) == 0 || args[0] == "" {
		return fmt.Errorf("remote command has no executable")
	}
	environment, err := r.environment(command)
	if err != nil {
		return err
	}
	words := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		words = append(words, entry)
	}
	if len(words) > 0 {
		words = append([]string{"env"}, words...)
	}
	words = append(words, args...)
	result, err := r.client.RunStreaming(ctx, sshworkspace.ExecOptions{
		Dir: command.GetCmd().Dir, Stdin: stdin, Stdout: stdout, Stderr: stderr,
	}, words[0], words[1:]...)
	if err != nil {
		return &remoteExitError{result: result, err: err}
	}
	return nil
}

func sanitisedRemoteOutput(output []byte, err error) (string, error) {
	text := string(output)
	if err != nil {
		if text == "" {
			return "", utils.WrapError(err)
		}
		return text, goerrors.New(text)
	}
	return text, nil
}

type lockedRemoteBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *lockedRemoteBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func (b *lockedRemoteBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

func (r *sshCmdObjRunner) Run(command *oscommands.CmdObj) error {
	if command.Mutex() != nil {
		command.Mutex().Lock()
		defer command.Mutex().Unlock()
	}
	r.prepare(command)
	if command.ShouldStreamOutput() {
		return r.runAndStream(command)
	}
	var output lockedRemoteBuffer
	err := r.execute(context.Background(), command, command.GetCmd().Stdin, &output, &output)
	_, err = sanitisedRemoteOutput([]byte(output.String()), err)
	return err
}

func (r *sshCmdObjRunner) RunWithOutput(command *oscommands.CmdObj) (string, error) {
	if command.Mutex() != nil {
		command.Mutex().Lock()
		defer command.Mutex().Unlock()
	}
	r.prepare(command)
	if command.ShouldStreamOutput() {
		return "", r.runAndStream(command)
	}
	var stdout, stderr bytes.Buffer
	err := r.execute(context.Background(), command, command.GetCmd().Stdin, &stdout, &stderr)
	output := append(stdout.Bytes(), stderr.Bytes()...)
	return sanitisedRemoteOutput(output, err)
}

func (r *sshCmdObjRunner) RunWithOutputs(command *oscommands.CmdObj) (string, string, error) {
	if command.Mutex() != nil {
		command.Mutex().Lock()
		defer command.Mutex().Unlock()
	}
	r.prepare(command)
	if command.ShouldStreamOutput() {
		return "", "", r.runAndStream(command)
	}
	var stdout, stderr bytes.Buffer
	err := r.execute(context.Background(), command, command.GetCmd().Stdin, &stdout, &stderr)
	stderrText, err := sanitisedRemoteOutput(stderr.Bytes(), err)
	return stdout.String(), stderrText, err
}

func (r *sshCmdObjRunner) runAndStream(command *oscommands.CmdObj) error {
	writer := r.newWriter()
	var suppressed lockedRemoteBuffer
	if command.ShouldSuppressOutputUnlessError() {
		writer = &suppressed
	}
	var stream lockedRemoteWriter
	stream.writer = writer
	var stderr lockedRemoteBuffer
	var stdout lockedRemoteBuffer
	err := r.execute(context.Background(), command, command.GetCmd().Stdin,
		io.MultiWriter(&stream, &stdout), io.MultiWriter(&stream, &stderr))
	if err == nil || command.ShouldIgnoreEmptyError() && stderr.String() == "" {
		return nil
	}
	if command.ShouldSuppressOutputUnlessError() {
		_, _ = io.WriteString(r.newWriter(), suppressed.String())
	}
	if text := stderr.String(); text != "" {
		return goerrors.New(text)
	}
	if text := stdout.String(); text != "" {
		return goerrors.New(text)
	}
	return goerrors.New("Command exited with non-zero exit code, but no output")
}

type lockedRemoteWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *lockedRemoteWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(p)
}

func (r *sshCmdObjRunner) RunAndProcessLines(command *oscommands.CmdObj, onLine func(string) (bool, error)) error {
	if command.Mutex() != nil {
		command.Mutex().Lock()
		defer command.Mutex().Unlock()
	}
	r.prepare(command)
	if command.GetCredentialStrategy() != oscommands.NONE {
		return goerrors.New("cannot process remote command lines with credential handling")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	var stderr lockedRemoteBuffer
	go func() {
		// Only stdout is parsed. Diagnostics on stderr must never become Git
		// records, warnings, or commit fields in the line consumer.
		err := r.execute(ctx, command, command.GetCmd().Stdin, writer, &stderr)
		_ = writer.Close()
		done <- err
	}()
	scanner := bufio.NewScanner(reader)
	scanner.Split(utils.ScanLinesAndTruncateWhenLongerThanBuffer(bufio.MaxScanTokenSize))
	for scanner.Scan() {
		stop, err := onLine(scanner.Text())
		if err != nil {
			cancel()
			_ = reader.CloseWithError(err)
			<-done
			return err
		}
		if stop {
			cancel()
			_ = reader.Close()
			<-done
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		cancel()
		_ = reader.CloseWithError(err)
		<-done
		return err
	}
	_ = reader.Close()
	if err := <-done; err != nil {
		_, sanitized := sanitisedRemoteOutput(stderr.Bytes(), err)
		return sanitized
	}
	return nil
}
