package app

import (
	"context"
	"fmt"

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
	a.onElementShortcut(back, ui.Ctrl, ui.KeyBackquote, shortcut("terminal/modal-toggle", "Ctrl+`", tr("Toggle terminal")), a.toggleTerminal)
	a.onElementShortcut(back, ui.Ctrl|ui.Shift, ui.KeyBackquote, shortcut("terminal/modal-toggle-shifted", "Ctrl+~", tr("Toggle terminal")), a.toggleTerminal)
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
		a.onWindowShortcut(c, ui.Ctrl, ui.KeyBackquote, shortcut("terminal/toggle", "Ctrl+`", tr("Toggle terminal")), a.toggleTerminal)
		a.onWindowShortcut(c, ui.Ctrl|ui.Shift, ui.KeyBackquote, shortcut("terminal/toggle-shifted", "Ctrl+~", tr("Toggle terminal")), a.toggleTerminal)
	}
	// An open question owns Escape, whether or not the shortcuts themselves
	// are switched on. Its modal owns Enter, ahead of the focused button.
	if a.confirm != nil {
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("confirmation/cancel", "Escape", tr("Cancel")), a.cancelConfirm)
		return
	}
	// An open shortcut list owns Escape: it is how the dialog is dismissed,
	// whether or not the shortcuts themselves are switched on.
	if a.keybindingsOpen {
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("keybindings/close", "Escape", tr("Close a dialog or cancel the current step")), a.closeKeybindings)
		return
	}
	if a.commandLogOpen {
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("command-log/close", "Escape", tr("Close command log")), a.closeCommandLog)
		return
	}
	if a.commandPromptOpen {
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("command-prompt/close", "Escape", tr("Close a dialog or cancel the current step")), a.closeCommandPrompt)
		return
	}
	if a.prChecksOpen {
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("branches/pr-checks-close", "Escape", tr("Close a dialog or cancel the current step")), func() { a.prChecksOpen = false; a.prChecksToken++ })
		return
	}
	if a.settingsOpen {
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("settings/close", "Escape", tr("Close settings")), a.closeSettings)
		return
	}
	if a.patchPreviewOpen {
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("patch-preview/close", "Escape", tr("Close a dialog or cancel the current step")), a.discardPatchPreview)
		return
	}
	if a.customPatch.open {
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("history/custom-patch-close", "Escape", tr("Close a dialog or cancel the current step")), func() {
			if a.customPatch.previewOpen {
				a.backToCustomPatchBuilder()
			} else {
				a.closeCustomPatchBuilder()
			}
		})
		return
	}
	if a.diffFullscreen {
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("diff/close-fullscreen", "Escape", tr("Close a dialog or cancel the current step")), a.closeDiffFullscreen)
		return
	}
	if a.showCommitRewordForm {
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("history/reword-cancel", "Escape", tr("Close a dialog or cancel the current step")), a.closeCommitRewordForm)
		return
	}
	if a.showCommitTagForm || a.showCommitBranchForm {
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("commit-form/close-tag-branch", "Escape", tr("Close a dialog or cancel the current step")), func() {
			a.showCommitTagForm, a.showCommitBranchForm = false, false
		})
		return
	}
	if a.branchFullscreen {
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("branches/close-fullscreen", "Escape", tr("Close a dialog or cancel the current step")), a.escapeBranchFullscreen)
		if !a.settings.DisableShortcuts {
			a.onShortcut(c, 0, ui.KeyF6, shortcut("panes/cycle-next-fullscreen", "F6", tr("Move between panels")), func() { a.cycleKeyboardPane(c, 1) })
			a.onShortcut(c, ui.Shift, ui.KeyF6, shortcut("panes/cycle-previous-fullscreen", "Shift+F6", tr("Move between panels")), func() { a.cycleKeyboardPane(c, -1) })
		}
		return
	}
	if a.cloneOpen {
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("clone/close-form", "Escape", tr("Close a dialog or cancel the current step")), a.closeCloneForm)
		return
	}
	if a.state == stateCloning {
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("clone/cancel", "Escape", tr("Cancel cloning or close the clone form")), a.cancelCloneOperation)
		if !a.settings.DisableShortcuts {
			a.onShortcut(c, 0, ui.KeyF1, shortcut("keybindings/cloning-help", "F1", tr("Open the complete shortcut list")), a.openKeybindings)
			if !c.FocusTakesText() {
				a.onShortcut(c, ui.Shift, ui.KeySlash, shortcut("keybindings/cloning-current", "?", tr("Show shortcuts for the current page")), a.openCurrentKeybindings)
			}
		}
		return
	}
	if a.state == stateLoading {
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("repository/cancel-loading", "Escape", tr("Close a dialog or cancel the current step")), a.cancelLoading)
		return
	}
	if a.showCommitForm {
		if !a.settings.DisableShortcuts {
			a.onShortcut(c, ui.Ctrl, ui.KeyEnter, shortcut("changes/commit-staged", "Ctrl+Enter", tr("Commit the staged changes")), a.beginCommit)
		}
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("changes/cancel-commit", "Escape", tr("Cancel")), func() { a.showCommitForm = false })
		return
	}
	// Picking a range of lines is a mode of the Changes page: Escape is how it
	// is left, the same as its Cancel button.
	if a.lineSelectMode {
		a.onShortcut(c, 0, ui.KeyF6, shortcut("panes/cycle-next-line-selection", "F6", tr("Move between panels")), func() { a.cycleKeyboardPane(c, 1) })
		a.onShortcut(c, ui.Shift, ui.KeyF6, shortcut("panes/cycle-previous-line-selection", "Shift+F6", tr("Move between panels")), func() { a.cycleKeyboardPane(c, -1) })
		a.onShortcut(c, 0, ui.KeyEscape, shortcut("diff/stop-line-selection", "Escape", tr("Close a dialog or cancel the current step")), a.stopLineSelection)
		return
	}
	// Custom commands are registered before the Code save shortcuts too.
	// The switch disables both the defaults and user-defined bindings.
	if a.settings.DisableShortcuts {
		return
	}
	a.registerCustomCommandKeys(c)

	// Saving belongs to editing, including while the file explorer has focus.
	if a.workspaceTab == sectionCode && a.state == stateReady {
		a.onShortcut(c, ui.Cmd, ui.KeyS, shortcut("code/save-active", cmdShortcutChord()+"+S", tr("Save")), func() { a.saveCodeDocument(a.activeCodeDocument()) })
		if ui.Cmd != ui.Ctrl {
			a.onShortcut(c, ui.Ctrl, ui.KeyS, shortcut("code/save-active-control", "Ctrl+S", tr("Save")), func() { a.saveCodeDocument(a.activeCodeDocument()) })
		}
		a.onShortcut(c, ui.Cmd|ui.Shift, ui.KeyS, shortcut("code/save-all", cmdShortcutChord()+"+Shift+S", tr("Save")), a.saveCodeEdits)
	}
	// Preferences and the shortcut reference are always reachable, on the
	// picker as in the workspace, and so is the command log.
	a.onShortcut(c, ui.Ctrl, ui.KeyComma, shortcut("settings/open", "Ctrl+,", tr("Open settings")), a.openSettings)
	a.onShortcut(c, 0, ui.KeyF1, shortcut("keybindings/open-all", "F1", tr("Open the complete shortcut list")), a.openKeybindings)
	if !c.FocusTakesText() {
		a.onShortcut(c, ui.Shift, ui.KeySlash, shortcut("keybindings/open-current", "?", tr("Show shortcuts for the current page")), a.openCurrentKeybindings)
	}
	if !a.editorOnlyWorkspace() {
		a.onShortcut(c, ui.Ctrl, ui.KeyL, shortcut("command-log/open", "Ctrl+L", tr("Show the command log")), a.openCommandLog)
	}
	a.onShortcut(c, 0, ui.KeyF6, shortcut("panes/cycle-next", "F6", tr("Move between panels")), func() { a.cycleKeyboardPane(c, 1) })
	a.onShortcut(c, ui.Shift, ui.KeyF6, shortcut("panes/cycle-previous", "Shift+F6", tr("Move between panels")), func() { a.cycleKeyboardPane(c, -1) })
	a.onShortcut(c, 0, ui.KeyEscape, shortcut("navigation/back", "Escape", tr("Close a dialog or cancel the current step")), func() { a.keyboardBack(c) })
	if (a.state == stateEmpty || a.state == stateError) && !c.FocusTakesText() {
		a.onShortcut(c, 0, ui.KeyC, shortcut("picker/clone", "C", tr("Clone a remote repository")), a.openCloneForm)
	}
	if a.state != stateReady {
		return
	}
	if a.anyFormOpen() {
		return
	}

	if a.terminalVisible() {
		a.onShortcut(c, ui.Ctrl, ui.KeyF, shortcut("terminal/search", "Ctrl+F", tr("Search the active terminal")), a.openTerminalSearch)
		if active := a.activeTerminal(); active != nil && active.searchOpen {
			a.onShortcut(c, 0, ui.KeyEscape, shortcut("terminal/search-close", "Escape", tr("Close a dialog or cancel the current step")), a.closeActiveTerminalSearch)
			a.onShortcut(c, 0, ui.KeyF3, shortcut("terminal/search-next", "F3", tr("Move to the next / previous quick-search result")), func() { a.navigateTerminalSearch(1) })
			a.onShortcut(c, ui.Shift, ui.KeyF3, shortcut("terminal/search-previous", "Shift+F3", tr("Move to the next / previous quick-search result")), func() { a.navigateTerminalSearch(-1) })
		}
		// The shell owns the other keys while its pane shows, so digits and
		// letters reach it instead of switching sections or acting on rows.
		return
	}

	// The pane's editor owns the keyboard while it shows: the letters it
	// takes are text, not section switches or actions on the chosen file.
	if a.fileEditing || (a.workspaceTab == sectionChanges && a.conflictState != nil && a.conflictStageTab == 3) {
		if a.fileEditing {
			a.onShortcut(c, 0, ui.KeyEscape, shortcut("editor/close-file-edit", "Escape", tr("Close a dialog or cancel the current step")), a.closeFileEdit)
		} else {
			// Conflict choices use modified keys so typing in the marker editor
			// stays ordinary text. Navigation and undo match the toolbar exactly.
			a.onShortcut(c, 0, ui.KeyF7, shortcut("conflicts/next", "F7", tr("Move to the next / previous conflict")), func() { a.moveConflictSelection(1) })
			a.onShortcut(c, ui.Shift, ui.KeyF7, shortcut("conflicts/previous", "Shift+F7", tr("Move to the next / previous conflict")), func() { a.moveConflictSelection(-1) })
			a.onShortcut(c, ui.Alt, ui.Key1, shortcut("conflicts/keep-current", "Alt+1", tr("Keep the current / ancestor conflict hunk")), func() { a.resolveConflictSide(conflictCurrent) })
			a.onShortcut(c, ui.Alt, ui.Key2, shortcut("conflicts/keep-ancestor", "Alt+2", tr("Keep the current / ancestor conflict hunk")), func() { a.resolveConflictSide(conflictBase) })
			a.onShortcut(c, ui.Alt, ui.Key3, shortcut("conflicts/keep-incoming", "Alt+3", tr("Keep the incoming conflict hunk / both hunks")), func() { a.resolveConflictSide(conflictIncoming) })
			a.onShortcut(c, ui.Alt, ui.Key4, shortcut("conflicts/keep-both", "Alt+4", tr("Keep the incoming conflict hunk / both hunks")), func() { a.resolveConflictSide(conflictBoth) })
			// While Result has no unsaved manual text, conventional undo applies
			// to the hunk resolutions too. A dirty editor keeps its native text
			// undo until that buffer is saved, so the two histories do not race.
			if a.conflictView == nil || a.conflictView.Buffer == nil || !a.conflictView.Buffer.Modified() {
				a.onShortcut(c, ui.Cmd, ui.KeyZ, shortcut("conflicts/undo-command", cmdShortcutChord()+"+Z", tr("Undo the last conflict resolution")), a.undoConflictResolution)
				if ui.Cmd != ui.Ctrl {
					a.onShortcut(c, ui.Ctrl, ui.KeyZ, shortcut("conflicts/undo-control", "Ctrl+Z", tr("Undo the last conflict resolution")), a.undoConflictResolution)
				}
			}
			a.onShortcut(c, ui.Alt, ui.KeyZ, shortcut("conflicts/undo-resolution", "Alt+Z", tr("Undo the last conflict resolution")), a.undoConflictResolution)
		}
		if !a.editorOnlyWorkspace() {
			a.onShortcut(c, 0, ui.KeyF5, shortcut("repository/refresh-editing", "F5", tr("Refresh the repository")), a.beginRefresh)
		}
		return
	}

	// Custom bindings take precedence not just over Git commands but also F5,
	// Ctrl+L, section digits and navigation globals registered below.
	if !a.editorOnlyWorkspace() {
		a.onShortcut(c, 0, ui.KeyF5, shortcut("repository/refresh", "F5", tr("Refresh the repository")), a.beginRefresh)
	}
	if a.workspaceTab == sectionCode {
		a.onShortcut(c, ui.Cmd, ui.KeyP, shortcut("code/quick-open", cmdShortcutChord()+"+P", tr("Search files")), a.focusCodeQuickOpen)
		a.onShortcut(c, ui.Cmd|ui.Shift, ui.KeyF, shortcut("code/search", cmdShortcutChord()+"+Shift+F", tr("Search files")), a.focusCodeSearch)
		a.onShortcut(c, ui.Cmd|ui.Shift, ui.KeyH, shortcut("code/replace", cmdShortcutChord()+"+Shift+H", tr("Search files")), a.focusCodeReplace)
		a.onShortcut(c, ui.Cmd, ui.KeyW, shortcut("code/close-file", cmdShortcutChord()+"+W", tr("Close the active tab")), func() { a.closeCodeFile(a.code.active) })
	}
	a.onShortcut(c, 0, ui.KeyF3, shortcut("search/next", "F3", tr("Move to the next / previous quick-search result")), func() { a.navigateActiveSearch(1) })
	a.onShortcut(c, ui.Shift, ui.KeyF3, shortcut("search/previous", "Shift+F3", tr("Move to the next / previous quick-search result")), func() { a.navigateActiveSearch(-1) })
	if c.FocusTakesText() {
		return
	}
	if !a.editorOnlyWorkspace() {
		a.onShortcut(c, ui.Shift, ui.KeyW, shortcut("compare/open-shifted", "Shift+W", tr("Compare two refs")), a.openCompare)
		a.onShortcut(c, 0, ui.KeyW, shortcut("compare/open", "W", tr("Compare two refs")), a.openCompare)
		a.onShortcut(c, ui.Alt, ui.KeyW, shortcut("diff/toggle-whitespace", "Alt+W", tr("Ignore or show whitespace-only diff changes")), a.toggleDiffWhitespace)
		a.onShortcut(c, ui.Alt, ui.KeyU, shortcut("diff/context-lines", "Alt+U", tr("Cycle ordinary diff context lines")), a.cycleDiffContext)
	}
	if a.workspaceTab == sectionChanges {
		a.onShortcut(c, ui.Alt, ui.KeyJ, shortcut("changes/next-file", "Alt+J", tr("Go to the next changed file")), func() { a.changedFileNavigation(1) })
		a.onShortcut(c, ui.Alt, ui.KeyK, shortcut("changes/previous-file", "Alt+K", tr("Go to the previous changed file")), func() { a.changedFileNavigation(-1) })
	}
	a.onShortcut(c, 0, ui.KeySlash, shortcut("search/focus-active", "/", tr("Search the active panel")), a.focusActiveSearch)
	if !a.editorOnlyWorkspace() {
		a.onShortcut(c, ui.Shift, ui.KeySemicolon, shortcut("commands/prompt", ":", tr("Run a one-off shell command")), a.openCommandPrompt)
		a.onShortcut(c, ui.Ctrl, ui.KeyZ, shortcut("repository/undo", "Ctrl+Z", tr("Undo the latest eligible Git operation")), a.confirmUndo)
		a.onShortcut(c, ui.Ctrl|ui.Shift, ui.KeyZ, shortcut("repository/redo", "Ctrl+Shift+Z", tr("Redo the latest undone Git operation")), a.confirmRedo)
	}
	// The digits number the sections that show, so 1 is always the first
	// sidebar row even after some are hidden.
	visible := a.visibleSections()
	for i, key := range sectionKeys {
		if i >= len(visible) {
			break
		}
		tab := visible[i]
		a.onShortcut(c, 0, key, shortcut(fmt.Sprintf("workspace/open-section-%d", i+1), fmt.Sprintf("%d", (i+1)%10), tr("Switch to a workspace section")), func() { a.setWorkspaceTab(tab) })
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
	case sectionCompare:
		count = len(a.compare.files)
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
