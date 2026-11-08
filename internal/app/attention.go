package app

import (
	"context"
	"strings"
	"time"

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
	if a.actionBusy || a.session == nil {
		return
	}
	session := a.session
	generation := a.generation
	a.actionBusy = true
	go func() {
		defer a.recoverBackground()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stack, _ := session.StackedBranches(ctx)
		a.update(func() {
			if generation != a.generation {
				return
			}
			a.actionBusy = false
			a.confirmPushCandidates(stack)
		})
	}()
}

func (a *app) confirmPushCandidates(stack []string) {
	if len(stack) > 0 {
		detail := trf("This pushes %d stacked branch%s below %s first, then %s itself.", len(stack), pluralS(len(stack)), a.snapshot.Branch, a.snapshot.Branch) +
			"\n\n" + strings.Join(stack, " → ") + " → " + a.snapshot.Branch +
			"\n\n" + tr("Each push publishes that branch's unpushed commits. Nothing is force-pushed.")
		a.confirmAttention("Push", a.snapshot.Branch, detail, func() {
			a.beginRepositoryActionMessage(trf("Pushed %s and %d stacked branch%s", a.snapshot.Branch, len(stack), pluralS(len(stack))), "", func(ctx context.Context, s repository.Session) error {
				if err := s.PushStack(ctx, stack); err != nil {
					return err
				}
				return s.Push(ctx)
			}, nil)
		})
		return
	}
	a.confirmAttention("Push", a.snapshot.Branch,
		"This publishes local commits to the remote branch. Review the destination and commits before continuing.", func() {
			a.beginRepositoryActionMessage(tr("Pushed the branch"), "", func(ctx context.Context, s repository.Session) error { return s.Push(ctx) }, nil)
		})
}

// confirmPushFollowTags pushes the branch carrying the tags that point at the
// pushed commits, like git push --follow-tags.
func (a *app) confirmPushFollowTags() {
	a.confirmAttention("Push with tags", a.snapshot.Branch,
		"This publishes the branch like a normal push, and also pushes the tags that point at the commits being pushed.", func() {
			a.beginRepositoryActionMessage(tr("Pushed the branch with its tags"), "", func(ctx context.Context, s repository.Session) error { return s.PushFollowTags(ctx) }, nil)
		})
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
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
