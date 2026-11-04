package app

import (
	"context"

	"lazygitapp/internal/repository"
)

// ignoreSelectedFile records an untracked file in .gitignore, or in this
// clone's exclude file when local is true. Ignoring a tracked or staged file
// would not hide its changes, so the action deliberately does not offer it.
func (a *app) ignoreSelectedFile(local bool) {
	change, ok := a.selectedChange()
	if a.actionBusy || a.session == nil || !ok || change.file.Tracked || change.file.Staged {
		return
	}
	a.ignoreFile(change.file, local)
}

func (a *app) ignoreFile(file repository.File, local bool) {
	if a.actionBusy || a.session == nil || file.Tracked || file.Staged {
		return
	}
	message := trf("Added %s to .gitignore", file.Path)
	if local {
		message = trf("Added %s to .git/info/exclude", file.Path)
	}
	a.beginRepositoryActionMessage(message, file.Path,
		func(ctx context.Context, session repository.Session) error {
			if local {
				return session.ExcludeFile(ctx, file)
			}
			return session.IgnoreFile(ctx, file)
		}, nil)
}

// confirmRestoreCommitFile is the app's guard around a destructive historical
// restore. Git changes only the working tree; the index and history stay put.
func (a *app) confirmRestoreCommitFile(commit repository.Commit, file repository.CommitFile) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Restore Historical File",
		message:      trf("Replace %s with its version from %s?", file.Path, commit.ShortHash),
		detail:       "Current working-tree content at this path will be overwritten. The index will not change.",
		confirmLabel: "Restore file",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Restored %s from %s", file.Path, commit.ShortHash), file.Path,
				func(ctx context.Context, session repository.Session) error {
					return session.RestoreCommitFile(ctx, commit, file)
				}, nil)
		},
	})
}

// selectedHistoricalFile resolves Shift+R in the view that owns the current
// selection. File history takes precedence because its drawer sits above the
// ordinary page underneath it.
func (a *app) selectedHistoricalFile() (repository.Commit, repository.CommitFile, bool) {
	if a.fileHistoryOpen {
		if a.fileHistorySelected >= 0 && a.fileHistorySelected < len(a.fileHistoryCommits) &&
			a.fileHistorySelectedFile >= 0 && a.fileHistorySelectedFile < len(a.fileHistoryFiles) {
			return a.fileHistoryCommits[a.fileHistorySelected],
				a.fileHistoryFiles[a.fileHistorySelectedFile], true
		}
		return repository.Commit{}, repository.CommitFile{}, false
	}
	switch a.workspaceTab {
	case sectionHistory, sectionGraph:
		commit, ok := a.selectedVisibleCommit()
		if ok && a.selectedCommitFile >= 0 && a.selectedCommitFile < len(a.commitFiles) {
			return commit, a.commitFiles[a.selectedCommitFile], true
		}
	case sectionBranches:
		if a.selectedBranchCommit >= 0 && a.selectedBranchCommit < len(a.branchCommits) &&
			a.selectedBranchCommitFile >= 0 && a.selectedBranchCommitFile < len(a.branchCommitFiles) {
			return a.branchCommits[a.selectedBranchCommit],
				a.branchCommitFiles[a.selectedBranchCommitFile], true
		}
	}
	return repository.Commit{}, repository.CommitFile{}, false
}

func (a *app) confirmRestoreSelectedHistoricalFile() {
	if commit, file, ok := a.selectedHistoricalFile(); ok {
		a.confirmRestoreCommitFile(commit, file)
	}
}

func (a *app) selectedComparisonCommit() (repository.Commit, bool) {
	if a.fileHistoryOpen {
		if a.fileHistorySelected >= 0 && a.fileHistorySelected < len(a.fileHistoryCommits) {
			return a.fileHistoryCommits[a.fileHistorySelected], true
		}
		return repository.Commit{}, false
	}
	if a.workspaceTab == sectionBranches {
		if a.selectedBranchCommit >= 0 && a.selectedBranchCommit < len(a.branchCommits) {
			return a.branchCommits[a.selectedBranchCommit], true
		}
		return repository.Commit{}, false
	}
	if isCommitSection(a.workspaceTab) {
		return a.selectedVisibleCommit()
	}
	return repository.Commit{}, false
}

func (a *app) compareSelectedCommit(parent bool) {
	if commit, ok := a.selectedComparisonCommit(); ok {
		a.compareCommit(commit, parent)
	}
}

// compareCommit opens a prefilled comparison and runs it immediately. A root
// commit has no first parent, so Shift+H has nothing to compare for it.
func (a *app) compareCommit(commit repository.Commit, parent bool) {
	from, to := commit.Hash, "HEAD"
	if parent {
		if len(commit.Parents) == 0 {
			return
		}
		from, to = commit.Parents[0], commit.Hash
	}
	a.closeCompare()
	a.openCompare()
	if !a.compare.open {
		return
	}
	a.compare.from, a.compare.to = from, to
	a.beginCompare()
}
