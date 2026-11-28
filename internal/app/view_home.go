package app

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

const (
	pickerHeaderHeight   float32 = 76
	pickerFooterHeight   float32 = 32
	pickerContentWidth   float32 = 1200
	pickerSplitMinWidth  float32 = 780
	recentSearchHeight   float32 = 42
	recentHeaderHeight   float32 = 39
	recentFooterHeight   float32 = 36
	recentRowHeight      float32 = 74
	recentSplitRowHeight float32 = 86
	recentRowGap         float32 = 8
	recentListPadding    float32 = 10
	recentScrollbarGap   float32 = 8
)

func (a *app) welcomeView(c *ui.Context, topInset float32) {
	switch a.state {
	case stateCloning:
		a.cloneProgressView(c)
		return
	case stateLoading:
		a.loadingView(c)
		return
	}

	width, height := c.Size()
	editorMode := a.pickerOpenMode == int(openAllowPlainEditor)
	all := filterRecentMode(a.recentRepositories, "", &editorMode)
	hasHistory := len(all) > 0
	split := hasHistory && !a.pickerStacked && width >= pickerSplitMinWidth
	a.visibleRecent = filterRecentMode(a.recentRepositories, a.recentFilter, &editorMode)
	a.sortVisibleRecent()
	if !slices.Contains(a.visibleRecent, a.selectedRecent) {
		a.selectedRecent = -1
		if len(a.visibleRecent) > 0 {
			a.selectedRecent = a.visibleRecent[0]
		}
	}

	if len(a.visibleRecent) > 0 && (a.shellOverlayOpen == nil || !a.shellOverlayOpen()) {
		c.OnWindowShortcut(0, ui.KeyUp, func() { a.moveRecentSelection(-1) })
		c.OnWindowShortcut(0, ui.KeyDown, func() { a.moveRecentSelection(1) })
	}

	viewport := max(height-topInset-pickerHeaderHeight-pickerFooterHeight-2, 0)
	theme := c.Theme()
	ui.Box(c).Fill().Background(theme.Background).Children(func() {
		a.homeBackgroundView(c)
		ui.Column(c).Fill().Children(func() {
			a.pickerHeaderView(c, hasHistory, width >= pickerSplitMinWidth, split)
			ui.Divider(c)
			ui.Scroll(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
				switch {
				case !hasHistory:
					ui.Column(c).FillWidth().MinHeight(viewport).Padding(24, 20).
						Center().Children(func() {
						a.pickerLauncher(c, false, true)
					})
				case split:
					ui.Row(c).FillWidth().MaxWidth(pickerContentWidth).AlignSelf(ui.Center).
						MinHeight(viewport).AlignItems(ui.Stretch).Padding(48, 20, 28).Gap(40).Children(func() {
						a.pickerLauncher(c, true, false)
						a.recentSectionView(c, len(all), true)
					})
				default:
					ui.Column(c).FillWidth().MaxWidth(pickerContentWidth).AlignSelf(ui.Center).
						MinHeight(viewport).Padding(26, 30, 24).Gap(28).Children(func() {
						a.pickerLauncher(c, false, false)
						a.recentSectionView(c, len(all), false)
					})
				}
			})
			ui.Divider(c)
			a.pickerFooterView(c)
		})
	})
}

func (a *app) moveRecentSelection(delta int) {
	current := slices.Index(a.visibleRecent, a.selectedRecent)
	next := min(max(current+delta, 0), len(a.visibleRecent)-1)
	a.selectedRecent = a.visibleRecent[next]
	a.revealRecent = true
}

func (a *app) pickerHeaderView(c *ui.Context, hasHistory, wide, split bool) {
	theme := c.Theme()
	ui.Row(c).FillWidth().Height(pickerHeaderHeight).Shrink(0).Padding(0, 20).
		AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Box(c).Size(40, 40).Radius(11).Center().Shrink(0).
			Border(1, theme.Accent.Alpha(0.24)).Background(theme.Accent.Alpha(0.16)).Children(func() {
			ui.Icon(c, appIcon).FontSize(21).TextColor(theme.Accent)
		})
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, appName).FontSize(18).Bold().SingleLine()
			ui.Text(c, tr("A native Git client, built on Lazygit")).
				FontSize(theme.FontSize - 2).TextColor(theme.TextMuted).SingleLine()
		})
		ui.Row(c).AlignItems(ui.Center).Gap(2).Padding(2).
			Background(theme.Background).Border(1, theme.Border).Radius(7).Children(func() {
			a.pickerLayoutButton(c, "Side-by-side layout", splitIcon, false, hasHistory && wide, split)
			a.pickerLayoutButton(c, "Stacked layout", splitBelowIcon, true, hasHistory, !split && hasHistory)
		})
		themeButton := ui.ButtonBase(c).Size(32, 32).Radius(6).Center().TextColor(theme.TextMuted)
		if themeButton.Hovered() {
			themeButton.Background(theme.SurfaceHover)
		}
		a.themeMenuButton(c, themeButton)
		flatIconButton(c, "Keyboard shortcuts", "Keyboard shortcuts (F1)", keyboardIcon, 16, 8, false, a.openKeybindings)
		flatIconButton(c, "Settings", "Settings (Ctrl+,)", settingsIcon, 16, 8, false, a.openSettings)
	})
}

func (a *app) pickerLayoutButton(c *ui.Context, label string, icon *ui.SVG, stacked, enabled, selected bool) {
	theme := c.Theme()
	button := ui.ButtonBase(c.Key(label)).Label(tr(label)).Tooltip(tr(label)).
		Disabled(!enabled).Selected(selected).Size(24, 22).Radius(5).Center()
	switch {
	case selected:
		button.Background(theme.Accent.Alpha(0.16)).TextColor(theme.Accent)
	case enabled && button.Hovered():
		button.Background(theme.SurfaceHover)
	}
	button.Children(func() { ui.Icon(c, icon).FontSize(14) })
	button.OnClick(func() { a.pickerStacked = stacked })
}

func (a *app) pickerFooterView(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).FillWidth().Height(pickerFooterHeight).Shrink(0).Padding(0, 20).
		AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Box(c).Grow(1).MinWidth(0)
		ui.ButtonBase(c).Label(tr("Keyboard shortcuts")).Height(26).Padding(0, 6).
			AlignItems(ui.Center).Gap(7).TextColor(theme.TextMuted).Children(func() {
			ui.Icon(c, keyboardIcon).FontSize(12)
			pickerKeyHint(c, "F1")
			ui.Text(c, tr("Keyboard shortcuts")).FontSize(11).SingleLine()
		}).OnClick(a.openKeybindings)
		ui.Row(c).Grow(1).MinWidth(0).Justify(ui.End).Children(func() {
			ui.Text(c, "lazygitGo <path>").FontSize(11).SingleLine().TextColor(theme.TextMuted).
				Tooltip(tr("Open a repository or folder from the terminal:") +
					"\n  lazygitGo .\n  lazygitGo ~/work/project\n  lazygitGo ssh://user@host/path\n" +
					tr("If the app is already running, the path opens in a new tab."))
		})
	})
}

func pickerKeyHint(c *ui.Context, key string) {
	theme := c.Theme()
	ui.Box(c).Padding(1, 4).Center().Radius(4).Border(1, theme.Border).Children(func() {
		ui.Text(c, key).FontSize(11).TextColor(theme.TextMuted)
	})
}

func (a *app) pickerLauncher(c *ui.Context, split, empty bool) {
	theme := c.Theme()
	editorMode := a.pickerOpenMode == int(openAllowPlainEditor)
	launcher := ui.Column(c.Key("workspace-launcher")).Gap(22).FillWidth()
	align := ui.Center
	titleSize := float32(30)
	switch {
	case split:
		launcher.Width(310).Shrink(0).AlignSelf(ui.Start).Padding(6, 0, 0).Gap(25)
		align = ui.Start
	case empty:
		launcher.MaxWidth(650)
		titleSize = 36
	}
	launcher.Children(func() {
		ui.Column(c).AlignItems(align).Gap(8).Children(func() {
			ui.Text(c, strings.ToUpper(tr("Workspace"))).FontSize(10).LetterSpacing(1.5).
				Bold().TextColor(theme.TextMuted)
			ui.Text(c, tr("Open a workspace.")).FontSize(titleSize).Bold()
			ui.Text(c, tr("Pick up where you left off, or start something new.")).
				FontSize(14).TextAlign(align).TextColor(theme.TextMuted)
		})
		pickerOpenModeControl(c, &a.pickerOpenMode, split).OnChange(func() {
			a.recentFilter = ""
			a.selectedRecent = -1
		})
		a.pickerPathView(c, editorMode, split)
		switch {
		case a.state == stateError:
			a.pickerErrorView(c)
		case a.offerInitPath != "":
			a.initPromptView(c)
		}
		if split {
			ui.Divider(c)
		}
		if !editorMode {
			a.pickerActionsView(c, split)
		}
	})
}

func pickerOpenModeControl(c *ui.Context, selected *int, split bool) ui.Element {
	theme := c.Theme()
	labels := []string{"Git repository", "Editor"}
	icons := []*ui.SVG{branchIcon, fileCodeIcon}
	c.Key("workspace-mode")
	parts := ui.SegmentedBase(c, selected, len(labels))
	track := parts.Track.Width(290).MaxWidthPercent(100).AlignSelf(ui.Center).
		Height(48).AlignItems(ui.Stretch).Padding(4).Gap(4).Radius(9).Background(theme.Background).
		Border(1, theme.Border).Label(tr("Open mode"))
	if split {
		track.FillWidth()
	}
	track.Children(func() {
		for i, label := range labels {
			chosen := i == *selected
			segment := parts.Segment(i).Grow(1).Basis(0).MinWidth(0).Radius(6).Center().Gap(7)
			switch {
			case chosen:
				segment.Background(theme.Accent.Alpha(0.16)).TextColor(theme.Accent)
			case segment.Hovered():
				segment.Background(theme.SurfaceHover)
			default:
				segment.TextColor(theme.TextMuted)
			}
			segment.Children(func() {
				ui.Icon(c, icons[i]).FontSize(14)
				text := ui.Text(c, tr(label)).FontSize(12).SingleLine()
				if chosen {
					text.Bold()
				}
			})
		}
	})
	return track
}

func (a *app) pickerPathView(c *ui.Context, editorMode, split bool) {
	theme := c.Theme()
	label, placeholder := "Repository path", "/path/to/repository"
	if editorMode {
		label, placeholder = "Folder path", "/path/to/folder"
	}
	width, _ := c.Size()
	stack := split || width < 480
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Text(c, tr(label)).AlignSelf(ui.Start).FontSize(12).Bold().
			OnClick(func() { a.pickerPathFocus.Focus(c) })
		fields := ui.Row
		if stack {
			fields = ui.Column
		}
		fields(c).FillWidth().Gap(10).Children(func() {
			bar := ui.Row(c.Key("workspace-path-field")).Height(44).Padding(0, 13).
				AlignItems(ui.Center).Gap(10).Background(theme.Surface).
				Border(1, theme.Border).Radius(8)
			if stack {
				bar.FillWidth()
			} else {
				bar.Grow(1).MinWidth(0)
			}
			bar.Children(func() {
				ui.Icon(c, folderIcon).FontSize(16).TextColor(theme.TextMuted)
				ui.TextInputBase(c.Key("workspace-path"), &a.path).
					Label(tr(label)).Placeholder(tr(placeholder)).Bind(&a.pickerPathFocus).AutoFocus().
					Grow(1).MinWidth(0).Height(38).OnSubmit(a.openFromPicker)
			})
			browse := ui.PrimaryButton(c.Key("workspace-browse"), "").
				Label(tr("Browse files…")).Tooltip(tr("Choose a folder — it opens once you pick it")).
				Height(44).Padding(0, 18).Radius(8).Center().Gap(9)
			if stack {
				browse.FillWidth()
			} else {
				browse.MinWidth(162)
			}
			browse.Children(func() {
				ui.Icon(c, folderIcon).FontSize(16)
				ui.Text(c, tr("Browse files…")).Bold().SingleLine()
			}).OnClick(a.browse)
		})
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			pickerKeyHint(c, "↵")
			ui.Text(c, tr("Enter opens the path")).FontSize(11).TextColor(theme.TextMuted)
		})
	})
}

func (a *app) pickerActionsView(c *ui.Context, split bool) {
	layout := ui.Row
	if split {
		layout = ui.Column
	}
	actions := layout(c).Gap(8)
	if !split {
		actions.AlignSelf(ui.Center).Justify(ui.Center).Wrap()
	}
	actions.Children(func() {
		pickerAction(c, "Clone repository", "From a remote Git URL", downloadIcon, split, a.openCloneForm)
		pickerAction(c, "Open SSH", "Connect to a remote workspace", remotesIcon, split, a.openSSHWorkspacePicker)
		if a.openOverviewTab != nil {
			pickerAction(c, "Overview", "View your repositories together", workspaceOverviewIcon, split, a.openOverviewPicker)
		}
	})
}

func pickerAction(c *ui.Context, label, description string, icon *ui.SVG, split bool, run func()) {
	theme := c.Theme()
	button := ui.ButtonBase(c.Key(label)).Label(tr(label)).Tooltip(tr(description)).
		AlignItems(ui.Center).Padding(0, 12).Gap(12).Radius(8)
	if split {
		button.FillWidth().Height(50)
	} else {
		button.Height(38).Border(1, theme.Border)
	}
	if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	button.Children(func() {
		ui.Icon(c, icon).FontSize(16).TextColor(theme.Accent)
		if split {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, tr(label)).FontSize(14).SingleLine()
				ui.Text(c, tr(description)).FontSize(11).TextColor(theme.TextMuted).SingleLine()
			})
			ui.Icon(c, chevronRightIcon).FontSize(13).TextColor(theme.TextMuted)
		} else {
			ui.Text(c, tr(label)).FontSize(14).SingleLine()
		}
	}).OnClick(run)
}

func (a *app) recentSectionView(c *ui.Context, total int, split bool) {
	theme := c.Theme()
	services := c.Services()
	editorMode := a.pickerOpenMode == int(openAllowPlainEditor)
	title, searchLabel, noMatches := "Recent repositories", "Search recent repositories", "No repositories match your search"
	columnLabel, singular, plural := "Repository / branch", "%d repository", "%d repositories"
	actionWidth := float32(84)
	if editorMode {
		title, searchLabel, noMatches = "Recent folders", "Search recent folders", "No folders match your search"
		columnLabel, singular, plural = "Folder", "%d folder", "%d folders"
		actionWidth = 56
	}
	width, _ := c.Size()
	showTime := width >= 490
	rowHeight := recentRowHeight
	gap := float32(12)
	section := ui.Column(c.Key("workspace-recent")).Grow(1).MinWidth(0)
	if split {
		rowHeight = recentSplitRowHeight
		gap = 20
	} else {
		section.FillWidth()
	}
	section.Gap(gap).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Grow(1).Children(func() {
				ui.Text(c, tr(title)).FontSize(20).Bold()
				ui.Text(c, strconv.Itoa(total)).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
			})
			ui.SearchField(c.Key("recent-filter"), &a.recentFilter).
				Label(tr(searchLabel)).Placeholder(tr("Filter by name or path…")).
				Width(234).MaxWidthPercent(100).Height(recentSearchHeight).FontSize(theme.FontSize).Radius(8).
				OnSubmit(func() {
					if a.selectedRecent >= 0 {
						a.openRecent(a.recentRepositories[a.selectedRecent].Path)
					}
				})
		})

		ui.Column(c).FillWidth().Grow(1).Background(theme.Surface).Border(1, theme.Border).Radius(12).Clip().Children(func() {
			ui.Row(c).FillWidth().Height(recentHeaderHeight).Padding(0, 16+recentListPadding+recentScrollbarGap, 0, 16+recentListPadding).AlignItems(ui.Center).
				Background(theme.SurfaceHover).Gap(8).Children(func() {
				a.recentSortHeader(c, columnLabel, true, 0)
				if showTime {
					a.recentSortHeader(c, "Last opened", false, recentTimeWidth)
				}
				ui.Box(c).Width(actionWidth)
			})
			ui.Divider(c)
			minRowsHeight := 2*rowHeight + recentRowGap + 2*recentListPadding
			switch {
			case len(a.visibleRecent) == 0:
				ui.Column(c).FillWidth().Grow(1).MinHeight(minRowsHeight).
					Background(theme.Background).Center().Gap(7).Padding(26).Children(func() {
					ui.Icon(c, searchIcon).FontSize(20).TextColor(theme.TextMuted)
					ui.Text(c, tr(noMatches)).Bold()
					ui.Text(c, tr("Try a different name or path.")).TextColor(theme.TextMuted)
				})
			default:
				ui.Scroll(c).FillWidth().Grow(1).Basis(0).MinHeight(minRowsHeight).
					Background(theme.Background).KeepScrollbars(true).Label(tr(title)).Children(func() {
					ui.Column(c).FillWidth().Padding(recentListPadding, recentListPadding+recentScrollbarGap, recentListPadding, recentListPadding).Gap(recentRowGap).Children(func() {
						for _, i := range a.visibleRecent {
							a.recentRowView(c, i, rowHeight, showTime, services)
						}
					})
				})
			}
			ui.Divider(c)
			ui.Row(c).FillWidth().Height(recentFooterHeight).Padding(0, 16).AlignItems(ui.Center).Gap(8).Children(func() {
				count := len(a.visibleRecent)
				countLabel := plural
				if count == 1 {
					countLabel = singular
				}
				ui.Text(c, trf(countLabel, count)).Grow(1).MinWidth(0).
					SingleLine().FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
			})
		})
	})
}

func (a *app) recentRowView(c *ui.Context, i int, height float32, showTime bool, services ui.Services) {
	theme := c.Theme()
	repo := a.recentRepositories[i]
	selected := a.selectedRecent == i
	c.Key(fmt.Sprintf("recent:%t:%s", repo.EditorOnly, repo.Path))
	row := ui.Row(c).FillWidth().Height(height).AlignItems(ui.Stretch).
		Background(theme.Surface).Border(1, theme.Border).Radius(8).Clip().
		Focusable().Role(ui.RoleButton).Label(trf("Open %s", repo.Path)).FocusRing(true).AccessSelected(selected)
	switch {
	case selected:
		row.Background(theme.Accent.Alpha(0.12).Over(theme.Surface))
		if a.revealRecent {
			row.ScrollIntoView()
			if !c.FocusTakesText() {
				row.Focus()
			}
			a.revealRecent = false
		}
	case row.Hovered():
		row.Background(theme.SurfaceHover)
	}
	row.OnShortcut(0, ui.KeyEnter, func() { a.openRecent(repo.Path) }).
		OnShortcut(0, ui.KeySpace, func() { a.openRecent(repo.Path) })
	row.Children(func() {
		marker := ui.Box(c).Width(3)
		if selected {
			marker.Background(theme.Accent)
		}
		ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Padding(0, 16, 0, 13).Gap(8).Children(func() {
			ui.Box(c).Size(35, 35).Shrink(0).Radius(9).Center().
				Background(theme.Accent.Alpha(0.14)).Children(func() {
				ui.Icon(c, folderIcon).FontSize(16).TextColor(theme.Accent)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Padding(0, 0, 0, 4).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(6).MinWidth(0).Children(func() {
					title := ui.Text(c, repo.displayName()).FontSize(14).Bold().SingleLine()
					if repo.renamed() {
						title.Tooltip(repo.Path)
					}
					if repo.Pinned {
						ui.Icon(c, pinIcon).FontSize(12).TextColor(theme.Accent).Shrink(0).
							Label(tr("Pinned")).Tooltip(tr("Pinned to the top"))
					}
				})
				ui.Text(c, repo.Path).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).SingleLine().Tooltip(repo.Path)
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
				if !repo.EditorOnly && identity != "" {
					ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
						a.branchBadge(c, identity, repo.Worktree)
						if ownerPath := strings.TrimSpace(repo.RepositoryPath); repo.Worktree && ownerPath != "" {
							ui.Text(c, tr("Main worktree")+" · "+repoName(ownerPath)).
								MinWidth(0).SingleLine().FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).
								Tooltip(tr("Main worktree") + ": " + ownerPath)
						}
					})
				}
			})
			if showTime {
				ui.Text(c, relTime(repo.LastOpened)).Width(recentTimeWidth).Shrink(0).
					SingleLine().FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
			}
			a.recentRowActions(c, repo, services)
		})
	}).OnClick(func() {
		a.selectedRecent = i
		a.openRecent(repo.Path)
	}).PopoverMenu(func(m *ui.Menu) {
		a.recentRowMenu(m, repo, services)
	})
}

func (a *app) recentRowActions(c *ui.Context, repo recentRepository, services ui.Services) {
	theme := c.Theme()
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(func() {
		if !repo.EditorOnly {
			// Existing checkouts are one click away from the row, without
			// opening the source repository first or replacing the whole row.
			worktrees := ui.ButtonBase(c).Size(28, 30).Radius(theme.Radius).Center().
				Label(tr("Open worktree") + " " + repo.Path).
				Tooltip(tr("Choose a main or linked worktree to open"))
			if worktrees.Hovered() {
				worktrees.Background(theme.SurfacePressed)
			}
			worktrees.Children(func() {
				ui.Icon(c, worktreesIcon).FontSize(16).TextColor(theme.TextMuted)
			})
			// Only the card whose menu is active owns the shared open state.
			// Binding every row to it makes the next row steal and close the
			// menu during the same frame, when several rows are visible.
			menuOptions := ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd}
			if a.homeWorktrees.source == repo.Path {
				menuOptions.Open = &a.homeWorktrees.menuOpen
			}
			worktrees.PopoverMenuButton(func(m *ui.Menu) {
				a.homeWorktreeMenu(m, repo.Path)
			}, menuOptions)
		}
		editorTooltip := "Open this repository in the editor"
		if repo.EditorOnly {
			editorTooltip = "Open this folder in the editor"
		}
		editor := ui.ButtonBase(c).Size(28, 30).Radius(theme.Radius).Center().
			Label(trf("Open %s in editor", repo.Path)).
			Tooltip(tr(editorTooltip))
		if editor.Hovered() {
			editor.Background(theme.SurfacePressed)
		}
		editor.Children(func() {
			ui.Icon(c, fileCodeIcon).FontSize(16).TextColor(theme.TextMuted)
		})
		if editor.Clicked() {
			a.openRecentInEditor(repo.Path)
		}
		more := ui.ButtonBase(c).Size(28, 30).Radius(theme.Radius).Center().
			Label(tr("More actions") + " " + repo.Path).Tooltip(tr("More actions"))
		if more.Hovered() {
			more.Background(theme.SurfacePressed)
		}
		more.Children(func() {
			ui.Icon(c, moreIcon).FontSize(16).TextColor(theme.TextMuted)
		})
		more.PopoverMenuButton(func(m *ui.Menu) {
			a.recentRowMenu(m, repo, services)
		}, ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd})
	})
}

// recentRowMenu is shared by the More button and the row’s context menu.
func (a *app) recentRowMenu(m *ui.Menu, repo recentRepository, services ui.Services) {
	if m.Item(tr("Open")).Chosen() {
		a.openRecent(repo.Path)
	}
	if m.Item(tr("Open in editor")).Chosen() {
		a.openRecentInEditor(repo.Path)
	}
	if !repo.EditorOnly && a.openTab != nil && m.Item(tr("Open in new tab")).Chosen() {
		a.openTab(repo.Path, a.pickerOpenPolicy())
	}
	if !repo.EditorOnly && m.Item(tr("Open worktree")).Chosen() {
		a.openHomeWorktrees(repo.Path)
	}
	pinLabel := "Pin to top"
	if repo.Pinned {
		pinLabel = "Unpin from top"
	}
	if m.Item(tr(pinLabel)).Chosen() {
		a.setRecentPinned(repo.Path, repo.EditorOnly, !repo.Pinned)
	}
	if m.Item(tr("Copy path")).Chosen() {
		a.notifyCopied(services, "repository path", repo.Path)
	}
	if m.Item(tr("Rename")).Chosen() {
		a.openRecentRename(repo.Path)
	}
	if !repo.EditorOnly && m.Item(tr("Set remote web address")).Chosen() {
		a.openRecentRemoteWebURL(repo.Path)
	}
	if !repo.EditorOnly && m.Item(tr("Config PR/MR host")).Chosen() {
		a.openHostingAccess()
	}
	if m.Item(tr("Remove from list")).Chosen() {
		// The menu must not rewrite the list this build is reading;
		// view() applies it next pass.
		a.pendingForget = repo.Path
		a.pendingForgetEditorOnly = repo.EditorOnly
	}
}

// sortVisibleRecent orders the filtered list by the clicked column. The default,
// last opened with the newest first, is the history's own order and is left
// alone; the sort is stable, so ties keep that order.
func (a *app) sortVisibleRecent() {
	repos := a.recentRepositories
	// Pinned cards stay on top in every order; the sort only orders each group.
	defer slices.SortStableFunc(a.visibleRecent, func(left, right int) int {
		switch {
		case repos[left].Pinned == repos[right].Pinned:
			return 0
		case repos[left].Pinned:
			return -1
		}
		return 1
	})
	if !a.recentSortByName && !a.recentSortReversed {
		return
	}
	slices.SortStableFunc(a.visibleRecent, func(left, right int) int {
		var order int
		if a.recentSortByName {
			order = strings.Compare(strings.ToLower(repos[left].displayName()), strings.ToLower(repos[right].displayName()))
		} else {
			// Newest first.
			order = cmp.Compare(repos[right].LastOpened, repos[left].LastOpened)
		}
		if a.recentSortReversed {
			order = -order
		}
		return order
	})
}

// recentSortHeader is one column title that sorts by itself when clicked: a
// first click picks the column, the next ones flip its direction. The active
// column shows its direction with a sort icon; the other shows one on hover.
// Each column starts in the direction people expect: names from A to Z, and
// times with the newest first.
func (a *app) recentSortHeader(c *ui.Context, label string, byName bool, width float32) {
	theme := c.Theme()
	active := a.recentSortByName == byName
	name := strings.ToLower(tr(label))
	button := ui.ButtonBase(c).Padding(2, 0).AlignItems(ui.Center).Justify(ui.Start).Gap(5)
	button.Label(trf("Sort by %s", name)).Checked(active)
	tooltip := trf("Sort by %s", name)
	switch {
	case active && !a.recentSortReversed:
		tooltip = trf("Sorted by %s. Click to reverse.", name)
	case active:
		tooltip = trf("Sorted by %s, reversed. Click to restore.", name)
	}
	button.Tooltip(tooltip)
	if width > 0 {
		button.Width(width)
	} else {
		button.Grow(1).MinWidth(0)
	}
	hovered := button.Hovered()
	if button.Clicked() {
		if active {
			a.recentSortReversed = !a.recentSortReversed
		} else {
			a.recentSortByName, a.recentSortReversed = byName, false
		}
	}
	button.Children(func() {
		color, alpha := theme.TextMuted, float32(0.55)
		if active {
			color, alpha = theme.Text, 1
		}
		ui.Text(c, tr(label)).SingleLine().FontSize(theme.FontSize - 1).TextColor(color).Shrink(1).MinWidth(0)
		if active || hovered {
			// Names run A to Z and times newest first, so reversing flips each.
			glyph := sortDescendingIcon
			if byName != (active && a.recentSortReversed) {
				glyph = sortAscendingIcon
			}
			ui.Icon(c, glyph).FontSize(theme.FontSize - 1).TextColor(color.Alpha(alpha)).Shrink(0)
		}
	})
}

// openRecentInEditor opens the repository in a tab and lands on the Code page,
// where its files and the editor are, instead of on the page the settings pick.
func (a *app) openRecentInEditor(path string) {
	if !a.sectionVisible(sectionCode) {
		a.setSectionVisible(sectionCode, true)
		a.saveSettings()
	}
	a.startSectionOnce = sectionCode + 1
	a.openRecent(path)
}

// recentTimeWidth is the last-opened column's width, in the header and in every
// row, wide enough for its title and the sort icon beside it.
const recentTimeWidth = float32(112)
