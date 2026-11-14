package app

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

func selectAgentBatchTasks(h *harness, tasks ...agentwork.Task) {
	for _, task := range tasks {
		if selected := h.app.agents.batch.selected[task.ID]; selected != nil {
			*selected = true
		}
	}
}

func agentBatchResultFor(t *testing.T, results []agentBatchResult, taskID string) agentBatchResult {
	t.Helper()
	for _, result := range results {
		if result.TaskID == taskID {
			return result
		}
	}
	t.Fatalf("no batch result for task %s: %#v", taskID, results)
	return agentBatchResult{}
}

func TestAgentBatchTestsRequireExplicitSelectionAndKeepIndependentResults(t *testing.T) {
	h, store := newAgentTestHarness(t)
	first := createAgentTestTask(t, h, store, agentwork.Task{
		Title: "First", Status: agentwork.StatusReadyForReview, WorktreePath: "/work/first",
		Profile: agentwork.Profile{TestCommand: "test first"},
	})
	second := createAgentTestTask(t, h, store, agentwork.Task{
		Title: "Second", Status: agentwork.StatusReadyForReview, WorktreePath: "/work/second",
		Profile: agentwork.Profile{TestCommand: "test second"},
	})
	h.app.agents.tasks = []agentwork.Task{first.Clone(), second.Clone()}
	starts, terminalCalls := captureAgentTerminalOptions(t)
	h.app.beginAgentBatchCenter()
	for _, selected := range h.app.agents.batch.selected {
		if *selected {
			t.Fatal("batch tasks were implicitly authorized")
		}
	}
	selectAgentBatchTasks(h, first, second)
	h.app.queueSelectedAgentTests()

	optionsByWorktree := map[string]terminal.Options{}
	for range 2 {
		options := receiveAgentTerminalOptions(t, starts)
		optionsByWorktree[options.Dir] = options
	}
	h.waitFor(t, "both independent test results to start", func() bool {
		return agentBatchResultFor(t, h.app.agents.batch.tests, first.ID).Status == agentBatchRunning &&
			agentBatchResultFor(t, h.app.agents.batch.tests, second.ID).Status == agentBatchRunning &&
			!h.app.agents.mutating[first.ID] && !h.app.agents.mutating[second.ID]
	})
	if got := terminalCalls.Load(); got != 2 {
		t.Fatalf("terminal starts = %d, want two independent tasks", got)
	}

	optionsByWorktree[first.WorktreePath].OnExit(0)
	optionsByWorktree[second.WorktreePath].OnExit(9)
	h.waitFor(t, "independent pass and failure", func() bool {
		return agentBatchResultFor(t, h.app.agents.batch.tests, first.ID).Status == agentBatchPassed &&
			agentBatchResultFor(t, h.app.agents.batch.tests, second.ID).Status == agentBatchFailed &&
			!h.app.agents.mutating[first.ID] && !h.app.agents.mutating[second.ID]
	})
	for index, task := range []agentwork.Task{first, second} {
		persisted, err := store.Get(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if persisted.Status != agentwork.StatusReadyForReview {
			t.Fatalf("task %d test inferred lifecycle/review readiness: %s", index, persisted.Status)
		}
		want := agentwork.TestStatusPassed
		if index == 1 {
			want = agentwork.TestStatusFailed
		}
		if persisted.LastTest == nil || persisted.LastTest.Status != want {
			t.Fatalf("task %d durable test result = %#v, want %s", index, persisted.LastTest, want)
		}
	}
}

func TestAgentBatchTestsWaitForTerminalCapacityThenStartQueuedTask(t *testing.T) {
	h, store := newAgentTestHarness(t)
	first := createAgentTestTask(t, h, store, agentwork.Task{
		Title: "Capacity one", WorktreePath: "/work/capacity-one", Profile: agentwork.Profile{TestCommand: "test one"},
	})
	second := createAgentTestTask(t, h, store, agentwork.Task{
		Title: "Capacity two", WorktreePath: "/work/capacity-two", Profile: agentwork.Profile{TestCommand: "test two"},
	})
	h.app.term.sessions = make([]terminalSessionState, terminalSessionLimit-1)
	h.app.agents.tasks = []agentwork.Task{first.Clone(), second.Clone()}
	starts, terminalCalls := captureAgentTerminalOptions(t)
	h.app.beginAgentBatchCenter()
	selectAgentBatchTasks(h, first, second)
	h.app.queueSelectedAgentTests()
	firstOptions := receiveAgentTerminalOptions(t, starts)

	if got := terminalCalls.Load(); got != 1 {
		t.Fatalf("capacity-limited queue started %d terminals, want one", got)
	}
	queued := agentBatchResultFor(t, h.app.agents.batch.tests, second.ID)
	if queued.Status != agentBatchQueued || queued.Detail == "" {
		t.Fatalf("second task was not retained as queued: %#v", queued)
	}

	firstOptions.OnExit(0)
	h.updates.waitForUpdate(t)
	secondOptions := receiveAgentTerminalOptions(t, starts)
	if secondOptions.Dir != second.WorktreePath {
		t.Fatalf("next queued test directory = %q, want %q", secondOptions.Dir, second.WorktreePath)
	}
	h.waitFor(t, "queued task to start after capacity becomes available", func() bool {
		return agentBatchResultFor(t, h.app.agents.batch.tests, second.ID).Status == agentBatchRunning
	})
	secondOptions.OnExit(0)
	h.waitFor(t, "both capacity-limited tests to finish", func() bool {
		return agentBatchResultFor(t, h.app.agents.batch.tests, first.ID).Status == agentBatchPassed &&
			agentBatchResultFor(t, h.app.agents.batch.tests, second.ID).Status == agentBatchPassed &&
			!h.app.agents.mutating[first.ID] && !h.app.agents.mutating[second.ID]
	})
}

func TestAgentBatchFastExitWaitsForInitialRunningMutation(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{
		Status: agentwork.StatusExited, WorktreePath: "/work/fast-exit",
		Profile: agentwork.Profile{TestCommand: "test fast"},
	})
	starts, _ := captureAgentTerminalOptions(t)
	h.app.beginAgentBatchCenter()
	selectAgentBatchTasks(h, task)
	h.app.queueSelectedAgentTests()
	options := receiveAgentTerminalOptions(t, starts)
	if !h.app.agents.mutating[task.ID] {
		t.Fatal("test did not reach its initial durable Running mutation")
	}

	// The UI loop has not delivered the Running mutation yet. This is the fast
	// process shutdown that used to be discarded instead of queued behind it.
	options.OnExit(0)
	h.waitFor(t, "fast test completion after the Running mutation", func() bool {
		return agentBatchResultFor(t, h.app.agents.batch.tests, task.ID).Status == agentBatchPassed &&
			!h.app.agents.mutating[task.ID] && h.app.agents.testCompletions[task.ID].finishedAt.IsZero() &&
			len(h.app.term.sessions) == 0
	})
	persisted, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.LastTest == nil || persisted.LastTest.Status != agentwork.TestStatusPassed ||
		persisted.LastTest.ExitCode == nil || *persisted.LastTest.ExitCode != 0 ||
		persisted.LastTest.StartedAt.IsZero() || persisted.LastTest.FinishedAt.Before(persisted.LastTest.StartedAt) {
		t.Fatalf("fast durable completion = %#v", persisted.LastTest)
	}
	if persisted.Status != agentwork.StatusExited {
		t.Fatalf("fast passing test changed lifecycle to %q", persisted.Status)
	}
}

func TestAgentBatchMutationCompletionStartsWaitingTest(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{
		Title: "Mutation wait", WorktreePath: "/work/mutation-wait",
		Profile: agentwork.Profile{TestCommand: "test after mutation"},
	})
	h.app.agents.tasks = []agentwork.Task{task.Clone()}
	starts, terminalCalls := captureAgentTerminalOptions(t)

	// Hold the per-task lock without starting any terminal. The batch must be
	// pumped by this mutation's completion, not by a later process exit.
	h.app.mutateAgentTask(task.ID, func(current *agentwork.Task) error {
		current.Title = "Mutation finished"
		return nil
	})
	h.app.beginAgentBatchCenter()
	selectAgentBatchTasks(h, task)
	h.app.queueSelectedAgentTests()
	queued := agentBatchResultFor(t, h.app.agents.batch.tests, task.ID)
	if queued.Status != agentBatchQueued || queued.Detail != tr("Waiting for the task's current durable update.") {
		t.Fatalf("test did not wait for the durable mutation: %#v", queued)
	}
	if terminalCalls.Load() != 0 {
		t.Fatal("test terminal started while its task mutation was active")
	}

	h.waitFor(t, "mutation completion to start the waiting test", func() bool {
		return agentBatchResultFor(t, h.app.agents.batch.tests, task.ID).Status == agentBatchRunning &&
			terminalCalls.Load() == 1
	})
	options := receiveAgentTerminalOptions(t, starts)
	if options.Dir != task.WorktreePath {
		t.Fatalf("automatically started test directory = %q, want %q", options.Dir, task.WorktreePath)
	}
	options.OnExit(0)
	h.waitFor(t, "automatically started test to finish", func() bool {
		return agentBatchResultFor(t, h.app.agents.batch.tests, task.ID).Status == agentBatchPassed &&
			!h.app.agents.mutating[task.ID]
	})
}

func TestAgentBatchCompletionSurvivesRepositoryGenerationChangeWithoutOldUITask(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{
		Title: "Previous repository", Status: agentwork.StatusExited,
		WorktreePath: "/work/previous-repository",
		Profile:      agentwork.Profile{TestCommand: "test previous repository"},
	})
	starts, _ := captureAgentTerminalOptions(t)
	h.app.beginAgentBatchCenter()
	selectAgentBatchTasks(h, task)
	h.app.queueSelectedAgentTests()
	receiveAgentTerminalOptions(t, starts)
	if !h.app.agents.mutating[task.ID] {
		t.Fatal("test did not reach its initial durable Running mutation")
	}

	// Queue a fast exit behind Running, then move to another repository before
	// either mutation reaches the UI loop. Both durable writes must still run.
	// Invoke the generation-aware operation seam directly: terminal.Options.OnExit
	// is itself delivered through the UI loop, whose repository guard runs first.
	h.app.agentTerminalExitedAtGeneration(agentTestOperationID(task.ID), h.app.generation, 0)
	h.app.generation++
	h.app.resetRepositoryState(repository.Snapshot{Root: "/work/new-repository"})

	h.waitFor(t, "quarantined test completion after repository change", func() bool {
		_, pending := h.app.agents.testCompletions[task.ID]
		return !h.app.agents.mutating[task.ID] && !pending
	})
	persisted, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.LastTest == nil || persisted.LastTest.Status != agentwork.TestStatusPassed ||
		persisted.LastTest.ExitCode == nil || *persisted.LastTest.ExitCode != 0 ||
		persisted.LastTest.FinishedAt.IsZero() {
		t.Fatalf("generation change lost durable completion: %#v", persisted.LastTest)
	}
	if len(h.app.agents.tasks) != 0 || h.app.agents.loadedRepo != "" {
		t.Fatalf("old repository task leaked into the new UI: tasks=%#v loadedRepo=%q", h.app.agents.tasks, h.app.agents.loadedRepo)
	}
}

func TestAgentBatchTerminalStartupFailureCompletesAndStartsQueuedTask(t *testing.T) {
	h, store := newAgentTestHarness(t)
	first := createAgentTestTask(t, h, store, agentwork.Task{
		Title: "Startup failure", WorktreePath: "/work/startup-failure",
		Profile: agentwork.Profile{TestCommand: "test failure"},
	})
	second := createAgentTestTask(t, h, store, agentwork.Task{
		Title: "Startup successor", WorktreePath: "/work/startup-successor",
		Profile: agentwork.Profile{TestCommand: "test successor"},
	})
	h.app.agents.tasks = []agentwork.Task{first.Clone(), second.Clone()}
	h.app.term.sessions = make([]terminalSessionState, terminalSessionLimit-1)
	original := newTerminal
	peers := make(chan monitoredTerminalPeer, 1)
	var calls atomic.Int32
	newTerminal = func(options terminal.Options) (*terminal.Terminal, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("construction refused")
		}
		return newMonitoredTerminal(options, peers)
	}
	t.Cleanup(func() {
		newTerminal = original
		select {
		case peer := <-peers:
			_ = peer.Close()
		default:
		}
	})
	h.app.beginAgentBatchCenter()
	selectAgentBatchTasks(h, first, second)
	h.app.queueSelectedAgentTests()

	h.waitFor(t, "startup failure and queued successor", func() bool {
		return agentBatchResultFor(t, h.app.agents.batch.tests, first.ID).Status == agentBatchFailed &&
			agentBatchResultFor(t, h.app.agents.batch.tests, second.ID).Status == agentBatchRunning &&
			!h.app.agents.mutating[first.ID] && !h.app.agents.mutating[second.ID]
	})
	if calls.Load() != 2 {
		t.Fatalf("terminal constructions = %d, want failure plus queued successor", calls.Load())
	}
	persisted, err := store.Get(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.LastTest == nil || persisted.LastTest.Status != agentwork.TestStatusFailed ||
		persisted.LastTest.ExitCode != nil || persisted.LastTest.FinishedAt.IsZero() {
		t.Fatalf("startup failure invented an exit or stayed Running: %#v", persisted.LastTest)
	}
	if persisted.Status == agentwork.StatusReadyForReview {
		t.Fatal("startup failure inferred review readiness")
	}
}

func TestAgentBatchManualCloseCompletesOnceAndStartsQueuedTask(t *testing.T) {
	h, store := newAgentTestHarness(t)
	first := createAgentTestTask(t, h, store, agentwork.Task{
		Title: "Closed test", WorktreePath: "/work/closed-test",
		Profile: agentwork.Profile{TestCommand: "test closed"},
	})
	second := createAgentTestTask(t, h, store, agentwork.Task{
		Title: "Close successor", WorktreePath: "/work/close-successor",
		Profile: agentwork.Profile{TestCommand: "test successor"},
	})
	h.app.agents.tasks = []agentwork.Task{first.Clone(), second.Clone()}
	h.app.term.sessions = make([]terminalSessionState, terminalSessionLimit-1)
	starts, calls := captureAgentTerminalOptions(t)
	h.app.beginAgentBatchCenter()
	selectAgentBatchTasks(h, first, second)
	h.app.queueSelectedAgentTests()
	firstOptions := receiveAgentTerminalOptions(t, starts)
	h.waitFor(t, "the first test to finish starting", func() bool {
		return !h.app.agents.mutating[first.ID] && h.app.terminalSessionCount() == terminalSessionLimit &&
			!h.app.term.sessions[h.app.term.active].loading
	})

	h.app.closeTerminalAt(h.app.term.active)
	h.waitFor(t, "manual close failure and queued successor", func() bool {
		return agentBatchResultFor(t, h.app.agents.batch.tests, first.ID).Status == agentBatchFailed &&
			agentBatchResultFor(t, h.app.agents.batch.tests, second.ID).Status == agentBatchRunning &&
			!h.app.agents.mutating[first.ID] && !h.app.agents.mutating[second.ID]
	})
	secondOptions := receiveAgentTerminalOptions(t, starts)
	firstOptions.OnExit(0)
	secondOptions.OnExit(7)
	h.waitFor(t, "late exits without duplicate completion", func() bool {
		return agentBatchResultFor(t, h.app.agents.batch.tests, second.ID).Status == agentBatchFailed &&
			!h.app.agents.mutating[first.ID] && !h.app.agents.mutating[second.ID]
	})
	if calls.Load() != 2 {
		t.Fatalf("late exit restarted or duplicated terminal work: %d starts", calls.Load())
	}
	persisted, err := store.Get(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.LastTest == nil || persisted.LastTest.Status != agentwork.TestStatusFailed ||
		persisted.LastTest.ExitCode != nil {
		t.Fatalf("late exit overwrote manual termination: %#v", persisted.LastTest)
	}
	if got := agentBatchResultFor(t, h.app.agents.batch.tests, first.ID); got.Detail != tr("Terminal closed before the process reported an exit code.") {
		t.Fatalf("manual close detail = %q", got.Detail)
	}
}

func TestAgentBatchStopLeavesCapacityLimitedTestQueuedNoLonger(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{Profile: agentwork.Profile{TestCommand: "test"}})
	h.app.term.sessions = make([]terminalSessionState, terminalSessionLimit)
	_, terminalCalls := captureAgentTerminalOptions(t)
	h.app.beginAgentBatchCenter()
	selectAgentBatchTasks(h, task)
	h.app.queueSelectedAgentTests()
	h.app.stopQueuedAgentTests()
	result := agentBatchResultFor(t, h.app.agents.batch.tests, task.ID)
	if result.Status != agentBatchSkipped || terminalCalls.Load() != 0 {
		t.Fatalf("stopped capacity-limited result = %#v, starts=%d", result, terminalCalls.Load())
	}
	persisted, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.LastTest != nil {
		t.Fatalf("stopped queued test fabricated durable execution: %#v", persisted.LastTest)
	}
}

func TestAgentBatchBackAbandonsQueueAndLaterReviewHasNoStaleControls(t *testing.T) {
	h, store := newAgentTestHarness(t)
	first := createAgentTestTask(t, h, store, agentwork.Task{
		Title: "Abandoned first", Status: agentwork.StatusExited, BaseCommit: "fixed-first", WorktreePath: "/work/abandoned-first",
	})
	second := createAgentTestTask(t, h, store, agentwork.Task{
		Title: "Abandoned second", Status: agentwork.StatusExited, BaseCommit: "fixed-second", WorktreePath: "/work/abandoned-second",
	})
	third := createAgentTestTask(t, h, store, agentwork.Task{
		Title: "Standalone", Status: agentwork.StatusExited, BaseCommit: "fixed-third", WorktreePath: "/work/standalone",
	})
	h.app.agents.tasks = []agentwork.Task{first.Clone(), second.Clone(), third.Clone()}
	h.app.beginAgentBatchCenter()
	selectAgentBatchTasks(h, first, second)
	h.app.startSelectedAgentReviews()
	h.app.closeAgentReview()

	if got := agentBatchResultFor(t, h.app.agents.batch.reviews, second.ID); got.Status != agentBatchSkipped {
		t.Fatalf("Back left a pending review reachable: %#v", got)
	}
	h.app.openAgentReview(third)
	h.tester.Frame()
	if h.tester.HasText("Review 1 of 2") {
		t.Fatal("standalone review retained an abandoned queue badge")
	}
	if _, found := h.tester.Find("Next review"); found {
		t.Fatal("standalone review retained an abandoned Next action")
	}
	position := h.app.agents.batch.reviewPosition
	h.app.advanceAgentBatchReview()
	if h.app.agents.review.taskID != third.ID || h.app.agents.batch.reviewPosition != position {
		t.Fatal("advance escaped the abandoned queue into another task")
	}
}

func TestAgentBatchReviewQueueUsesFixedTargetsWithoutChangingReadiness(t *testing.T) {
	h, store := newAgentTestHarness(t)
	first := createAgentTestTask(t, h, store, agentwork.Task{
		Title: "Review first", Status: agentwork.StatusExited, BaseCommit: "fixed-first", WorktreePath: "/work/review-first",
	})
	second := createAgentTestTask(t, h, store, agentwork.Task{
		Title: "Review second", Status: agentwork.StatusFailed, BaseCommit: "fixed-second", WorktreePath: "/work/review-second",
	})
	h.app.agents.tasks = []agentwork.Task{first.Clone(), second.Clone()}
	h.app.beginAgentBatchCenter()
	selectAgentBatchTasks(h, first, second)
	h.app.startSelectedAgentReviews()

	if !h.app.agents.review.open || h.app.agents.review.taskID != first.ID ||
		h.app.agents.review.base != first.BaseCommit || h.app.agents.review.worktree != first.WorktreePath {
		t.Fatalf("first review target = %#v", h.app.agents.review)
	}
	h.tester.Frame()
	if !h.tester.HasText("Review 1 of 2") {
		t.Fatalf("review queue progress is missing from %q", h.tester.Texts())
	}
	if bounds, found := h.tester.Find("Next review"); !found || bounds.W > 40 || bounds.H > 30 {
		t.Fatalf("next-review action is not a compact icon button: bounds=%+v found=%t", bounds, found)
	}
	h.app.advanceAgentBatchReview()
	if !h.app.agents.review.open || h.app.agents.review.taskID != second.ID ||
		h.app.agents.review.base != second.BaseCommit || h.app.agents.review.worktree != second.WorktreePath {
		t.Fatalf("second review target = %#v", h.app.agents.review)
	}
	h.app.advanceAgentBatchReview()
	if h.app.agents.review.open || !h.app.agents.batch.open {
		t.Fatalf("completed review queue did not return to its independent results: review=%t batch=%t", h.app.agents.review.open, h.app.agents.batch.open)
	}
	for _, task := range []agentwork.Task{first, second} {
		result := agentBatchResultFor(t, h.app.agents.batch.reviews, task.ID)
		if result.Status != agentBatchVisited {
			t.Fatalf("review result for %s = %#v", task.Title, result)
		}
		persisted, err := store.Get(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if persisted.Status != task.Status || persisted.BaseCommit != task.BaseCommit {
			t.Fatalf("review visit changed task identity/readiness: before=%#v after=%#v", task, persisted)
		}
	}
}
