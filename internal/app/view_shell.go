package app

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
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
	workspaceSectionCount
)

var workspaceSections = []string{
	"Editor", "Changes", "History", "Graph", "Branches", "Remotes", "Stash", "Tags",
	"Worktrees", "Agent tasks", "Submodules", "Reflog", "Compare refs",
}

// workspaceSectionIDs are the stable names of the sections the settings
// store, in the order of workspaceSections. A stored id keeps working if the
// sections are ever reordered.
var workspaceSectionIDs = []string{
	"code", "changes", "history", "graph", "branches", "remotes", "stash", "tags",
	"worktrees", "agents", "submodules", "reflog", "compare",
}

// defaultVisibleSections shows every workspace section on a fresh install.
// Users can still hide specialist sections in Settings.
var defaultVisibleSections = slices.Clone(workspaceSectionIDs)

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
	} else {
		a.welcomeView(c)
	}
	a.branchFullscreenView(c, overlayTopInset)
	a.diffFullscreenView(c, overlayTopInset)
	a.commitActionModals(c)
	a.rebasePlanModal(c)
	a.branchFormModal(c)
	a.stackSyncModal(c)
	a.moveCommitsModal(c)
	a.recoveryModal(c)
	a.stashFormModal(c)
	a.remoteFormModal(c)
	a.remoteWebURLFormModal(c)
	a.tagFormModal(c)
	a.worktreeFormModal(c)
	a.agentTaskFormModal(c)
	a.conflictAssistantModal(c)
	a.changeSplitterModal(c)
	a.worktreeMoveFormModal(c)
	a.worktreeLockFormModal(c)
	a.worktreeRepairFormModal(c)
	a.worktreePruneFormModal(c)
	a.submoduleFormModal(c)
	a.homeWorktreesModal(c)
	a.recentRenameModal(c)
	a.codeFileFormModal(c)
	a.codeWorkspaceFolderModal(c)
	a.terminalRenameModal(c)
	a.cloneModal(c)
	a.bisectFormModal(c)
	a.exportPatchFormModal(c)
	a.pullRequestFormModal(c)
	a.pullRequestChecksModal(c)
	a.sshWorkspaceModal(c)
	a.patchPreviewModal(c)
	a.customPatchModal(c)
	a.customPatchHistoryModal(c)
	a.changeCourierModal(c)
	a.commandLogModal(c)
	a.commandPromptModal(c)
	a.continueReviewModal(c)
	a.settingsModal(c)
	a.keybindingsModal(c)
	a.commitFilterModal(c)
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
	c.SetTheme(themeForSettings(a.settings, systemDark))
	a.applyBufferFonts()
	a.syncHomeBackground()
}

// welcomeView is the repository picker shown before a repository is open and
// when opening one failed: the window's own brand bar, one way in, then the
// history as cards. While a repository opens it gives way to the loading page,
// so the picker is never shown as if it were ready to use.
func (a *app) welcomeView(c *ui.Context) {
	if a.state == stateCloning {
		a.cloneProgressView(c)
		return
	}
	if a.state == stateLoading {
		a.loadingView(c)
		return
	}
	_, height := c.Size()
	// The window's context still measures the whole window under the shell,
	// so take off the title bar and its divider as well as this page's bar,
	// then the launcher's own padding: a minimum that left it in would make
	// the page a little taller than the window and show a scroll bar that
	// has nowhere to go.
	viewport := max(height-titleBarHeight-1-pickerHeaderHeight, 0)
	page := max(viewport-pickerPadTop-pickerPadBottom, 0)
	ui.Box(c).Fill().Background(c.Theme().Background).Children(func() {
		a.homeBackgroundView(c)
		ui.Column(c).Fill().Children(func() {
			a.pickerHeaderView(c)
			// The launcher starts near the top of what the header leaves, so
			// the page does not open with a large empty band above it. The
			// history follows in the same scroll view when it outgrows the window.
			ui.Scroll(c).Fill().Children(func() {
				ui.Column(c).FillWidth().MinHeight(page).
					AlignItems(ui.Center).Justify(ui.Start).
					Padding(pickerPadTop, pickerPadSide, pickerPadBottom).Gap(20).Children(func() {
					mode := pickerOpenModeControl(c, &a.pickerOpenMode)
					if mode.Changed() {
						a.recentFilter = ""
						a.selectedRecent = -1
					}
					a.pickerHeroView(c)
					switch a.state {
					case stateEmpty:
						if a.offerInitPath != "" {
							a.initPromptView(c)
						}
						a.recentSectionView(c)
					case stateError:
						// The history stays on screen after a failure, so a
						// wrong path never hides the repositories the user
						// can pick instead.
						a.pickerErrorView(c)
						a.recentSectionView(c)
					}
				})
			})
		})
	})
}

// pickerHeaderHeight is the brand bar's height: its padding above and below
// the 36-DIP mark. The launcher centres in what the bar leaves.
const pickerHeaderHeight float32 = 72

// The launcher's room inside the scroll view. Kept as constants because the
// page's minimum height leaves them out. The launch card and repository history
// share the same content width, including when the window leaves room on both sides.
const (
	pickerPadTop       float32 = 20
	pickerPadSide      float32 = 32
	pickerPadBottom    float32 = 44
	pickerContentWidth float32 = 1000
)

// pickerHeaderView is the picker's own bar: the app's name on the left, the
// three app-wide controls on the right. The window's title bar already names
// the page, so this bar stays short.
func (a *app) pickerHeaderView(c *ui.Context) {
	theme := c.Theme()
	ui.Box(c).FillWidth().Padding(18, 32).Children(func() {
		// The bar lines up with the history below it, so the page reads as
		// one column rather than two widths of content.
		ui.Row(c).MaxWidth(pickerContentWidth).AlignSelf(ui.Center).FillWidth().
			AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Box(c).Size(36, 36).Radius(10).Center().
				Background(theme.Accent.Alpha(0.15)).Children(func() {
				ui.Icon(c, appIcon).FontSize(19).TextColor(theme.Accent)
			})
			ui.Column(c).Gap(1).Grow(1).Children(func() {
				ui.Text(c, appName).FontSize(18).Bold()
				ui.Text(c, tr("A native Git client, built on Lazygit")).
					FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
			})
			a.themeButton(c)
			iconButton(c, "Keyboard shortcuts", "Keyboard shortcuts (F1)", keyboardIcon,
				false, a.openKeybindings)
			iconButton(c, "Settings", "Settings (Ctrl+,)", settingsIcon, false, a.openSettings)
		})
	})
}

// pickerHeroView keeps one clear way into a workspace: enter its path or
// browse for it. The launcher itself is flat; only its interactive path field
// has a border, so Home does not become a stack of nested cards.
func (a *app) pickerHeroView(c *ui.Context) {
	theme := c.Theme()
	editorMode := a.pickerOpenMode == int(openAllowPlainEditor)
	title, description := "Open a repository", "Your changes, history, branches, and remotes — all in one focused place."
	folderLabel, placeholder := "Repository folder", "/path/to/repository"
	if editorMode {
		title = "Open a folder"
		description = "Browse, search, edit files, and use a terminal — no Git repository required."
		folderLabel, placeholder = "Folder", "/path/to/folder"
	}
	ui.Column(c).MaxWidth(pickerContentWidth).FillWidth().AlignSelf(ui.Center).Gap(12).
		Padding(2, 0, 8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(16).Wrap().Children(func() {
			ui.Column(c).Grow(1).MinWidth(280).Gap(4).Children(func() {
				ui.Text(c, tr(title)).FontSize(22).Bold()
				ui.Text(c, tr(description)).TextColor(theme.TextMuted).MaxWidth(560)
			})
			if !editorMode {
				ui.Row(c).Justify(ui.End).AlignItems(ui.Center).Gap(4).Wrap().Children(func() {
					pickerRemoteAction(c, "Open SSH",
						"Open a Git folder through a saved SSH profile", remotesIcon, a.openSSHWorkspacePicker)
					pickerRemoteAction(c, "Clone",
						"Clone an HTTPS or SSH repository and open it (C)", downloadIcon, a.openCloneForm)
				})
			}
		})
		bar := ui.Row(c).FillWidth().Height(46).AlignItems(ui.Center).Gap(9).
			Padding(5, 6, 5, 13).Background(theme.Surface).Border(1, theme.Border).
			Radius(theme.Radius + 4)
		if bar.Hovered() {
			bar.Border(1, theme.Accent.Alpha(0.55))
		}
		bar.Children(func() {
			ui.Icon(c, folderOpenIcon).FontSize(18).TextColor(theme.TextMuted)
			path := ui.TextInputBase(c, &a.path).
				Label(tr(folderLabel)).AutoFocus().
				Placeholder(tr(placeholder)).
				Tooltip(tr("Terminal: open a Git repository or folder path directly in TanGit")).
				FontSize(15).Height(34).Grow(1)
			if path.Submitted() {
				a.openFromPicker()
			}
			browse := ui.PrimaryButton(c, "").
				Label(tr("Browse…")).
				Tooltip(tr("Choose a folder — it opens once you pick it")).
				Gap(6).Height(34).Radius(theme.Radius + 2)
			clicked := browse.Clicked()
			browse.Children(func() {
				ui.Icon(c, folderOpenIcon).FontSize(14)
				ui.Text(c, tr("Browse…"))
			})
			if clicked {
				a.browse()
			}
		})
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
			hint := "↵  " + tr("Enter opens the path")
			if !editorMode {
				hint += "   ·   C  " + tr("Clone a remote repository")
			}
			ui.Text(c, hint).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
			ui.Box(c).Grow(1).MinWidth(8)
			ui.Text(c, "F1  "+tr("Keyboard shortcuts")).
				FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
			ui.Text(c, "·").FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
			ui.Text(c, "tangit <path>").FontSize(theme.FontSize - 2).TextColor(theme.TextMuted).
				Tooltip(tr("Terminal: open a Git repository or folder path directly in TanGit"))
		})
	})
}

// pickerOpenModeControl is Home's top-level Git/Editor switch. The selected
// segment wears an accent face, rather than light theme's nearly invisible
// white-on-gray default, and controls both the launcher and its recent list.
func pickerOpenModeControl(c *ui.Context, selected *int) ui.Element {
	theme := c.Theme()
	labels := []string{tr("Git repository"), tr("Editor")}
	parts := ui.SegmentedBase(c, selected, len(labels))
	track := parts.Track.AlignItems(ui.Stretch).Padding(theme.Space(0.5)).Gap(theme.Space(0.5)).
		Radius(theme.Radius).Background(theme.Surface).Border(1, theme.Border).Label(tr("Open mode"))
	track.Children(func() {
		for i, label := range labels {
			chosen := i == *selected
			segment := parts.Segment(i).Padding(theme.Space(1), theme.Space(3)).
				Radius(max(theme.Radius-theme.Space(0.5), 0))
			switch {
			case chosen:
				segment.Background(theme.Accent.Alpha(0.14)).TextColor(theme.Accent)
			case segment.Hovered():
				segment.Background(theme.SurfaceHover)
			}
			segment.Children(func() {
				text := ui.Text(c, label).SingleLine()
				if chosen {
					text.Bold()
				}
			})
		}
	})
	return track
}

// pickerRemoteAction keeps SSH and Clone visible as quiet text actions; the
// path field's Browse button remains the launcher's only filled action.
func pickerRemoteAction(c *ui.Context, label, tooltip string, icon *ui.SVG, run func()) {
	theme := c.Theme()
	button := ui.ButtonBase(c).Label(tr(label)).Tooltip(tr(tooltip)).
		Height(38).Padding(6, 10).Center().Gap(7).Radius(theme.Radius)
	if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	button.Children(func() {
		ui.Icon(c, icon).FontSize(16).TextColor(theme.TextMuted)
		ui.Text(c, tr(label)).FontSize(14)
	})
	if button.Clicked() {
		run()
	}
}

// pickerErrorView is a failed open, kept above the history: the message says
// what happened, and the cards below still offer a way on.
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
			ui.Box(c).Size(64, 64).Radius(18).Center().
				Background(theme.Accent.Alpha(0.16)).Children(func() {
				ui.Icon(c, appIcon).FontSize(30).TextColor(theme.Accent)
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
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			iconTextButton(c, "Initialize repository", "Create a Git repository in this folder",
				initRepoIcon, true, a.initializeRepository)
			iconTextButton(c, "Choose another folder", "Pick a different folder",
				folderOpenIcon, false, a.chooseAnotherFolder)
			iconTextButton(c, "Not now", "Leave the folder alone and stay on the picker",
				closeIcon, false, a.dismissInitOffer)
		})
	})
}

// recentSectionView is the active Home tab's searchable Git or plain-folder
// history. Its filtered grid carries original indices through to every card.
func (a *app) recentSectionView(c *ui.Context) {
	editorMode := a.pickerOpenMode == int(openAllowPlainEditor)
	all := filterRecentMode(a.recentRepositories, "", &editorMode)
	a.visibleRecent = nil
	if len(all) == 0 {
		a.emptyRecentView(c)
		return
	}
	theme := c.Theme()
	services := c.Services()
	a.visibleRecent = filterRecentMode(a.recentRepositories, a.recentFilter, &editorMode)
	title, searchLabel, noMatches := "Recent repositories", "Search recent repositories", "No repositories match your search"
	if editorMode {
		title, searchLabel, noMatches = "Recent folders", "Search recent folders", "No folders match your search"
	}
	ui.Column(c).MaxWidth(pickerContentWidth).FillWidth().AlignSelf(ui.Center).Gap(12).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Wrap().Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
					ui.Text(c, tr(title)).FontSize(17).Bold()
					ui.Text(c, fmt.Sprintf("(%d)", len(all))).
						FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
				})
				ui.Box(c).Grow(1).MinWidth(12)
				ui.SearchField(c, &a.recentFilter).
					Label(tr(searchLabel)).
					Placeholder(tr("Filter by name or path…")).
					Width(240).Height(34)
			})
			if len(a.visibleRecent) == 0 {
				ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(7).Padding(24).
					Background(theme.Surface.Alpha(0.6)).Border(1, theme.Border).
					BorderStyle(ui.BorderDashed).Radius(theme.Radius + 6).Children(func() {
					ui.Icon(c, searchIcon).FontSize(22).TextColor(theme.TextMuted)
					ui.Text(c, tr(noMatches)).Bold()
					ui.Text(c, tr("Try a different name or path.")).TextColor(theme.TextMuted)
				})
				return
			}
			ui.GridView(c, &a.recentGrid, len(a.visibleRecent), 400, 118, func(i int) {
				a.recentCardView(c, a.visibleRecent[i], services)
			}).FillWidth()
		})
}

// recentCardView keeps the folder identity and the common destinations one
// click away. Lower-frequency card management lives under one More menu, also
// available by right-click, instead of filling every card with icon buttons.
func (a *app) recentCardView(c *ui.Context, i int, services ui.Services) {
	theme := c.Theme()
	repo := a.recentRepositories[i]
	selected := a.selectedRecent == i
	c.Key(fmt.Sprintf("recent:%t:%s", repo.EditorOnly, repo.Path))
	card := ui.Column(c).Fill().Gap(6).Padding(12, 14).
		Focusable().Role(ui.RoleButton).Label(trf("Open %s", repo.Path)).FocusRing(true).
		Background(theme.Surface).Border(1, theme.Border.Alpha(0.8)).
		Radius(theme.Radius + 4)
	if card.Hovered() && !selected {
		card.Background(theme.SurfaceHover).
			Border(1, theme.Accent.Alpha(0.4))
	}
	if selected {
		card.Background(theme.SurfaceHover).Border(1, theme.Accent)
	}
	card.OnShortcut(0, ui.KeyEnter, func() { a.openRecent(repo.Path) }).
		OnShortcut(0, ui.KeySpace, func() { a.openRecent(repo.Path) })
	card.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Box(c).Size(30, 30).Radius(9).Center().
				Background(theme.Accent.Alpha(0.12)).Children(func() {
				ui.Icon(c, folderIcon).FontSize(16).TextColor(theme.Accent)
			})
			ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
				title := ui.Text(c, repo.displayName()).FontSize(15).Bold().SingleLine()
				if repo.renamed() {
					title.Tooltip(repo.Path)
				}
				ui.Text(c, repo.Path).TextColor(theme.TextMuted).
					FontSize(theme.FontSize - 2).SingleLine().Tooltip(repo.Path)
				if repo.Worktree && strings.TrimSpace(repo.RepositoryPath) != "" {
					ownerPath := repo.RepositoryPath
					owner := tr("Main worktree") + " · " + repoName(ownerPath)
					ui.Text(c, owner).TextColor(theme.TextMuted).
						FontSize(theme.FontSize - 2).SingleLine().
						Tooltip(tr("Main worktree") + ": " + ownerPath)
				}
			})
			if !repo.EditorOnly {
				// Existing checkouts are one click away from the card, without
				// opening the source repository first or replacing the whole card.
				worktrees := ui.ButtonBase(c).Size(28, 28).Radius(theme.Radius).Center().
					Label(tr("Open worktree") + " " + repo.Path).
					Tooltip(tr("Choose a main or linked worktree to open"))
				if worktrees.Hovered() {
					worktrees.Background(theme.SurfacePressed)
				}
				worktrees.Children(func() {
					ui.Icon(c, worktreesIcon).FontSize(16).TextColor(theme.TextMuted)
				})
				// Only the card whose menu is active owns the shared open state.
				// Binding every card to it makes the next card steal and close the
				// menu during the same frame, especially when the cards are compact.
				menuOptions := ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd}
				if a.homeWorktrees.source == repo.Path {
					menuOptions.Open = &a.homeWorktrees.menuOpen
				}
				worktrees.PopoverMenuButton(func(m *ui.Menu) {
					a.homeWorktreeMenu(m, repo.Path)
				}, menuOptions)
			}
			editorTooltip := "Open this repository in your editor"
			if repo.EditorOnly {
				editorTooltip = "Open this folder in your editor"
			}
			editor := ui.ButtonBase(c).Size(28, 28).Radius(theme.Radius).Center().
				Label(trf("Open %s in editor", repo.Path)).
				Tooltip(tr(editorTooltip))
			if editor.Hovered() {
				editor.Background(theme.SurfacePressed)
			}
			editor.Children(func() {
				ui.Icon(c, fileCodeIcon).FontSize(16).TextColor(theme.TextMuted)
			})
			if editor.Clicked() {
				a.launchEditor(repo.Path, 0)
			}
			more := ui.ButtonBase(c).Size(28, 28).Radius(theme.Radius).Center().
				Label(tr("More actions") + " " + repo.Path).Tooltip(tr("More actions"))
			if more.Hovered() {
				more.Background(theme.SurfacePressed)
			}
			more.Children(func() {
				ui.Icon(c, moreIcon).FontSize(16).TextColor(theme.TextMuted)
			})
			more.PopoverMenuButton(func(m *ui.Menu) {
				a.recentCardMenu(m, repo, services)
			}, ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd})
		})
		identity := strings.TrimSpace(repo.Branch)
		if identity == "(no branch)" {
			identity = ""
		}
		if repo.Worktree {
			if identity == "" {
				identity = strings.TrimSpace(repo.WorktreeName)
			}
			if identity == "" {
				identity = tr("Worktree")
			} else {
				identity = tr("Worktree") + " · " + identity
			}
		}
		ui.Box(c).Grow(1)
		ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(8).Padding(0, 0, 0, 34).Children(func() {
			if identity != "" {
				ui.Badge(c, identity).SingleLine().MaxWidth(240).MinWidth(0).Tooltip(identity)
			}
			ui.Box(c).Grow(1)
			if rel := relTime(repo.LastOpened); rel != "" {
				ui.Text(c, rel).TextColor(theme.TextMuted).
					FontSize(theme.FontSize - 2).SingleLine()
			}
		})
	}).OnClick(func() {
		a.selectedRecent = i
		a.openRecent(repo.Path)
	}).PopoverMenu(func(m *ui.Menu) {
		a.recentCardMenu(m, repo, services)
	})
}

// recentCardMenu is shared by the visible More button and the card's
// right-click menu, so simplifying the canvas never removes an action.
func (a *app) recentCardMenu(m *ui.Menu, repo recentRepository, services ui.Services) {
	if m.Item(tr("Open")).Chosen() {
		a.openRecent(repo.Path)
	}
	if m.Item(tr("Open in editor")).Chosen() {
		a.launchEditor(repo.Path, 0)
	}
	if !repo.EditorOnly && a.openTab != nil && m.Item(tr("Open in new tab")).Chosen() {
		a.openTab(repo.Path)
	}
	if !repo.EditorOnly && m.Item(tr("Open worktree")).Chosen() {
		a.openHomeWorktrees(repo.Path)
	}
	if m.Item(tr("Copy path")).Chosen() {
		a.notifyCopied(services, "repository path", repo.Path)
	}
	if m.Item(tr("Rename…")).Chosen() {
		a.openRecentRename(repo.Path)
	}
	if m.Item(tr("Remove from list")).Chosen() {
		// The menu must not rewrite the list this build is reading;
		// view() applies it next pass.
		a.pendingForget = repo.Path
		a.pendingForgetEditorOnly = repo.EditorOnly
	}
}

// emptyRecentView is the picker before it has anything to remember: a quiet
// invitation without introducing another decorative card.
func (a *app) emptyRecentView(c *ui.Context) {
	theme := c.Theme()
	title := "Open a Git repository to begin"
	description := "Changes, history, branches, remotes, stash, tags, worktrees, and submodules in one window."
	if a.pickerOpenMode == int(openAllowPlainEditor) {
		title = "Open a folder to begin"
		description = "Browse, search, and edit files or use a terminal without initializing Git."
	}
	ui.Column(c).MaxWidth(720).FillWidth().AlignSelf(ui.Center).
		AlignItems(ui.Center).Gap(10).Padding(22).Children(func() {
		ui.Box(c).Size(54, 54).Radius(16).Center().
			Background(theme.Accent.Alpha(0.14)).Children(func() {
			ui.Icon(c, folderOpenIcon).FontSize(26).TextColor(theme.Accent)
		})
		ui.Text(c, tr(title)).FontSize(18).Bold()
		ui.Text(c, tr(description)).TextColor(theme.TextMuted).TextAlign(ui.Center).MaxWidth(520)
		// No second Browse button: the bar above already holds the one way
		// in, and two of them on one page read as two different actions.
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
	target := float32(0)
	if a.term.panelOpen {
		target = a.term.percent
		if a.term.panelMaximized {
			target = 100
		}
	}
	box := ui.Box(c)
	size := target
	if a.term.panelOpen && !a.term.panelMaximized {
		size = box.Animate("terminal-size", target, 200*time.Millisecond)
	} else {
		// Hiding and maximizing switch directly between layouts; their targets
		// are not rendered as fractional sizes, so animating would only rebuild.
		box.Jump("terminal-size", target)
	}
	dock := a.terminalDock()
	terminalFirst := dock == terminalDockTop || dock == terminalDockLeft

	switch {
	case !a.term.panelOpen || size <= 0.5:
		// Hiding must not shrink the live shell along with the animation. Once
		// its view is gone MyGo retains the last grid until it is shown again.
		a.workspaceView(c)
	case a.term.panelMaximized:
		// Maximization is a layout switch, not a resize animation. Waiting for
		// the spring leaves the workspace visible while its terminal is gone.
		a.terminalPaneView(c)
	case box.Settled("terminal-size"):
		if terminalFirst {
			a.term.shown = a.term.percent
		} else {
			a.term.shown = 100 - a.term.percent
		}
		workspace := func() { a.workspaceView(c) }
		terminal := func() { a.terminalPaneView(c) }
		first, second := workspace, terminal
		if terminalFirst {
			first, second = terminal, workspace
		}
		if dock == terminalDockLeft || dock == terminalDockRight {
			paneSplitPercent(c, &a.term.shown, first, second).Grow(1)
		} else {
			paneSplitVerticalPercent(c, &a.term.shown, first, second).Grow(1)
		}
		restored := 100 - a.term.shown
		if terminalFirst {
			restored = a.term.shown
		}
		if a.term.percent != restored {
			a.term.percent = restored
			box.Jump("terminal-size", a.term.percent)
		}
	default:
		// Animate an empty dock on the way in. Mounting terminal.View at every
		// fractional size would make fish/zsh redraw prompts for each PTY resize.
		a.terminalDockOpeningView(c, dock, size)
	}
}

// terminalDockOpeningView reserves the animated edge before mounting the live
// terminal. Empty sizing keeps hidden and intermediate PTY dimensions stable.
func (a *app) terminalDockOpeningView(c *ui.Context, dock string, size float32) {
	horizontal := dock == terminalDockLeft || dock == terminalDockRight
	terminalFirst := dock == terminalDockTop || dock == terminalDockLeft
	if horizontal {
		ui.Row(c).Grow(1).Children(func() {
			if terminalFirst {
				ui.Row(c).FillHeight().WidthPercent(size)
				ui.Divider(c)
			}
			ui.Column(c).Grow(1).Children(func() { a.workspaceView(c) })
			if !terminalFirst {
				ui.Divider(c)
				ui.Row(c).FillHeight().WidthPercent(size)
			}
		})
		return
	}
	ui.Column(c).Grow(1).Children(func() {
		if terminalFirst {
			ui.Column(c).FillWidth().HeightPercent(size)
			ui.Divider(c)
		}
		ui.Column(c).Grow(1).Children(func() { a.workspaceView(c) })
		if !terminalFirst {
			ui.Divider(c)
			ui.Column(c).FillWidth().HeightPercent(size)
		}
	})
}

// workspaceView is the section sidebar beside the active section, its width
// set by the divider between them.
func (a *app) workspaceView(c *ui.Context) {
	collapsiblePaneSplitLane(c, &a.navPaneWidth, &a.navPaneCollapsed,
		"Expand workspace sidebar", func() {
			a.navView(c)
		}, func() {
			ui.Column(c).Grow(1).FillHeight().Children(func() {
				if a.showCommitForm && a.workspaceTab == sectionChanges {
					a.commitForm(c)
				} else {
					a.contentView(c)
				}
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
	headerWidth := a.measuredActionGroupWidth(c, header)
	compactOperation := !a.editorOnlyWorkspace() && a.snapshot.Operation != "" &&
		(headerWidth <= 0 || headerWidth < 800)
	header.Children(func() {
		base := filepath.Base(a.snapshot.Root)
		if base == "." || base == string(filepath.Separator) || base == "" {
			base = a.snapshot.Root
		}
		if !compactOperation {
			ui.Text(c, base).FontSize(16).Bold().Tooltip(a.snapshot.Root)
		}
		if !a.editorOnlyWorkspace() {
			if !compactOperation && a.openedLinkedWorktree() {
				ui.Badge(c, tr("Linked worktree")).Tooltip(a.snapshot.Root)
				if ownerPath := a.openedMainWorktreePath(); ownerPath != "" {
					owner := tr("Main worktree") + " · " + repoName(ownerPath)
					ui.Badge(c, owner).SingleLine().MaxWidth(240).MinWidth(0).
						Label(tr("Main worktree") + ": " + ownerPath).Tooltip(owner + "\n" + ownerPath)
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
			branchView := ui.Text(c, branch).Bold().Tooltip(branchTooltip)
			if compactOperation {
				if branchTooltip == "" {
					branchView.Tooltip(branch)
				}
				branchView.SingleLine().MaxWidth(120).MinWidth(0)
			}
			if !compactOperation {
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
		}

		ui.Row(c).Grow(1).Justify(ui.End).Gap(6).Children(func() {
			if !a.editorOnlyWorkspace() && a.snapshot.Operation != "" {
				conflicts := conflictedFileCount(a.snapshot.Files)
				continueTooltip := tr("Continue the interrupted operation")
				if conflicts > 0 {
					continueTooltip = trf("Resolve conflicts before continuing (%d remaining)", conflicts)
				}
				if a.operationCan(repository.OperationActionContinue) {
					if a.location.IsRemote() {
						continueTooltip = tr("Continue checks use fully trusted local commands and are disabled for SSH workspaces")
					}
					iconTextButtonState(c, "Continue", continueTooltip, continueIcon, false,
						a.actionBusy || conflicts > 0 || a.location.IsRemote(), func() {
							if a.actionBusy || hasConflictedFiles(a.snapshot.Files) {
								return
							}
							a.confirmContinueOperation()
						})
				}
				if a.operationCan(repository.OperationActionSkip) {
					iconTextButtonState(c, "Skip…", "Skip the stopped step and continue the remaining operation", skipIcon, false,
						a.actionBusy, a.confirmSkipOperation)
				}
				if a.operationCan(repository.OperationActionAbort) {
					iconTextButton(c, "Abort…", "Abort the interrupted operation", abortIcon, false,
						func() {
							if a.actionBusy {
								return
							}
							a.confirmAbortOperation()
						})
				}
			}
			// Labeled actions lead the toolbar; optional icon-only actions follow.
			editorOnly := a.editorOnlyWorkspace()
			repositoryActions := !editorOnly && a.anyHeaderActionVisible("fetch", "pull", "push")
			editorVisible := a.headerActionVisible("editor") && a.snapshot.Root != "" &&
				(!a.location.IsRemote() || effectiveEditorCommand(a.settings.EditorCommand) == builtInEditorCommand)
			refreshVisible := !editorOnly && a.headerActionVisible("refresh")
			workspaceActions := refreshVisible || editorVisible
			applicationActions := a.anyHeaderActionVisible("theme", "settings")
			if !editorOnly && a.headerActionVisible("fetch") {
				iconButton(c, "Fetch", "Fetch from the remotes", fetchIcon,
					a.actionBusy || len(a.snapshot.Remotes) == 0, func() {
						a.beginRepositoryActionMessage(tr(a.fetchSuccessMessage()), "",
							func(ctx context.Context, session repository.Session) error {
								return a.fetchWithSetting(ctx, session)
							}, nil)
					})
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
			if repositoryActions && (workspaceActions || applicationActions) {
				toolbarSeparator(c)
			}
			if refreshVisible {
				iconButton(c, "Refresh", "Refresh (F5)", refreshIcon, a.actionBusy, a.beginRefresh)
			}
			if editorVisible {
				a.editorButton(c)
			}
			if workspaceActions && applicationActions {
				toolbarSeparator(c)
			}
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
				ui.Text(row.Context, tr(workspaceSections[section])).Grow(1).SingleLine()
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
	a.syncNavSelection()
	a.showCommitForm = false
	if isCommitSection(previous) && isCommitSection(tab) {
		a.selectedCommit = selectedCommitIndex(a.visibleCommits(), selectedHash)
	}
}

// isCommitSection reports whether a workspace section lists commits.
func isCommitSection(tab int) bool {
	return tab == sectionHistory || tab == sectionGraph
}

// sectionCount is the badge a sidebar section shows, 0 for none. History has
// none: its count is on the list's own header, where the filter changes it.
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
	islandCard(c, ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(4, 12)).Children(func() {
		a.repositoryPathView(c)
		if a.actionError != "" {
			// The error is one line here, whatever Git printed: a long
			// message left the bar to wrap, and its height pushed the
			// workspace off the top of the window. The whole message is
			// in the command log, which the button beside it opens.
			line := statusErrorLine(a.actionError)
			ui.Text(c, line).TextColor(theme.Danger).
				SingleLine().MaxWidth(520).Tooltip(shorten(a.actionError, 400))
		}
		a.keyboardPane(c, "status", ui.Row(c).Grow(1).Justify(ui.End).Gap(8).Focusable()).Children(func() {
			if a.actionBusy || a.customCommandRunning {
				ui.Spinner(c).Label(tr("Working"))
				ui.Text(c, tr("Working…")).TextColor(theme.TextMuted)
			}
			if !a.editorOnlyWorkspace() && a.statusActionVisible("commands") {
				a.commandsMenuButton(c)
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
