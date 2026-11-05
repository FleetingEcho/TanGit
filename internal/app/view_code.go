package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
)

func (a *app) codeView(c *ui.Context) {
	a.applyPendingCodeActions()
	a.ensureCodeFiles()
	a.syncGoLanguageServer()
	if a.code.filter != a.code.appliedFilter || a.code.treeDirty {
		a.syncCodeTree()
	}
	// File-index loads and folder reveals sync explicitly. Steady-state frames
	// retain the sorted rows; MyGo virtualizes their painting and hit testing.
	a.collapsibleContentPaneSplit(c, &a.code.paneWidth, &a.workspacePaneCollapsed[sectionCode],
		"Expand file explorer", func() {
			a.codeSidebar(c)
		}, func() {
			a.codeEditorPane(c)
		})
}

func (a *app) codeSidebar(c *ui.Context) {
	theme := c.Theme()
	islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(10)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Icon(c, fileCodeIcon).FontSize(15).TextColor(theme.Accent)
			ui.Text(c, tr("Editor")).Bold().Grow(1)
			a.repositoryExternalEditorButton(c)
			flatIconButton(c, "New File…", "Create a file in the selected folder", newFileIcon, 14, 4, a.code.fileOperationBusy, func() {
				a.openCodeFileForm(codeFileFormNewFile, a.selectedCodeDirectory(), "")
			})
			flatIconButton(c, "New Folder…", "Create a folder in the selected folder", newFolderIcon, 14, 4, a.code.fileOperationBusy, func() {
				a.openCodeFileForm(codeFileFormNewFolder, a.selectedCodeDirectory(), "")
			})
			flatIconButton(c, "Refresh files", "Reload the repository file list", refreshIcon, 14, 4, a.code.filesLoading || a.code.fileOperationBusy, func() {
				a.code.filesLoaded = false
				a.requestCodeFiles()
			})
			paneCollapseButton(c, &a.workspacePaneCollapsed[sectionCode], "Collapse file explorer")
		})
		a.codeSearchFolderSelector(c)
		ui.Segmented(c, &a.code.sidebar, tr("Files"), tr("Search")).Label(tr("Code sidebar"))
		if a.code.sidebar == codeSidebarSearch {
			a.codeSearchSidebar(c)
			return
		}
		a.codeFileExplorer(c)
	})
}

func (a *app) codeFileExplorer(c *ui.Context) {
	theme := c.Theme()
	field := ui.SearchField(c, &a.code.filter).Bind(&a.sectionSearch[sectionCode]).
		Label(tr("Quick open")).Placeholder(tr("Filter files (Ctrl+P)"))
	if field.Changed() {
		a.syncCodeTree()
	}
	if field.Submitted() {
		a.openSelectedCodeFile()
	}
	if strings.TrimSpace(a.code.filter) == "" {
		a.codeRepositoryRootRow(c, theme)
	}
	if a.code.filesLoading {
		ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
			ui.Spinner(c).Label(tr("Loading files"))
			ui.Text(c, tr("Loading files…")).TextColor(theme.TextMuted)
		})
		return
	}
	if a.code.filesError != "" {
		ui.Column(c).Grow(1).Center().Gap(8).Padding(8).Children(func() {
			ui.Text(c, tr("The file list could not be read.")).Bold().TextAlign(ui.Center)
			ui.Text(c, a.code.filesError).TextColor(theme.Danger).TextAlign(ui.Center)
			if ui.Button(c, tr("Try again")).Clicked() {
				a.requestCodeFiles()
			}
		})
		return
	}
	if len(a.code.rows) == 0 {
		message := "This repository has no files"
		if strings.TrimSpace(a.code.filter) != "" {
			message = "No files match this filter"
		}
		listSearchEmpty(c, message)
		return
	}
	list := a.keyboardList(c, &a.code.list, len(a.code.rows)).Grow(1).Rows(func(row ui.ListRow) {
		if row.Index < 0 || row.Index >= len(a.code.rows) {
			return
		}
		item := a.code.rows[row.Index]
		if item.isDirectory() {
			directory := a.fileTreeDirectoryRow(row.Context, codeFileTreeScope, item, row.Selected(), func() {
				a.selectCodeRow(row.Index)
				// fileTreeDirectoryRow toggles folding after this callback. Defer
				// the one necessary rebuild until the next build pass.
				a.code.treeDirty = true
			}).Drag(codeExplorerDrag{path: item.path})
			directory.PopoverMenu(func(m *ui.Menu) {
				a.codeExplorerMenu(m, item, row.Context.Services())
			})
			if _, over := ui.DragOver[codeExplorerDrag](directory); over {
				directory.Background(theme.Accent.Alpha(0.16))
			}
			if dropped, ok := ui.Drop[codeExplorerDrag](directory); ok && !a.code.fileOperationBusy {
				a.code.pendingMoveSource = dropped.path
				a.code.pendingMoveTarget = item.path
			}
			return
		}
		button := ui.ButtonBase(row.Context).Grow(1).Gap(6).Padding(6, 8, 6, float32(8+16*item.depth)).
			Label(tr("Open") + " " + item.path).
			Drag(codeExplorerDrag{path: item.path}).
			PopoverMenu(func(m *ui.Menu) { a.codeExplorerMenu(m, item, row.Context.Services()) })
		button.Children(func() {
			fileTypeMark(row.Context, item.path, 13, row.Selected())
			ui.Text(row.Context, item.name).Grow(1).SingleLine()
			if status := a.codeFileStatus(item.path); status != "" {
				ui.Text(row.Context, status).FontSize(theme.FontSize - 1).Bold().TextColor(theme.Warning)
			}
		})
		if button.DoubleClicked() {
			a.selectCodeRow(row.Index)
			a.openPinnedCodeFile(item.path, 0)
		} else if button.Clicked() {
			a.selectCodeRow(row.Index)
			a.openCodeFile(item.path, 0)
		}
	})
	if a.keyboardListChanged(&a.code.list, list) {
		a.selectCodeRow(a.code.selectedRow)
	}
}

// codeRepositoryRootRow makes the opened workspace root a real creation and
// drop target. A nested workspace has no ancestor rows outside that root.
func (a *app) codeRepositoryRootRow(c *ui.Context, theme *ui.Theme) {
	folder := a.code.searchFolder
	label := tr("Repository root")
	tooltip := a.snapshot.Root
	if folder != "" {
		label = tr("Workspace root") + " " + folder
		tooltip = a.repositoryPath(folder)
	}
	root := ui.ButtonBase(c).FillWidth().Gap(7).Padding(6, 8).Label(label).Tooltip(tooltip)
	root.Children(func() {
		ui.Icon(c, folderOpenIcon).FontSize(13).TextColor(theme.Accent)
		ui.Text(c, label).Grow(1).SingleLine()
	})
	root.PopoverMenu(func(m *ui.Menu) {
		if m.Item(tr("New File…")).Disabled(a.code.fileOperationBusy).Chosen() {
			a.openCodeFileForm(codeFileFormNewFile, folder, "")
		}
		if m.Item(tr("New Folder…")).Disabled(a.code.fileOperationBusy).Chosen() {
			a.openCodeFileForm(codeFileFormNewFolder, folder, "")
		}
	})
	if _, over := ui.DragOver[codeExplorerDrag](root); over {
		root.Background(theme.Accent.Alpha(0.16))
	}
	if dropped, ok := ui.Drop[codeExplorerDrag](root); ok && !a.code.fileOperationBusy {
		a.code.pendingMoveSource = dropped.path
		a.code.pendingMoveTarget = folder
	}
}

// codeSearchFolderSelector keeps the workspace root visible above either
// sidebar tab. The field is a draft until Enter, while the folder and clear
// buttons commit their new root immediately and rerun any active search.
func (a *app) codeSearchFolderSelector(c *ui.Context) {
	ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
		folder := ui.TextInput(c, &a.code.searchFolderDraft).Label(tr("Workspace folder")).
			Placeholder(a.snapshot.Root).Grow(1)
		if folder.Submitted() && a.openCodeWorkspaceFolder(a.code.searchFolderDraft) {
			if strings.TrimSpace(a.code.searchQuery) != "" {
				a.beginCodeSearch()
			}
		}
		flatIconButton(c, "Choose workspace folder", "Open a folder as the Editor workspace root", folderOpenIcon, 14, 4,
			false, a.browseCodeSearchFolder)
		if a.code.searchFolder != "" {
			flatIconButton(c, "Open entire repository", "Clear the workspace folder scope", closeIcon, 13, 4,
				false, func() {
					if a.openCodeWorkspaceFolder("") && strings.TrimSpace(a.code.searchQuery) != "" {
						a.beginCodeSearch()
					}
				})
		}
	})
}

func (a *app) codeSearchSidebar(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(3).Children(func() {
		input := ui.SearchField(c, &a.code.searchQuery).Bind(&a.code.searchFocus).
			Label(tr("Search in files")).Placeholder(tr("Search repository content")).Grow(1)
		if input.Changed() {
			a.codeSearchQueryChanged()
		}
		if input.Submitted() {
			a.beginCodeSearch()
		}
		a.codeSearchOptionButton(c, "Aa", "Match case", &a.code.searchCaseSensitive)
		a.codeSearchOptionButton(c, "ab", "Match whole word", &a.code.searchWholeWord)
		a.codeSearchOptionButton(c, ".*", "Use regular expression", &a.code.searchRegularExpr)
		hasFilters := strings.TrimSpace(a.code.searchInclude) != "" || strings.TrimSpace(a.code.searchExclude) != ""
		flatToggleIconButton(c, "File filters", "Show or hide include and exclude globs", filterIcon, 12, 3,
			a.code.searchFiltersOpen || hasFilters, func() {
				a.code.searchFiltersOpen = !a.code.searchFiltersOpen
			})
		run := compactIconButton(ui.PrimaryButton(c, "")).Width(26)
		run.Label(tr("Run search")).Tooltip(tr("Search repository content")).
			Disabled(a.code.filesLoading || strings.TrimSpace(a.code.searchQuery) == "")
		run.Children(func() {
			ui.Icon(c, searchIcon).FontSize(12)
		})
		if run.Clicked() {
			a.beginCodeSearch()
		}
	})
	a.codeSearchBackendControls(c)
	if a.code.searchFiltersOpen {
		include := ui.TextInput(c, &a.code.searchInclude).Label(tr("Files to include")).Placeholder(tr("files to include"))
		if include.Submitted() {
			a.beginCodeSearch()
		}
		exclude := ui.TextInput(c, &a.code.searchExclude).Label(tr("Files to exclude")).Placeholder(tr("files to exclude"))
		if exclude.Submitted() {
			a.beginCodeSearch()
		}
	}
	if a.code.searchError != "" {
		ui.Text(c, a.code.searchError).TextColor(theme.Danger).FontSize(theme.FontSize - 1).SingleLine().Ellipsis("…").Tooltip(a.code.searchError)
	}
	if a.code.filesLoading || (a.code.searchRunning && !a.code.searchLoadingMore) {
		label := tr("Searching files")
		if a.code.filesLoading {
			label = tr("Loading files")
		}
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Spinner(c).Label(label)
			ui.Text(c, label+"…").TextColor(theme.TextMuted).Grow(1)
		})
		return
	}
	if a.code.searchSubmitted == "" {
		ui.Column(c).Grow(1).Center().Gap(8).Padding(12).Children(func() {
			ui.Icon(c, searchIcon).FontSize(22).TextColor(theme.TextMuted)
			ui.Text(c, tr("Search project files")).TextColor(theme.TextMuted).TextAlign(ui.Center)
			ui.Text(c, tr("Large repository? Search may be slow. Use an external editor instead.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).TextAlign(ui.Center)
			a.repositoryExternalEditorButtonWithText(c, "External")
		})
		return
	}
	if len(a.code.searchResults) == 0 {
		if a.code.searchBackend == workspaceSearchBackendRipgrep {
			ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
				ui.Spacer(c)
				a.codeSearchBackendBadge(c)
			})
		}
		listSearchEmpty(c, "No matches found")
		return
	}
	ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
		ui.Text(c, trf("%d matches", len(a.code.searchResults))).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
		if a.code.searchHasMore {
			flatIconButton(c, "Load more matches", "Load 500 more matches",
				loadMoreIcon, 14, 3, a.code.searchLoadingMore, a.loadMoreCodeSearch)
		}
		a.codeSearchBackendBadge(c)
	})
	list := a.keyboardList(c, &a.code.searchList, len(a.code.searchRows)).Grow(1).Rows(func(row ui.ListRow) {
		if row.Index < 0 || row.Index >= len(a.code.searchRows) {
			return
		}
		item := a.code.searchRows[row.Index]
		if item.isFile() {
			a.codeSearchFileRow(row, item)
			return
		}
		if item.match < 0 || item.match >= len(a.code.searchResults) {
			return
		}
		a.codeSearchMatchRow(row, a.code.searchResults[item.match])
	})
	// Selection alone does not open results while navigating. Enter opens the
	// chosen match through keyboardEnter; a pointer click opens it directly.
	if a.keyboardListChanged(&a.code.searchList, list) {
		a.selectCodeSearchRow(a.code.searchSelectedRow)
	}
}

// codeSearchHasRipgrep caches executable discovery per workspace. Avoiding a
// LookPath on every frame keeps the always-visible switch inexpensive.
func (a *app) codeSearchHasRipgrep() bool {
	if !a.code.searchRipgrepKnown {
		_, err := workspaceSearchLookPath("rg")
		a.code.searchRipgrepAvailable = err == nil
		a.code.searchRipgrepKnown = true
	}
	return a.code.searchRipgrepAvailable
}

func (a *app) codeSearchBackendControls(c *ui.Context) {
	ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
		if a.codeSearchHasRipgrep() {
			useRipgrep := !a.code.searchNativeOnly
			toggle := ui.Switch(c, &useRipgrep).
				Size(c.Theme().Space(7), c.Theme().Space(3.5)).Radius(c.Theme().Space(1.75)).
				Label(tr("Use ripgrep")).Tooltip(tr("Use ripgrep when available; turn off to use the internal Go search"))
			ui.Text(c, "ripgrep").FontSize(c.Theme().FontSize - 1).TextColor(c.Theme().TextMuted)
			if toggle.Changed() {
				a.code.searchNativeOnly = !useRipgrep
				if a.code.searchSubmitted != "" {
					a.beginCodeSearch()
				}
			}
		}
		ui.Spacer(c)
		pageLimit := trf("Search may be slow in huge repositories. Each page is limited to %d matches, and files larger than 8 MiB are skipped.", codeSearchPageSize)
		ui.Badge(c, trf("%d per page", codeSearchPageSize)).Label(pageLimit).Tooltip(pageLimit)
	})
}

func (a *app) codeSearchBackendBadge(c *ui.Context) {
	var name, label string
	switch a.code.searchBackend {
	case workspaceSearchBackendGo:
		name, label = "Go", tr("Search powered by the internal Go scanner")
	case workspaceSearchBackendRipgrep:
		name, label = "ripgrep", tr("Search powered by ripgrep")
	default:
		return
	}
	ui.Badge(c, name).Label(label).Tooltip(label)
}

func (a *app) codeSearchOptionButton(c *ui.Context, text, label string, active *bool) {
	theme := c.Theme()
	button := compactIconButton(ui.ButtonBase(c)).Size(24, 24).Label(tr(label)).Tooltip(tr(label)).Checked(*active)
	if *active {
		button.Background(theme.Accent.Alpha(0.16))
	} else if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	button.Children(func() {
		color := theme.TextMuted
		if *active {
			color = theme.Accent
		}
		ui.Text(c, text).FontSize(theme.FontSize - 1).Bold().TextColor(color)
	})
	if button.Clicked() {
		*active = !*active
		if a.code.searchSubmitted != "" {
			a.beginCodeSearch()
		}
	}
}

func (a *app) codeSearchFileRow(row ui.ListRow, item codeSearchRow) {
	theme := row.Context.Theme()
	collapsed := a.code.searchCollapsed[item.path]
	arrow, action := "▾", "Collapse file results"
	if collapsed {
		arrow, action = "▸", "Expand file results"
	}
	button := ui.ButtonBase(row.Context).Grow(1).Gap(5).Padding(5, 7).
		Label(tr(action) + " " + item.path).Tooltip(item.path)
	button.Children(func() {
		color := secondaryRowColor(theme, row)
		ui.Text(row.Context, arrow).TextColor(color).Width(12)
		fileTypeMark(row.Context, item.path, 12, row.Selected())
		ui.Text(row.Context, filepath.Base(item.path)).Bold().Grow(1).SingleLine().Tooltip(item.path)
		ui.Textf(row.Context, "%d", item.matches).TextColor(color).FontSize(theme.FontSize - 1)
	})
	if button.Clicked() {
		a.code.searchSelectedRow = row.Index
		a.toggleCodeSearchFile(item.path)
	}
}

func (a *app) codeSearchMatchRow(row ui.ListRow, match workspaceSearchMatch) {
	theme := row.Context.Theme()
	text := strings.TrimSpace(match.Text)
	label := fmt.Sprintf("%s:%d:%d: %s", match.Path, match.Line, match.Column, text)
	button := ui.ButtonBase(row.Context).Grow(1).Gap(8).Padding(4, 7, 4, 26).
		Label(tr("Open") + " " + label)
	button.Children(func() {
		ui.Textf(row.Context, "%d", match.Line).Width(30).TextAlign(ui.End).
			TextColor(secondaryRowColor(theme, row)).FontSize(theme.FontSize - 1)
		ui.RichText(row.Context, codeSearchMatchHighlightSpans(text, a.code.searchSubmittedOptions, theme)...).
			Grow(1).SingleLine().Ellipsis("…")
	})
	if button.DoubleClicked() {
		a.selectCodeSearchRow(row.Index)
		a.openCodeLocationMode(match.Path, match.Line, match.Column-1, true)
	} else if button.Clicked() {
		a.selectCodeSearchRow(row.Index)
		a.openCodeSearchMatch(match)
	}
}

func codeSearchHighlightSpans(text, query string, caseSensitive bool, theme *ui.Theme) []ui.Span {
	return codeSearchMatchHighlightSpans(text, workspaceSearchOptions{Query: query, CaseSensitive: caseSensitive}, theme)
}

func codeSearchMatchHighlightSpans(text string, options workspaceSearchOptions, theme *ui.Theme) []ui.Span {
	start, end := workspaceSearchRangeWithOptions(text, options)
	if start < 0 {
		return []ui.Span{{Text: text}}
	}
	spans := make([]ui.Span, 0, 3)
	if start > 0 {
		spans = append(spans, ui.Span{Text: text[:start]})
	}
	spans = append(spans, ui.Span{Text: text[start:end], Background: theme.Warning.Alpha(0.38)})
	if end < len(text) {
		spans = append(spans, ui.Span{Text: text[end:]})
	}
	return spans
}

func (a *app) codeFileStatus(path string) string {
	for _, file := range a.snapshot.Files {
		if file.Path == path {
			return strings.TrimSpace(file.Status)
		}
	}
	return ""
}

type codeLanguageServerStatus struct {
	name     string
	running  bool
	starting bool
	failed   bool
	detail   string
}

// codeLanguageServerStatuses includes every live workspace server, not just
// the one associated with the active extension. Profile order keeps the hover
// panel stable as documents open and close.
func (a *app) codeLanguageServerStatuses() []codeLanguageServerStatus {
	statuses := make([]codeLanguageServerStatus, 0, len(a.additionalLanguageServers)+1)
	for _, profile := range languageServerProfilesWithSettings(a.settings.LanguageServers) {
		status := codeLanguageServerStatus{name: profile.Name}
		if profile.ID == goLanguageServerID {
			status.running = a.goLanguageServer != nil
			status.starting = a.goLanguageServerStarting
			status.failed = a.goLanguageServerError != ""
			status.detail = a.goLanguageServerError
		} else if server := a.additionalLanguageServers[profile.ID]; server != nil {
			status.running = server.connection != nil
			status.starting = server.starting
			status.failed = server.error != ""
			status.detail = server.error
		} else {
			continue
		}
		if status.running || status.starting || status.failed {
			statuses = append(statuses, status)
		}
	}
	return statuses
}

// codeLanguageServersButton keeps the document path readable. The persistent
// control is just an online-style status icon; names and errors belong to its
// hover panel rather than consuming the editor toolbar on every frame.
func (a *app) codeLanguageServersButton(c *ui.Context) {
	statuses := a.codeLanguageServerStatuses()
	if len(statuses) == 0 {
		a.code.languageServersOpen = false
		return
	}
	theme := c.Theme()
	color := theme.Success
	running := false
	starting := false
	for _, status := range statuses {
		running = running || status.running
		starting = starting || status.starting
		if status.failed {
			color = theme.Danger
		}
	}
	if color != theme.Danger && !running && starting {
		color = theme.Warning
	}
	button := compactIconButton(ui.ButtonBase(c)).Label(tr("Language servers")).
		Tooltip(tr("Show language servers"))
	button.Children(func() {
		ui.Icon(c, languageServerOnlineIcon).FontSize(16).TextColor(color)
	})
	hovered := button.Hovered()
	if !hovered {
		a.code.languageServersHoverBlocked = false
	} else if !a.code.languageServersHoverBlocked {
		a.code.languageServersOpen = true
	}
	if button.Clicked() {
		a.code.languageServersHoverBlocked = false
		a.code.languageServersOpen = !a.code.languageServersOpen
	}
	popover := ui.Popover(c, button, &a.code.languageServersOpen, func() {
		ui.Column(c).Gap(5).Padding(4).Children(func() {
			ui.Text(c, tr("Language servers")).Bold().Padding(3, 5)
			for _, status := range statuses {
				label, stateColor := tr("Running"), theme.Success
				switch {
				case status.failed:
					label, stateColor = tr("Error"), theme.Danger
				case status.starting:
					label, stateColor = tr("Starting"), theme.Warning
				}
				row := ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(4, 5).
					Label(tr("Language servers") + ": " + status.name).Tooltip(status.detail)
				row.Children(func() {
					ui.Text(c, "●").TextColor(stateColor).FontSize(theme.FontSize - 2)
					ui.Text(c, status.name).Grow(1).SingleLine()
					ui.Text(c, label).TextColor(stateColor).FontSize(theme.FontSize - 1)
				})
			}
		})
	})
	// Escape takes precedence over a pointer that is still over the icon. The
	// next genuine hover can open the panel again.
	if popover.Dismissed() {
		a.code.languageServersHoverBlocked = true
	}
}

func (a *app) codeEditorPane(c *ui.Context) {
	theme := c.Theme()
	pane := islandCard(c, ui.Column(c).Grow(1)).Clip()
	pane.OnShortcut(ui.Cmd, ui.KeyS, func() { a.saveCodeDocument(a.activeCodeDocument()) })
	if ui.Cmd != ui.Ctrl {
		pane.OnShortcut(ui.Ctrl, ui.KeyS, func() { a.saveCodeDocument(a.activeCodeDocument()) })
	}
	pane.OnShortcut(ui.Cmd|ui.Shift, ui.KeyS, a.saveCodeEdits)
	pane.OnShortcut(ui.Alt, ui.KeyLeft, func() { a.navigateCodeHistory(true) })
	pane.OnShortcut(ui.Alt, ui.KeyRight, func() { a.navigateCodeHistory(false) })
	pane.Children(func() {
		if len(a.code.tabs) == 0 {
			a.codeEmptyEditor(c)
			return
		}
		if a.code.activeTab < 0 || a.code.activeTab >= len(a.code.tabs) {
			a.code.activeTab = max(0, min(len(a.code.tabs)-1, a.code.activeTab))
		}
		if a.code.tabs[a.code.activeTab] != a.code.active {
			a.code.active = a.code.tabs[a.code.activeTab]
		}
		a.codeTabs(c)
		document := a.activeCodeDocument()
		if document == nil {
			a.codeEmptyEditor(c)
			return
		}
		ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(7, 14).Children(func() {
			back := compactIconButton(ui.Button(c, "")).Label(tr("Navigate back")).
				Tooltip(tr("Navigate back") + " (Alt+Left)").Disabled(len(a.code.back) == 0)
			back.Children(func() { ui.Text(c, "←").FontSize(theme.FontSize + 1) })
			if back.Clicked() {
				a.navigateCodeHistory(true)
			}
			forward := compactIconButton(ui.Button(c, "")).Label(tr("Navigate forward")).
				Tooltip(tr("Navigate forward") + " (Alt+Right)").Disabled(len(a.code.forward) == 0)
			forward.Children(func() { ui.Text(c, "→").FontSize(theme.FontSize + 1) })
			if forward.Clicked() {
				a.navigateCodeHistory(false)
			}
			ui.Text(c, document.path).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1).SingleLine().Tooltip(document.path)
			a.codeLanguageServersButton(c)
			if isMarkdownDocument(document.path) {
				tooltip := "Show Markdown preview"
				if document.markdownPreviewOpen {
					tooltip = "Hide Markdown preview"
				}
				flatToggleIconButton(c, "Toggle Markdown preview", tooltip, splitIcon, 15, 4,
					document.markdownPreviewOpen, func() {
						document.markdownPreviewOpen = !document.markdownPreviewOpen
					})
			}
			if document.saveError != "" {
				ui.Text(c, tr("Could not save")+": "+document.saveError).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
			} else if document.branchNotice != "" {
				ui.Text(c, document.branchNotice).TextColor(theme.Warning).FontSize(theme.FontSize - 1).
					SingleLine().Ellipsis("…").Tooltip(document.branchNotice)
			} else if document.readOnly {
				ui.Text(c, tr("Read only")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			}
		})
		ui.Box(c).Height(1).Background(theme.Border)
		a.codeDocumentView(c, document)
	})
}

func (a *app) codeTabs(c *ui.Context) {
	theme := c.Theme()
	ui.ScrollHorizontal(c).FillWidth().Children(func() {
		tabs := ui.TabsBase(c, &a.code.activeTab, len(a.code.tabs))
		tabs.List.Gap(2).Padding(4).Background(theme.Background).Children(func() {
			closePath := ""
			for index, path := range a.code.tabs {
				document := a.code.documents[path]
				c.Key("code-tab:" + path)
				tab := tabs.Tab(index).Gap(6).Padding(5, 8).Radius(theme.Radius).
					Label(tr("Editor tab") + " " + path)
				if index == a.code.activeTab {
					tab.Background(theme.Surface)
				} else if tab.Hovered() {
					tab.Background(theme.SurfaceHover)
				}
				tab.PopoverMenu(func(m *ui.Menu) {
					a.codeTabMenu(m, path, index, c.Services())
				})
				if tab.DoubleClicked() && a.code.preview == path {
					a.code.preview = ""
				}
				tab.Children(func() {
					fileTypeMark(c, path, 12, false)
					title := ui.Text(c, filepath.Base(path)).SingleLine().MaxWidth(180).Tooltip(path)
					if a.code.preview == path {
						title.Italic()
					}
					if document != nil && document.saveError != "" {
						ui.Icon(c, conflictIcon).FontSize(12).TextColor(theme.Danger)
					}
					close := compactIconButton(ui.ButtonBase(c)).Label(tr("Close") + " " + path).
						Tooltip(tr("Close tab")).Disabled(a.code.fileOperationBusy)
					close.Children(func() {
						if codeDocumentModified(document) && !close.Hovered() {
							ui.Text(c, "●").FontSize(10).TextColor(theme.Text).Label(tr("Unsaved changes"))
						} else {
							ui.Icon(c, closeIcon).FontSize(11).TextColor(theme.TextMuted)
						}
					})
					if close.Clicked() {
						closePath = path
					}
				})
			}
			if closePath != "" {
				a.closeCodeFile(closePath)
			}
		})
	})
	ui.Box(c).Height(1).Background(theme.Border)
}

func (a *app) codeEmptyEditor(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).Grow(1).Center().Gap(10).Padding(24).Children(func() {
		ui.Icon(c, fileCodeIcon).FontSize(30).TextColor(theme.TextMuted)
		ui.Text(c, tr("Open a file from the explorer")).Bold()
		ui.Text(c, tr("Use Quick open to filter paths, then press Enter")).TextColor(theme.TextMuted).TextAlign(ui.Center)
	})
}

func (a *app) codeDocumentView(c *ui.Context, document *codeDocument) {
	theme := c.Theme()
	if document.loading {
		ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
			ui.Spinner(c).Label(tr("Loading file"))
			ui.Text(c, tr("Loading file…")).TextColor(theme.TextMuted)
		})
		return
	}
	if document.binary {
		binaryFileView(c, document.path, document.imagePreview, nil)
		return
	}
	if document.loadError != "" {
		ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
			ui.Text(c, tr("The file could not be read.")).Bold()
			ui.Text(c, document.loadError).TextColor(theme.Danger)
			if ui.Button(c, tr("Try again")).Clicked() {
				a.requestCodeDocument(document)
			}
		})
		return
	}
	if document.state == nil {
		return
	}
	source := func() {
		editorView := editor.View(c, document.state).Grow(1).FillWidth().ReadOnly(document.readOnly)
		a.keyboardPane(c, "code-editor", editorView).Label(tr("Code editor")).MinWidth(0)
	}
	if !document.markdownPreviewOpen {
		source()
		return
	}
	paneSplitPercent(c, &a.code.markdownPreviewPercent, source, func() {
		a.codeMarkdownPreview(c, document)
	})
}

func isMarkdownDocument(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

func (a *app) codeMarkdownPreview(c *ui.Context, document *codeDocument) {
	if a.code.markdownOptions == nil {
		a.code.markdownOptions = &markdown.Options{WrapCode: true}
	}
	ui.Scroll(c).Fill().MinWidth(0).Label(tr("Markdown preview")).Children(func() {
		ui.Column(c).Padding(16).Children(func() {
			markdown.View(c, document.state.Buffer.String(), a.code.markdownOptions)
		})
	})
}

func (a *app) focusCodeQuickOpen() {
	a.code.sidebar = codeSidebarFiles
	a.workspacePaneCollapsed[sectionCode] = false
	a.sectionSearch[sectionCode].Focus()
}

func (a *app) focusCodeSearch() {
	a.code.sidebar = codeSidebarSearch
	a.workspacePaneCollapsed[sectionCode] = false
	a.code.searchFocus.Focus()
}
