package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type keyboardPane struct {
	id     string
	handle *ui.Handle
}

func (a *app) keyboardPane(c *ui.Context, id string, e ui.Element) ui.Element {
	if a.keyboardHandles == nil {
		a.keyboardHandles = map[string]*ui.Handle{}
	}
	handle := a.keyboardHandles[id]
	if handle == nil {
		handle = &ui.Handle{}
		a.keyboardHandles[id] = handle
	}
	a.keyboardPanes = append(a.keyboardPanes, keyboardPane{id, handle})
	// FocusWithin also reports pointer focus. Track the modality separately:
	// Handle.Focus does not set MyGo's focus-visible state for F6-style moves.
	focused := handle.FocusWithin(c)
	accent := c.Theme().Accent
	return e.Bind(handle).FocusRing(true).ObserveInput(func(ev ui.InputEvent) {
		switch ev.Kind {
		case ui.InputKeyDown:
			a.keyboardFocusVisible = true
		case ui.InputPointerDown:
			a.keyboardFocusVisible = false
		}
	}).DrawOver(func(p *ui.Painter, r ui.Rect) {
		if focused && a.keyboardFocusVisible {
			p.Stroke(r, accent.Alpha(0.8), 4, 1)
		}
	})
}

func (a *app) keyboardListID(s *ui.ListState) string {
	switch s {
	case &a.navList:
		return "sidebar"
	case &a.fileList:
		return "changes"
	case &a.stagedList:
		return "staged"
	case &a.mergeList:
		return "conflicts"
	case &a.commitList:
		return "commits"
	case &a.commitFileList:
		return "commit-files"
	case &a.branchList:
		return "branches"
	case &a.branchCommitList:
		return "branch-commits"
	case &a.branchCommitFileList:
		return "branch-files"
	case &a.remoteList:
		return "remotes"
	case &a.remoteBranches:
		return "remote-branches"
	case &a.stashList:
		return "stashes"
	case &a.stashFileList:
		return "stash-files"
	case &a.tagList:
		return "tags"
	case &a.worktreeList:
		return "worktrees"
	case &a.submoduleList:
		return "submodules"
	case &a.reflogList:
		return "reflog"
	case &a.fileHistoryList:
		return "file-history"
	case &a.fileHistoryFileList:
		return "file-history-files"
	case &a.compare.list:
		return "comparison-files"
	case &a.diffFullscreenFileList:
		return "fullscreen-files"
	}
	return fmt.Sprintf("list:%p", s)
}

// Register before Rows: a list remains the same widget, with the toolkit's
// arrow-key selection, accessibility, scrolling, Tab and context-menu support.
func (a *app) keyboardList(c *ui.Context, s *ui.ListState, n int) ui.Element {
	id := a.keyboardListID(s)
	c.Key("keyboard-list:" + id)
	e := a.keyboardPanelTabs(id, a.keyboardPane(c, id, ui.List(c, s, n)).Label(tr(keyboardPaneLabel(id))))
	if (s == &a.fileList || s == &a.stagedList || s == &a.mergeList) && e.FocusWithin() && a.keyboardLastList != s {
		a.keyboardLastList = s
		if a.keyboardMoved == nil {
			a.keyboardMoved = map[*ui.ListState]bool{}
		}
		a.keyboardMoved[s] = true
	}
	if id != "sidebar" && id != "fullscreen-files" && id != "comparison-files" {
		e.PopoverMenu(func(m *ui.Menu) { a.keyboardListMenu(c, s, m) })
	}
	if !a.settings.DisableShortcuts {
		e.OnShortcut(0, ui.KeyJ, func() { a.moveKeyboardList(s, n, 1) })
		e.OnShortcut(0, ui.KeyK, func() { a.moveKeyboardList(s, n, -1) })
		e.OnSubmit(func() {
			if s.Selected != nil && *s.Selected < 0 && n > 0 {
				*s.Selected = 0
				if a.keyboardMoved == nil {
					a.keyboardMoved = map[*ui.ListState]bool{}
				}
				a.keyboardMoved[s] = true
			}
			a.keyboardEnter(id)
		})
	}
	return e
}

func (a *app) moveKeyboardList(s *ui.ListState, n, delta int) {
	if s.Selected == nil || n == 0 {
		return
	}
	selected := max(0, min(n-1, *s.Selected+delta))
	if selected == *s.Selected {
		return
	}
	*s.Selected = selected
	// Several views derive their row index from a source selection before they
	// build the list. Update that source now, so the next build cannot undo j/k.
	switch s {
	case &a.fileList, &a.stagedList, &a.mergeList:
		sections := splitChangeRows(a.changeRows)
		base := sections.changesBase
		if s == &a.stagedList {
			base = sections.stagedBase
		}
		if s == &a.mergeList {
			base = sections.mergeBase
		}
		if base >= 0 {
			a.selectedFile = base + selected
		}
	case &a.navList:
		if section, ok := a.navRow(selected); ok {
			a.setWorkspaceTab(section)
		}
	case &a.branchList:
		a.selectBranchRow(selected)
	case &a.remoteList:
		a.selectRemoteRow(selected)
	case &a.remoteBranches:
		a.selectRemoteBranchRow(selected)
	case &a.stashList:
		a.selectStashRow(selected)
	case &a.stashFileList:
		a.selectStashFileRow(selected)
	case &a.tagList:
		a.selectTagRow(selected)
	case &a.worktreeList:
		a.selectWorktreeRow(selected)
	case &a.submoduleList:
		a.selectSubmoduleRow(selected)
	case &a.reflogList:
		a.selectReflogRow(selected)
	case &a.diffFullscreenFileList:
		if selected < len(a.diffFullscreenRows) {
			a.diffFullscreenRows[selected].choose()
		}
	case &a.commitFileList:
		a.selectCommitFileRow(selected)
	case &a.branchCommitFileList:
		a.selectBranchCommitFileRow(selected)
	}
	s.ScrollIntoView(selected)
	if a.keyboardMoved == nil {
		a.keyboardMoved = map[*ui.ListState]bool{}
	}
	a.keyboardMoved[s] = true
}

func (a *app) keyboardListChanged(s *ui.ListState, e ui.Element) bool {
	moved := a.keyboardMoved[s]
	delete(a.keyboardMoved, s)
	return e.Changed() || moved
}

func (a *app) visibleKeyboardPanes() []keyboardPane {
	panes := a.keyboardPanes
	if len(panes) == 0 {
		panes = a.keyboardPrevious
	}
	if !a.diffFullscreen && !a.compare.open && !a.branchFullscreen {
		return panes
	}
	visible := make([]keyboardPane, 0, len(panes))
	for _, pane := range panes {
		shown := false
		switch {
		case a.diffFullscreen:
			shown = strings.HasPrefix(pane.id, "fullscreen-")
		case a.compare.open:
			shown = strings.HasPrefix(pane.id, "comparison-")
		case a.branchFullscreen:
			shown = strings.HasPrefix(pane.id, "branch-")
		}
		if shown {
			visible = append(visible, pane)
		}
	}
	return visible
}

func (a *app) focusedKeyboardPane(c *ui.Context) string {
	panes := a.visibleKeyboardPanes()
	for _, p := range panes {
		if p.handle.FocusWithin(c) {
			return p.id
		}
	}
	return ""
}

func (a *app) cycleKeyboardPane(c *ui.Context, delta int) {
	a.cycleKeyboardPaneFrom(a.focusedKeyboardPane(c), delta)
}

func (a *app) cycleKeyboardPaneFrom(current string, delta int) {
	panes := a.visibleKeyboardPanes()
	if len(panes) == 0 {
		return
	}
	index := slices.IndexFunc(panes, func(p keyboardPane) bool { return p.id == current })
	if index < 0 {
		index = 0
		if delta > 0 {
			index = -1
		}
	}
	next := (index + delta + len(panes)) % len(panes)
	a.keyboardFocusVisible = true
	a.keyboardFocus = panes[next].id
}

func (a *app) primaryKeyboardPane() string {
	switch a.workspaceTab {
	case sectionChanges:
		if len(a.mergeRows) > 0 && a.mergeExpanded {
			return "conflicts"
		}
		if len(a.stagedRows) > 0 && a.stagedExpanded {
			return "staged"
		}
		return "changes"
	case sectionHistory, sectionGraph:
		return "commits"
	case sectionBranches:
		return "branches"
	case sectionRemotes:
		return "remotes"
	case sectionStash:
		return "stashes"
	case sectionTags:
		return "tags"
	case sectionWorktrees:
		return "worktrees"
	case sectionSubmodules:
		return "submodules"
	case sectionReflog:
		return "reflog"
	}
	return "sidebar"
}

func (a *app) finishKeyboardFrame(c *ui.Context) {
	paneModal := a.diffFullscreen || a.compare.open || a.branchFullscreen
	if a.modalOpen() {
		if !a.keyboardModalWasOpen {
			a.keyboardModalReturn = a.keyboardLastPane
		}
		a.keyboardModalWasOpen = true
		if !paneModal {
			return
		}
	}
	if a.keyboardModalWasOpen && !a.modalOpen() {
		a.keyboardModalWasOpen = false
		if a.focusedKeyboardPane(c) == "" {
			a.keyboardFocus = a.keyboardModalReturn
		}
	}
	a.keyboardCurrentGroup = a.keyboardActionGroup(c)
	current := a.focusedKeyboardPane(c)
	if current != "" {
		a.keyboardLastPane = current
	}
	if !paneModal && (!a.keyboardSectionKnown || a.keyboardSection != a.workspaceTab) {
		a.keyboardSectionKnown = true
		a.keyboardSection = a.workspaceTab
		if current != "sidebar" {
			a.keyboardFocus = a.primaryKeyboardPane()
		}
	}
	if a.keyboardFocus != "" {
		target := a.keyboardFocus
		a.keyboardFocus = ""
		for _, p := range a.visibleKeyboardPanes() {
			if p.id == target {
				p.handle.Focus(c)
				return
			}
		}
		if a.commitFilesLoading || a.branchCommitsLoading || a.branchCommitFilesLoading || a.stashFilesLoading || a.fileHistoryLoading || a.commitDiffLoading || a.branchCommitDiffLoading || a.stashDiffLoading || a.diffLoading {
			a.keyboardFocus = target
		} else if !paneModal {
			primary := a.primaryKeyboardPane()
			for _, p := range a.visibleKeyboardPanes() {
				if p.id == primary {
					p.handle.Focus(c)
					return
				}
			}
		}
	}
}

func (a *app) keyboardEnter(id string) {
	a.keyboardFocusVisible = true
	if a.keyboardToggleDirectory(id) {
		return
	}
	switch id {
	case "sidebar":
		if section, ok := a.navRow(a.navSelected); ok {
			a.setWorkspaceTab(section)
			a.keyboardFocus = a.primaryKeyboardPane()
		}
	case "changes", "staged", "conflicts":
		a.keyboardFocus = "diff"
	case "commit-files":
		a.keyboardFocus = "history-diff"
	case "branch-files":
		a.keyboardFocus = "branch-diff"
	case "stash-files":
		a.keyboardFocus = "stash-diff"
	case "comparison-files":
		a.keyboardFocus = "comparison-diff"
	case "file-history-files":
		a.keyboardFocus = "file-history-diff"
	case "fullscreen-files":
		a.keyboardFocus = "fullscreen-diff"
	case "commits":
		a.keyboardFocus = "commit-files"
	case "branches":
		a.keyboardFocus = "branch-commits"
	case "branch-commits":
		a.keyboardFocus = "branch-files"
	case "remotes":
		a.keyboardFocus = "remote-branches"
	case "stashes":
		a.keyboardFocus = "stash-files"
	case "file-history":
		a.keyboardFocus = "file-history-files"
	}
}

func (a *app) keyboardDiffViewer(c *ui.Context, state *editor.DiffState) ui.Element {
	return a.keyboardDiffViewerWithID(c, state, "")
}

func (a *app) keyboardDiffViewerWithID(c *ui.Context, state *editor.DiffState, override string) ui.Element {
	id := "diff"
	switch state {
	case a.commitDiffState:
		id = "history-diff"
	case a.branchCommitDiffState:
		id = "branch-diff"
	case a.stashDiffState:
		id = "stash-diff"
	case a.fileHistoryDiffState:
		id = "file-history-diff"
	case a.compare.diffState:
		id = "comparison-diff"
	}
	if override != "" {
		id = override
	}
	e := a.keyboardPanelTabs(id, a.keyboardPane(c, id, editor.DiffView(c, state).Grow(1)).Label(tr("Diff")))

	selectable := state == a.diffState && state.LineSelection
	if e.FocusWithin() {
		a.keyboardDiffCanSelect = selectable
	}
	if !a.settings.DisableShortcuts {
		for _, command := range diffKeyboardCommands() {
			if command.selection && !selectable {
				continue
			}
			command := command
			e.OnShortcut(command.mods, command.key, func() { command.run(a, state) })
		}
	}

	return e
}

func keyboardPaneLabel(id string) string {
	switch id {
	case "sidebar":
		return "Workspace"
	case "changes":
		return "Changes"
	case "staged":
		return "Staged Changes"
	case "conflicts":
		return "Merge Changes"
	case "fullscreen-files":
		return "Changed files"
	case "commits", "branch-commits", "file-history":
		return "Commits"
	case "commit-files", "branch-files", "stash-files", "comparison-files", "file-history-files":
		return "Commit files"
	case "branches":
		return "Branches"
	case "remotes":
		return "Remotes"
	case "remote-branches":
		return "Remote branches"
	case "stashes":
		return "Stash"
	case "tags":
		return "Tags"
	case "worktrees":
		return "Worktrees"
	case "submodules":
		return "Submodules"
	case "reflog":
		return "Reflog"
	}
	return "Workspace"
}

// The list itself owns this menu as well as each row, so Shift+F10 works
// without tabbing into a row's child buttons or needing a mouse first.
func (a *app) keyboardListMenu(c *ui.Context, s *ui.ListState, m *ui.Menu) {
	if s.Selected == nil || *s.Selected < 0 {
		return
	}
	index := *s.Selected
	row := ui.ListRow{Context: c, Index: index}
	services := c.Services()
	switch s {
	case &a.commitList:
		if commit, ok := a.selectedVisibleCommit(); ok {
			a.commitRowMenu(m, commit, services)
		}
	case &a.fileList, &a.stagedList, &a.mergeList:
		sections := splitChangeRows(a.changeRows)
		base := sections.changesBase
		if s == &a.stagedList {
			base = sections.stagedBase
		}
		if s == &a.mergeList {
			base = sections.mergeBase
		}
		if base >= 0 {
			a.fileRowMenu(m, base+index, services)
		}
	case &a.branchList:
		a.branchRowMenu(m, row, services)
	case &a.remoteList:
		a.remoteRowMenu(m, row, services)
	case &a.remoteBranches:
		if remote, branch, ok := a.currentRemoteBranchSelection(); ok {
			a.remoteBranchRowMenu(m, row, services, remote, branch)
		}
	case &a.stashList:
		a.stashRowMenu(m, row)
	case &a.tagList:
		a.tagRowMenu(m, row, services)
	case &a.worktreeList:
		a.worktreeRowMenu(m, row, services)
	case &a.submoduleList:
		a.submoduleRowMenu(m, row)
	case &a.reflogList:
		if entry, ok := sourceRow(a.visibleReflog(), a.snapshot.Reflog, index); ok {
			a.reflogRowMenu(m, entry, services)
		}
	case &a.commitFileList, &a.branchCommitFileList, &a.fileHistoryFileList:
		if commit, file, ok := a.selectedHistoricalFile(); ok {
			a.commitFileMenu(m, &commit, file, services)
		}
	case &a.branchCommitList:
		if index < len(a.branchCommits) {
			a.commitRowMenuWithHistory(m, a.branchCommits[index], services, a.currentBranchName() == a.branchCommitsBranch)
		}
	case &a.fileHistoryList:
		if index < len(a.fileHistoryCommits) {
			a.commitRowMenu(m, a.fileHistoryCommits[index], services)
		}
	case &a.stashFileList:
		if a.selectedStash >= 0 && a.selectedStash < len(a.snapshot.Stashes) && a.selectedStashFile >= 0 && a.selectedStashFile < len(a.stashFiles) {
			a.stashFileMenu(m, a.snapshot.Stashes[a.selectedStash], a.stashFiles[a.selectedStashFile], services)
		}
	}
}

func (a *app) keyboardBack(c *ui.Context) {
	a.keyboardFocusVisible = true
	switch a.focusedKeyboardPane(c) {
	case "diff":
		a.keyboardFocus = "changes"
	case "history-diff":
		a.keyboardFocus = "commit-files"
	case "branch-diff":
		a.keyboardFocus = "branch-files"
	case "stash-diff":
		a.keyboardFocus = "stash-files"
	case "file-history-diff":
		a.keyboardFocus = "file-history-files"
	case "comparison-diff":
		a.keyboardFocus = "comparison-files"
	case "commit-files":
		a.keyboardFocus = "commits"
	case "branch-files":
		a.keyboardFocus = "branch-commits"
	case "branch-commits":
		a.keyboardFocus = "branches"
	case "remote-branches":
		a.keyboardFocus = "remotes"
	case "stash-files":
		a.keyboardFocus = "stashes"
	default:
		a.keyboardFocus = a.primaryKeyboardPane()
	}
}

func (a *app) selectKeyboardDiffLines(state *editor.DiffState, delta int, extend bool) {
	if !state.LineSelection {
		return
	}
	if state != a.diffState {
		return
	}
	if !a.lineSelectMode {
		a.startLineSelection()
	}
	selection, ok := state.SelectedLines()
	if delta > 0 {
		state.Run(editor.MoveDown)
	} else if delta < 0 {
		state.Run(editor.MoveUp)
	}
	line, side := state.CursorPos().Line, state.ActiveSide()
	if !extend || !ok || selection.Side != side {
		a.keyboardLineAnchor = line
	}
	selected := editor.DiffLineRange{Side: side, Start: min(a.keyboardLineAnchor, line), End: max(a.keyboardLineAnchor, line)}
	state.SetSelectedLines(selected, true)
	a.lineSelectRanges = []editor.DiffLineRange{selected}
	a.lineSelectRange, a.lineSelectHas = selected, true
}

func (a *app) keyboardToggleDirectory(id string) bool {
	switch id {
	case "changes", "staged", "conflicts":
		if a.selectedFile >= 0 && a.selectedFile < len(a.changeRows) {
			row := a.changeRows[a.selectedFile]
			if row.isDirectory() {
				a.toggleFileTreeDirectory(fileTreeChangesScope, row.directory)
				return true
			}
		}
	case "commit-files":
		if dir := selectedFileTreeDirectory(a.commitFileRows, a.selectedCommitFileRow); dir != "" {
			a.toggleFileTreeDirectory(fileTreeCommitScope, dir)
			return true
		}
	case "branch-files":
		if dir := selectedFileTreeDirectory(a.branchCommitFileRows, a.selectedBranchCommitFileRow); dir != "" {
			a.toggleFileTreeDirectory(fileTreeBranchScope, dir)
			return true
		}
	case "stash-files":
		if dir := selectedFileTreeDirectory(a.stashFileRows, a.selectedStashFileRow); dir != "" {
			a.toggleFileTreeDirectory(fileTreeStashScope, dir)
			return true
		}
	}
	return false
}

// Tab leaves data panels as one unit instead of walking every virtual row's
// checkbox and action button. This is deliberately scoped to lists and
// read-only diffs: toolbars, forms, and editable text retain native Tab behavior.
func (a *app) keyboardPanelTabs(id string, e ui.Element) ui.Element {
	if a.settings.DisableShortcuts {
		return e
	}
	// Never let a background list claim a dialog's native Tab key.
	if a.modalOpen() {
		if a.confirm != nil || a.settingsOpen || a.keybindingsOpen || a.commandLogOpen || a.anyFormOpen() && !a.compare.open {
			return e
		}
		if a.diffFullscreen && !strings.HasPrefix(id, "fullscreen-") {
			return e
		}
		if a.compare.open && !strings.HasPrefix(id, "comparison-") {
			return e
		}
		if a.branchFullscreen && !strings.HasPrefix(id, "branch-") {
			return e
		}
	}
	e.OnShortcut(0, ui.KeyTab, func() { a.cycleKeyboardPaneFrom(id, 1) })
	e.OnShortcut(ui.Shift, ui.KeyTab, func() { a.cycleKeyboardPaneFrom(id, -1) })
	return e
}
