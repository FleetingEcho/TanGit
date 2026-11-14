package app

import (
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/workspace"
)

func captureAgentTerminalOptions(t *testing.T) (<-chan terminal.Options, *atomic.Int32) {
	t.Helper()
	original := newTerminal
	starts := make(chan terminal.Options, terminalSessionLimit+1)
	peers := make(chan monitoredTerminalPeer, terminalSessionLimit+1)
	var calls atomic.Int32
	newTerminal = func(options terminal.Options) (*terminal.Terminal, error) {
		calls.Add(1)
		options.Command = append([]string(nil), options.Command...)
		options.Env = append([]string(nil), options.Env...)
		starts <- options
		return newMonitoredTerminal(options, peers)
	}
	t.Cleanup(func() {
		newTerminal = original
		for {
			select {
			case peer := <-peers:
				_ = peer.Close()
			default:
				return
			}
		}
	})
	return starts, &calls
}

func receiveAgentTerminalOptions(t *testing.T, starts <-chan terminal.Options) terminal.Options {
	t.Helper()
	select {
	case options := <-starts:
		return options
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the agent terminal to start")
		return terminal.Options{}
	}
}

func TestAgentTerminalLaunchUsesExactTargetAndReopensExistingSession(t *testing.T) {
	t.Setenv("SHELL", "/bin/agent-shell")
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{
		WorktreePath: "/worktrees/search task",
		SessionID:    "agent-session-17",
		Profile: agentwork.Profile{
			LaunchCommand: "agent --prompt {promptFile} --worktree {worktree}",
			ResumeCommand: "agent resume {sessionId}",
		},
	})
	starts, terminalCalls := captureAgentTerminalOptions(t)

	h.app.launchAgentTask(task.ID, false)
	options := receiveAgentTerminalOptions(t, starts)
	h.waitFor(t, "the agent launch and terminal startup", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && current.Status == agentwork.StatusRunning &&
			!h.app.agents.mutating[task.ID] && len(h.app.term.sessions) == 1 &&
			!h.app.term.sessions[0].loading
	})
	requestPath, err := store.RequestFile(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantCommand := h.app.agentTaskCommand(task, false, requestPath)
	wantEnvironment := []string{
		"TANGIT_AGENT_OPERATION_ID=" + task.ID,
		"TANGIT_AGENT_REQUEST_FILE=" + requestPath,
		"TANGIT_AGENT_TASK_ID=" + task.ID,
		"TANGIT_AGENT_WORKTREE=" + task.WorktreePath,
	}
	if options.Dir != task.WorktreePath {
		t.Fatalf("terminal directory = %q, want task worktree %q", options.Dir, task.WorktreePath)
	}
	if !slices.Equal(options.Command, wantCommand) {
		t.Fatalf("terminal command = %#v, want %#v", options.Command, wantCommand)
	}
	if !slices.Equal(options.Env, wantEnvironment) {
		t.Fatalf("terminal environment = %#v, want %#v", options.Env, wantEnvironment)
	}
	first := h.app.term.sessions[0]
	if first.agentTaskID != task.ID || first.worktree != task.WorktreePath ||
		!slices.Equal(first.command, wantCommand) || !slices.Equal(first.environment, wantEnvironment) {
		t.Fatalf("terminal session does not retain its agent target: %+v", first)
	}

	// Launching the same task again selects and reveals its existing terminal;
	// it neither creates a duplicate session nor replaces the live command with
	// the resume template.
	h.app.hideTerminalView()
	h.app.launchAgentTask(task.ID, true)
	h.waitFor(t, "the repeated launch mutation", func() bool {
		return !h.app.agents.mutating[task.ID]
	})
	if !h.app.term.panelOpen || len(h.app.term.sessions) != 1 || h.app.term.active != 0 {
		t.Fatalf("reopening did not select the one existing agent terminal: %+v", h.app.term)
	}
	if got := h.app.term.sessions[0]; got.id != first.id || got.groupID != first.groupID ||
		got.agentTaskID != task.ID || !slices.Equal(got.command, wantCommand) {
		t.Fatalf("reopening replaced the agent terminal: before=%+v after=%+v", first, got)
	}
	if got := terminalCalls.Load(); got != 1 {
		t.Fatalf("terminal starts = %d after repeated launch, want 1", got)
	}
}

func TestAgentLaunchRefusalsLeaveTaskReady(t *testing.T) {
	for _, test := range []struct {
		name       string
		remote     bool
		atLimit    bool
		toastTitle string
	}{
		{name: "terminal session limit", atLimit: true, toastTitle: "Terminal limit reached"},
		{name: "remote worktree", remote: true, toastTitle: "Remote agent launch is not available yet"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, store := newAgentTestHarness(t)
			if test.remote {
				h.app.snapshot.Root = "/srv/project"
				h.app.location = workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: "/srv/project"}
			}
			if test.atLimit {
				h.app.term.sessions = make([]terminalSessionState, terminalSessionLimit)
			}
			task := createAgentTestTask(t, h, store, agentwork.Task{
				Profile: agentwork.Profile{LaunchCommand: "agent {worktree}"},
			})
			_, terminalCalls := captureAgentTerminalOptions(t)

			h.app.launchAgentTask(task.ID, false)

			current, ok := h.app.selectedAgentTask()
			if !ok || current.Status != agentwork.StatusReady || h.app.agents.mutating[task.ID] {
				t.Fatalf("refused launch changed or started changing task state: %+v, mutating=%v", current, h.app.agents.mutating)
			}
			persisted, err := store.Get(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Status != agentwork.StatusReady {
				t.Fatalf("persisted task status = %q after refused launch, want %q", persisted.Status, agentwork.StatusReady)
			}
			if got := terminalCalls.Load(); got != 0 {
				t.Fatalf("refused launch started %d terminals, want 0", got)
			}
			if test.remote && len(h.app.term.sessions) != 0 {
				t.Fatalf("remote refusal created terminal sessions: %+v", h.app.term.sessions)
			}
			if test.atLimit && len(h.app.term.sessions) != terminalSessionLimit {
				t.Fatalf("session-limit refusal changed terminal count to %d", len(h.app.term.sessions))
			}
			if len(h.app.pendingToasts) != 1 || h.app.pendingToasts[0].title != test.toastTitle {
				t.Fatalf("refusal notification = %+v, want title %q", h.app.pendingToasts, test.toastTitle)
			}
		})
	}
}

func TestAgentTerminalFastExitWaitsForInitialLaunchMutation(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{
		Profile: agentwork.Profile{LaunchCommand: "agent {worktree}"},
	})
	starts, _ := captureAgentTerminalOptions(t)
	h.app.launchAgentTask(task.ID, false)
	options := receiveAgentTerminalOptions(t, starts)
	if !h.app.agents.mutating[task.ID] {
		t.Fatal("agent did not reach its initial launch mutation")
	}

	options.OnExit(0)
	h.waitFor(t, "the queued fast agent exit", func() bool {
		current, ok := h.app.selectedAgentTask()
		_, pending := h.app.agents.processCompletions[task.ID]
		return ok && current.Status == agentwork.StatusExited && !pending &&
			!h.app.agents.mutating[task.ID] && len(h.app.term.sessions) == 0
	})
	persisted, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != agentwork.StatusExited || persisted.ExitCode == nil || *persisted.ExitCode != 0 {
		t.Fatalf("fast agent exit was dropped: status=%q exit=%v", persisted.Status, persisted.ExitCode)
	}
}

func TestAgentTerminalExitPersistsProcessOutcome(t *testing.T) {
	for _, test := range []struct {
		name       string
		code       int
		wantStatus agentwork.Status
	}{
		{name: "zero is exited awaiting explicit review", code: 0, wantStatus: agentwork.StatusExited},
		{name: "nonzero is failed", code: 17, wantStatus: agentwork.StatusFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("SHELL", "/bin/agent-shell")
			h, store := newAgentTestHarness(t)
			task := createAgentTestTask(t, h, store, agentwork.Task{
				Profile: agentwork.Profile{LaunchCommand: "agent {worktree}"},
			})
			starts, _ := captureAgentTerminalOptions(t)
			h.app.launchAgentTask(task.ID, false)
			options := receiveAgentTerminalOptions(t, starts)
			h.waitFor(t, "the running agent", func() bool {
				current, ok := h.app.selectedAgentTask()
				return ok && current.Status == agentwork.StatusRunning &&
					!h.app.agents.mutating[task.ID] && len(h.app.term.sessions) == 1 &&
					!h.app.term.sessions[0].loading
			})
			if options.OnExit == nil {
				t.Fatal("agent terminal has no exit callback")
			}

			options.OnExit(test.code)
			h.waitFor(t, "the persisted process outcome", func() bool {
				current, ok := h.app.selectedAgentTask()
				return ok && current.Status == test.wantStatus &&
					!h.app.agents.mutating[task.ID] && len(h.app.term.sessions) == 0
			})
			persisted, err := store.Get(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Status != test.wantStatus || persisted.ExitCode == nil || *persisted.ExitCode != test.code {
				t.Fatalf("persisted outcome = status %q, exit %v; want status %q, exit %d", persisted.Status, persisted.ExitCode, test.wantStatus, test.code)
			}
			if test.code == 0 && persisted.Status == agentwork.StatusReadyForReview {
				t.Fatal("a zero process exit advanced the task to ReadyForReview without explicit review")
			}
		})
	}
}
