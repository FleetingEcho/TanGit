package app

import (
	"context"
	"strings"
	"time"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// beginFetch is the single repository-fetch entry shared by toolbar,
// keyboard and Command Catalog actions.
func (a *app) beginFetch() {
	a.beginRepositoryActionMessage(tr(a.fetchSuccessMessage()), "", func(ctx context.Context, session repository.Session) error {
		return a.fetchWithSetting(ctx, session)
	}, nil)
}

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
	if a.actionBusy || a.session == nil || a.confirm != nil {
		return
	}
	session := a.session
	generation := a.generation
	// Preflight only gathers preview information. It must not mark an actual
	// push as running before its confirmation has even appeared.
	var stack []string
	a.confirmAttention("Push", a.snapshot.Branch,
		"This publishes local commits to the remote branch. Review the destination and commits before continuing.",
		func() { a.pushBranches(stack) })
	request := a.confirm
	request.disabled = true
	parent := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		ctx, cancel := context.WithTimeout(parent, 10*time.Second)
		defer cancel()
		discovered, _ := requireBranchRemoteSession(session).StackedBranches(ctx)
		a.update(func() {
			// A close, accepted confirmation, or a different repository invalidates
			// this preview. In particular, cancellation cannot open it again.
			if generation != a.generation || a.confirm != request {
				return
			}
			stack = discovered
			a.completePushConfirmation(request, discovered)
		})
	}()
}

func (a *app) completePushConfirmation(request *confirmRequest, stack []string) {
	request.disabled = false
	if len(stack) == 0 {
		return
	}
	request.detail = trf("This pushes %d stacked branch%s below %s first, then %s itself.", len(stack), pluralS(len(stack)), a.snapshot.Branch, a.snapshot.Branch) +
		"\n\n" + strings.Join(stack, " → ") + " → " + a.snapshot.Branch +
		"\n\n" + tr("Each push publishes that branch's unpushed commits. Nothing is force-pushed.")
}

// pushBranches is called only by the accepted confirmation, with the preview
// finished. Its actionBusy/Working transition therefore describes real work.
func (a *app) pushBranches(stack []string) {
	if len(stack) == 0 {
		a.beginRepositoryActionMessage(tr("Pushed the branch"), "", func(ctx context.Context, s repository.Session) error { return requireBranchRemoteSession(s).Push(ctx) }, nil)
		return
	}
	a.beginRepositoryActionMessage(trf("Pushed %s and %d stacked branch%s", a.snapshot.Branch, len(stack), pluralS(len(stack))), "", func(ctx context.Context, s repository.Session) error {
		if err := requireBranchRemoteSession(s).PushStack(ctx, stack); err != nil {
			return err
		}
		return requireBranchRemoteSession(s).Push(ctx)
	}, nil)
}

// confirmPushFollowTags pushes the branch carrying the tags that point at the
// pushed commits, like git push --follow-tags.
func (a *app) confirmPushFollowTags() {
	a.confirmAttention("Push with tags", a.snapshot.Branch,
		"This publishes the branch like a normal push, and also pushes the tags that point at the commits being pushed.", func() {
			a.beginRepositoryActionMessage(tr("Pushed the branch with its tags"), "", func(ctx context.Context, s repository.Session) error {
				return requireBranchRemoteSession(s).PushFollowTags(ctx)
			}, nil)
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
			a.beginRepositoryActionMessage(tr("Pulled the latest changes"), "", func(ctx context.Context, s repository.Session) error { return requireBranchRemoteSession(s).Pull(ctx) }, nil)
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
			return requireBranchRemoteSession(s).PullWithStrategy(ctx, strategy)
		}, nil)
	})
}

func (a *app) confirmContinueOperation() {
	if a.actionBusy || a.session == nil || hasConflictedFiles(a.snapshot.Files) ||
		!a.operationCan(repository.OperationActionContinue) {
		return
	}
	if a.location.IsRemote() {
		a.notify("warning", "Continue checks run locally", tr("Continue checks use fully trusted local shell commands in the repository root. They are not sandboxed and cannot safely act on an SSH workspace, so Continue is disabled here."))
		return
	}
	a.openContinueReview()
}
