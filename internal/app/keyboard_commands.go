package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// These definitions drive both shortcut registration and the help reference.
// Shift is explicit: lazygit's p (pull) and P (push) are different commands.
type keyboardCommand struct {
	group, keys, label string
	mods               ui.Modifiers
	key                ui.Key
	run                func(*app)
}

func keyboardCommands() []keyboardCommand {
	commands := []keyboardCommand{
		{"Everywhere", "p", "Pull from the upstream branch", 0, ui.KeyP, (*app).confirmPull},
		{"Everywhere", "P", "Push to the remote", ui.Shift, ui.KeyP, (*app).confirmPush},
		{"Everywhere", "z", "Undo the latest eligible Git operation", 0, ui.KeyZ, (*app).confirmUndo},
		{"Everywhere", "Z", "Redo the latest undone Git operation", ui.Shift, ui.KeyZ, (*app).confirmRedo},
		{"Changes", "Space", "Stage or unstage the chosen file, or the lines picked in its diff", 0, ui.KeySpace, (*app).toggleStageSelected},
		{"Changes", "c", "Write a commit message", 0, ui.KeyC, func(a *app) {
			if hasStagedFiles(a.snapshot.Files) {
				a.showCommitForm = true
			}
		}},
		{"Changes", "Ctrl+Enter", "Commit the staged changes", ui.Ctrl, ui.KeyEnter, (*app).beginCommit},
		{"Changes", "A", "Amend the chosen commit with staged changes", ui.Shift, ui.KeyA, func(a *app) {
			if hasStagedFiles(a.snapshot.Files) {
				a.commitAmend = true
				a.showCommitForm = true
			}
		}},
		{"Changes", "a", "Stage all changes", 0, ui.KeyA, func(a *app) {
			a.beginRepositoryActionMessage(tr("Staged all changes"), "", func(ctx context.Context, s repository.Session) error { return s.StageAll(ctx) }, nil)
		}},
		{"Changes", "s", "Stash changes…", 0, ui.KeyS, (*app).openStashForm},
		{"Changes", "f", "Fetch from the remotes", 0, ui.KeyF, func(a *app) {
			a.beginRepositoryActionMessage(tr("Fetched from the remotes"), "", func(ctx context.Context, s repository.Session) error { return s.Fetch(ctx) }, nil)
		}},
		{"Changes", "i", "Ignore file in .gitignore", 0, ui.KeyI, func(a *app) { a.ignoreSelectedFile(false) }},
		{"Changes", "I", "Exclude file locally", ui.Shift, ui.KeyI, func(a *app) { a.ignoreSelectedFile(true) }},
		{"Changes", "d", "Discard the chosen file's changes", 0, ui.KeyD, (*app).confirmDiscardSelected},
		{"Changes", "b", "Blame the chosen file", 0, ui.KeyB, (*app).beginBlame},
		{"History and graph", "Ctrl+Space", "Toggle batch selection", ui.Ctrl, ui.KeySpace, (*app).toggleSelectedCommitForBatch},
		{"History and graph", "r", "Reword the chosen commit", 0, ui.KeyR, (*app).rewordSelectedCommit},
		{"History and graph", "A", "Amend the chosen commit with staged changes", ui.Shift, ui.KeyA, (*app).confirmSelectedAmendCommit},
		{"History and graph", "s", "Squash the chosen commit into its older neighbor", 0, ui.KeyS, func(a *app) { a.confirmSelectedCombineCommit(repository.CombineSquash) }},
		{"History and graph", "f", "Fix up the chosen commit into its older neighbor", 0, ui.KeyF, func(a *app) { a.confirmSelectedCombineCommit(repository.CombineFixup) }},
		{"History and graph", "F", "Create fixup commit from staged changes…", ui.Shift, ui.KeyF, func(a *app) {
			if c, ok := a.selectedVisibleCommit(); ok {
				a.confirmCreateFixupCommit(c)
			}
		}},
		{"History and graph", "S", "Apply fixups from this commit through HEAD…", ui.Shift, ui.KeyS, func(a *app) {
			if c, ok := a.selectedVisibleCommit(); ok {
				a.confirmApplyFixupCommits(c)
			}
		}},
		{"History and graph", "d", "Drop the chosen commit", 0, ui.KeyD, (*app).confirmSelectedDropCommit},
		{"History and graph", "t", "Revert the chosen commit", 0, ui.KeyT, func(a *app) { a.confirmCommitAction(true) }},
		{"History and graph", "C", "Copy commits for cherry-pick", ui.Shift, ui.KeyC, (*app).copyKeyboardCommits},
		{"History and graph", "V", "Cherry-pick copied commits", ui.Shift, ui.KeyV, (*app).pasteKeyboardCommits},
		{"History and graph", "Ctrl+K", "Move the chosen commit toward newer history", ui.Ctrl, ui.KeyK, func(a *app) { a.confirmSelectedMoveCommit(repository.MoveCommitNewer) }},
		{"History and graph", "Ctrl+J", "Move the chosen commit toward older history", ui.Ctrl, ui.KeyJ, func(a *app) { a.confirmSelectedMoveCommit(repository.MoveCommitOlder) }},
		{"History and graph", "Space", "Checkout this commit", 0, ui.KeySpace, func(a *app) {
			if c, ok := a.selectedVisibleCommit(); ok {
				a.confirmCheckoutCommit(c)
			}
		}},
		{"Branches", "Space", "Check out the chosen branch", 0, ui.KeySpace, (*app).checkoutSelectedBranch},
		{"Branches", "n", "Create a branch", 0, ui.KeyN, (*app).beginNewBranch},
		{"Branches", "M", "Merge into current…", ui.Shift, ui.KeyM, (*app).confirmMergeSelectedBranch},
		{"Branches", "r", "Rebase current onto this…", 0, ui.KeyR, func(a *app) {
			if b, ok := a.branchAt(a.selectedBranchRow); ok {
				a.confirmRebaseOntoBranch(b)
			}
		}},
		{"Branches", "d", "Delete the chosen branch", 0, ui.KeyD, (*app).confirmDeleteSelectedBranch},
		{"Branches", "u", "Unset upstream", 0, ui.KeyU, (*app).unsetSelectedUpstream},
		{"Remotes", "n", "Add remote", 0, ui.KeyN, (*app).openAddRemote},
		{"Remotes", "f", "Fetch remote", 0, ui.KeyF, (*app).fetchSelectedRemote},
		{"Remotes", "e", "Edit remote…", 0, ui.KeyE, (*app).editSelectedRemote},
		{"Remotes", "d", "Remove remote…", 0, ui.KeyD, (*app).confirmRemoveSelectedRemote},
		{"Remote branches", "Space", "Check out the chosen remote branch", 0, ui.KeySpace, (*app).checkoutSelectedRemoteBranch},
		{"Remote branches", "Shift+Space", "Checkout detached…", ui.Shift, ui.KeySpace, (*app).confirmCheckoutSelectedRemoteBranch},
		{"Remote branches", "u", "Set upstream", 0, ui.KeyU, (*app).setSelectedRemoteBranchUpstream},
		{"Remote branches", "M", "Merge into current…", ui.Shift, ui.KeyM, (*app).confirmMergeSelectedRemoteBranch},
		{"Remote branches", "r", "Rebase current onto this…", 0, ui.KeyR, (*app).confirmRebaseSelectedRemoteBranch},
		{"Remote branches", "d", "Delete remote branch…", 0, ui.KeyD, (*app).confirmDeleteSelectedRemoteBranch},
		{"Stash", "Space", "Apply the chosen stash", 0, ui.KeySpace, (*app).applySelectedStash},
		{"Stash", "g", "Pop (apply and drop)…", 0, ui.KeyG, func(a *app) { a.confirmStashMutation(true) }},
		{"Stash", "d", "Drop the chosen stash", 0, ui.KeyD, func(a *app) { a.confirmStashMutation(false) }},
		{"Tags", "n", "Create tag", 0, ui.KeyN, (*app).openTagForm},
		{"Tags", "d", "Delete tag…", 0, ui.KeyD, (*app).confirmDeleteSelectedTag},
		{"Tags", "Shift+P", "Push tag…", ui.Shift, ui.KeyP, (*app).beginPushSelectedTag},
		{"Worktrees", "n", "Create worktree", 0, ui.KeyN, (*app).beginNewWorktree},
		{"Worktrees", "o", "Open worktree", 0, ui.KeyO, (*app).openSelectedWorktree},
		{"Worktrees", "d", "Remove worktree…", 0, ui.KeyD, (*app).confirmDeleteSelectedWorktree},
		{"Submodules", "n", "Add Submodule", 0, ui.KeyN, func(a *app) { a.showSubmoduleForm = true }},
		{"Submodules", "u", "Update submodule", 0, ui.KeyU, (*app).updateSelectedSubmodule},
		{"Submodules", "d", "Delete submodule…", 0, ui.KeyD, (*app).confirmDeleteSelectedSubmodule},
	}
	for _, group := range []string{"History and graph", "Historical files"} {
		commands = append(commands,
			keyboardCommand{group, "h", "Compare commit with HEAD", 0, ui.KeyH, func(a *app) { a.compareSelectedCommit(false) }},
			keyboardCommand{group, "H", "Compare parent with commit", ui.Shift, ui.KeyH, func(a *app) { a.compareSelectedCommit(true) }},
			keyboardCommand{group, "Shift+R", "Restore the chosen historical file after confirmation", ui.Shift, ui.KeyR, (*app).confirmRestoreSelectedHistoricalFile},
		)
	}
	return commands

}

func (a *app) keyboardActionGroup(c *ui.Context) string {
	switch a.focusedKeyboardPane(c) {
	case "toolbar", "status", "sidebar":
		return ""
	case "diff", "history-diff", "branch-diff", "stash-diff", "file-history-diff", "comparison-diff":
		return "Diff review"
	case "commit-files", "branch-files", "stash-files", "comparison-files", "file-history-files", "file-history", "branch-commits":
		return "Historical files"

	case "remote-branches":
		return "Remote branches"
	}
	switch a.workspaceTab {
	case sectionChanges:
		return "Changes"
	case sectionHistory, sectionGraph:
		return "History and graph"
	case sectionBranches:
		return "Branches"
	case sectionRemotes:
		return "Remotes"
	case sectionStash:
		return "Stash"
	case sectionTags:
		return "Tags"
	case sectionWorktrees:
		return "Worktrees"
	case sectionSubmodules:
		return "Submodules"
	case sectionReflog:
		return "Reflog"
	}
	return ""
}

func (a *app) registerGitCommands(c *ui.Context) {
	if c.FocusTakesText() {
		return
	}
	group := a.keyboardActionGroup(c)
	for _, command := range keyboardCommands() {
		// A selected tag's publish action takes the same P as global push.
		if command.group == "Everywhere" && group == "Tags" && command.key == ui.KeyP && command.mods == ui.Shift {
			continue
		}
		if command.group != "Everywhere" && command.group != group {
			continue
		}
		command := command
		c.OnShortcut(command.mods, command.key, func() { command.run(a) })
	}
}

func (a *app) copyKeyboardCommits() {
	var selected []repository.Commit
	if isCommitSection(a.workspaceTab) && !a.fileHistoryOpen {
		selected = a.selectedCommitsForBatch()
	}
	if len(selected) == 0 {
		if commit, ok := a.selectedComparisonCommit(); ok {
			selected = []repository.Commit{commit}
		}
	}
	if len(selected) == 0 {
		return
	}
	a.keyboardCopied = append([]repository.Commit(nil), selected...)
	a.notify("success", "Copied commits for cherry-pick", tr("Press Shift+V in History to cherry-pick the copied commits."))
}

func (a *app) pasteKeyboardCommits() {
	if len(a.keyboardCopied) == 0 || a.actionBusy || a.session == nil {
		return
	}
	commits := append([]repository.Commit(nil), a.keyboardCopied...)
	lines := make([]string, len(commits))
	for i, commit := range commits {
		lines[i] = commit.ShortHash + " " + commit.Subject
	}
	a.askConfirm(confirmRequest{title: "Cherry-pick Commit", message: trf("Cherry-pick %d commits onto %s?", len(commits), a.snapshot.Branch), detail: strings.Join(lines, "\n"), confirmLabel: "Cherry-pick", run: func() {
		a.beginRepositoryActionMessage(tr("Applied batch commit action"), "", func(ctx context.Context, s repository.Session) error { return s.CherryPickCommits(ctx, commits) }, nil)
	}})
}
