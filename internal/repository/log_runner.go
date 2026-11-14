package repository

import (
	"strings"
	"sync"

	"github.com/jesseduffield/lazygit/pkg/commands/oscommands"
)

// registeredCommandRunner gives streamed output an unambiguous owner. The
// GUI hook newWriter has no command argument, so a writer registered late
// cannot be associated merely by "the last command name". Register the real
// CmdObj.Args here and serialize streamed command execution: this covers both
// direct git builders and the SSH runner, including hook stderr/stdout.
type registeredCommandRunner struct {
	run      oscommands.ICmdObjRunner
	log      *CommandLog
	streamMu sync.Mutex
}

func (r *registeredCommandRunner) start(cmd *oscommands.CmdObj) func() {
	// Lazygit treats credential-aware commands as streams even without an
	// explicit StreamOutput flag. They use runAndStreamAux and require the
	// same owner token and serialization as explicitly streamed commands.
	if cmd.ShouldStreamOutput() || cmd.GetCredentialStrategy() != oscommands.NONE {
		r.streamMu.Lock()
		if cmd.ShouldLog() {
			r.log.registerStreamingCommand(repoCommandText(cmd.Args()))
		} else {
			r.log.setSilentStream(true)
		}
		return func() {
			r.log.endStreamingCommand()
			r.streamMu.Unlock()
		}
	}
	// Ordinary commands keep their existing library log callback. Logging a
	// second registration here would produce an entry before every command.
	return func() {}
}

func (r *registeredCommandRunner) Run(cmd *oscommands.CmdObj) error {
	finish := r.start(cmd)
	defer finish()
	return r.run.Run(cmd)
}

func (r *registeredCommandRunner) RunWithOutput(cmd *oscommands.CmdObj) (string, error) {
	finish := r.start(cmd)
	defer finish()
	return r.run.RunWithOutput(cmd)
}

func (r *registeredCommandRunner) RunWithOutputs(cmd *oscommands.CmdObj) (string, string, error) {
	finish := r.start(cmd)
	defer finish()
	return r.run.RunWithOutputs(cmd)
}

func (r *registeredCommandRunner) RunAndProcessLines(cmd *oscommands.CmdObj, onLine func(string) (bool, error)) error {
	finish := r.start(cmd)
	defer finish()
	return r.run.RunAndProcessLines(cmd, onLine)
}

// repoCommandText deliberately matches CmdObj.ToString, so the later lazygit
// log-callback is recognized as the same command rather than duplicated.
func repoCommandText(arguments []string) string {
	words := append([]string(nil), arguments...)
	for i, word := range words {
		if strings.Contains(word, " ") {
			words[i] = `"` + word + `"`
		}
	}
	return strings.Join(words, " ")
}
