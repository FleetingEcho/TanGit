package app

import (
	"context"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// sectionKeys are the digit shortcuts that switch sections, in the order of
// workspaceSections: 1 to 9, then 0 for the last.
var sectionKeys = []ui.Key{
	ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5,
	ui.Key6, ui.Key7, ui.Key8, ui.Key9, ui.Key0,
}

// registerShortcuts installs the keyboard shortcuts of the window. They
// register on the view's root, so they are window shortcuts that fire
// wherever the focus is; a shortcut on a container would fire only while
// something inside that container has the focus, which leaves the keyboard
// dead as a repository opens.
//
// While a form is open only Escape is registered: a text input still takes
// the letters it types before a window shortcut, but keeping them
// unregistered is what lets the list type-ahead and every other key press
// reach the form.
func (a *app) registerShortcuts(c *ui.Context) {
	// An open question owns Escape: it is how a destructive action is
	// called off, whether or not the shortcuts themselves are switched on.
	if a.confirm != nil {
		c.OnShortcut(0, ui.KeyEscape, a.cancelConfirm)
		return
	}
	// An open shortcut list owns Escape: it is how the dialog is dismissed,
	// whether or not the shortcuts themselves are switched on.
	if a.keybindingsOpen {
		c.OnShortcut(0, ui.KeyEscape, a.closeKeybindings)
		return
	}
	if a.commandLogOpen {
		c.OnShortcut(0, ui.KeyEscape, a.closeCommandLog)
		return
	}
	if a.settingsOpen {
		c.OnShortcut(0, ui.KeyEscape, a.closeSettings)
		return
	}
	if a.compare.open {
		c.OnShortcut(0, ui.KeyEscape, a.closeCompare)
		return
	}
	if a.diffFullscreen {
		c.OnShortcut(0, ui.KeyEscape, a.closeDiffFullscreen)
		return
	}
	if a.showCommitRewordForm {
		c.OnShortcut(0, ui.KeyEscape, a.closeCommitRewordForm)
		return
	}
	if a.showCommitTagForm || a.showCommitBranchForm {
		c.OnShortcut(0, ui.KeyEscape, func() {
			a.showCommitTagForm, a.showCommitBranchForm = false, false
		})
		return
	}
	if a.state == stateLoading {
		c.OnShortcut(0, ui.KeyEscape, a.cancelLoading)
		return
	}
	if a.showCommitForm {
		c.OnShortcut(0, ui.KeyEscape, func() { a.showCommitForm = false })
		return
	}
	// Picking a range of lines is a mode of the Changes page: Escape is how it
	// is left, the same as its Cancel button.
	if a.lineSelectMode {
		c.OnShortcut(0, ui.KeyEscape, a.stopLineSelection)
		return
	}
	// The settings dialog's switch turns every action shortcut off. The
	// header icons still reach each action, and the dialogs keep Escape.
	if a.settings.DisableShortcuts {
		return
	}
	// Preferences and the shortcut reference are always reachable, on the
	// picker as in the workspace, and so is the command log.
	c.OnShortcut(ui.Ctrl, ui.KeyComma, a.openSettings)
	c.OnShortcut(0, ui.KeyF1, a.openKeybindings)
	c.OnShortcut(ui.Shift, ui.KeySlash, a.openCurrentKeybindings)
	c.OnShortcut(ui.Ctrl, ui.KeyL, a.openCommandLog)
	if a.state != stateReady {
		return
	}
	if a.anyFormOpen() {
		return
	}

	// Ctrl+` is always live: it is how the terminal's keyboard is given back.
	c.OnShortcut(ui.Ctrl, ui.KeyBackquote, a.toggleTerminal)
	if a.term.open {
		// The shell owns the keyboard while its pane shows, so digits and
		// letters reach it instead of switching sections or acting on rows.
		return
	}

	// The pane's editor owns the keyboard while it shows: the letters it
	// takes are text, not section switches or actions on the chosen file.
	if a.fileEditing || (a.workspaceTab == sectionChanges && a.conflictState != nil) {
		if a.fileEditing {
			c.OnShortcut(0, ui.KeyEscape, a.closeFileEdit)
		} else {
			// Conflict choices use modified keys so typing in the marker editor
			// stays ordinary text. Navigation and undo match the toolbar exactly.
			c.OnShortcut(0, ui.KeyF7, func() { a.moveConflictSelection(1) })
			c.OnShortcut(ui.Shift, ui.KeyF7, func() { a.moveConflictSelection(-1) })
			c.OnShortcut(ui.Alt, ui.Key1, func() { a.resolveConflictSide(conflictCurrent) })
			c.OnShortcut(ui.Alt, ui.Key2, func() { a.resolveConflictSide(conflictBase) })
			c.OnShortcut(ui.Alt, ui.Key3, func() { a.resolveConflictSide(conflictIncoming) })
			c.OnShortcut(ui.Alt, ui.Key4, func() { a.resolveConflictSide(conflictBoth) })
			c.OnShortcut(ui.Alt, ui.KeyZ, a.undoConflictResolution)
		}
		c.OnShortcut(0, ui.KeyF5, a.beginRefresh)
		return
	}

	c.OnShortcut(0, ui.KeyF5, a.beginRefresh)
	c.OnShortcut(0, ui.KeyW, a.openCompare)
	c.OnShortcut(ui.Alt, ui.KeyW, a.toggleDiffWhitespace)
	c.OnShortcut(ui.Alt, ui.KeyU, a.cycleDiffContext)
	c.OnShortcut(0, ui.KeySlash, a.focusActiveSearch)
	c.OnShortcut(0, ui.KeyF3, func() { a.navigateActiveSearch(1) })
	c.OnShortcut(ui.Shift, ui.KeyF3, func() { a.navigateActiveSearch(-1) })
	c.OnShortcut(ui.Ctrl, ui.KeyZ, a.confirmUndo)
	c.OnShortcut(ui.Ctrl|ui.Shift, ui.KeyZ, a.confirmRedo)
	// The digits number the sections that show, so 1 is always the first
	// sidebar row even after some are hidden.
	visible := a.visibleSections()
	for i, key := range sectionKeys {
		if i >= len(visible) {
			break
		}
		tab := visible[i]
		c.OnShortcut(0, key, func() { a.setWorkspaceTab(tab) })
	}

	if a.fileHistoryOpen {
		c.OnShortcut(0, ui.KeyH, func() { a.compareSelectedCommit(false) })
		c.OnShortcut(ui.Shift, ui.KeyH, func() { a.compareSelectedCommit(true) })
		c.OnShortcut(ui.Shift, ui.KeyR, a.confirmRestoreSelectedHistoricalFile)
		return
	}
	switch a.workspaceTab {
	case sectionChanges:
		c.OnShortcut(0, ui.KeySpace, a.toggleStageSelected)
		c.OnShortcut(0, ui.KeyI, func() { a.ignoreSelectedFile(false) })
		c.OnShortcut(ui.Shift, ui.KeyI, func() { a.ignoreSelectedFile(true) })
		c.OnShortcut(0, ui.KeyD, a.confirmDiscardSelected)
		c.OnShortcut(0, ui.KeyB, a.beginBlame)
		c.OnShortcut(ui.Ctrl, ui.KeyEnter, a.beginCommit)
		if hasStagedFiles(a.snapshot.Files) {
			c.OnShortcut(0, ui.KeyC, func() { a.showCommitForm = true })
		}
	case sectionHistory, sectionGraph:
		c.OnShortcut(0, ui.KeyH, func() { a.compareSelectedCommit(false) })
		c.OnShortcut(ui.Shift, ui.KeyH, func() { a.compareSelectedCommit(true) })
		c.OnShortcut(ui.Shift, ui.KeyR, a.confirmRestoreSelectedHistoricalFile)
		c.OnShortcut(0, ui.KeyP, func() { a.confirmCommitAction(false) })
		c.OnShortcut(0, ui.KeyV, func() { a.confirmCommitAction(true) })
		c.OnShortcut(0, ui.KeyR, a.rewordSelectedCommit)
		c.OnShortcut(0, ui.KeyA, a.confirmSelectedAmendCommit)
		c.OnShortcut(0, ui.KeyS, func() { a.confirmSelectedCombineCommit(repository.CombineSquash) })
		c.OnShortcut(0, ui.KeyF, func() { a.confirmSelectedCombineCommit(repository.CombineFixup) })
		c.OnShortcut(0, ui.KeyD, a.confirmSelectedDropCommit)
		c.OnShortcut(ui.Ctrl, ui.KeyK, func() { a.confirmSelectedMoveCommit(repository.MoveCommitNewer) })
		c.OnShortcut(ui.Ctrl, ui.KeyJ, func() { a.confirmSelectedMoveCommit(repository.MoveCommitOlder) })
	case sectionBranches:
		c.OnShortcut(0, ui.KeyH, func() { a.compareSelectedCommit(false) })
		c.OnShortcut(ui.Shift, ui.KeyH, func() { a.compareSelectedCommit(true) })
		c.OnShortcut(ui.Shift, ui.KeyR, a.confirmRestoreSelectedHistoricalFile)
		c.OnShortcut(0, ui.KeySpace, a.checkoutSelectedBranch)
		c.OnShortcut(0, ui.KeyM, a.confirmMergeSelectedBranch)
		c.OnShortcut(0, ui.KeyD, a.confirmDeleteSelectedBranch)
		c.OnShortcut(0, ui.KeyN, a.beginNewBranch)
		c.OnShortcut(0, ui.KeyU, a.unsetSelectedUpstream)
	case sectionRemotes:
		c.OnShortcut(0, ui.KeyD, a.confirmRemoveSelectedRemote)
		c.OnShortcut(0, ui.KeyN, a.openAddRemote)
		c.OnShortcut(0, ui.KeyF, a.fetchSelectedRemote)
		c.OnShortcut(0, ui.KeyE, a.editSelectedRemote)
		c.OnShortcut(0, ui.KeyU, a.setSelectedRemoteBranchUpstream)
		c.OnShortcut(0, ui.KeySpace, a.checkoutSelectedRemoteBranch)
		c.OnShortcut(ui.Shift, ui.KeySpace, a.confirmCheckoutSelectedRemoteBranch)
		c.OnShortcut(0, ui.KeyM, a.confirmMergeSelectedRemoteBranch)
		c.OnShortcut(0, ui.KeyR, a.confirmRebaseSelectedRemoteBranch)
		c.OnShortcut(ui.Shift, ui.KeyD, a.confirmDeleteSelectedRemoteBranch)
		services := c.Services()
		c.OnShortcut(0, ui.KeyC, func() { a.copySelectedRemoteBranch(services) })
	case sectionStash:
		c.OnShortcut(0, ui.KeySpace, a.applySelectedStash)
		c.OnShortcut(0, ui.KeyP, func() { a.confirmStashMutation(true) })
		c.OnShortcut(0, ui.KeyD, func() { a.confirmStashMutation(false) })
	case sectionTags:
		c.OnShortcut(0, ui.KeyP, a.beginPushSelectedTag)
		c.OnShortcut(0, ui.KeyD, a.confirmDeleteSelectedTag)
		c.OnShortcut(0, ui.KeyN, a.openTagForm)
	case sectionWorktrees:
		c.OnShortcut(0, ui.KeyO, a.openSelectedWorktree)
		c.OnShortcut(0, ui.KeyD, a.confirmDeleteSelectedWorktree)
		c.OnShortcut(0, ui.KeyN, a.beginNewWorktree)
	case sectionSubmodules:
		c.OnShortcut(0, ui.KeyU, a.updateSelectedSubmodule)
		c.OnShortcut(0, ui.KeyD, a.confirmDeleteSelectedSubmodule)
		c.OnShortcut(0, ui.KeyN, func() { a.showSubmoduleForm = true })
	}
}

// focusActiveSearch makes / the common keyboard entry to filtering. History
// and Graph use their structured Git-backed filter; every list panel focuses
// its local search without moving the current selection.
func (a *app) focusActiveSearch() {
	count := 1 // Changes always draws its search, even with a clean tree.
	switch a.workspaceTab {
	case sectionHistory, sectionGraph:
		a.openCommitFilter(a.workspaceTab)
		return
	case sectionBranches:
		count = len(a.snapshot.Branches)
	case sectionRemotes:
		count = len(a.snapshot.Remotes)
	case sectionStash:
		count = len(a.snapshot.Stashes)
	case sectionTags:
		count = len(a.snapshot.Tags)
	case sectionWorktrees:
		count = len(a.snapshot.Worktrees)
	case sectionSubmodules:
		count = len(a.snapshot.Submodules)
	case sectionReflog:
		count = len(a.snapshot.Reflog)
	}
	if count == 0 {
		return
	}
	a.workspacePaneCollapsed[a.workspaceTab] = false
	a.sectionSearch[a.workspaceTab].Focus()
}

func (a *app) checkoutSelectedBranch() {
	if a.selectedBranch < 0 || a.selectedBranch >= len(a.snapshot.Branches) {
		return
	}
	if branch := a.snapshot.Branches[a.selectedBranch]; !branch.Head {
		a.checkoutBranch(branch.Name)
	}
}

func (a *app) beginNewBranch() {
	a.openBranchForm("")
}

func (a *app) applySelectedStash() {
	if a.selectedStash < 0 || a.selectedStash >= len(a.snapshot.Stashes) {
		return
	}
	a.applyStash(a.snapshot.Stashes[a.selectedStash])
}

func (a *app) beginPushSelectedTag() {
	if a.selectedTag < 0 || a.selectedTag >= len(a.snapshot.Tags) || len(a.snapshot.Remotes) == 0 {
		return
	}
	if a.tagRemote == "" {
		a.tagRemote = a.snapshot.Remotes[0].Name
	}
	a.showTagPushForm = true
}

func (a *app) openTagForm() {
	a.showTagForm = true
	a.tagRef = "HEAD"
}

func (a *app) beginNewWorktree() {
	a.showWorktreeForm = true
	a.worktreeBase = "HEAD"
}

func (a *app) updateSelectedSubmodule() {
	if a.selectedSubmodule < 0 || a.selectedSubmodule >= len(a.snapshot.Submodules) {
		return
	}
	submodule := a.snapshot.Submodules[a.selectedSubmodule]
	a.beginRepositoryActionMessage(trf("Updated %s", submodule.Name), "",
		func(ctx context.Context, session repository.Session) error {
			return session.UpdateSubmodule(ctx, submodule.Path)
		}, nil)
}
