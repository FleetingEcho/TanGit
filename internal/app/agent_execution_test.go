package app

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/workspace"
)

func TestAgentTestsUseIndependentTerminalAndPersistPassedExit(t *testing.T) {
	t.Setenv("SHELL", "/bin/agent-test-shell")
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{
		WorktreePath: "/worktrees/task with spaces",
		Profile: agentwork.Profile{
			LaunchCommand: "agent launch {worktree}",
			ResumeCommand: "agent resume {sessionId}",
			SetupCommand:  "setup-only-for-agent-launch",
			TestCommand:   "test-bin --worktree {worktree} --task {{task}} --prompt {promptFile}",
		},
	})
	starts, terminalCalls := captureAgentTerminalOptions(t)

	h.app.launchAgentTask(task.ID, false)
	agentOptions := receiveAgentTerminalOptions(t, starts)
	h.waitFor(t, "the independent agent terminal", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && current.Status == agentwork.StatusRunning &&
			!h.app.agents.mutating[task.ID] && len(h.app.term.sessions) == 1 &&
			!h.app.term.sessions[0].loading
	})

	h.app.runAgentTests(task.ID)
	testOptions := receiveAgentTerminalOptions(t, starts)
	h.waitFor(t, "the running test result", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && current.LastTest != nil && current.LastTest.Status == agentwork.TestStatusRunning &&
			!h.app.agents.mutating[task.ID] && len(h.app.term.sessions) == 2 &&
			!h.app.term.sessions[0].loading && !h.app.term.sessions[1].loading
	})
	requestPath, err := store.RequestFile(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantCommand := shellCommandForTerminal(h.app.expandAgentTaskCommand(task, task.Profile.TestCommand, requestPath))
	if agentOptions.Dir != task.WorktreePath || testOptions.Dir != task.WorktreePath {
		t.Fatalf("agent/test directories = %q/%q, want %q", agentOptions.Dir, testOptions.Dir, task.WorktreePath)
	}
	if !slices.Equal(testOptions.Command, wantCommand) {
		t.Fatalf("test command = %#v, want %#v", testOptions.Command, wantCommand)
	}
	wantAgentEnvironment := agentTaskEnvironment(task, requestPath, task.ID)
	wantTestEnvironment := agentTaskEnvironment(task, requestPath, agentTestOperationID(task.ID))
	if !slices.Equal(agentOptions.Env, wantAgentEnvironment) || !slices.Equal(testOptions.Env, wantTestEnvironment) {
		t.Fatalf("test environment = %#v (want %#v), agent environment = %#v (want %#v)",
			testOptions.Env, wantTestEnvironment, agentOptions.Env, wantAgentEnvironment)
	}
	if h.app.term.sessions[0].agentTaskID != task.ID ||
		h.app.term.sessions[1].agentTaskID != agentTestOperationID(task.ID) {
		t.Fatalf("agent and test terminals share an operation identity: %+v", h.app.term.sessions)
	}
	running, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	startedAt := running.LastTest.StartedAt
	if startedAt.IsZero() || running.LastTest.Command != wantCommand[2] || running.LastTest.ExitCode != nil {
		t.Fatalf("running test result = %#v", running.LastTest)
	}

	// A second Run selects the existing test process and does not reset its
	// durable start timestamp or start another terminal.
	h.app.runAgentTests(task.ID)
	if got := terminalCalls.Load(); got != 2 {
		t.Fatalf("duplicate test run started terminals: got %d total starts, want 2", got)
	}
	unchanged, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged.LastTest.StartedAt.Equal(startedAt) || unchanged.LastTest.Status != agentwork.TestStatusRunning {
		t.Fatalf("duplicate test run replaced its result: %#v", unchanged.LastTest)
	}

	// The agent can finish without disturbing the test process or its result.
	agentOptions.OnExit(0)
	h.waitFor(t, "the agent exit while tests remain running", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && current.Status == agentwork.StatusExited && current.LastTest != nil &&
			current.LastTest.Status == agentwork.TestStatusRunning && !h.app.agents.mutating[task.ID] &&
			len(h.app.term.sessions) == 1 && h.app.term.sessions[0].agentTaskID == agentTestOperationID(task.ID)
	})

	testOptions.OnExit(0)
	h.waitFor(t, "the passed test result", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && current.LastTest != nil && current.LastTest.Status == agentwork.TestStatusPassed &&
			!h.app.agents.mutating[task.ID] && len(h.app.term.sessions) == 0
	})
	persisted, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != agentwork.StatusExited || persisted.ExitCode == nil || *persisted.ExitCode != 0 {
		t.Fatalf("test exit changed the agent outcome: status=%q exit=%v", persisted.Status, persisted.ExitCode)
	}
	if persisted.LastTest == nil || persisted.LastTest.ExitCode == nil || *persisted.LastTest.ExitCode != 0 ||
		!persisted.LastTest.StartedAt.Equal(startedAt) || persisted.LastTest.FinishedAt.Before(startedAt) {
		t.Fatalf("passed test result = %#v", persisted.LastTest)
	}
	if persisted.Status == agentwork.StatusReadyForReview {
		t.Fatal("passing tests inferred review readiness")
	}
}

func TestAgentTestsFailedExitChangesOnlyTestResult(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{
		Status:  agentwork.StatusReadyForReview,
		Profile: agentwork.Profile{TestCommand: "go test ./..."},
	})
	starts, _ := captureAgentTerminalOptions(t)
	h.app.runAgentTests(task.ID)
	options := receiveAgentTerminalOptions(t, starts)
	h.waitFor(t, "the running test", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && current.LastTest != nil && current.LastTest.Status == agentwork.TestStatusRunning &&
			!h.app.agents.mutating[task.ID] && len(h.app.term.sessions) == 1 && !h.app.term.sessions[0].loading
	})

	options.OnExit(12)
	h.waitFor(t, "the failed test result", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && current.LastTest != nil && current.LastTest.Status == agentwork.TestStatusFailed &&
			!h.app.agents.mutating[task.ID] && len(h.app.term.sessions) == 0
	})
	persisted, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != agentwork.StatusReadyForReview || persisted.ExitCode != nil {
		t.Fatalf("test failure changed task/agent state: status=%q agentExit=%v", persisted.Status, persisted.ExitCode)
	}
	if persisted.LastTest.ExitCode == nil || *persisted.LastTest.ExitCode != 12 || persisted.LastTest.FinishedAt.IsZero() {
		t.Fatalf("failed test result = %#v", persisted.LastTest)
	}
}

func TestAgentTestRefusalsLeaveDurableRecordUnchanged(t *testing.T) {
	for _, test := range []struct {
		name      string
		remote    bool
		atLimit   bool
		noCommand bool
	}{
		{name: "no command", noCommand: true},
		{name: "remote worktree", remote: true},
		{name: "terminal limit", atLimit: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, store := newAgentTestHarness(t)
			code := 0
			started := time.Now().Add(-time.Minute).UTC()
			command := "go test ./..."
			if test.noCommand {
				command = " \t"
			}
			task := createAgentTestTask(t, h, store, agentwork.Task{
				Status: agentwork.StatusReadyForReview,
				LastTest: &agentwork.TestResult{
					Status: agentwork.TestStatusPassed, Command: "previous test", ExitCode: &code,
					StartedAt: started, FinishedAt: started.Add(time.Second),
				},
				Profile: agentwork.Profile{TestCommand: command},
			})
			if test.remote {
				h.app.snapshot.Root = "/srv/project"
				h.app.location = workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: "/srv/project"}
			}
			if test.atLimit {
				h.app.term.sessions = make([]terminalSessionState, terminalSessionLimit)
			}
			before, err := store.Get(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, terminalCalls := captureAgentTerminalOptions(t)

			h.app.runAgentTests(task.ID)

			after, err := store.Get(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("refused test changed durable task:\nbefore=%#v\nafter=%#v", before, after)
			}
			if h.app.agents.mutating[task.ID] {
				t.Fatal("refused test started a task mutation")
			}
			if got := terminalCalls.Load(); got != 0 {
				t.Fatalf("refused test started %d terminals", got)
			}
		})
	}
}

func TestAgentRequestChangesAppendsFeedbackAndResumesSameTask(t *testing.T) {
	t.Setenv("SHELL", "/bin/agent-resume-shell")
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{
		Status:    agentwork.StatusReadyForReview,
		Request:   "Original task request.",
		SessionID: " session-42 ",
		Profile: agentwork.Profile{
			LaunchCommand: "agent launch {promptFile}",
			ResumeCommand: "agent resume {sessionId} --prompt {promptFile} --request {request}",
		},
	})
	h.app.agents.review.taskID = task.ID
	starts, _ := captureAgentTerminalOptions(t)
	feedback := "Please preserve backwards compatibility.\nAdd a regression test."

	h.app.requestAgentChanges(feedback)
	h.waitFor(t, "the resumed agent and durable feedback", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && current.Status == agentwork.StatusRunning && !h.app.agents.mutating[task.ID] &&
			len(h.app.term.sessions) == 1 && !h.app.term.sessions[0].loading
	})
	options := receiveAgentTerminalOptions(t, starts)
	persisted, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ID != task.ID || persisted.WorktreePath != task.WorktreePath || persisted.Branch != task.Branch {
		t.Fatalf("resume replaced task identity: before=%#v after=%#v", task, persisted)
	}
	if persisted.SessionID != "session-42" {
		t.Fatalf("session ID = %q, want trimmed existing session", persisted.SessionID)
	}
	if strings.Count(persisted.Request, agentReviewerFeedbackStart) != 1 ||
		strings.Count(persisted.Request, agentReviewerFeedbackEnd) != 1 ||
		!strings.Contains(persisted.Request, feedback) {
		t.Fatalf("reviewer feedback was not delimited and appended exactly once: %q", persisted.Request)
	}
	requestPath, err := store.RequestFile(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	request, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(request) != persisted.Request {
		t.Fatalf("request file = %q, durable request = %q", request, persisted.Request)
	}
	wantCommand := h.app.agentTaskCommand(persisted, true, requestPath)
	if !slices.Equal(options.Command, wantCommand) || options.Dir != task.WorktreePath {
		t.Fatalf("resume terminal = command %#v dir %q, want %#v in %q", options.Command, options.Dir, wantCommand, task.WorktreePath)
	}
	if h.app.term.sessions[0].agentTaskID != task.ID {
		t.Fatalf("resume created a different agent operation: %+v", h.app.term.sessions[0])
	}
	tasks, err := store.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != task.ID {
		t.Fatalf("request changes created tasks: %#v", tasks)
	}
}

func TestAgentRequestChangesRejectedLaunchPreservesReviewState(t *testing.T) {
	for _, test := range []struct {
		name     string
		remote   bool
		atLimit  bool
		noResume bool
	}{
		{name: "remote worktree", remote: true},
		{name: "terminal limit", atLimit: true},
		{name: "missing resume command", noResume: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, store := newAgentTestHarness(t)
			resume := "agent resume {sessionId} --prompt {promptFile}"
			if test.noResume {
				resume = " "
			}
			task := createAgentTestTask(t, h, store, agentwork.Task{
				Status:  agentwork.StatusReadyForReview,
				Request: "Keep the original request unchanged.",
				Profile: agentwork.Profile{ResumeCommand: resume},
			})
			h.app.agents.review.taskID = task.ID
			if test.remote {
				h.app.snapshot.Root = "/srv/project"
				h.app.location = workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: "/srv/project"}
			}
			if test.atLimit {
				h.app.term.sessions = make([]terminalSessionState, terminalSessionLimit)
			}
			before, err := store.Get(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, terminalCalls := captureAgentTerminalOptions(t)

			h.app.requestAgentChanges(feedbackForAgentTest)
			h.waitFor(t, "review state after rejected resume", func() bool {
				current, ok := h.app.selectedAgentTask()
				return ok && current.Status == agentwork.StatusReadyForReview &&
					current.Request == before.Request && !h.app.agents.mutating[task.ID]
			})

			after, err := store.Get(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.Status != agentwork.StatusReadyForReview || after.Request != before.Request {
				t.Fatalf("rejected resume changed review state: before=%#v after=%#v", before, after)
			}
			if got := terminalCalls.Load(); got != 0 {
				t.Fatalf("rejected resume started %d terminals", got)
			}
			if !test.atLimit && len(h.app.term.sessions) != 0 {
				t.Fatalf("rejected resume left terminal sessions: %+v", h.app.term.sessions)
			}
		})
	}
}

const feedbackForAgentTest = "Please fix the review finding."

func TestAgentRequestChangesRetryDoesNotDuplicateFeedback(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{
		Status:  agentwork.StatusReadyForReview,
		Request: "Original request.",
		Profile: agentwork.Profile{ResumeCommand: "agent resume {promptFile}"},
	})
	h.app.agents.review.taskID = task.ID
	h.app.term.sessions = make([]terminalSessionState, terminalSessionLimit)
	starts, _ := captureAgentTerminalOptions(t)

	h.app.requestAgentChanges(feedbackForAgentTest)
	h.waitFor(t, "the rejected first resume to roll back", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && current.Status == agentwork.StatusReadyForReview && current.Request == task.Request &&
			!h.app.agents.mutating[task.ID]
	})
	h.app.term.sessions = nil
	h.app.requestAgentChanges(feedbackForAgentTest)
	h.waitFor(t, "the successful retry", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && current.Status == agentwork.StatusRunning && !h.app.agents.mutating[task.ID] &&
			len(h.app.term.sessions) == 1 && !h.app.term.sessions[0].loading
	})
	_ = receiveAgentTerminalOptions(t, starts)
	persisted, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(persisted.Request, feedbackForAgentTest); got != 1 {
		t.Fatalf("retry appended feedback %d times: %q", got, persisted.Request)
	}
}
