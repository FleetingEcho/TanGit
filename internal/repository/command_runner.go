package repository

import (
	"os/exec"

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

type backgroundCmdRunner struct {
	inner oscommands.ICmdObjRunner
}

func (r *backgroundCmdRunner) prepare(command *oscommands.CmdObj) {
	configureBackgroundCommand(command.GetCmd())
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
