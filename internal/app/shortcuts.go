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

// registerTerminalModalShortcuts keeps the terminal toggle in the active
// dialog's focus scope. MyGo deliberately suspends window shortcuts built
// behind a Modal, so the root registrations alone cannot provide a global
// toggle while a dialog is open.
func (a *app) registerTerminalModalShortcuts(back ui.Element) {
	if a.state != stateReady {
		return
	}
	back.OnShortcut(ui.Ctrl, ui.KeyBackquote, a.toggleTerminal)
	back.OnShortcut(ui.Ctrl|ui.Shift, ui.KeyBackquote, a.toggleTerminal)
}

// registerShortcuts installs the page's action shortcuts and the terminal's
// window-wide exception. The app view is nested in the tabbed shell, so the
// terminal must use OnWindowShortcut rather than the app container's scope.
//
// While a form is open its ordinary action shortcuts stay unregistered so
// text input and type-ahead receive those keys. The terminal toggle is the
// exception: it is registered before every modal and preference early return.
func (a *app) registerShortcuts(c *ui.Context) {
	if a.state == stateReady {
		// Ctrl+` works regardless of focus, terminal ownership, open forms or
		// the general shortcuts switch. Also accept the shifted physical key,
		// commonly written Ctrl+~, rather than making the user release Shift.
		c.OnWindowShortcut(ui.Ctrl, ui.KeyBackquote, a.toggleTerminal)
		c.OnWindowShortcut(ui.Ctrl|ui.Shift, ui.KeyBackquote, a.toggleTerminal)
	}
	// An open question owns Escape, whether or not the shortcuts themselves
	// are switched on. Its modal owns Enter, ahead of the focused button.
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
	if a.branchFullscreen {
		c.OnShortcut(0, ui.KeyEscape, a.escapeBranchFullscreen)
		if !a.settings.DisableShortcuts {
			c.OnShortcut(0, ui.KeyF6, func() { a.cycleKeyboardPane(c, 1) })
			c.OnShortcut(ui.Shift, ui.KeyF6, func() { a.cycleKeyboardPane(c, -1) })
		}
		return
	}
	if a.state == stateLoading {
		c.OnShortcut(0, ui.KeyEscape, a.cancelLoading)
		return
	}
	if a.showCommitForm {
		if !a.settings.DisableShortcuts {
			c.OnShortcut(ui.Ctrl, ui.KeyEnter, a.beginCommit)
		}
		c.OnShortcut(0, ui.KeyEscape, func() { a.showCommitForm = false })
		return
	}
	// Picking a range of lines is a mode of the Changes page: Escape is how it
	// is left, the same as its Cancel button.
	if a.lineSelectMode {
		c.OnShortcut(0, ui.KeyF6, func() { a.cycleKeyboardPane(c, 1) })
		c.OnShortcut(ui.Shift, ui.KeyF6, func() { a.cycleKeyboardPane(c, -1) })
		c.OnShortcut(0, ui.KeyEscape, a.stopLineSelection)
		return
	}
	// Saving belongs to editing, including while the file explorer has focus.
	if a.workspaceTab == sectionCode && a.state == stateReady {
		c.OnShortcut(ui.Cmd, ui.KeyS, func() { a.saveCodeDocument(a.activeCodeDocument()) })
		if ui.Cmd != ui.Ctrl {
			c.OnShortcut(ui.Ctrl, ui.KeyS, func() { a.saveCodeDocument(a.activeCodeDocument()) })
		}
		c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyS, a.saveCodeEdits)
	}
	// The hotkeys switch turns every action shortcut off. The
	// header icons still reach each action, and the dialogs keep Escape.
	if a.settings.DisableShortcuts {
		return
	}
	// Preferences and the shortcut reference are always reachable, on the
	// picker as in the workspace, and so is the command log.
	c.OnShortcut(ui.Ctrl, ui.KeyComma, a.openSettings)
	c.OnShortcut(0, ui.KeyF1, a.openKeybindings)
	if !c.FocusTakesText() {
		c.OnShortcut(ui.Shift, ui.KeySlash, a.openCurrentKeybindings)
	}
	c.OnShortcut(ui.Ctrl, ui.KeyL, a.openCommandLog)
	c.OnShortcut(0, ui.KeyF6, func() { a.cycleKeyboardPane(c, 1) })
	c.OnShortcut(ui.Shift, ui.KeyF6, func() { a.cycleKeyboardPane(c, -1) })
	c.OnShortcut(0, ui.KeyEscape, func() { a.keyboardBack(c) })
	if a.state != stateReady {
		return
	}
	if a.anyFormOpen() {
		return
	}

	if a.terminalVisible() {
		c.OnShortcut(ui.Ctrl, ui.KeyF, a.openTerminalSearch)
		if active := a.activeTerminal(); active != nil && active.searchOpen {
			c.OnShortcut(0, ui.KeyEscape, a.closeActiveTerminalSearch)
			c.OnShortcut(0, ui.KeyF3, func() { a.navigateTerminalSearch(1) })
			c.OnShortcut(ui.Shift, ui.KeyF3, func() { a.navigateTerminalSearch(-1) })
		}
		// The shell owns the other keys while its pane shows, so digits and
		// letters reach it instead of switching sections or acting on rows.
		return
	}

	// The pane's editor owns the keyboard while it shows: the letters it
	// takes are text, not section switches or actions on the chosen file.
	if a.fileEditing || (a.workspaceTab == sectionChanges && a.conflictState != nil && a.conflictStageTab == 3) {
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
	if a.workspaceTab == sectionCode {
		c.OnShortcut(ui.Cmd, ui.KeyP, a.focusCodeQuickOpen)
		c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyF, a.focusCodeSearch)
		c.OnShortcut(ui.Cmd, ui.KeyW, func() { a.closeCodeFile(a.code.active) })
	}
	c.OnShortcut(0, ui.KeyF3, func() { a.navigateActiveSearch(1) })
	c.OnShortcut(ui.Shift, ui.KeyF3, func() { a.navigateActiveSearch(-1) })
	if c.FocusTakesText() {
		return
	}
	c.OnShortcut(ui.Shift, ui.KeyW, a.openCompare)
	c.OnShortcut(0, ui.KeyW, a.openCompare)
	c.OnShortcut(ui.Alt, ui.KeyW, a.toggleDiffWhitespace)
	c.OnShortcut(ui.Alt, ui.KeyU, a.cycleDiffContext)
	if a.workspaceTab == sectionChanges {
		c.OnShortcut(ui.Alt, ui.KeyJ, func() { a.changedFileNavigation(1) })
		c.OnShortcut(ui.Alt, ui.KeyK, func() { a.changedFileNavigation(-1) })
	}
	c.OnShortcut(0, ui.KeySlash, a.focusActiveSearch)
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

	a.registerGitCommands(c)

}

// focusActiveSearch makes / the common keyboard entry to filtering. History
// and Graph use their structured Git-backed filter; every list panel focuses
// its local search without moving the current selection.
func (a *app) focusActiveSearch() {
	count := 1 // Changes always draws its search, even with a clean tree.
	switch a.workspaceTab {
	case sectionCode:
		if a.code.sidebar == codeSidebarSearch {
			a.focusCodeSearch()
		} else {
			a.focusCodeQuickOpen()
		}
		return
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
	a.confirmAttention("Update", submodule.Path, "This updates submodule working trees to their recorded commits. Review local submodule changes before continuing.", func() {
		a.beginRepositoryActionMessage(trf("Updated %s", submodule.Name), "",
			func(ctx context.Context, session repository.Session) error {
				return session.UpdateSubmodule(ctx, submodule.Path)
			}, nil)
	})
}
