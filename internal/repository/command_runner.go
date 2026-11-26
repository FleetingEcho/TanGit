package repository

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/jesseduffield/lazygit/pkg/commands/oscommands"
)

// backgroundCmdBuilder preserves Lazygit's command construction and execution
// while applying the platform's GUI-safe child-process attributes immediately
// before every run. Doing this in the runner also covers commands that Git
// loaders construct internally after a repository has opened.
func backgroundCmdBuilder(builder *oscommands.CmdObjBuilder) *oscommands.CmdObjBuilder {
	return builder.CloneWithNewRunner(func(inner oscommands.ICmdObjRunner) oscommands.ICmdObjRunner {
		return &backgroundCmdRunner{inner: inner}
	})
}

// backgroundExec covers the few commands that need exec.Cmd's CombinedOutput
// directly because Git uses exit status 1 for an ordinary diff result. Those
// commands intentionally bypass CmdObjRunner, so configure them at extraction.
func backgroundExec(command *oscommands.CmdObj) *exec.Cmd {
	cmd := command.GetCmd()
	configureBackgroundCommand(cmd)
	return cmd
}

// commandContext preserves Lazygit's argv, repository directory and command
// runner while replacing the local exec.Cmd with one whose cancellation also
// kills Git's process group. Remote builders keep their normal command object;
// runWithOutputContext passes ctx to the SSH runner instead.
func (s *gitSession) commandContext(ctx context.Context, args []string) (*oscommands.CmdObj, error) {
	command := s.cmd.New(args)
	if s.remote != nil {
		return command, nil
	}
	original := command.GetCmd()
	contextual := exec.CommandContext(ctx, original.Path, original.Args[1:]...)
	contextual.Dir = original.Dir
	contextual.Env = append([]string(nil), original.Env...)
	contextual.Stdin = original.Stdin
	configureCloneCommand(contextual)
	contextual.WaitDelay = 5 * time.Second
	// CmdObjBuilder's Git wrapper intentionally does not expose NewFromCmd,
	// but GetCmd returns the same fresh, never-started exec.Cmd owned by the
	// command. Replace that empty value before Start so every Lazygit runner
	// and command option remains attached to the cancellable process.
	*original = *contextual
	return command, nil
}

func (s *gitSession) runWithOutputContext(ctx context.Context, command *oscommands.CmdObj) (string, error) {
	var output string
	var err error
	if s.remote != nil {
		output, err = s.remote.runWithOutputContext(ctx, command)
	} else {
		output, err = command.RunWithOutput()
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return "", contextErr
	}
	return output, err
}

// output preserves stdout as bytes, including NULs and invalid UTF-8 in Git
// objects and binary patches. Remote execution never passes through a string.
func (s *gitSession) output(command *oscommands.CmdObj) ([]byte, []byte, error) {
	if s.remote != nil {
		var stdout, stderr bytes.Buffer
		s.remote.prepare(command)
		err := s.remote.execute(context.Background(), command, command.GetCmd().Stdin, &stdout, &stderr)
		return stdout.Bytes(), stderr.Bytes(), err
	}
	cmd := backgroundExec(command)
	stdout, err := cmd.Output()
	if exit, ok := err.(*exec.ExitError); ok {
		return stdout, exit.Stderr, err
	}
	return stdout, nil, err
}

func (s *gitSession) saveStagedChanges(message string) error {
	if s.sshClient != nil && s.git.Version.IsOlderThan(2, 35, 0) {
		// Lazygit's compatibility implementation evaluates host shell commands
		// and files. Refuse it rather than touching similarly named GUI paths.
		return fmt.Errorf("stash only staged changes over SSH requires Git 2.35 or newer")
	}
	return s.git.Stash.SaveStagedChanges(message)
}

func (s *gitSession) syncRemotePath(name string) error {
	if s.sshClient == nil {
		return s.syncDirectory(name)
	}
	_, err := s.sshClient.Run(context.Background(), "", "sync", "-d", "--", name)
	return err
}

func (s *gitSession) combinedOutput(command *oscommands.CmdObj) ([]byte, error) {
	if s.remote != nil {
		var stdout, stderr bytes.Buffer
		s.remote.prepare(command)
		err := s.remote.execute(context.Background(), command, command.GetCmd().Stdin, &stdout, &stderr)
		return append(stdout.Bytes(), stderr.Bytes()...), err
	}
	return backgroundExec(command).CombinedOutput()
}

type backgroundCmdRunner struct {
	inner oscommands.ICmdObjRunner
}

func (r *backgroundCmdRunner) prepare(command *oscommands.CmdObj) {
	cmd := command.GetCmd()
	configureBackgroundCommand(cmd)
	if cmd.Cancel != nil {
		// gitCmdObjRunner clones exec.Cmd before retrying. exec.CommandContext's
		// Cancel closure captures the original Cmd, so a shallow clone would
		// kill a nil Process instead of the clone actually being executed.
		// Rebind cancellation to this clone while preserving process-group kill.
		configureCloneCommand(cmd)
	}
}

func (r *backgroundCmdRunner) Run(command *oscommands.CmdObj) error {
	r.prepare(command)
	return r.inner.Run(command)
}

func (r *backgroundCmdRunner) RunWithOutput(command *oscommands.CmdObj) (string, error) {
	r.prepare(command)
	return r.inner.RunWithOutput(command)
}

func (r *backgroundCmdRunner) RunWithOutputs(command *oscommands.CmdObj) (string, string, error) {
	r.prepare(command)
	return r.inner.RunWithOutputs(command)
}

func (r *backgroundCmdRunner) RunAndProcessLines(command *oscommands.CmdObj, onLine func(string) (bool, error)) error {
	r.prepare(command)
	return r.inner.RunAndProcessLines(command, onLine)
}
