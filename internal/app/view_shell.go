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
	a.branchFormModal(c)
	a.moveCommitsModal(c)
	a.stashFormModal(c)
	a.remoteFormModal(c)
	a.remoteWebURLFormModal(c)
	a.tagFormModal(c)
	a.worktreeFormModal(c)
	a.agentTaskFormModal(c)
	a.worktreeMoveFormModal(c)
	a.worktreeLockFormModal(c)
	a.worktreeRepairFormModal(c)
	a.worktreePruneFormModal(c)
	a.submoduleFormModal(c)
	a.recentRenameModal(c)
	a.codeFileFormModal(c)
	a.terminalRenameModal(c)
	a.cloneModal(c)
	a.bisectFormModal(c)
	a.exportPatchFormModal(c)
	a.pullRequestFormModal(c)
	a.pullRequestChecksModal(c)
	a.sshWorkspaceModal(c)
	a.patchPreviewModal(c)
	a.customPatchModal(c)
	a.changeCourierModal(c)
	a.commandLogModal(c)
	a.commandPromptModal(c)
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
					Padding(pickerPadTop, pickerPadSide, pickerPadBottom).Gap(34).Children(func() {
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

// pickerHeroView keeps one clear way into a repository: enter its path or
// browse for it. Opening an existing repository and initializing a plain
// folder both go through this field, without a duplicate quick-action panel.
func (a *app) pickerHeroView(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).MaxWidth(pickerContentWidth).FillWidth().AlignSelf(ui.Center).Gap(11).
		Padding(22).Background(theme.Surface).Border(1, theme.Border).
		Radius(theme.Radius+10).
		Shadow(0, 12, 30, 0, ui.RGBA(0, 0, 0, 0.13)).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(16).Wrap().Children(func() {
			ui.Column(c).Grow(1).MinWidth(280).Gap(5).Children(func() {
				ui.Text(c, tr("Open a repository")).FontSize(25).Bold()
				ui.Text(c, tr("Your changes, history, branches, and remotes — all in one focused place.")).
					TextColor(theme.TextMuted).MaxWidth(560)
			})
			ui.Row(c).Justify(ui.End).AlignItems(ui.Center).Gap(10).Wrap().Children(func() {
				pickerRemoteAction(c, "Open SSH",
					"Open a Git folder through a saved SSH profile", remotesIcon, a.openSSHWorkspacePicker)
				pickerRemoteAction(c, "Clone",
					"Clone an HTTPS or SSH repository and open it (C)", downloadIcon, a.openCloneForm)
			})
		})
		bar := ui.Row(c).FillWidth().Height(48).AlignItems(ui.Center).Gap(9).
			Padding(5, 6, 5, 13).Background(theme.Background).Border(1, theme.Border).
			Radius(theme.Radius + 6)
		if bar.Hovered() {
			bar.Border(1, theme.Accent.Alpha(0.55))
		}
		bar.Children(func() {
			ui.Icon(c, folderOpenIcon).FontSize(18).TextColor(theme.TextMuted)
			path := ui.TextInputBase(c, &a.path).
				Label(tr("Repository folder")).AutoFocus().
				Placeholder(tr("/path/to/repository")).
				FontSize(15).Height(36).Grow(1)
			if path.Submitted() {
				a.openFromPicker()
			}
			browse := ui.PrimaryButton(c, "").
				Label(tr("Browse…")).
				Tooltip(tr("Choose a folder — it opens once you pick it")).
				Gap(6).Height(36).Radius(theme.Radius + 4)
			clicked := browse.Clicked()
			browse.Children(func() {
				ui.Icon(c, folderOpenIcon).FontSize(14)
				ui.Text(c, tr("Browse…"))
			})
			if clicked {
				a.browse()
			}
		})
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(16).Wrap().Children(func() {
			pickerHint(c, "↵", "Enter opens the path")
			pickerHint(c, "C", "Clone a remote repository")
			pickerHint(c, "Ctrl+,", "Settings")
			pickerHint(c, "tangit <path>", "Terminal: open a repository path directly in TanGit")
		})
	})
}

// pickerRemoteAction gives the two secondary ways into a repository a generous
// target without letting their full descriptions crowd out the launch card.
func pickerRemoteAction(c *ui.Context, label, tooltip string, icon *ui.SVG, run func()) {
	theme := c.Theme()
	button := ui.Button(c, "").Label(tr(label)).Tooltip(tr(tooltip)).
		Height(38).Padding(6, 14).Center().Gap(8).Radius(theme.Radius + 4)
	button.Children(func() {
		ui.Icon(c, icon).FontSize(17)
		ui.Text(c, tr(label)).FontSize(15)
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

// pickerHint is one quiet keyboard hint under the path field.
func pickerHint(c *ui.Context, key, label string) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Box(c).Padding(2, 6).Radius(theme.Radius).Border(1, theme.Border).
			Background(theme.Background).Children(func() {
			ui.Text(c, key).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
		})
		ui.Text(c, tr(label)).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
	})
}

// recentSectionView is a searchable history of the repositories opened
// before. Its filtered grid carries original indices through to every card.
func (a *app) recentSectionView(c *ui.Context) {
	if len(a.recentRepositories) == 0 {
		a.emptyRecentView(c)
		return
	}
	theme := c.Theme()
	services := c.Services()
	a.visibleRecent = filterRecent(a.recentRepositories, a.recentFilter)
	ui.Column(c).MaxWidth(pickerContentWidth).FillWidth().AlignSelf(ui.Center).Gap(14).
		Children(func() {
			// Give the history its own visual section instead of letting its
			// heading float in the space below the launch card.
			ui.Divider(c).FillWidth()
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Wrap().Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, tr("Recent repositories")).FontSize(18).Bold()
					ui.Badge(c, fmt.Sprintf("%d", len(a.recentRepositories)))
				})
				ui.Box(c).Grow(1).MinWidth(12)
				ui.SearchField(c, &a.recentFilter).
					Label(tr("Search recent repositories")).
					Placeholder(tr("Filter by name or path…")).
					Width(280).Height(36)
			})
			if len(a.visibleRecent) == 0 {
				ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(7).Padding(24).
					Background(theme.Surface.Alpha(0.6)).Border(1, theme.Border).
					BorderStyle(ui.BorderDashed).Radius(theme.Radius + 6).Children(func() {
					ui.Icon(c, searchIcon).FontSize(22).TextColor(theme.TextMuted)
					ui.Text(c, tr("No repositories match your search")).Bold()
					ui.Text(c, tr("Try a different name or path.")).TextColor(theme.TextMuted)
				})
				return
			}
			ui.GridView(c, &a.recentGrid, len(a.visibleRecent), 300, 128, func(i int) {
				a.recentCardView(c, a.visibleRecent[i], services)
			}).FillWidth()
		})
}

// recentCardView is one repository of the history: where it is, what it is
// called, when it was last opened, and the two quiet actions that name it or
// let it go. A plain click opens it.
func (a *app) recentCardView(c *ui.Context, i int, services ui.Services) {
	theme := c.Theme()
	repo := a.recentRepositories[i]
	selected := a.selectedRecent == i
	c.Key("recent:" + repo.Path)
	card := ui.Column(c).Fill().Gap(8).Padding(14).
		Focusable().Role(ui.RoleButton).Label(trf("Open %s", repo.Path)).FocusRing(true).
		Background(theme.Surface).Border(1, theme.Border).
		Radius(theme.Radius + 5)
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
			ui.Box(c).Size(34, 34).Radius(10).Center().
				Background(theme.Accent.Alpha(0.14)).Children(func() {
				ui.Icon(c, folderIcon).FontSize(17).TextColor(theme.Accent)
			})
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				title := ui.Text(c, repo.displayName()).FontSize(15).Bold().SingleLine()
				if repo.renamed() {
					title.Tooltip(repo.Path)
				}
				ui.Text(c, repo.Path).TextColor(theme.TextMuted).
					FontSize(theme.FontSize - 2).SingleLine().Tooltip(repo.Path)
			})
			// The configured-editor action sits in the card's quiet top-right
			// corner, apart from the click that opens the repository in TanGit.
			editor := ui.ButtonBase(c).Size(28, 28).Radius(theme.Radius).Center().
				Label(trf("Open %s in editor", repo.Path)).
				Tooltip(tr("Open this repository in your editor"))
			if editor.Hovered() {
				editor.Background(theme.SurfacePressed)
			}
			editor.Children(func() {
				ui.Icon(c, fileCodeIcon).FontSize(17).TextColor(theme.TextMuted)
			})
			if editor.Clicked() {
				a.launchEditor(repo.Path, 0)
			}
		})
		ui.Box(c).Grow(1)
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Icon(c, historyIcon).FontSize(11).TextColor(theme.TextMuted)
			if rel := relTime(repo.LastOpened); rel != "" {
				ui.Text(c, rel).TextColor(theme.TextMuted).
					FontSize(theme.FontSize - 2)
			}
			ui.Box(c).Grow(1)
			ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
				ui.Icon(c, openRepoIcon).FontSize(12).TextColor(theme.Accent)
				ui.Text(c, tr("Open")).FontSize(theme.FontSize - 2).
					Bold().TextColor(theme.Accent)
			})
			// The card's name, which is only ever the card's: a small
			// pencil, apart from the click that opens it.
			pickerCardAction(c, trf("Rename %s", repo.Path), "Rename this card", editIcon, func() {
				a.openRecentRename(repo.Path)
			})
			// The card's way out of the history: a small trash button,
			// apart from the click that opens it.
			pickerCardAction(c, trf("Remove %s from the recent list", repo.Path),
				"Remove from the recent list", trashIcon, func() {
					// The build must not rewrite the list it is reading;
					// view() applies it next pass.
					a.pendingForget = repo.Path
				})
		})
	}).OnClick(func() {
		// A plain click opens the repository: on the picker a card is a
		// choice of where to go, not a step towards a second gesture. The
		// card is chosen too, so a failed open comes back to it.
		a.selectedRecent = i
		a.openRecent(repo.Path)
	}).PopoverMenu(func(m *ui.Menu) {
		if m.Item(tr("Open")).Chosen() {
			a.openRecent(repo.Path)
		}
		if m.Item(tr("Open in editor")).Chosen() {
			a.launchEditor(repo.Path, 0)
		}
		if a.openTab != nil && m.Item(tr("Open in new tab")).Chosen() {
			a.openTab(repo.Path)
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
		}
	})
}

// pickerCardAction is the flat icon button in a card's corner: it stays quiet
// until the pointer is on it, so a card reads as a repository first.
func pickerCardAction(c *ui.Context, label, tooltip string, icon *ui.SVG, run func()) {
	theme := c.Theme()
	button := ui.ButtonBase(c).Size(24, 24).Radius(theme.Radius).Center()
	button.Label(label).Tooltip(tr(tooltip))
	if button.Hovered() {
		button.Background(theme.SurfacePressed)
	}
	button.Children(func() {
		ui.Icon(c, icon).FontSize(13).TextColor(theme.TextMuted)
	})
	if button.Clicked() {
		run()
	}
}

// emptyRecentView is the picker before it has anything to remember: one
// dashed invitation instead of an empty stretch of page.
func (a *app) emptyRecentView(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).MaxWidth(720).FillWidth().AlignSelf(ui.Center).
		AlignItems(ui.Center).Gap(12).Padding(30).
		Background(theme.Surface.Alpha(0.55)).Border(1, theme.Border).
		BorderStyle(ui.BorderDashed).Radius(theme.Radius + 8).Children(func() {
		ui.Box(c).Size(54, 54).Radius(16).Center().
			Background(theme.Accent.Alpha(0.14)).Children(func() {
			ui.Icon(c, folderOpenIcon).FontSize(26).TextColor(theme.Accent)
		})
		ui.Text(c, tr("Open a Git repository to begin")).FontSize(18).Bold()
		ui.Text(c, tr("Changes, history, branches, remotes, stash, tags, worktrees, and submodules in one window.")).
			TextColor(theme.TextMuted).TextAlign(ui.Center).MaxWidth(520)
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
			a.headerView(c)
			a.mainArea(c)
			a.statusBar(c)
		})
	})
	a.fileHistoryDrawer(c, overlayTopInset)
}

// mainArea docks the terminal below the workspace. Maximizing temporarily
// gives it the whole content area without changing the height restored later.
func (a *app) mainArea(c *ui.Context) {
	target := float32(0)
	if a.term.panelOpen {
		target = a.term.percent
		if a.term.panelMaximized {
			target = 100
		}
	}
	box := ui.Box(c)
	height := box.Animate("terminal-height", target, 200*time.Millisecond)

	switch {
	case !a.term.panelOpen || height <= 0.5:
		// Hiding must not shrink the live shell along with the animation. Once
		// its view is gone MyGo retains the last grid until it is shown again.
		a.workspaceView(c)
	case a.term.panelMaximized && box.Settled("terminal-height"):
		a.terminalPaneView(c)
	case box.Settled("terminal-height"):
		a.term.shown = 100 - a.term.percent
		paneSplitVerticalPercent(c, &a.term.shown, func() {
			a.workspaceView(c)
		}, func() {
			a.terminalPaneView(c)
		}).Grow(1)
		if a.term.shown != 100-a.term.percent {
			a.term.percent = 100 - a.term.shown
			box.Jump("terminal-height", a.term.percent)
		}
	default:
		ui.Column(c).Grow(1).Children(func() {
			ui.Column(c).Grow(1).Children(func() {
				a.workspaceView(c)
			})
			ui.Divider(c)
			// Animate empty dock space on the way in. Mounting terminal.View at
			// every fractional height would make fish/zsh print a new prompt for
			// each PTY resize, including their one-row intermediate layouts.
			ui.Column(c).FillWidth().HeightPercent(height)
		})
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
				if a.showCommitForm && a.workspaceTab == sectionChanges {
					a.commitForm(c)
				} else {
					a.contentView(c)
				}
			})
		})
}

func (a *app) headerView(c *ui.Context) {
	theme := c.Theme()
	// A service callback is safe to keep for this window's lifetime. This one
	// deliberately targets the user's desktop, never a remote SSH browser.
	if a.openExternalURL == nil {
		a.openExternalURL = c.Services().OpenURL
	}
	a.keyboardPane(c, "toolbar", islandCard(c, ui.Row(c).AlignItems(ui.Center).Gap(10).Padding(6, 14)).Focusable()).Children(func() {
		base := filepath.Base(a.snapshot.Root)
		if base == "." || base == string(filepath.Separator) || base == "" {
			base = a.snapshot.Root
		}
		ui.Text(c, base).FontSize(16).Bold().Tooltip(a.snapshot.Root)

		branch := a.snapshot.Branch
		if branch == "" && a.snapshot.Head != "" {
			branch = trf("detached %s", a.snapshot.Head)
		}
		ui.Text(c, tr("●")).TextColor(theme.Success)
		ui.Text(c, branch).Bold()
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

		ui.Row(c).Grow(1).Justify(ui.End).Gap(6).Children(func() {
			if a.snapshot.Operation != "" {
				conflicts := conflictedFileCount(a.snapshot.Files)
				continueTooltip := tr("Continue the interrupted operation")
				if conflicts > 0 {
					continueTooltip = trf("Resolve conflicts before continuing (%d remaining)", conflicts)
				}
				iconTextButtonState(c, "Continue", continueTooltip, continueIcon, false,
					a.actionBusy || conflicts > 0, func() {
						if a.actionBusy || hasConflictedFiles(a.snapshot.Files) {
							return
						}
						a.confirmContinueOperation()
					})
				iconTextButton(c, "Abort…", "Abort the interrupted operation", abortIcon, false,
					func() {
						if a.actionBusy {
							return
						}
						a.confirmAbortOperation()
					})
			}
			// Labeled actions lead the toolbar; icon-only actions follow them.
			iconButton(c, "Fetch", "Fetch from the remotes", fetchIcon,
				a.actionBusy || len(a.snapshot.Remotes) == 0, func() {
					a.beginRepositoryActionMessage(tr(a.fetchSuccessMessage()), "",
						func(ctx context.Context, session repository.Session) error {
							return a.fetchWithSetting(ctx, session)
						}, nil)
				})
			iconButton(c, "Pull", "Pull from the upstream branch", pullIcon,
				a.actionBusy, func() {
					a.confirmPull()
				})
			primaryIconButton(c, "Push", "Push to the remote", pushIcon,
				a.actionBusy, func() {
					a.confirmPush()
				})
			iconButton(c, "Refresh", "Refresh (F5)", refreshIcon, a.actionBusy, a.beginRefresh)
			a.editorButton(c)
			a.themeButton(c)
			iconButton(c, "Settings", "Settings (Ctrl+,)", settingsIcon, false, a.openSettings)
		})
	})
}

// sectionVisible reports whether the sidebar shows a section. A stored id
// that names no section is ignored; normalizeVisibleSections keeps at least
// one, so the workspace never wakes up without a pane.
func (a *app) sectionVisible(section int) bool {
	if section < 0 || section >= len(workspaceSectionIDs) {
		return false
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
const pathCopiedFor = 2 * time.Second

// repositoryPathView shows the path of the open repository at the bar's left,
// with a copy icon after it. A click on either the icon or the path copies
// the path and puts "Copied" in its place for two seconds, then the path
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
		// and the one after two seconds puts the path back.
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
			a.commandsMenuButton(c)
			a.pullRequestMenuButton(c)
			a.terminalButton(c)
			iconButton(c, "Keyboard shortcuts", "Keyboard shortcuts (F1)", keyboardIcon,
				false, a.openKeybindings)
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
