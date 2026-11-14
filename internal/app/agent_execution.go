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
		return
	}
	// Selecting an existing test terminal is a no-op for the durable result.
	if a.selectAgentOperationTerminal(agentTestOperationID(taskID)) {
		return
	}
	store, err := a.ensureAgentStore()
	if err != nil {
		a.notify("error", "Could not run agent tests", safeError(err))
		return
	}
	requestPath, err := store.RequestFile(task.ID)
	if err != nil {
		return
	}
	// Expand only trusted profile text, and perform no setup command or task
	// status transition for this independent operation.
	expanded := a.expandAgentTaskCommand(task, task.Profile.TestCommand, requestPath)
	command := shellCommandForTerminal(expanded)
	if len(command) == 0 {
		a.notify("warning", "Agent test command is not configured", "Set a test command for this task in Settings → Commands.")
		return
	}
	if !a.openAgentTestTerminal(task, command, requestPath) {
		return
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
}

func (a *app) agentTestTerminalExited(taskID string, code int) {
	finishedAt := time.Now().UTC()
	a.mutateAgentTask(taskID, func(task *agentwork.Task) error {
		result := agentwork.TestResult{}
		if task.LastTest != nil {
			result = task.LastTest.Clone()
		}
		result.Status = agentwork.TestStatusFailed
		if code == 0 {
			result.Status = agentwork.TestStatusPassed
		}
		result.ExitCode = &code
		result.FinishedAt = finishedAt
		task.LastTest = &result
		// In particular, do not derive Status or ReadyForReview from a test exit.
		return nil
	})
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
