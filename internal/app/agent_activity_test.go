package app

import (
	"strings"
	"testing"
	"time"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/workspace"
)

func agentActivityCount(task agentwork.Task, kind agentwork.ActivityKind) int {
	count := 0
	for _, activity := range task.Activity {
		if activity.Kind == kind {
			count++
		}
	}
	return count
}

func latestAgentActivity(t *testing.T, store *agentwork.Store, taskID string, kind agentwork.ActivityKind) agentwork.Activity {
	t.Helper()
	task, err := store.Get(taskID)
	if err != nil {
		t.Fatal(err)
	}
	for index := len(task.Activity) - 1; index >= 0; index-- {
		if task.Activity[index].Kind == kind {
			return task.Activity[index]
		}
	}
	t.Fatalf("task has no %q activity: %#v", kind, task.Activity)
	return agentwork.Activity{}
}

func TestAgentLaunchAndResumeRecordExpandedCommands(t *testing.T) {
	t.Setenv("SHELL", "/bin/agent-activity-shell")
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{
		SessionID: " session-activity ",
		Profile: agentwork.Profile{
			LaunchCommand: "agent launch {worktree} --session {sessionId}",
			ResumeCommand: "agent resume {sessionId} --prompt {promptFile}",
			SetupCommand:  "setup-activity",
		},
	})
	starts, _ := captureAgentTerminalOptions(t)

	h.app.launchAgentTask(task.ID, false)
	launchOptions := receiveAgentTerminalOptions(t, starts)
	h.waitFor(t, "the recorded agent launch", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && !h.app.agents.mutating[task.ID] && agentActivityCount(current, agentwork.ActivityLaunch) == 1
	})
	launch := latestAgentActivity(t, store, task.ID, agentwork.ActivityLaunch)
	if launch.Command != agentActivityCommand(launchOptions.Command) || !strings.Contains(launch.Command, "setup-activity") ||
		launch.SessionID != "session-activity" || !strings.HasPrefix(launch.Reference, "terminal:") || launch.CreatedAt.IsZero() {
		t.Fatalf("launch activity = %#v, terminal command = %#v", launch, launchOptions.Command)
	}

	launchOptions.OnExit(0)
	h.waitFor(t, "the exited agent before resume", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && current.Status == agentwork.StatusExited && !h.app.agents.mutating[task.ID] && len(h.app.term.sessions) == 0
	})
	h.app.launchAgentTask(task.ID, true)
	resumeOptions := receiveAgentTerminalOptions(t, starts)
	h.waitFor(t, "the recorded agent resume", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && !h.app.agents.mutating[task.ID] && agentActivityCount(current, agentwork.ActivityResume) == 1
	})
	resume := latestAgentActivity(t, store, task.ID, agentwork.ActivityResume)
	if resume.Command != agentActivityCommand(resumeOptions.Command) || !strings.Contains(resume.Command, "agent resume") ||
		strings.Contains(resume.Command, "setup-activity") || resume.SessionID != "session-activity" {
		t.Fatalf("resume activity = %#v, terminal command = %#v", resume, resumeOptions.Command)
	}
	persisted, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != agentwork.StatusRunning {
		t.Fatalf("recording history inferred a different task outcome: %q", persisted.Status)
	}
}

func TestAgentReviewerFeedbackRecordsOneResumeCommand(t *testing.T) {
	t.Setenv("SHELL", "/bin/agent-review-activity-shell")
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{
		Status:    agentwork.StatusReadyForReview,
		Request:   "Original request.",
		SessionID: "review-session",
		Profile: agentwork.Profile{
			LaunchCommand: "agent launch {promptFile}",
			ResumeCommand: "agent resume {sessionId} --prompt {promptFile} --request {request}",
		},
	})
	h.app.agents.review.taskID = task.ID
	starts, _ := captureAgentTerminalOptions(t)

	h.app.requestAgentChanges("Please add a regression test.")
	h.waitFor(t, "reviewer resume history", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && !h.app.agents.mutating[task.ID] && agentActivityCount(current, agentwork.ActivityResume) == 1
	})
	options := receiveAgentTerminalOptions(t, starts)
	resume := latestAgentActivity(t, store, task.ID, agentwork.ActivityResume)
	if resume.Command != agentActivityCommand(options.Command) || !strings.Contains(resume.Command, "Please add a regression test.") ||
		resume.SessionID != "review-session" || !strings.HasPrefix(resume.Reference, "terminal:") {
		t.Fatalf("reviewer resume activity = %#v, terminal command = %#v", resume, options.Command)
	}
}

func TestAgentRejectedTerminalsRecordNoLaunchOrResume(t *testing.T) {
	for _, test := range []struct {
		name   string
		remote bool
	}{
		{name: "session limit"},
		{name: "remote worktree", remote: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, store := newAgentTestHarness(t)
			if test.remote {
				h.app.snapshot.Root = "/srv/project"
				h.app.location = workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: "/srv/project"}
			} else {
				h.app.term.sessions = make([]terminalSessionState, terminalSessionLimit)
			}
			task := createAgentTestTask(t, h, store, agentwork.Task{
				Profile: agentwork.Profile{LaunchCommand: "agent launch {worktree}"},
			})
			h.app.launchAgentTask(task.ID, false)
			persisted, err := store.Get(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(persisted.Activity) != 0 || h.app.agents.mutating[task.ID] {
				t.Fatalf("rejected launch recorded activity: %#v", persisted.Activity)
			}
		})
	}
}

func TestAgentRejectedReviewerResumeDoesNotDuplicateCommandOnRetry(t *testing.T) {
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
	h.waitFor(t, "rejected reviewer resume rollback", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && current.Status == agentwork.StatusReadyForReview && current.Request == task.Request && !h.app.agents.mutating[task.ID]
	})
	persisted, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count := agentActivityCount(persisted, agentwork.ActivityResume); count != 0 {
		t.Fatalf("rejected reviewer resume recorded %d commands: %#v", count, persisted.Activity)
	}

	h.app.term.sessions = nil
	h.app.requestAgentChanges(feedbackForAgentTest)
	h.waitFor(t, "successful reviewer retry", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && !h.app.agents.mutating[task.ID] && agentActivityCount(current, agentwork.ActivityResume) == 1
	})
	_ = receiveAgentTerminalOptions(t, starts)
	persisted, err = store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count := agentActivityCount(persisted, agentwork.ActivityResume); count != 1 {
		t.Fatalf("reviewer retry recorded %d resume commands, want one", count)
	}
}

func TestAgentRecentActivityViewShowsCompactLatestHistory(t *testing.T) {
	h, _ := newAgentTestHarness(t)
	createdAt := time.Date(2026, 11, 13, 23, 4, 5, 0, time.UTC)
	h.app.agents.tasks = []agentwork.Task{{
		ID: "selected", Title: "Activity task", Status: agentwork.StatusRunning,
		Activity: []agentwork.Activity{
			{Kind: agentwork.ActivityLaunch, CreatedAt: createdAt.Add(-3 * time.Minute), Command: "older launch"},
			{Kind: agentwork.ActivityTest, StatusTo: string(agentwork.TestStatusPassed), CreatedAt: createdAt.Add(-2 * time.Minute)},
			{Kind: agentwork.ActivityCheckpoint, CreatedAt: createdAt.Add(-time.Minute), CheckpointID: "checkpoint-1", SnapshotID: "snapshot-1"},
			{Kind: agentwork.ActivityResume, CreatedAt: createdAt, Command: "agent resume", SessionID: "session-1", Reference: "terminal:2"},
		},
	}}
	h.app.agents.selectedTaskID = "selected"
	h.tester.Frame()
	if !h.tester.HasText("Recent activity") || !h.tester.HasText("Resume agent") ||
		!h.tester.HasText("Checkpoint saved") || !h.tester.HasText(agentActivityTimestamp(agentwork.Activity{CreatedAt: createdAt})) {
		t.Fatalf("recent activity is not rendered: %q", h.tester.Texts())
	}
	if h.tester.HasText("Launch agent") {
		t.Fatalf("compact activity rendered an older fourth event: %q", h.tester.Texts())
	}
	tooltip := agentActivityTooltip(h.app.agents.tasks[0].Activity[3])
	for _, expected := range []string{"agent resume", "session-1", "terminal:2"} {
		if !strings.Contains(tooltip, expected) {
			t.Fatalf("activity tooltip %q does not contain %q", tooltip, expected)
		}
	}
}
