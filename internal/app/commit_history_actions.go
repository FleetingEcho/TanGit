package app

import (
	"context"

	"lazygitapp/internal/repository"
)

// commitHistoryAction describes a one-shot history rewrite. Keeping its labels
// and repository call together lets the menu and keyboard share both the
// confirmation wording and safety checks.
type commitHistoryAction struct {
	title        string
	message      string
	detail       string
	confirmLabel string
	success      string
	destructive  bool
	run          func(context.Context, repository.Session) error
}

func (a *app) confirmCommitHistoryAction(commit repository.Commit, action commitHistoryAction) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        action.title,
		message:      action.message,
		detail:       commit.Subject + "\n\n" + action.detail,
		confirmLabel: action.confirmLabel,
		destructive:  action.destructive,
		run: func() {
			a.beginRepositoryActionMessage(action.success, "", action.run, nil)
		},
	})
}

func (a *app) confirmDropCommit(commit repository.Commit) {
	a.confirmCommitHistoryAction(commit, commitHistoryAction{
		title:        "Drop Commit",
		message:      trf("Drop %s from the current branch?", commit.ShortHash),
		detail:       tr("The commit and its changes are removed. It and every later commit will have new hashes."),
		confirmLabel: "Drop commit",
		success:      trf("Dropped %s", commit.ShortHash),
		destructive:  true,
		run: func(ctx context.Context, session repository.Session) error {
			return session.DropCommit(ctx, commit)
		},
	})
}

func (a *app) confirmMoveCommit(commit repository.Commit, direction repository.CommitMove) {
	newer := direction == repository.MoveCommitNewer
	action := commitHistoryAction{
		title:        "Move Commit",
		confirmLabel: "Move commit",
		destructive:  true,
		run: func(ctx context.Context, session repository.Session) error {
			return session.MoveCommit(ctx, commit, direction)
		},
	}
	if newer {
		action.message = trf("Move %s one place toward HEAD?", commit.ShortHash)
		action.detail = tr("This swaps the commit with its newer neighbor and rewrites both histories.")
		action.success = trf("Moved %s toward HEAD", commit.ShortHash)
	} else {
		action.message = trf("Move %s one place toward older history?", commit.ShortHash)
		action.detail = tr("This swaps the commit with its older neighbor and rewrites both histories.")
		action.success = trf("Moved %s toward older history", commit.ShortHash)
	}
	a.confirmCommitHistoryAction(commit, action)
}

func (a *app) confirmCombineCommit(commit repository.Commit, mode repository.CombineMode) {
	fixup := mode == repository.CombineFixup
	action := commitHistoryAction{
		title:        "Squash Commit",
		confirmLabel: "Squash commit",
		success:      trf("Squashed %s into its older commit", commit.ShortHash),
		destructive:  true,
		run: func(ctx context.Context, session repository.Session) error {
			return session.CombineCommit(ctx, commit, mode)
		},
	}
	action.message = trf("Squash %s into its older commit?", commit.ShortHash)
	action.detail = tr("Git combines both changes and commit messages. The selected commit and later commits receive new hashes.")
	if fixup {
		action.title = "Fixup Commit"
		action.message = trf("Fix up %s into its older commit?", commit.ShortHash)
		action.detail = tr("Git combines both changes and discards the selected commit's message. The selected commit and later commits receive new hashes.")
		action.confirmLabel = "Fixup commit"
		action.success = trf("Fixed up %s into its older commit", commit.ShortHash)
	}
	a.confirmCommitHistoryAction(commit, action)
}

func (a *app) confirmAmendCommit(commit repository.Commit) {
	a.confirmCommitHistoryAction(commit, commitHistoryAction{
		title:        "Amend Commit with Staged Changes",
		message:      trf("Add staged changes to %s?", commit.ShortHash),
		detail:       tr("Only staged changes are added. The message stays unchanged; amending an older commit rewrites it and later commits."),
		confirmLabel: "Amend commit",
		success:      trf("Amended %s with staged changes", commit.ShortHash),
		destructive:  true,
		run: func(ctx context.Context, session repository.Session) error {
			return session.AmendCommit(ctx, commit)
		},
	})
}

func (a *app) confirmCreateFixupCommit(commit repository.Commit) {
	a.confirmCommitHistoryAction(commit, commitHistoryAction{
		title:        "Create Fixup Commit",
		message:      trf("Create a fixup for %s from staged changes?", commit.ShortHash),
		detail:       tr("Git creates a new fixup! commit at HEAD. History is not rewritten until the fixups are applied."),
		confirmLabel: "Create fixup",
		success:      trf("Created a fixup for %s", commit.ShortHash),
		run: func(ctx context.Context, session repository.Session) error {
			return session.CreateFixupCommit(ctx, commit)
		},
	})
}

func (a *app) confirmApplyFixupCommits(commit repository.Commit) {
	a.confirmCommitHistoryAction(commit, commitHistoryAction{
		title:        "Apply Fixup Commits",
		message:      trf("Apply fixups from %s through HEAD?", commit.ShortHash),
		detail:       tr("Git autosquashes matching fixup! commits into their targets and rewrites every commit in this range."),
		confirmLabel: "Apply fixups",
		success:      trf("Applied fixups from %s", commit.ShortHash),
		destructive:  true,
		run: func(ctx context.Context, session repository.Session) error {
			return session.ApplyFixupCommits(ctx, commit)
		},
	})
}

func (a *app) selectedVisibleCommit() (repository.Commit, bool) {
	commits := a.visibleCommits()
	if a.selectedCommit < 0 || a.selectedCommit >= len(commits) {
		return repository.Commit{}, false
	}
	return commits[a.selectedCommit], true
}

func (a *app) confirmSelectedDropCommit() {
	if commit, ok := a.selectedVisibleCommit(); ok {
		a.confirmDropCommit(commit)
	}
}

func (a *app) confirmSelectedMoveCommit(direction repository.CommitMove) {
	if commit, ok := a.selectedVisibleCommit(); ok {
		a.confirmMoveCommit(commit, direction)
	}
}

func (a *app) confirmSelectedCombineCommit(mode repository.CombineMode) {
	if commit, ok := a.selectedVisibleCommit(); ok {
		a.confirmCombineCommit(commit, mode)
	}
}

func (a *app) confirmSelectedAmendCommit() {
	if commit, ok := a.selectedVisibleCommit(); ok {
		a.confirmAmendCommit(commit)
	}
}

// confirmEditCommit starts a rebase paused at the commit. The existing
// operation toolbar provides Continue and Abort, and the commit's changes can
// be edited or amended while Git waits.
func (a *app) confirmEditCommit(commit repository.Commit) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Edit Commit",
		message:      trf("Pause a rebase at %s?", commit.ShortHash),
		detail:       commit.Subject + "\n\n" + tr("Git rewrites the newer commits, then stops with this commit checked out. Edit or amend it, then Continue the rebase. Abort restores your original history."),
		confirmLabel: "Edit commit",
		run: func() {
			a.beginRepositoryActionMessage(trf("Paused rebase at %s", commit.ShortHash), "",
				func(ctx context.Context, session repository.Session) error {
					return session.EditCommit(ctx, commit)
				}, nil)
		},
	})
}
