package app

import (
	"strings"

	"lazygitapp/internal/agentwork"
)

type agentBatchResultStatus string

const (
	agentBatchQueued   agentBatchResultStatus = "queued"
	agentBatchRunning  agentBatchResultStatus = "running"
	agentBatchPassed   agentBatchResultStatus = "passed"
	agentBatchFailed   agentBatchResultStatus = "failed"
	agentBatchSkipped  agentBatchResultStatus = "skipped"
	agentBatchPending  agentBatchResultStatus = "pending"
	agentBatchCurrent  agentBatchResultStatus = "current"
	agentBatchVisited  agentBatchResultStatus = "visited"
	agentBatchExcluded agentBatchResultStatus = "excluded"
)

// agentBatchResult is an independent UI result. Test completion updates only
// the matching test result; review visits never change task lifecycle or imply
// approval. Durable test outcomes remain owned by agent_execution.go.
type agentBatchResult struct {
	TaskID string
	Title  string
	Status agentBatchResultStatus
	Detail string
}

type agentBatchUIState struct {
	open           bool
	ordered        []agentwork.Task
	selected       map[string]*bool
	tests          []agentBatchResult
	reviews        []agentBatchResult
	reviewPosition int
}

func (a *app) beginAgentBatchCenter() {
	if a.agents.review.open {
		return
	}
	state := &a.agents.batch
	state.open = true
	state.ordered = nil
	state.selected = map[string]*bool{}
	for _, task := range a.agents.tasks {
		if !agentTaskActive(task) {
			continue
		}
		state.ordered = append(state.ordered, task.Clone())
		state.selected[task.ID] = new(bool)
	}
}

func (a *app) resetAgentBatch() {
	a.agents.batch = agentBatchUIState{reviewPosition: -1}
}

func (a *app) selectedAgentBatchTasks() []agentwork.Task {
	state := &a.agents.batch
	tasks := make([]agentwork.Task, 0, len(state.ordered))
	for _, task := range state.ordered {
		if selected := state.selected[task.ID]; selected != nil && *selected {
			tasks = append(tasks, task.Clone())
		}
	}
	return tasks
}

func (a *app) currentAgentBatchTask(taskID string) (agentwork.Task, bool) {
	for _, task := range a.agents.tasks {
		if task.ID == taskID && agentTaskActive(task) {
			return task.Clone(), true
		}
	}
	return agentwork.Task{}, false
}

func (a *app) queueSelectedAgentTests() {
	state := &a.agents.batch
	if agentBatchHasPendingTests(state.tests) {
		return
	}
	selected := a.selectedAgentBatchTasks()
	if len(selected) == 0 {
		a.notify("warning", "No agent tasks selected", tr("Select explicit tasks before queuing tests."))
		return
	}
	state.tests = nil
	for _, task := range selected {
		result := agentBatchResult{TaskID: task.ID, Title: task.Title, Status: agentBatchQueued}
		switch {
		case strings.TrimSpace(task.Profile.TestCommand) == "":
			result.Status = agentBatchSkipped
			result.Detail = tr("No test command is configured.")
		case a.location.IsRemote():
			result.Status = agentBatchSkipped
			result.Detail = tr("Generic agent terminals currently require a local worktree.")
		}
		state.tests = append(state.tests, result)
	}
	a.pumpAgentTestBatch()
}

func agentBatchHasPendingTests(results []agentBatchResult) bool {
	for _, result := range results {
		if result.Status == agentBatchQueued || result.Status == agentBatchRunning {
			return true
		}
	}
	return false
}

// pumpAgentTestBatch uses currently available terminal capacity and leaves the
// remaining work queued. Terminal exits call it again, so a full terminal panel
// is backpressure rather than a lost or falsely failed test.
func (a *app) pumpAgentTestBatch() {
	state := &a.agents.batch
	for index := range state.tests {
		result := &state.tests[index]
		if result.Status != agentBatchQueued {
			continue
		}
		task, ok := a.currentAgentBatchTask(result.TaskID)
		if !ok || strings.TrimSpace(task.Profile.TestCommand) == "" {
			result.Status = agentBatchSkipped
			result.Detail = tr("The task or its test command is no longer available.")
			continue
		}
		if a.terminalSessionCount() >= terminalSessionLimit {
			result.Detail = tr("Waiting for terminal capacity; no process has started.")
			return
		}
		if a.agents.mutating[task.ID] {
			result.Detail = tr("Waiting for the task's current durable update.")
			return
		}
		if !a.startAgentTests(task.ID) {
			result.Detail = tr("Waiting to start; no test result has been inferred.")
			return
		}
		result.Status = agentBatchRunning
		result.Detail = tr("Running in the task's explicit worktree.")
	}
}

func (a *app) finishAgentBatchTest(taskID string, completion agentTestCompletion) {
	state := &a.agents.batch
	for index := range state.tests {
		result := &state.tests[index]
		if result.TaskID != taskID || result.Status != agentBatchRunning {
			continue
		}
		result.Status = agentBatchFailed
		result.Detail = completion.detail
		if completion.exitCode != nil {
			if *completion.exitCode == 0 {
				result.Status = agentBatchPassed
			}
			result.Detail = trf("Exit code: %d", *completion.exitCode)
		}
		break
	}
	a.pumpAgentTestBatch()
}

func (a *app) stopQueuedAgentTests() {
	for index := range a.agents.batch.tests {
		result := &a.agents.batch.tests[index]
		if result.Status == agentBatchQueued {
			result.Status = agentBatchSkipped
			result.Detail = tr("Stopped before a terminal was started.")
		}
	}
}

func (a *app) startSelectedAgentReviews() {
	state := &a.agents.batch
	selected := a.selectedAgentBatchTasks()
	if len(selected) == 0 {
		a.notify("warning", "No agent tasks selected", tr("Select explicit tasks before starting a review queue."))
		return
	}
	state.reviews = nil
	state.reviewPosition = -1
	for _, task := range selected {
		result := agentBatchResult{TaskID: task.ID, Title: task.Title, Status: agentBatchPending}
		if task.BaseCommit == "" || task.WorktreePath == "" {
			result.Status = agentBatchExcluded
			result.Detail = tr("The fixed BaseCommit or explicit worktree is unavailable.")
		}
		state.reviews = append(state.reviews, result)
	}
	state.open = false
	a.openNextAgentBatchReview()
}

func (a *app) openNextAgentBatchReview() {
	state := &a.agents.batch
	state.reviewPosition++
	for state.reviewPosition < len(state.reviews) {
		result := &state.reviews[state.reviewPosition]
		if result.Status != agentBatchPending {
			state.reviewPosition++
			continue
		}
		task, ok := a.currentAgentBatchTask(result.TaskID)
		if !ok {
			result.Status = agentBatchExcluded
			result.Detail = tr("The task is no longer active.")
			state.reviewPosition++
			continue
		}
		result.Status = agentBatchCurrent
		result.Detail = tr("Open for inspection; no approval or readiness change is inferred.")
		a.openAgentReview(task)
		return
	}
	state.reviewPosition = len(state.reviews)
	state.open = true
}

func (a *app) finishCurrentAgentBatchReview() {
	state := &a.agents.batch
	if state.reviewPosition < 0 || state.reviewPosition >= len(state.reviews) {
		return
	}
	result := &state.reviews[state.reviewPosition]
	if a.agents.review.taskID != result.TaskID {
		return
	}
	if result.Status == agentBatchCurrent {
		result.Status = agentBatchVisited
		result.Detail = tr("Visited only; task readiness and acceptance were not changed.")
	}
}

func (a *app) abandonAgentBatchReview() {
	a.finishCurrentAgentBatchReview()
	state := &a.agents.batch
	for index := range state.reviews {
		result := &state.reviews[index]
		if result.Status == agentBatchPending {
			result.Status = agentBatchSkipped
			result.Detail = tr("Review queue closed before this task was inspected.")
		}
	}
	state.reviewPosition = -1
}

func (a *app) advanceAgentBatchReview() {
	current, _ := agentBatchReviewCounts(a.agents.batch, a.agents.review.taskID)
	if !a.agents.review.open || current == 0 {
		return
	}
	a.finishCurrentAgentBatchReview()
	// Advancing within the queue is not the Back action: retain its state while
	// replacing only the review session and its fixed target.
	a.resetAgentReview()
	a.openNextAgentBatchReview()
}

func agentBatchReviewCounts(state agentBatchUIState, taskID string) (current, total int) {
	if state.reviewPosition < 0 || state.reviewPosition >= len(state.reviews) {
		return 0, 0
	}
	result := state.reviews[state.reviewPosition]
	if result.TaskID != taskID || result.Status != agentBatchCurrent {
		return 0, 0
	}
	return state.reviewPosition + 1, len(state.reviews)
}
