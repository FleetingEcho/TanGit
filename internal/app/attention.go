package app

import (
	"context"

	"lazygitapp/internal/repository"
)

// confirmAttention is shared by every entry point for operations that need
// review. Capture their targets before opening the modal, rather than reading
// the current row or form again after the user confirms.
func (a *app) confirmAttention(title, target, detail string, run func()) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        title,
		message:      target,
		detail:       tr(detail),
		confirmLabel: "Continue",
		destructive:  title == "Amend commit" || title == "Resolve conflict",
		run:          run,
	})
}

func (a *app) confirmPush() {
	a.confirmAttention("Push", a.snapshot.Branch,
		"This publishes local commits to the remote branch. Review the destination and commits before continuing.", func() {
			a.beginRepositoryActionMessage(tr("Pushed the branch"), "", func(ctx context.Context, s repository.Session) error { return s.Push(ctx) }, nil)
		})
}

func (a *app) confirmPull() {
	a.confirmAttention("Pull", a.snapshot.Branch,
		"This integrates upstream changes into the current branch and working tree. Git may merge or rebase, and conflicts may require resolution.", func() {
			a.beginRepositoryActionMessage(tr("Pulled the latest changes"), "", func(ctx context.Context, s repository.Session) error { return s.Pull(ctx) }, nil)
		})
}

func (a *app) confirmPullStrategy(strategy repository.PullStrategy) {
	title := "Pull with merge"
	detail := "Fetch the upstream branch and merge it into the current branch. This creates a merge commit when needed, and conflicts may require resolution. It does not change your Git configuration."
	if strategy == repository.PullRebase {
		title = "Pull with rebase"
		detail = "Fetch the upstream branch and replay your local commits onto its tip. This rewrites your local commit history, and conflicts may require resolution. It does not change your Git configuration."
	}
	a.confirmAttention(title, a.snapshot.Branch, detail, func() {
		a.beginRepositoryActionMessage(tr("Pulled the latest changes"), "", func(ctx context.Context, s repository.Session) error {
			return s.PullWithStrategy(ctx, strategy)
		}, nil)
	})
}

func (a *app) confirmContinueOperation() {
	if hasConflictedFiles(a.snapshot.Files) {
		return
	}
	a.confirmAttention("Continue", a.snapshot.Operation,
		"This continues the interrupted Git operation using the current staged resolutions. Review them before continuing.", func() {
			a.beginRepositoryActionMessage(tr("Continued the operation"), "", func(ctx context.Context, s repository.Session) error { return s.ContinueOperation(ctx) }, nil)
		})
}
