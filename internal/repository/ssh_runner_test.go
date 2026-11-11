package repository

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jesseduffield/lazygit/pkg/commands/oscommands"
	"github.com/jesseduffield/lazygit/pkg/common"
	"github.com/jesseduffield/lazygit/pkg/config"
	"github.com/sasha-s/go-deadlock"
	"github.com/sirupsen/logrus"

	"lazygitapp/internal/sshworkspace"
)

type fakeSSHExecCall struct {
	options sshworkspace.ExecOptions
	name    string
	args    []string
	stdin   string
}

type fakeSSHExecutor struct {
	stdout string
	stderr string
	result sshworkspace.ExecResult
	err    error
	hook   func(context.Context, sshworkspace.ExecOptions) (sshworkspace.ExecResult, error)
	mu     sync.Mutex
	calls  []fakeSSHExecCall
}

func (f *fakeSSHExecutor) RunStreaming(ctx context.Context, options sshworkspace.ExecOptions, name string, args ...string) (sshworkspace.ExecResult, error) {
	var stdin []byte
	if options.Stdin != nil {
		stdin, _ = io.ReadAll(options.Stdin)
	}
	f.mu.Lock()
	f.calls = append(f.calls, fakeSSHExecCall{
		options: options, name: name, args: append([]string(nil), args...), stdin: string(stdin),
	})
	f.mu.Unlock()
	if f.hook != nil {
		return f.hook(ctx, options)
	}
	_, _ = io.WriteString(options.Stdout, f.stdout)
	_, _ = io.WriteString(options.Stderr, f.stderr)
	return f.result, f.err
}

func sshRunnerForTest(executor sshCommandExecutor) *sshCmdObjRunner {
	return &sshCmdObjRunner{
		client: executor, log: logrus.NewEntry(logrus.New()),
		logCmd: func(string, bool) {}, newWriter: func() io.Writer { return io.Discard },
	}
}

func sshCommandForTest(runner *sshCmdObjRunner, args ...string) *oscommands.CmdObj {
	cmn := common.NewDummyCommon()
	guiIO := oscommands.NewGuiIO(
		cmn.Log, func(string, bool) {}, func() io.Writer { return io.Discard },
		func(oscommands.CredentialType) <-chan string { return nil },
	)
	osCommand := oscommands.NewOSCommand(cmn, config.NewDummyAppConfig(), oscommands.GetPlatform(), guiIO)
	builder := osCommand.Cmd.CloneWithNewRunner(func(oscommands.ICmdObjRunner) oscommands.ICmdObjRunner {
		return runner
	})
	return builder.New(args)
}

func TestSSHRunnerUsesRemoteArgvFilteredEnvironmentAndStreams(t *testing.T) {
	executor := &fakeSSHExecutor{stdout: "standard output", stderr: "diagnostic"}
	runner := sshRunnerForTest(executor)
	command := sshCommandForTest(runner, "git", "diff", "--", "file with spaces")
	command.SetWd("/remote/repository").SetStdin("requested input")
	command.AddEnvVars(
		"PATH=/do/not/disclose", "HOME=/also/private", "SECRET=value",
		"GIT_INDEX_FILE=/tmp/index file", "LANG=C", "LC_MESSAGES=C", "TERM=xterm-256color",
	)

	stdout, stderr, err := command.RunWithOutputs()
	if err != nil || stdout != "standard output" || stderr != "diagnostic" {
		t.Fatalf("RunWithOutputs() = %q, %q, %v", stdout, stderr, err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("calls = %d", len(executor.calls))
	}
	call := executor.calls[0]
	if call.options.Dir != "/remote/repository" || call.stdin != "requested input" {
		t.Fatalf("remote options = %+v, stdin = %q", call.options, call.stdin)
	}
	if call.name != "env" || len(call.args) < 4 {
		t.Fatalf("remote argv = %q %q", call.name, call.args)
	}
	joined := strings.Join(call.args, "\n")
	for _, forbidden := range []string{"PATH=", "HOME=", "SECRET="} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("local environment leaked in %q", call.args)
		}
	}
	for _, wanted := range []string{
		"GIT_INDEX_FILE=/tmp/index file",
		"LANG=C",
		"LC_MESSAGES=C",
		"TERM=xterm-256color",
		"git", "diff", "--", "file with spaces",
	} {
		found := false
		for _, arg := range call.args {
			if arg == wanted {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("remote argv %q does not contain %q", call.args, wanted)
		}
	}
}

func TestSSHRunnerFiltersRepositoryLocationEnvironment(t *testing.T) {
	executor := &fakeSSHExecutor{}
	runner := sshRunnerForTest(executor)
	command := sshCommandForTest(runner, "git", "status")
	command.AddEnvVars("GIT_DIR=/gui/repo/.git", "GIT_WORK_TREE=/gui/repo", "GIT_INDEX_FILE=/remote/index")
	if _, _, err := command.RunWithOutputs(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(executor.calls[0].args, "\n")
	if strings.Contains(joined, "/gui/") || !strings.Contains(joined, "GIT_INDEX_FILE=/remote/index") {
		t.Fatalf("unexpected inherited repository environment: %q", executor.calls[0].args)
	}

	runner.repositoryEnvironment = map[string]string{
		"GIT_DIR": "/remote/repo.git", "GIT_WORK_TREE": "/remote/worktree",
	}
	command = sshCommandForTest(runner, "git", "status")
	command.AddEnvVars(
		"GIT_DIR=/remote/repo.git", "GIT_WORK_TREE=/wrong/worktree",
		"GIT_DIR=/wrong/repo.git", // even a later duplicate is rejected independently
	)
	executor.calls = nil
	if _, _, err := command.RunWithOutputs(); err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(executor.calls[0].args, "\n")
	if !strings.Contains(joined, "GIT_DIR=/remote/repo.git") || strings.Contains(joined, "/wrong/") || strings.Contains(joined, "GIT_WORK_TREE=/wrong/worktree") {
		t.Fatalf("discovered remote environment was not matched exactly: %q", executor.calls[0].args)
	}
}

func TestSSHRunnerDisablesCredentialPromptsForEveryOutputMode(t *testing.T) {
	for _, run := range []struct {
		name string
		run  func(*oscommands.CmdObj) error
	}{
		{name: "run", run: func(command *oscommands.CmdObj) error { return command.Run() }},
		{name: "combined", run: func(command *oscommands.CmdObj) error { _, err := command.RunWithOutput(); return err }},
		{name: "separate", run: func(command *oscommands.CmdObj) error { _, _, err := command.RunWithOutputs(); return err }},
	} {
		t.Run(run.name, func(t *testing.T) {
			executor := &fakeSSHExecutor{}
			runner := sshRunnerForTest(executor)
			command := sshCommandForTest(runner, "git", "fetch").FailOnCredentialRequest()
			if err := run.run(command); err != nil {
				t.Fatal(err)
			}
			if len(executor.calls) != 1 || !strings.Contains(strings.Join(executor.calls[0].args, " "), "GIT_TERMINAL_PROMPT=0") {
				t.Fatalf("credential policy missing from %#v", executor.calls)
			}
		})
	}
}

func TestSSHRunnerProcessesOnlyStdoutAndReturnsRemoteErrors(t *testing.T) {
	executor := &fakeSSHExecutor{stdout: "one\ntwo\n", stderr: "not a record\n"}
	runner := sshRunnerForTest(executor)
	var lines []string
	if err := sshCommandForTest(runner, "git", "log").RunAndProcessLines(func(line string) (bool, error) {
		lines = append(lines, line)
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, ",") != "one,two" {
		t.Fatalf("processed lines = %q", lines)
	}

	executor = &fakeSSHExecutor{stderr: "remote failure\n", result: sshworkspace.ExecResult{ExitCode: 9}, err: errors.New("exit status 9")}
	runner = sshRunnerForTest(executor)
	err := sshCommandForTest(runner, "git", "log").RunAndProcessLines(func(string) (bool, error) {
		return false, nil
	})
	if err == nil || !strings.Contains(err.Error(), "remote failure") {
		t.Fatalf("line-processing error = %v", err)
	}
}

func TestSSHRunnerEarlyLineStopCancelsCommand(t *testing.T) {
	cancelled := make(chan struct{})
	executor := &fakeSSHExecutor{hook: func(ctx context.Context, options sshworkspace.ExecOptions) (sshworkspace.ExecResult, error) {
		_, _ = io.WriteString(options.Stdout, "first\nsecond\n")
		<-ctx.Done()
		close(cancelled)
		return sshworkspace.ExecResult{ExitCode: -1}, ctx.Err()
	}}
	runner := sshRunnerForTest(executor)
	if err := sshCommandForTest(runner, "git", "log").RunAndProcessLines(func(string) (bool, error) {
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("remote command was not cancelled")
	}
}

func TestSSHRunnerHoldsCommandMutex(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	executor := &fakeSSHExecutor{hook: func(ctx context.Context, options sshworkspace.ExecOptions) (sshworkspace.ExecResult, error) {
		enteredOnce.Do(func() { close(entered) })
		<-release
		return sshworkspace.ExecResult{}, nil
	}}
	runner := sshRunnerForTest(executor)
	mutex := &deadlock.Mutex{}
	first := sshCommandForTest(runner, "git", "first").WithMutex(mutex)
	second := sshCommandForTest(runner, "git", "second").WithMutex(mutex)
	firstDone := make(chan error, 1)
	go func() { firstDone <- first.Run() }()
	<-entered
	secondDone := make(chan error, 1)
	go func() { secondDone <- second.Run() }()
	select {
	case err := <-secondDone:
		t.Fatalf("second command entered without the mutex: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	// The second call uses the same hook, so its entry and release are complete.
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

func TestSafeRemoteEnvironmentRejectsMalformedNames(t *testing.T) {
	for _, name := range []string{"", "9GITTEST", "GIT-BAD", "GIT\nBAD"} {
		if safeRemoteEnvironment(name) {
			t.Errorf("accepted %q", name)
		}
	}
	for _, name := range []string{"GIT_DIR", "LC_ALL", "LANG", "TZ"} {
		if !safeRemoteEnvironment(name) {
			t.Errorf("rejected %q", name)
		}
	}
}

func TestCombinedOutputPreservesRemoteExitStatus(t *testing.T) {
	executor := &fakeSSHExecutor{result: sshworkspace.ExecResult{ExitCode: 1}, err: errors.New("exit status 1")}
	runner := sshRunnerForTest(executor)
	var output bytes.Buffer
	err := runner.execute(context.Background(), sshCommandForTest(runner, "git", "diff"), strings.NewReader("input"), &output, &output)
	if code, ok := commandExitCode(err); !ok || code != 1 {
		t.Fatalf("commandExitCode(%v) = %d, %v", err, code, ok)
	}
}
