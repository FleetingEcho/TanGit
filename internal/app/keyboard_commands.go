package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/agentwork"
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
		{"Changes", "Ctrl+I", "Preview and apply a patch or diff file", ui.Ctrl, ui.KeyI, (*app).beginPatchImport},
		{"Changes", "M", "Apply a mailbox patch with a three-way fallback…", ui.Shift, ui.KeyM, (*app).beginMailboxPatchImport},
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
			a.beginRepositoryActionMessage(tr(a.fetchSuccessMessage()), "", func(ctx context.Context, s repository.Session) error { return a.fetchWithSetting(ctx, s) }, nil)
		}},
		{"Changes", "i", "Ignore file in .gitignore", 0, ui.KeyI, func(a *app) { a.ignoreSelectedFile(false) }},
		{"Changes", "I", "Exclude file locally", ui.Shift, ui.KeyI, func(a *app) { a.ignoreSelectedFile(true) }},
		{"Changes", "d", "Discard the chosen file's changes", 0, ui.KeyD, (*app).confirmDiscardSelected},
		{"Changes", "b", "Blame the chosen file", 0, ui.KeyB, (*app).beginBlame},
		{"Changes", "r", "Mark the chosen file reviewed or unreviewed", 0, ui.KeyR, (*app).toggleSelectedReviewed},
		{"Changes", "R", "Go to the next unreviewed file", ui.Shift, ui.KeyR, (*app).selectNextUnreviewed},
		{"Changes", "Alt+J", "Go to the next changed file", ui.Alt, ui.KeyJ, func(a *app) { a.changedFileNavigation(1) }},
		{"Changes", "Alt+K", "Go to the previous changed file", ui.Alt, ui.KeyK, func(a *app) { a.changedFileNavigation(-1) }},
		{"History and graph", "Ctrl+Space", "Toggle batch selection", ui.Ctrl, ui.KeySpace, (*app).toggleSelectedCommitForBatch},
		{"History and graph", "Ctrl+B", "Add all files from the chosen commit to Custom Patch", ui.Ctrl, ui.KeyB, (*app).addSelectedCustomPatchCommit},
		{"History and graph", "B", "Review Custom Patch", ui.Shift, ui.KeyB, (*app).openCustomPatchBuilder},
		{"History and graph", "e", "Edit commit (pause rebase)", 0, ui.KeyE, func(a *app) {
			if c, ok := a.selectedVisibleCommit(); ok {
				a.confirmEditCommit(c)
			}
		}},
		{"History and graph", "r", "Reword the chosen commit", 0, ui.KeyR, (*app).rewordSelectedCommit},
		{"History and graph", "b", "Start bisect, or mark the current test commit bad", 0, ui.KeyB, func(a *app) {
			if a.snapshot.Bisect != nil && a.snapshot.Bisect.Active {
				a.confirmBisectMark("bad", "")
				return
			}
			if c, ok := a.selectedVisibleCommit(); ok {
				a.openBisectStartForm(c)
			}
		}},
		{"History and graph", "g", "Mark the current test commit good (bisect)", 0, ui.KeyG, func(a *app) { a.confirmBisectMark("good", "") }},
		{"History and graph", "k", "Skip the current test commit (bisect)", 0, ui.KeyK, func(a *app) { a.confirmBisectMark("skip", "") }},
		{"History and graph", "R", "Reset the current bisect", ui.Shift, ui.KeyR, (*app).confirmBisectReset},
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
		{"Branches", "Shift+O", "Create pull request…", ui.Shift, ui.KeyO, (*app).openCreatePullRequestForm},
		{"Branches", "Shift+G", "Open pull request in browser", ui.Shift, ui.KeyG, (*app).confirmOpenPullRequest},
		{"Branches", "Shift+K", "View pull request checks", ui.Shift, ui.KeyK, (*app).openPullRequestChecks},
		{"Remotes", "n", "Add remote", 0, ui.KeyN, (*app).openAddRemote},
		{"Remotes", "f", "Fetch remote", 0, ui.KeyF, (*app).fetchSelectedRemote},
		{"Remotes", "e", "Edit remote…", 0, ui.KeyE, (*app).editSelectedRemote},
		{"Remotes", "o", "Open the remote in a browser", 0, ui.KeyO, (*app).openSelectedRemoteWebURL},
		{"Remotes", "c", "Copy the remote's web URL", 0, ui.KeyC, (*app).copySelectedRemoteWebURL},
		{"Remotes", "Shift+O", "Edit the remote's web URL…", ui.Shift, ui.KeyO, (*app).editSelectedRemoteWebURL},
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
		{"Tags", "Shift+A", "Push all tags…", ui.Shift, ui.KeyA, (*app).beginPushAllTags},
		{"Worktrees", "n", "Create worktree", 0, ui.KeyN, (*app).beginNewWorktree},
		{"Worktrees", "o", "Open worktree", 0, ui.KeyO, (*app).openSelectedWorktree},
		{"Worktrees", "e", "Open worktree in editor", 0, ui.KeyE, (*app).openSelectedWorktreeInEditor},
		{"Worktrees", "t", "Open worktree terminal", 0, ui.KeyT, (*app).openSelectedWorktreeTerminal},
		{"Worktrees", "m", "Move worktree…", 0, ui.KeyM, (*app).openSelectedWorktreeMoveForm},
		{"Worktrees", "f", "Fetch worktree", 0, ui.KeyF, (*app).fetchSelectedWorktree},
		{"Worktrees", "l", "Lock or unlock worktree", 0, ui.KeyL, (*app).toggleSelectedWorktreeLock},
		{"Worktrees", "p", "Prune stale worktree records…", 0, ui.KeyP, (*app).confirmPruneWorktrees},
		{"Worktrees", "r", "Repair worktree metadata", 0, ui.KeyR, (*app).repairWorktrees},
		{"Worktrees", "d", "Remove worktree…", 0, ui.KeyD, (*app).confirmDeleteSelectedWorktree},
		{"Worktrees", "Shift+D", "Force remove worktree…", ui.Shift, ui.KeyD, (*app).confirmForceDeleteSelectedWorktree},
		{"Agent tasks", "n", "New Agent Task", 0, ui.KeyN, (*app).beginNewAgentTask},
		{"Agent tasks", "r", "Launch or resume selected agent", 0, ui.KeyR, func(a *app) {
			if task, ok := a.selectedAgentTask(); ok {
				a.launchAgentTask(task.ID, task.Status != agentwork.StatusReady)
			}
		}},
		{"Agent tasks", "o", "Open task worktree", 0, ui.KeyO, (*app).openSelectedAgentWorktree},
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
	commands = append(commands,
		keyboardCommand{"Historical files", "Ctrl+B", "Add the chosen historical file to Custom Patch", ui.Ctrl, ui.KeyB, (*app).addSelectedCustomPatchFile},
	)
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
	case sectionAgents:
		return "Agent tasks"
	case sectionSubmodules:
		return "Submodules"
	case sectionReflog:
		return "Reflog"
	}
	return ""
}

// registerCustomCommandKeys gives user bindings first refusal across all
// global workspace shortcuts. Callers must invoke it before global keys, after
// modal and text-input early returns so typing and dialogs remain intact.
func (a *app) registerCustomCommandKeys(c *ui.Context) {
	if c.FocusTakesText() || a.state != stateReady || a.terminalVisible() || a.fileEditing {
		return
	}
	if a.workspaceTab == sectionChanges && a.conflictState != nil && a.conflictStageTab == 3 {
		return
	}
	group := a.keyboardActionGroup(c)
	for _, command := range a.settings.CustomCommands {
		if !customCommandInContext(command, group) {
			continue
		}
		mods, key, ok := parseCustomCommandKey(command.Key)
		if !ok {
			continue
		}
		command := command
		c.OnShortcut(mods, key, func() { a.runCustomCommandByMenu(command) })
	}
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
