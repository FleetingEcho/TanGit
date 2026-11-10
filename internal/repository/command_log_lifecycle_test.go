package repository

import (
	"strings"
	"testing"

	"github.com/jesseduffield/lazygit/pkg/commands/oscommands"
)

func TestCommandBeginCompletesTheMatchingRunningEntry(t *testing.T) {
	log := NewCommandLog()
	slow := log.Begin("slow-command")
	fast := log.Begin("fast-command")
	fast("fast completed", false)

	entries := log.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if !entries[0].Running || entries[0].Output != "" {
		t.Fatalf("slow command should still be running: %+v", entries[0])
	}
	if entries[1].Running || entries[1].Output != "fast completed" || entries[1].Failed {
		t.Fatalf("fast completion was not recorded: %+v", entries[1])
	}

	slow("slow completed", true)
	entries = log.Entries()
	for i, entry := range entries {
		if entry.Running {
			t.Fatalf("entry %d remained running: %+v", i, entry)
		}
	}
	if !entries[0].Failed || entries[0].Output != "slow completed" {
		t.Fatalf("slow completion was not recorded: %+v", entries[0])
	}
}

func TestConcurrentCommandDoesNotCaptureStreamingGitOutput(t *testing.T) {
	log := NewCommandLog()
	log.logCommand("git fetch", false)
	writer := log.newWriter()
	if _, err := writer.Write([]byte("fetch stdout begins\n")); err != nil {
		t.Fatal(err)
	}
	done := log.BeginUserCommand("custom external command")
	if _, err := writer.Write([]byte("fetch output ends\n")); err != nil {
		t.Fatal(err)
	}
	done("custom stdout\n", false)

	entries := log.Entries()
	for _, entry := range entries {
		if entry.Origin == CommandOriginGit && !strings.Contains(entry.Output, "fetch output ends") {
			t.Fatalf("Git writer lost its own output: %+v", entry)
		}
		if entry.Origin == CommandOriginUser && strings.Contains(entry.Output, "fetch output") {
			t.Fatalf("custom entry inherited Git output: %+v", entry)
		}
	}
}

func TestStreamFragmentsStayAttachedWhileOtherCommandsFinish(t *testing.T) {
	log := NewCommandLog()
	log.logCommand("git credentials", false)
	writer := log.newWriter()
	if _, err := writer.Write([]byte("https://alice:split-")); err != nil {
		t.Fatal(err)
	}
	done := log.BeginUserCommand("gh status")
	done("done\n", false)
	if _, err := writer.Write([]byte("secret@example.com\n")); err != nil {
		t.Fatal(err)
	}
	for _, entry := range log.Entries() {
		if entry.Origin == CommandOriginGit && !strings.Contains(entry.Output, "[redacted]") {
			t.Fatalf("Git stream was not safely redacted: %+v", entry)
		}
		if strings.Contains(entry.Output, "split-secret") {
			t.Fatalf("credential escaped during interleaved writes: %+v", entry)
		}
		if entry.Origin == CommandOriginUser && strings.Contains(entry.Output, "http") {
			t.Fatalf("custom entry inherited a credential fragment: %+v", entry)
		}
	}
}

type commandTransportRunner struct {
	log *CommandLog
}

func (r commandTransportRunner) Run(cmd *oscommands.CmdObj) error {
	writer := r.log.newWriter()
	_, err := writer.Write([]byte("push hook diagnostics\n"))
	return err
}

func (r commandTransportRunner) RunWithOutput(cmd *oscommands.CmdObj) (string, error) {
	writer := r.log.newWriter()
	_, err := writer.Write([]byte("push hook diagnostics\n"))
	return "", err
}

func (r commandTransportRunner) RunWithOutputs(*oscommands.CmdObj) (string, string, error) {
	return "", "", nil
}

func (r commandTransportRunner) RunAndProcessLines(*oscommands.CmdObj, func(string) (bool, error)) error {
	return nil
}

func TestRegisteredRunnerBindsWriterToTheExecutingCommand(t *testing.T) {
	log := NewCommandLog()
	log.logCommand("git --version", false)
	runner := registeredCommandRunner{run: commandTransportRunner{log: log}, log: log}
	cmd := (&oscommands.CmdObjBuilder{}).New([]string{"git", "push"})
	cmd.StreamOutput()
	if err := runner.Run(cmd); err != nil {
		t.Fatal(err)
	}
	entries := log.Entries()
	if len(entries) != 2 {
		t.Fatalf("command count = %d, want exactly one per command, got %+v", len(entries), entries)
	}
	if entries[0].Command != "git --version" || entries[0].Output != "" {
		t.Fatalf("version absorbed another command's output: %+v", entries[0])
	}
	if entries[1].Command != "git push" || entries[1].Output != "push hook diagnostics\n" {
		t.Fatalf("push did not receive only its hook output: %+v", entries[1])
	}
}

func TestRegistrationOwnershipSurvivesAnInterveningLoggedRead(t *testing.T) {
	log := NewCommandLog()
	next := log.nextID
	log.registerStreamingCommand("git push origin main")
	log.logCommand("git rev-parse HEAD", false)
	if next+1 >= log.nextID {
		t.Fatal("intervening command was not registered")
	}
	writer := log.newWriter()
	if _, err := writer.Write([]byte("owned push output\n")); err != nil {
		t.Fatal(err)
	}
	// The callback for the original stream must be suppressed even after a
	// different command advanced currentID.
	log.logCommand("git push origin main", false)
	entries := log.Entries()
	if len(entries) != 2 {
		t.Fatalf("registration produced duplicates: %+v", entries)
	}
	if got := entries[0].Output; got != "owned push output\n" {
		t.Fatalf("push lost its own output: %q", got)
	}
	if entries[1].Output != "" {
		t.Fatalf("intervening read stole the output: %+v", entries[1])
	}
}

func TestCredentialAwareOperationRegistersBeforeWriter(t *testing.T) {
	log := NewCommandLog()
	inner := commandTransportRunner{log: log}
	runner := registeredCommandRunner{run: inner, log: log}
	cmd := (&oscommands.CmdObjBuilder{}).New([]string{"git", "push", "--tags"}).
		PromptOnCredentialRequest(nil)
	if kd := cmd.GetCredentialStrategy(); kd == oscommands.NONE {
		t.Fatal("fixture did not request credential-aware behavior")
	}
	if _, err := runner.RunWithOutput(cmd); err != nil {
		t.Fatal(err)
	}
	entries := log.Entries()
	if len(entries) != 1 || entries[0].Command != "git push --tags" ||
		entries[0].Output != "push hook diagnostics\n" {
		t.Fatalf("credential command lost registration or output: %+v", entries)
	}
	if log.expectedWriterID != 0 || log.silentStream {
		t.Fatal("registration did not close after completion")
	}
}

func TestFailRangeExclusiveAndOutputLimits(t *testing.T) {
	log := NewCommandLog()
	log.Record("previous complete", "", false)
	marker := log.Marker()
	log.FailRange(marker, marker, "validation failed before Git started")
	entries := log.Entries()
	if entries[0].Failed {
		t.Fatal("an earlier command was incorrectly blamed for a later preflight failure")
	}
	if len(entries) != 2 || entries[1].Command != "Git action" || !entries[1].Failed {
		t.Fatalf("expected a separate failure record: %+v", entries)
	}
	start := log.Marker()
	log.Record("huge output", strings.Repeat("x", commandOutputLimit), false)
	log.FailRange(start, log.Marker(), "at-cap error")
	last := log.Entries()[len(log.Entries())-1]
	if last.Command != "huge output" || !last.Failed {
		t.Fatalf("failed command was not selected: %+v", last)
	}
	if len(last.Output) > commandOutputLimit {
		t.Fatalf("log overflowed the output limit: %d", len(last.Output))
	}

	log.logCommand("propagated command", false)
	writer := log.newWriter()
	if _, err := writer.Write([]byte(strings.Repeat("y", commandOutputLimit))); err != nil {
		t.Fatal(err)
	}
	start = log.Marker()
	log.FailRange(start, log.Marker(), "pending partial output error")
	lastEntries := log.Entries()
	pending := lastEntries[len(lastEntries)-1]
	if len(pending.Output) > commandOutputLimit {
		t.Fatalf("pending stream overflowed output limit: %d", len(pending.Output))
	}
	if !pending.Failed {
		t.Fatal("pending stream was not marked failed")
	}
}
