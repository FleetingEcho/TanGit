package app

import (
	"context"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

// agentAcceptEligibility deliberately excludes uncommitted output: Accept is
// a committed-delivery action and must not report the whole final task as done
// while staged, unstaged or untracked work remains only in its worktree.
func (a *app) agentAcceptEligibility() (string, bool) {
	review := &a.agents.review
	task, ok := a.agentReviewTask()
	if !ok || !review.open || review.loading || review.err != "" || a.actionBusy {
		return "", false
	}
	if task.Status != agentwork.StatusReadyForReview || len(review.commits) == 0 || len(review.dirtyFiles) != 0 {
		return "", false
	}
	return task.ID, true
}

func (a *app) confirmAcceptAgentCommits() {
	taskID, ok := a.agentAcceptEligibility()
	if !ok {
		return
	}
	commits := append([]repository.Commit(nil), a.agents.review.commits...)
	branch := a.snapshot.Branch
	a.askConfirm(confirmRequest{
		title:        "Accept agent commits",
		message:      trf("Cherry-pick %d agent commits into %s?", len(commits), branch),
		detail:       tr("Git applies the captured commit range oldest first. Conflicts leave the task ready for review so you can resolve or abort the operation."),
		confirmLabel: "Cherry-pick commits",
		run: func() {
			a.beginRepositoryActionMessage(tr("Accepted agent commits"), "", func(ctx context.Context, session repository.Session) error {
				return session.CherryPickCommits(ctx, commits)
			}, func() {
				a.finishAcceptedAgentTask(taskID)
			})
		},
	})
}

func (a *app) confirmMergeAgentTask() {
	taskID, ok := a.agentAcceptEligibility()
	if !ok {
		return
	}
	task, found := a.agentReviewTask()
	if !found || task.Branch == "" {
		return
	}
	branch := a.snapshot.Branch
	a.askConfirm(confirmRequest{
		title:        "Accept agent branch",
		message:      trf("Merge %s into %s?", task.Branch, branch),
		detail:       tr("Git creates a merge commit when a fast-forward is not possible. The task worktree and branch are preserved after acceptance."),
		confirmLabel: "Merge agent branch",
		run: func() {
			a.beginRepositoryActionMessage(tr("Accepted agent branch"), "", func(ctx context.Context, session repository.Session) error {
				return session.MergeBranch(ctx, task.Branch)
			}, func() {
				a.finishAcceptedAgentTask(taskID)
			})
		},
	})
}

func (a *app) finishAcceptedAgentTask(taskID string) {
	a.mutateAgentTask(taskID, func(task *agentwork.Task) error {
		if task.Status == agentwork.StatusReadyForReview {
			task.Status = agentwork.StatusDone
		}
		return nil
	})
}
