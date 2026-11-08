package app

import (
	"fmt"
	"strings"
	"time"

	"lazygitapp/internal/agentwork"
)

// runAgentTests starts an independent terminal in the task's explicit
// worktree. Test execution is an operation on the task, not a task lifecycle
// transition: running or passing tests never implies review readiness.
func (a *app) runAgentTests(taskID string) {
	a.startAgentTests(taskID)
}

// startAgentTests reports whether a live test terminal was started or selected.
// Batch coordination uses that result to keep capacity-limited work queued.
func (a *app) startAgentTests(taskID string) bool {
	var task agentwork.Task
	found := false
	for _, candidate := range a.agents.tasks {
		if candidate.ID == taskID {
			task = candidate.Clone()
			found = true
			break
		}
	}
	if !found || task.Status == agentwork.StatusArchived || a.agents.mutating[taskID] {
		return false
	}
	// Selecting an existing test terminal is a no-op for the durable result.
	if a.selectAgentOperationTerminal(agentTestOperationID(taskID)) {
		return true
	}
	store, err := a.ensureAgentStore()
	if err != nil {
		a.notify("error", "Could not run agent tests", safeError(err))
		return false
	}
	requestPath, err := store.RequestFile(task.ID)
	if err != nil {
		return false
	}
	// Expand only trusted profile text, and perform no setup command or task
	// status transition for this independent operation.
	expanded := a.expandAgentTaskCommand(task, task.Profile.TestCommand, requestPath)
	command := shellCommandForTerminal(expanded)
	if len(command) == 0 {
		a.notify("warning", "Agent test command is not configured", "Set a test command for this task in Settings → Commands.")
		return false
	}
	if !a.openAgentTestTerminal(task, command, requestPath) {
		return false
	}
	startedAt := time.Now().UTC()
	a.mutateAgentTask(task.ID, func(current *agentwork.Task) error {
		current.LastTest = &agentwork.TestResult{
			Status:    agentwork.TestStatusRunning,
			Command:   expanded,
			StartedAt: startedAt,
		}
		return nil
	})
	return true
}

// runAgentProfile starts the profile's development/run command independently
// of agent execution and test reporting. Its terminal is still bound to the
// exact task/worktree and carries the same resource-isolation environment.
func (a *app) runAgentProfile(taskID string) {
	var task agentwork.Task
	found := false
	for _, candidate := range a.agents.tasks {
		if candidate.ID == taskID {
			task = candidate.Clone()
			found = true
			break
		}
	}
	if !found || task.Status == agentwork.StatusArchived {
		return
	}
	if a.selectAgentOperationTerminal(agentRunOperationID(taskID)) {
		return
	}
	store, err := a.ensureAgentStore()
	if err != nil {
		a.notify("error", "Could not run agent profile", safeError(err))
		return
	}
	requestPath, err := store.RequestFile(task.ID)
	if err != nil {
		return
	}
	command := shellCommandForTerminal(a.expandAgentTaskCommand(task, task.Profile.RunCommand, requestPath))
	if len(command) == 0 {
		a.notify("warning", "Agent run command is not configured", "Add a run command and any required port, database, or cache resources to this task profile.")
		return
	}
	a.openAgentRunTerminal(task, command, requestPath)
}

// agentTestCompletion preserves the difference between a process outcome and
// a test that never reported one. Startup failure and manual termination are
// failures without an invented process exit code.
type agentTestCompletion struct {
	generation uint64
	exitCode   *int
	detail     string
	finishedAt time.Time
}

func agentTestExitCompletion(code int) agentTestCompletion {
	return agentTestCompletion{exitCode: &code, finishedAt: time.Now().UTC()}
}

func agentTestFailureCompletion(detail string) agentTestCompletion {
	return agentTestCompletion{detail: detail, finishedAt: time.Now().UTC()}
}

func (a *app) recordAgentTestCompletion(taskID string, completion agentTestCompletion) {
	a.recordAgentTestCompletionAtGeneration(taskID, a.generation, completion)
}

func (a *app) recordAgentTestCompletionAtGeneration(taskID string, generation uint64, completion agentTestCompletion) {
	completion.generation = generation
	// A very fast process can finish before the initial Running write returns.
	// Keep its outcome until that durable mutation releases the per-task lock.
	if a.agents.mutating[taskID] {
		if a.agents.testCompletions == nil {
			a.agents.testCompletions = map[string]agentTestCompletion{}
		}
		if _, pending := a.agents.testCompletions[taskID]; !pending {
			a.agents.testCompletions[taskID] = completion
		}
	} else {
		a.persistAgentTestCompletion(taskID, completion)
	}
	a.finishAgentBatchTest(taskID, completion)
}

func (a *app) persistAgentTestCompletion(taskID string, completion agentTestCompletion) {
	a.mutateAgentTaskAtGeneration(taskID, completion.generation, func(task *agentwork.Task) error {
		result := agentwork.TestResult{}
		if task.LastTest != nil {
			result = task.LastTest.Clone()
		}
		result.Status = agentwork.TestStatusFailed
		if completion.exitCode != nil && *completion.exitCode == 0 {
			result.Status = agentwork.TestStatusPassed
		}
		result.ExitCode = nil
		if completion.exitCode != nil {
			code := *completion.exitCode
			result.ExitCode = &code
		}
		result.FinishedAt = completion.finishedAt
		task.LastTest = &result
		// In particular, do not derive Status or ReadyForReview from a test exit.
		return nil
	})
}

func (a *app) flushAgentTestCompletion(taskID string) {
	if a.agents.mutating[taskID] {
		return
	}
	completion, pending := a.agents.testCompletions[taskID]
	if !pending {
		return
	}
	delete(a.agents.testCompletions, taskID)
	a.persistAgentTestCompletion(taskID, completion)
}

const (
	agentReviewerFeedbackStart = "<!-- TANGIT REVIEWER FEEDBACK START -->"
	agentReviewerFeedbackEnd   = "<!-- TANGIT REVIEWER FEEDBACK END -->"
)

func agentReviewerFeedback(request, feedback string) string {
	block := "\n\n" + agentReviewerFeedbackStart + "\n\n" + strings.TrimSpace(feedback) + "\n\n" + agentReviewerFeedbackEnd + "\n"
	// A rejected resume restores the prior request. This suffix check also
	// prevents an immediate same-feedback retry from duplicating the block if
	// restoring that request could not be completed.
	if strings.HasSuffix(request, block) {
		return request
	}
	return request + block
}

// requestAgentChanges appends durable reviewer feedback and then resumes the
// same agent operation with the profile's ResumeCommand. No task or worktree
// is created. The terminal is opened only after the request has been saved so
// prompt-file readers see the feedback immediately.
func (a *app) requestAgentChanges(feedback string) {
	task, ok := a.agentReviewTask()
	if !ok || task.Status != agentwork.StatusReadyForReview || a.agents.mutating[task.ID] {
		return
	}
	if strings.TrimSpace(feedback) == "" {
		a.notify("warning", "Reviewer feedback is required", "Describe the changes the agent should make.")
		return
	}
	if strings.TrimSpace(task.Profile.ResumeCommand) == "" {
		a.notify("warning", "Agent resume command is not configured", "Set a resume command for this task in Settings → Commands.")
		return
	}
	store, err := a.ensureAgentStore()
	if err != nil {
		a.notify("error", "Could not request agent changes", safeError(err))
		return
	}
	requestPath, err := store.RequestFile(task.ID)
	if err != nil {
		return
	}
	originalRequest := task.Request
	a.mutateAgentTask(task.ID, func(current *agentwork.Task) error {
		if current.Status != agentwork.StatusReadyForReview {
			return fmt.Errorf("agent task is no longer ready for review")
		}
		current.Request = agentReviewerFeedback(current.Request, feedback)
		current.SessionID = strings.TrimSpace(current.SessionID)
		current.Status = agentwork.StatusRunning
		return nil
	}, func(updated agentwork.Task) {
		command := a.agentTaskCommand(updated, true, requestPath)
		if a.openAgentTerminal(updated, command, requestPath) {
			a.recordAgentActivity(updated.ID, agentwork.Activity{
				Kind: agentwork.ActivityResume, Command: agentActivityCommand(command), SessionID: updated.SessionID,
				Reference: a.agentTerminalReference(updated.ID),
			}, nil)
			return
		}
		// A refused terminal start must not strand the task as Running or leave
		// feedback which would be appended again on the user's next attempt.
		a.mutateAgentTask(updated.ID, func(current *agentwork.Task) error {
			if current.Status == agentwork.StatusRunning && current.Request == updated.Request {
				current.Request = originalRequest
				current.Status = agentwork.StatusReadyForReview
			}
			return nil
		})
		a.notify("error", "Could not resume agent", "The reviewer feedback was not sent; the task remains ready for review.")
	})
}
