package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// The workspace sections, in tab order; the indices match a.workspaceTab and
// the digit shortcuts.
const (
	sectionCode = iota
	sectionChanges
	sectionHistory
	sectionGraph
	sectionBranches
	sectionRemotes
	sectionStash
	sectionTags
	sectionWorktrees
	sectionAgents
	sectionSubmodules
	sectionReflog
	sectionCompare
	sectionOverview
	sectionPullRequests
	workspaceSectionCount
)

var workspaceSections = []string{
	"Editor", "Changes", "History", "Graph", "Branches", "Remotes", "Stash", "Tags",
	"Worktrees", "Agent tasks", "Submodules", "Reflog", "Compare refs", "Overview",
	"PR/MR",
}

// workspaceSectionIDs are the stable names of the sections the settings
// store, in the order of workspaceSections. A stored id keeps working if the
// sections are ever reordered.
var workspaceSectionIDs = []string{
	"code", "changes", "history", "graph", "branches", "remotes", "stash", "tags",
	"worktrees", "agents", "submodules", "reflog", "compare", "overview",
	"pull-requests",
}

// defaultVisibleSections shows every workspace section on a fresh install
// except Agents, a specialist task workbench that Settings turns on. Users can
// still hide other specialist sections in Settings.
var defaultVisibleSections = slices.DeleteFunc(slices.Clone(workspaceSectionIDs),
	func(id string) bool { return id == "agents" })

// defaultSectionOrder places the new Overview first without renumbering or
// rewriting any pre-existing section enum. Non-empty legacy orders append it
// during migration instead of changing the user's established relative order.
func defaultSectionOrder() []string {
	return append([]string{"overview"}, slices.DeleteFunc(slices.Clone(workspaceSectionIDs),
		func(id string) bool { return id == "overview" })...)
}

// navPaneDefault is the section sidebar's width, until its divider moves it.
const navPaneDefault = 172

// uiFaultHook lets a test inject a fault while a frame builds, to prove the
// boundary below contains it. It is nil in the running app.
var uiFaultHook func()

func (a *app) view(c *ui.Context) {
	a.frame(c, c.Theme().Dark, 0)
}

// frame builds one page under a theme whose light or dark base the window
// settled: a tab draws inside the window's chrome, which does the same, so
// the System theme resolves against the desktop rather than against a palette
// the chrome already applied.
func (a *app) frame(c *ui.Context, systemDark bool, topInset float32) {
	// A fault while building a frame must not close the window: it is caught,
	// remembered, and the next pass draws the safe screen. The handler returns
	// without building another element, because MyGo restores the frame's
	// parent only on a normal return, and the next pass starts from the root.
	defer a.recoverView(c)
	if a.crash != "" {
		a.crashView(c)
		return
	}
	if uiFaultHook != nil {
		uiFaultHook()
	}
	// The theme is settled before anything builds, so every pane, menu and
	// tooltip of the frame takes its colors and metrics.
	a.applySettingsFor(c, systemDark)
	a.bodyView(c, topInset)
}

// bodyView builds the page after the theme and the language are settled.
// topInset is the room the window's own chrome takes above the page, so the
// notifications and macOS's native window controls stay clear of overlays.
func (a *app) bodyView(c *ui.Context, topInset float32) {
	a.keyboardPrevious = append(a.keyboardPrevious[:0], a.keyboardPanes...)
	a.keyboardPanes = a.keyboardPanes[:0]
	a.applyPendingRecent()
	a.services = c.Services()
	a.installShortcutObserver(c)
	a.registerShortcuts(c)
	overlayTopInset := float32(0)
	if systemWindowControls {
		// AppKit keeps its traffic lights above window-relative overlays.
		// Leave them the title bar and its divider; other platforms draw
		// their controls in the overlay-free chrome and keep the old layout.
		overlayTopInset = topInset
	}
	if a.state == stateReady {
		a.flushEdits()
		a.appView(c, overlayTopInset)
	} else if a.overview != nil {
		a.overviewView(c)
	} else {
		a.welcomeView(c, topInset)
	}
	a.branchFullscreenView(c, overlayTopInset)
	a.diffFullscreenView(c, overlayTopInset)
	a.requestDiffFullscreen(c, overlayTopInset)
	a.commitActionModals(c)
	a.rebasePlanModal(c)
	a.branchFormModal(c)
	a.stackSyncModal(c)
	a.repositoryPerformanceModal(c)
	a.moveCommitsModal(c)
	a.recoveryModal(c)
	a.stashFormModal(c)
	a.remoteFormModal(c)
	a.remoteWebURLFormModal(c)
	a.commitFormModal(c)
	a.tagFormModal(c)
	a.worktreeFormModal(c)
	a.agentTaskFormModal(c)
	a.conflictAssistantModal(c)
	a.worktreeMoveFormModal(c)
	a.worktreeLockFormModal(c)
	a.worktreeRepairFormModal(c)
	a.worktreePruneFormModal(c)
	a.submoduleFormModal(c)
	a.homeWorktreesModal(c)
	a.overviewPickerModal(c)
	a.overviewSetupModal(c, a.pickerEdit)
	a.recentRenameModal(c)
	a.codeFileFormModal(c)
	a.codeWorkspaceFolderModal(c)
	a.codeLanguageDialogView(c)
	a.terminalRenameModal(c)
	a.cloneModal(c)
	a.bisectFormModal(c)
	a.exportPatchFormModal(c)
	a.pullRequestFormModal(c)
	a.pullRequestChecksModal(c)
	a.sshWorkspaceModal(c)
	a.hostingAccessModal(c)
	a.changeRequestFilterModal(c)
	a.changeRequestDisplayOptions(c)
	a.patchPreviewModal(c)
	a.customPatchModal(c)
	a.customPatchHistoryModal(c)
	a.changeCourierModal(c)
	a.commandLogModal(c)
	a.commandPromptModal(c)
	a.continueReviewModal(c)
	a.settingsModal(c)
	a.themeEditorModal(c)
	a.keybindingsModal(c)
	a.commitFilterModal(c)
	a.historyChangesModal(c)
	a.confirmModal(c)
	a.sshHostKeyModal(c)
	a.keyboardTextFocus = c.FocusTakesText()
	a.finishKeyboardFrame(c)
	// Notifications are queued by the actions this pass ran, so they are
	// added after everything else and drawn over it.
	a.flushToasts(c)
	a.toastViewport(c, topInset)
}

// recoverView contains a panic raised while the frame built. It records what
// happened, tells the command log, and asks for another frame, on which the
// safe screen draws. If the safe screen itself fails, the last good frame is
// left alone rather than scheduling a loop.
func (a *app) recoverView(c *ui.Context) {
	r := recover()
	if r == nil || a.crash != "" {
		return
	}
	a.crash = describeFault(r)
	if a.commandLog != nil {
		a.commandLog.Note("Unexpected UI failure: " + a.crash)
	}
	if c != nil {
		c.Invalidate()
	}
}

// describeFault turns a recovered value into a line worth showing.
func describeFault(r any) string {
	if err, ok := r.(error); ok {
		return safeError(err)
	}
	return fmt.Sprintf("%v", r)
}

// crashView is the safe screen a build fault falls back to. It keeps the
// window usable: the repository can be put aside and another opened, or the
// frame tried again once the cause has passed.
func (a *app) crashView(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).Fill().Center().Gap(14).Padding(28).Children(func() {
		ui.Box(c).Size(52, 52).Radius(14).Center().
			Background(theme.Danger.Alpha(0.14)).Children(func() {
			ui.Icon(c, conflictIcon).FontSize(26).TextColor(theme.Danger)
		})
		ui.Text(c, tr("This window hit an unexpected problem")).FontSize(21).Bold().
			TextAlign(ui.Center)
		ui.Text(c, tr("The fault was contained, and the repository was not changed by it. "+
			"Go back to the picker, or try drawing the window again.")).
			TextColor(theme.TextMuted).TextAlign(ui.Center).MaxWidth(560)
		ui.Text(c, a.crash).TextColor(theme.Danger).TextAlign(ui.Center).
			MaxWidth(560).Selectable()
		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c, tr("Back to repositories")).Clicked() {
				a.crash = ""
				a.closeRepository()
			}
			if ui.Button(c, tr("Try again")).Clicked() {
				a.crash = ""
			}
		})
	})
}

// applySettingsFor settles the language and the theme from the stored
// preferences. The System theme asks the window for the desktop's appearance
// rather than the palette this frame already applied, so the window chrome and
// its pages agree on it.
func (a *app) applySettingsFor(c *ui.Context, systemDark bool) {
	currentLanguage = normalizeLanguage(a.settings.Language)
	quitOnClose = a.settings.QuitOnClose
	c.SetTheme(themeForSettingsCached(a.settings, systemDark))
	a.applyBufferFonts()
	a.syncHomeBackground()
}

// pickerErrorView shows a failed open beside the path form while the recent
// list remains available.
func (a *app) pickerErrorView(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).MaxWidth(720).FillWidth().AlignSelf(ui.Center).Gap(6).
		Padding(16, 18).Background(theme.Danger.Alpha(0.1)).
		Border(1, theme.Danger.Alpha(0.45)).Radius(theme.Radius + 4).
		Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Icon(c, abortIcon).FontSize(16).TextColor(theme.Danger)
				ui.Text(c, tr("Could not open repository")).FontSize(16).
					Bold().TextColor(theme.Danger)
			})
			ui.Text(c, a.errMessage).TextColor(theme.TextMuted)
		})
}

// loadingView is the whole picker while a repository opens: the field, the
// history and the shortcuts step aside for one quiet page with the way out.
func (a *app) loadingView(c *ui.Context) {
	theme := c.Theme()
	ui.Box(c).Fill().Background(theme.Background).Children(func() {
		a.homeBackgroundView(c)
		ui.Column(c).Fill().Center().Gap(16).Padding(28).Children(func() {
			ui.Box(c).Size(80, 80).Center().Children(func() {
				appLogo(c, 80)
			})
			ui.Text(c, tr("Opening repository")).FontSize(22).Bold()
			if a.path != "" {
				ui.Text(c, a.path).TextColor(theme.TextMuted).
					MaxWidth(660).TextAlign(ui.Center).SingleLine().Tooltip(a.path)
			}
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Spinner(c).Label(tr("Opening repository"))
				ui.Text(c, tr("Reading branches, changes and history…")).TextColor(theme.TextMuted)
			})
			if ui.Button(c, tr("Cancel")).Tooltip(tr("Cancel (Escape) — stop opening and go back to the picker")).Clicked() {
				a.cancelLoading()
			}
		})
	})
}

// initPromptView offers to adopt a folder that holds no Git repository yet:
// initialize it here, choose another folder, or leave it as it is.
func (a *app) initPromptView(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).MaxWidth(720).FillWidth().AlignSelf(ui.Center).Gap(10).
		Padding(18).Background(theme.Warning.Alpha(0.1)).
		Border(1, theme.Warning.Alpha(0.5)).Radius(theme.Radius + 4).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Icon(c, conflictIcon).FontSize(17).TextColor(theme.Warning)
			ui.Text(c, tr("This folder is not a Git repository")).FontSize(16).Bold()
		})
		ui.Text(c, trf("%s has no Git repository yet. Initialize one here to manage its changes, or pick another folder.",
			a.offerInitPath)).TextColor(theme.TextMuted)
		ui.Row(c).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
			iconTextButton(c, "Initialize repository", "Create a Git repository in this folder",
				initRepoIcon, true, a.initializeRepository)
			iconTextButton(c, "Choose another folder", "Pick a different folder",
				folderOpenIcon, false, a.chooseAnotherFolder)
			iconTextButton(c, "Not now", "Leave the folder alone and stay on the picker",
				closeIcon, false, a.dismissInitOffer)
		})
	})
}

// appView is the workspace chrome once a repository is open: a compact
// header, the section sidebar beside the active section, and a status bar,
// each an island floating on the window's backdrop with the rest of the panes.
func (a *app) appView(c *ui.Context, overlayTopInset float32) {
	ui.Box(c).Fill().Background(islandBackdrop(c.Theme())).Children(func() {
		ui.Column(c).Fill().Padding(islandGap / 2).Children(func() {
			if !a.diffFullscreen {
				a.headerView(c)
			}
			a.mainArea(c)
			a.statusBar(c)
		})
	})
	a.fileHistoryDrawer(c, overlayTopInset)
}

// mainArea docks the terminal on any workspace edge. Maximizing temporarily
// gives it the whole content area without changing the size restored later.
func (a *app) mainArea(c *ui.Context) {
	if !a.term.panelOpen {
		a.workspaceView(c)
		return
	}
	if a.term.panelMaximized {
		a.terminalPaneView(c)
		return
	}

	// Mount the real split at its final size, including while a shell starts,
	// so the workspace doesn't reflow through an empty intermediate dock.
	dock := a.terminalDock()
	terminalFirst := dock == terminalDockTop || dock == terminalDockLeft
	workspace := func() { a.workspaceView(c) }
	terminal := func() { a.terminalPaneView(c) }
	first, second := workspace, terminal
	a.term.shown = 100 - a.term.percent
	if terminalFirst {
		first, second = terminal, workspace
		a.term.shown = a.term.percent
	}
	if dock == terminalDockLeft || dock == terminalDockRight {
		paneSplitPercent(c, &a.term.shown, first, second).Grow(1)
	} else {
		paneSplitVerticalPercent(c, &a.term.shown, first, second).Grow(1)
	}
	if terminalFirst {
		a.term.percent = a.term.shown
	} else {
		a.term.percent = 100 - a.term.shown
	}
}

// workspaceView is the section sidebar beside the active section, its width
// set by the divider between them.
func (a *app) workspaceView(c *ui.Context) {
	collapsiblePaneSplitLane(c, &a.navPaneWidth, &a.navPaneCollapsed,
		"Expand workspace sidebar", func() {
			a.navView(c)
		}, func() {
			ui.Column(c).Grow(1).FillHeight().Children(func() {
				a.contentView(c)
			})
		})
}

func (a *app) headerView(c *ui.Context) {
	a.headerViewWithID(c, "toolbar")
}

// headerViewWithID gives the full-window diff its own repository toolbar.
// Its distinct keyboard-pane id keeps focus inside the fullscreen view while
// leaving Fetch, Pull, Push and the other repository actions available.
func (a *app) headerViewWithID(c *ui.Context, keyboardID string) {
	theme := c.Theme()
	// A service callback is safe to keep for this window's lifetime. This one
	// deliberately targets the user's desktop, never a remote SSH browser.
	if a.openExternalURL == nil {
		a.openExternalURL = c.Services().OpenURL
	}
	header := a.keyboardPane(c, keyboardID, islandCard(c, ui.Row(c).AlignItems(ui.Center).Gap(10).Padding(6, 14)).Focusable()).Label(tr("Repository header"))
	header.Children(func() {
		if keyboardID == "fullscreen-toolbar" {
			// The full-window diff offers the way back where the eye starts.
			iconButton(c, "Restore diff view", "Restore diff view", chevronLeftIcon, false, a.closeDiffFullscreen)
		}
		base := filepath.Base(a.snapshot.Root)
		if base == "." || base == string(filepath.Separator) || base == "" {
			base = a.snapshot.Root
		}
		ui.Text(c, base).FontSize(16).Bold().Tooltip(a.snapshot.Root)
		if !a.editorOnlyWorkspace() {
			if a.openedLinkedWorktree() {
				ui.Badge(c, tr("Linked worktree")).Tooltip(a.snapshot.Root)
				if ownerPath := a.openedMainWorktreePath(); ownerPath != "" {
					owner := tr("Main worktree") + " · " + repoName(ownerPath)
					ownerBadge := ui.Badge(c, owner).SingleLine().MaxWidth(240).MinWidth(0).
						Label(tr("Main worktree") + ": " + ownerPath)
					richTooltip(c, ownerBadge, richTip{Title: owner, Mono: []string{ownerPath}, Font: a.settings.BufferFont})
				}
			}

			branch := a.snapshot.Branch
			branchTooltip := ""
			// Git calls a detached HEAD "(no branch)". Name the checkout's actual
			// kind instead, and keep its commit visible rather than losing detachment.
			if a.snapshot.Head != "" && (branch == "" || branch == "(no branch)") {
				shortHead := a.snapshot.Head
				if len(shortHead) > 7 {
					shortHead = shortHead[:7]
				}
				branch = trf("detached %s", shortHead)
				branchTooltip = trf("detached %s", a.snapshot.Head)
				if a.openedLinkedWorktree() {
					branch = tr("Worktree") + " · " + branch
				}
			} else if branch == "" && a.openedLinkedWorktree() {
				branch = tr("Worktree")
			}
			ui.Text(c, tr("●")).TextColor(theme.Success)
			ui.Text(c, branch).Bold().Tooltip(branchTooltip)
			if a.snapshot.ChangeCount() == 0 {
				ui.Badge(c, tr("Clean"))
			} else {
				ui.Badge(c, trf("%d changed", a.snapshot.ChangeCount()))
			}
			if a.snapshot.Operation != "" {
				ui.Badge(c, strings.ToUpper(a.snapshot.Operation))
			}
			for _, b := range a.snapshot.Branches {
				if !b.Head {
					continue
				}
				ahead, behind, known := aheadBehind(b)
				if !known {
					continue
				}
				if n, show := count(ahead); show {
					aheadCountView(c, n, b, theme.Success, theme.FontSize-1)
				}
				if n, show := count(behind); show {
					behindCountView(c, n, b, theme.Warning, theme.FontSize-1)
				}
			}
		}

		ui.Row(c).Grow(1).Justify(ui.End).Gap(6).Children(func() {
			// The command palette leads the toolbar, then repository, workspace and
			// application actions.
			editorOnly := a.editorOnlyWorkspace()
			repositoryActions := !editorOnly && a.anyHeaderActionVisible("fetch", "pull", "push")
			editorVisible := a.headerActionVisible("editor") && a.snapshot.Root != "" &&
				(!a.location.IsRemote() || effectiveEditorCommand(a.settings.EditorCommand) == builtInEditorCommand)
			refreshVisible := !editorOnly && a.headerActionVisible("refresh")
			workspaceActions := refreshVisible || editorVisible
			paletteVisible := a.showCommandPalette != nil && a.headerActionVisible("palette")
			themeSettings := a.anyHeaderActionVisible("theme", "settings")
			// The palette leads the toolbar; each non-empty group after it is
			// divided from the previous one.
			groupsDrawn := 0
			startGroup := func(visible bool) {
				if !visible {
					return
				}
				if groupsDrawn > 0 {
					toolbarSeparator(c)
				}
				groupsDrawn++
			}
			startGroup(paletteVisible)
			if paletteVisible {
				iconButton(c, "Command Palette", "Search commands ("+cmdShortcutChord()+"+Shift+P)", searchIcon, false, a.showCommandPalette)
			}
			startGroup(repositoryActions)
			if !editorOnly && a.headerActionVisible("fetch") {
				iconButton(c, "Fetch", "Fetch from the remotes", fetchIcon,
					a.actionBusy || len(a.snapshot.Remotes) == 0, a.beginFetch)
			}
			if !editorOnly && a.headerActionVisible("pull") {
				iconButton(c, "Pull", "Pull from the upstream branch", pullIcon,
					a.actionBusy, func() {
						a.confirmPull()
					})
			}
			if !editorOnly && a.headerActionVisible("push") {
				iconButton(c, "Push", "Push to the remote", pushIcon,
					a.actionBusy, func() {
						a.confirmPush()
					})
			}
			startGroup(workspaceActions)
			if refreshVisible {
				iconButton(c, "Refresh", "Refresh (F5)", refreshIcon, a.actionBusy, a.beginRefresh)
			}
			if editorVisible {
				a.editorButton(c)
			}
			startGroup(themeSettings)
			if a.headerActionVisible("theme") {
				a.themeButton(c)
			}
			if a.headerActionVisible("settings") {
				iconButton(c, "Settings", "Settings (Ctrl+,)", settingsIcon, false, a.openSettings)
			}
		})
	})
}

// editorOnlyWorkspace identifies a plain local folder. It deliberately does
// not alter the saved Git section visibility or order preferences.
func (a *app) editorOnlyWorkspace() bool {
	return a.state == stateReady && a.session == nil
}

// sectionVisible reports whether the sidebar shows a section. A stored id
// that names no section is ignored; normalizeVisibleSections keeps at least
// one, so the workspace never wakes up without a pane.
func (a *app) sectionVisible(section int) bool {
	if section < 0 || section >= len(workspaceSectionIDs) {
		return false
	}
	if a.editorOnlyWorkspace() {
		return section == sectionCode
	}
	if section == sectionHistory && a.overviewCommitTarget.Hash != "" {
		return true
	}
	return slices.Contains(a.settings.VisibleSections, workspaceSectionIDs[section])
}

// orderedSections lists every page in the user's chosen order.
func (a *app) orderedSections() []int {
	sections := make([]int, 0, len(workspaceSectionIDs))
	for _, id := range normalizeSectionOrder(a.settings.SectionOrder) {
		sections = append(sections, slices.Index(workspaceSectionIDs, id))
	}
	return sections
}

// visibleSections lists the sections the sidebar shows, in the user's order.
func (a *app) visibleSections() []int {
	sections := make([]int, 0, len(workspaceSectionIDs))
	for _, section := range a.orderedSections() {
		if a.sectionVisible(section) {
			sections = append(sections, section)
		}
	}
	return sections
}

// moveWorkspaceSection moves a page to the target's position. Visibility is
// independent, so disabled pages can be arranged before they are enabled.
func (a *app) moveWorkspaceSection(source, target string) bool {
	order := normalizeSectionOrder(a.settings.SectionOrder)
	from, to := slices.Index(order, source), slices.Index(order, target)
	if from < 0 || to < 0 || from == to {
		return false
	}
	order = slices.Delete(order, from, from+1)
	order = slices.Insert(order, to, source)
	a.settings.SectionOrder = order
	a.syncNavSelection()
	return true
}

// navRow maps a sidebar row to the section it shows. The row is out of range
// while a settings change has just removed it.
func (a *app) navRow(row int) (int, bool) {
	visible := a.visibleSections()
	if row < 0 || row >= len(visible) {
		return 0, false
	}
	return visible[row], true
}

// syncNavSelection points the sidebar list at the row of the active section,
// so a change to the visible sections does not leave it on another one.
func (a *app) syncNavSelection() {
	for row, section := range a.visibleSections() {
		if section == a.workspaceTab {
			a.navSelected = row
			return
		}
	}
	a.navSelected = 0
}

// setSectionVisible hides or shows a sidebar section. The last visible one
// cannot be hidden, so the workspace always has a pane to show.
func (a *app) setSectionVisible(section int, visible bool) {
	if section < 0 || section >= len(workspaceSectionIDs) {
		return
	}
	id := workspaceSectionIDs[section]
	shown := slices.DeleteFunc(slices.Clone(a.settings.VisibleSections),
		func(shown string) bool { return shown == id })
	if visible {
		shown = append(shown, id)
	} else if len(a.visibleSections()) <= 1 {
		return
	}
	a.settings.VisibleSections = normalizeVisibleSections(shown)
	a.ensureVisibleSection()
}

// ensureVisibleSection switches away from a section the settings hide, as
// when a stored file or a reset takes the active one out of the sidebar.
func (a *app) ensureVisibleSection() {
	if a.sectionVisible(a.workspaceTab) {
		a.syncNavSelection()
		return
	}
	if visible := a.visibleSections(); len(visible) > 0 {
		a.setWorkspaceTab(visible[0])
	}
}

// navView is the section sidebar: a keyboard-navigable list that switches the
// workspace pane.
func (a *app) navView(c *ui.Context) {
	theme := c.Theme()
	a.syncNavSelection()
	islandCard(c, ui.Column(c).Grow(1).Gap(2).Padding(10, 6)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Padding(2, 8).Children(func() {
			ui.Text(c, tr("Workspace")).TextColor(theme.TextMuted).Grow(1)
			paneCollapseButton(c, &a.navPaneCollapsed, "Collapse workspace sidebar")
		})
		list := a.keyboardList(c, &a.navList, len(a.visibleSections())).Grow(1).Rows(func(row ui.ListRow) {
			section, ok := a.navRow(row.Index)
			if !ok {
				return
			}
			ui.Row(row.Context).Gap(8).Padding(7, 10).Children(func() {
				iconColor := theme.TextMuted
				if row.Selected() {
					iconColor = theme.AccentText
				}
				ui.Icon(row.Context, sectionIcons[section]).FontSize(14).TextColor(iconColor)
				name := ui.Text(row.Context, tr(workspaceSections[section])).Grow(1).SingleLine()
				if section == sectionPullRequests {
					name.Tooltip(tr("Pull request or merge request"))
				}
				if count := a.sectionCount(section); count > 0 {
					ui.Badge(row.Context, fmt.Sprintf("%d", count))
				}
			})
		})
		if a.keyboardListChanged(&a.navList, list) {
			if section, ok := a.navRow(a.navSelected); ok {
				a.setWorkspaceTab(section)
			}
		}
	})
}

// setWorkspaceTab switches the active section, closes the commit form, and
// loads on-demand details the new section shows. History and Graph read the
// chosen commit's details as the pane draws, so they need nothing here.
func (a *app) setWorkspaceTab(tab int) {
	if tab < 0 || tab >= len(workspaceSectionIDs) || !a.sectionVisible(tab) {
		return
	}
	previous := a.workspaceTab
	// History may show a filtered list while the Graph shows every commit,
	// so the same index names different commits in the two sections. Carry
	// the chosen commit across by hash rather than by position.
	selectedHash := commitHashAt(a.visibleCommits(), a.selectedCommit)
	a.workspaceTab = tab
	if tab == sectionAgents && a.agents.loadedRepo != a.agentRepositoryID() && !a.agents.loading {
		a.refreshAgentTasks()
	}
	if tab == sectionOverview && previous != sectionOverview {
		a.prepareCockpit()
	}
	if tab == sectionPullRequests && !a.changeRequests.loaded && !a.changeRequests.loading {
		a.refreshChangeRequests()
	}
	a.syncNavSelection()
	a.showCommitForm = false
	if isCommitSection(tab) {
		commits := a.visibleCommits()
		// The first page can finish loading while another section is open.
		// Select its first commit if there is no valid choice to carry over.
		if isCommitSection(previous) || a.selectedCommit < 0 || a.selectedCommit >= len(commits) {
			a.commitList.Choose(selectedCommitIndex(commits, selectedHash))
		}
	}
}

// isCommitSection reports whether a workspace section lists commits.
func isCommitSection(tab int) bool {
	return tab == sectionHistory || tab == sectionGraph
}

// sectionCount is the badge a sidebar section shows, 0 for none. History and
// PR/MR keep their counts on their list headers, where filters and paging give
// the number its proper context.
func (a *app) sectionCount(section int) int {
	switch section {
	case sectionChanges:
		return len(a.snapshot.Files)
	case sectionBranches:
		return len(a.snapshot.Branches)
	case sectionRemotes:
		return len(a.snapshot.Remotes)
	case sectionStash:
		return len(a.snapshot.Stashes)
	case sectionTags:
		return len(a.snapshot.Tags)
	case sectionWorktrees:
		return len(a.snapshot.Worktrees)
	case sectionAgents:
		count := 0
		for _, task := range a.agents.tasks {
			if agentTaskActive(task) {
				count++
			}
		}
		return count
	case sectionSubmodules:
		return len(a.snapshot.Submodules)
	}
	return 0
}

func (a *app) contentView(c *ui.Context) {
	if isCommitSection(a.workspaceTab) && a.snapshot.Bisect != nil && a.snapshot.Bisect.Active {
		ui.Column(c).Grow(1).Gap(10).Children(func() {
			a.bisectBanner(c)
			a.workspaceContentView(c)
		})
		return
	}
	a.workspaceContentView(c)
}

func (a *app) workspaceContentView(c *ui.Context) {
	switch a.workspaceTab {
	case sectionChanges:
		a.collapsibleContentPaneSplit(c, &a.filePaneWidth, &a.filePaneCollapsed,
			"Expand file list", func() {
				a.filesView(c)
			}, func() {
				a.fileDetailsView(c)
			})
	case sectionHistory:
		a.historyView(c)
	case sectionGraph:
		a.graphView(c)
	case sectionBranches:
		a.branchesView(c)
	case sectionRemotes:
		a.remotesView(c)
	case sectionStash:
		a.stashView(c)
	case sectionTags:
		a.tagsView(c)
	case sectionWorktrees:
		a.worktreesView(c)
	case sectionAgents:
		a.agentsView(c)
	case sectionSubmodules:
		a.submodulesView(c)
	case sectionReflog:
		a.reflogView(c)
	case sectionCode:
		a.codeView(c)
	case sectionCompare:
		a.comparePage(c)
	case sectionOverview:
		a.cockpitView(c)
	case sectionPullRequests:
		a.changeRequestsView(c)
	}
}

// pathCopiedFor is how long the status bar shows "Copied" in place of the
// repository path after a double click copies it.
const pathCopiedFor = 500 * time.Millisecond

// repositoryPathView shows the path of the open repository at the bar's left,
// with a copy icon after it. A click on either the icon or the path copies
// the path and puts "Copied" in its place for half a second, then the path
// again, so it can go to a terminal or another app without a menu.
func (a *app) repositoryPathView(c *ui.Context) {
	theme := c.Theme()
	path := a.snapshot.Root
	if path == "" {
		return
	}
	text, tooltip := path, path
	if !a.pathCopiedAt.IsZero() {
		if c.Now().Sub(a.pathCopiedAt) < pathCopiedFor {
			text = tr("Copied")
			tooltip = tr("Copied repository path")
		} else {
			a.pathCopiedAt = time.Time{}
		}
	}
	copyPath := func() {
		c.WriteClipboard(path)
		a.pathCopiedAt = c.Now()
		// This frame was drawn with the path; the next one shows the copy,
		// and the one after half a second puts the path back.
		c.Invalidate()
		c.After(pathCopiedFor)
	}
	ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
		label := ui.Text(c, text).TextColor(theme.TextMuted).
			SingleLine().MaxWidth(380).Tooltip(tooltip)
		// A single click copies as a double click does: the path is one piece
		// of text, so both are the same act and neither needs a menu.
		if label.Clicked() || label.DoubleClicked() {
			copyPath()
		}
		button := ui.ButtonBase(c).Padding(1)
		button.Label(tr("Copy repository path")).Tooltip(tr("Copy the repository path to the clipboard"))
		button.Children(func() {
			ui.Icon(c, copyIcon).FontSize(12).TextColor(theme.TextMuted)
		})
		if button.Hovered() {
			button.Background(theme.SurfaceHover)
		}
		if button.Clicked() {
			copyPath()
		}
	})
}

func (a *app) statusBar(c *ui.Context) {
	theme := c.Theme()
	islandCard(c, ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(4, 12)).PopoverMenu(a.statusBarMenu).Children(func() {
		a.repositoryPathView(c)
		if a.actionError != "" {
			// The error is one line here, whatever Git printed: a long
			// message left the bar to wrap, and its height pushed the
			// workspace off the top of the window. The whole message is
			// in the command log, which the button beside it opens.
			line := statusErrorLine(a.actionError)
			errorButton := ui.ButtonBase(c).Padding(1, 3).Radius(theme.Radius).
				Label(tr("Command log") + ": " + line)
			richTooltip(c, errorButton, richTip{Title: tr("Command log"), Body: shorten(a.actionError, 400)})
			errorButton.Children(func() {
				ui.Text(c, line).TextColor(theme.Danger).
					SingleLine().MaxWidth(520)
			})
			if errorButton.Hovered() {
				errorButton.Background(theme.Danger.Alpha(0.08))
			}
			if errorButton.Clicked() {
				a.openFailedCommand(a.actionFailure)
			}
		}
		a.keyboardPane(c, "status", ui.Row(c).Grow(1).Justify(ui.End).Gap(8).Focusable()).Children(func() {
			if a.customCommandRunning || a.actionBusy && a.fileMutation.phase != fileMutationChecking {
				// The island's ordinary text is muted by design. Ongoing work must
				// not disappear into it: use a quiet but clearly tinted patch for
				// both the shape and the high-contrast text/spinner.
				ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(2, 6).Radius(theme.Radius).
					Background(theme.Warning.Alpha(0.14)).Children(func() {
					ui.Spinner(c).TextColor(theme.Warning).Label(tr("Working"))
					ui.Text(c, tr("Working…")).Bold().TextColor(theme.Warning)
				})
			}
			if !a.editorOnlyWorkspace() && a.statusActionVisible("pull-requests") {
				a.pullRequestMenuButton(c)
			}
			if a.statusActionVisible("terminal") {
				a.terminalButton(c)
			}
			if a.statusActionVisible("keyboard-shortcuts") {
				iconButton(c, "Keyboard shortcuts", "Keyboard shortcuts (F1)", keyboardIcon,
					false, a.openKeybindings)
			}
			if !a.editorOnlyWorkspace() && a.statusActionVisible("activity") {
				iconButton(c, "Activity & recovery", "Review repository operations and safely restore supported actions",
					historyIcon, false, a.openActivityRecovery)
			}
			if !a.editorOnlyWorkspace() && a.statusActionVisible("command-log") {
				button := ui.ButtonBase(c).AlignItems(ui.Center).Gap(5).Padding(2, 7).
					Radius(theme.Radius).Label(tr("Command log")).
					Tooltip(tr("Every Git command and its output (Ctrl+L)"))
				button.Children(func() {
					ui.Icon(c, logIcon).FontSize(12).TextColor(theme.TextMuted)
					ui.Text(c, tr("Command log")).FontSize(theme.FontSize - 1)
				})
				if button.Hovered() {
					button.Background(theme.SurfaceHover)
				}
				if button.Clicked() {
					a.openCommandLog()
				}
			}
		})
	})
}

// statusErrorLine picks the one line an action's error shows in the status
// bar: Git's own fatal or error line when it has one, else the first line
// that says anything. The whole message waits in the command log.
func statusErrorLine(text string) string {
	first := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if first == "" {
			first = line
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "fatal:") || strings.HasPrefix(lower, "error:") {
			return shorten(line, 140)
		}
	}
	return shorten(first, 140)
}

// recentRenameModal names a repository of the window: the picker's card and
// the page's tab share the one name. The folder, and the repository in it,
// keep theirs, so what is typed here only changes the label.
func (a *app) recentRenameModal(c *ui.Context) {
	if !a.showRenameRecent {
		return
	}
	theme := c.Theme()
	titleKey := "Rename repository"
	subtitleKey := "Give this card a name of its own."
	fieldKey := "Card name"
	noteKey := "This name is only the card's label; the folder and the repository are not renamed."
	if a.renameRecentFromTab {
		titleKey = "Rename tab"
		subtitleKey = "Give this tab a name of its own."
		fieldKey = "Tab name"
		noteKey = "This name labels the repository in its tab and in the recent list; the folder is not renamed."
	}
	closeModal := func() {
		a.showRenameRecent = false
		a.renameRecentFromTab = false
		a.renameRecentPath = ""
		a.renameRecentName = ""
	}
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(500).MaxWidthPercent(92).
				Padding(22).Gap(16).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr(titleKey)).
				Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
						ui.Box(c).Size(34, 34).Radius(9).Center().
							Background(theme.Accent.Alpha(0.14)).Children(func() {
							ui.Icon(c, editIcon).FontSize(18).TextColor(theme.Accent)
						})
						ui.Column(c).Gap(2).Grow(1).Children(func() {
							ui.Text(c, tr(titleKey)).FontSize(17).Bold()
							ui.Text(c, tr(subtitleKey)).
								TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
						})
						iconButton(c, "Close rename form", "Escape", closeIcon, false, closeModal)
					})
					ui.Column(c).Gap(4).Children(func() {
						ui.Text(c, tr(fieldKey)).TextColor(theme.TextMuted).
							FontSize(theme.FontSize - 1)
						input := ui.TextInput(c, &a.renameRecentName).
							Label(tr(fieldKey)).
							Placeholder(repoName(a.renameRecentPath)).
							AutoFocus()
						if input.Submitted() {
							a.beginRenameRecent()
						}
					})
					ui.Text(c, tr(noteKey)).
						TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
						ui.Text(c, trf("Up to %d characters. Leave it empty to show the folder name again.", recentNameLimit)).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
						if ui.Button(c, tr("Cancel")).Clicked() {
							closeModal()
						}
						if ui.PrimaryButton(c, tr("Save name")).Clicked() {
							a.beginRenameRecent()
						}
					})
				})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				closeModal()
			}
		})
	})
}
