package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
)

func (a *app) codeView(c *ui.Context) {
	a.syncCodeRecovery()
	a.applyPendingCodeActions()
	a.ensureCodeFiles()
	a.ensureCodeSearchIndex()
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
	islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(10)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
			a.codeWorkspaceFolderButton(c)
			if a.code.searchFolder != "" {
				flatIconButton(c, "Open entire repository", "Clear the workspace folder scope", arrowUpIcon, 14, 4, false, func() {
					a.openCodeWorkspaceFolderInExplorer("")
				})
			}
			a.repositoryExternalEditorButton(c)
			flatIconButton(c, "Refresh files", "Reload the repository file list", refreshIcon, 14, 4,
				a.code.filesLoading || a.code.fileOperationBusy, func() {
					a.code.filesLoaded = false
					a.requestCodeFiles()
				})
			paneCollapseButton(c, &a.workspacePaneCollapsed[sectionCode], "Collapse file explorer")
			a.codeEditorActionsButton(c)
		})
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
	explorer := ui.Column(c).Grow(1).Gap(8)
	explorer.Children(func() {
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
			loadingPane(c, "Loading files", "Loading files…")
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
			if a.editorOnlyWorkspace() {
				message = "This folder has no files"
			}
			if strings.TrimSpace(a.code.filter) != "" {
				message = "No files match this filter"
			}
			listSearchEmpty(c, message)
			return
		}
		guides := treeGuideGroups(a.code.rows)
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
				})
				guides.decorate(directory, item, row).Drag(codeExplorerDrag{path: item.path})
				if a.remoteTransfersAvailable() {
					directory.MinHeight(38)
				}
				directory.PopoverMenu(func(m *ui.Menu) {
					a.codeExplorerMenu(m, item, row.Context.Services())
					a.remoteTransferExplorerMenu(m, item)
				})
				directory.Children(func() {
					a.remoteTransferExplorerHover(row.Context, directory, item)
				})
				if _, over := ui.DragOver[codeExplorerDrag](directory); over {
					directory.Background(theme.Accent.Alpha(0.16))
				}
				if dropped, ok := ui.Drop[codeExplorerDrag](directory); ok && !a.code.fileOperationBusy {
					a.code.pendingMoveSource = dropped.path
					a.code.pendingMoveTarget = item.path
				}
				a.workspaceTransferDropTarget(directory, item.path, theme.Accent.Alpha(0.16))
				return
			}
			button := ui.ButtonBase(row.Context).Grow(1).Gap(6).Padding(6, 8, 6, float32(8+16*item.depth)).
				Label(tr("Open") + " " + item.path).
				Drag(codeExplorerDrag{path: item.path}).
				PopoverMenu(func(m *ui.Menu) {
					a.codeExplorerMenu(m, item, row.Context.Services())
					a.remoteTransferExplorerMenu(m, item)
				})
			if a.remoteTransfersAvailable() {
				button.MinHeight(38)
			}
			guides.decorate(button, item, row).Children(func() {
				fileTypeMark(row.Context, item.path, 13, row.Selected())
				ui.Text(row.Context, item.name).Grow(1).SingleLine()
				if status := a.codeFileStatus(item.path); status != "" {
					ui.Text(row.Context, status).FontSize(theme.FontSize - 1).Bold().TextColor(theme.Warning)
				}
				a.remoteTransferExplorerHover(row.Context, button, item)
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
	})
	// Files and folders dropped between rows belong to the visible workspace
	// root. More specific folder rows still receive their own drops first.
	a.workspaceTransferDropTarget(explorer, a.code.searchFolder, theme.Accent.Alpha(0.16))
}

// codeRepositoryRootRow makes the opened workspace root a real creation and
// drop target. A nested workspace has no ancestor rows outside that root.
func (a *app) codeRepositoryRootRow(c *ui.Context, theme *ui.Theme) {
	folder := a.code.searchFolder
	label := tr("Repository root")
	if a.editorOnlyWorkspace() {
		label = tr("Workspace folder")
	}
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
		a.remoteTransferRootMenu(m)
	})
	if _, over := ui.DragOver[codeExplorerDrag](root); over {
		root.Background(theme.Accent.Alpha(0.16))
	}
	if dropped, ok := ui.Drop[codeExplorerDrag](root); ok && !a.code.fileOperationBusy {
		a.code.pendingMoveSource = dropped.path
		a.code.pendingMoveTarget = folder
	}
	a.workspaceTransferDropTarget(root, folder, theme.Accent.Alpha(0.16))
}

// codeWorkspaceFolderButton keeps the current workspace in the compact header.
// Editing lives in a dialog so navigation cannot change the path accidentally.
func (a *app) codeWorkspaceFolderButton(c *ui.Context) {
	theme := c.Theme()
	workspacePath := a.snapshot.Root
	if folder, ok := normalizedCodeSearchFolder(a.code.searchFolder); ok && folder != "" {
		workspacePath = filepath.Join(workspacePath, filepath.FromSlash(folder))
	}
	button := ui.ButtonBase(c).Grow(1).MinWidth(0).Gap(7).Padding(4).Radius(theme.Radius).
		Label(tr("Change workspace folder")).Tooltip(workspacePath)
	button.Children(func() {
		ui.Icon(c, folderOpenIcon).FontSize(15).TextColor(theme.TextMuted)
		ui.Text(c, filepath.Base(filepath.Clean(workspacePath))).Bold().Grow(1).SingleLine().Ellipsis("…")
	})
	if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	if button.Clicked() {
		a.openCodeWorkspaceFolderForm()
	}
}

func (a *app) codeEditorActionsButton(c *ui.Context) {
	theme := c.Theme()
	button := ui.ButtonBase(c).Padding(4).Radius(theme.Radius).
		Label(tr("More actions")).Tooltip(tr("More actions"))
	button.Children(func() {
		ui.Icon(c, moreIcon).FontSize(15).TextColor(theme.TextMuted)
	})
	if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	button.PopoverMenuButton(func(m *ui.Menu) {
		if m.Item(tr("New File…")).Disabled(a.code.fileOperationBusy).Chosen() {
			a.openCodeFileForm(codeFileFormNewFile, a.selectedCodeDirectory(), "")
		}
		if m.Item(tr("New Folder…")).Disabled(a.code.fileOperationBusy).Chosen() {
			a.openCodeFileForm(codeFileFormNewFolder, a.selectedCodeDirectory(), "")
		}
	})
}

func (a *app) codeSearchSidebar(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Children(func() {
		input := ui.SearchField(c, &a.code.searchQuery).Bind(&a.code.searchFocus).
			Label(tr("Search in files")).Placeholder(tr("Search in files…")).Grow(1)
		if input.Changed() {
			a.codeSearchQueryChanged()
		}
		if input.Submitted() {
			a.beginCodeSearch()
		}
	})
	// Match options stay left-aligned; disclosure and filters form a quiet
	// toolbar on the right, following the familiar editor-search layout.
	ui.Row(c).AlignItems(ui.Center).Gap(3).Children(func() {
		a.codeSearchOptionButton(c, "Aa", "Match case", &a.code.searchCaseSensitive)
		a.codeSearchOptionButton(c, "ab", "Match whole word", &a.code.searchWholeWord)
		a.codeSearchOptionButton(c, ".*", "Use regular expression", &a.code.searchRegularExpr)
		ui.Spacer(c)
		hasFilters := strings.TrimSpace(a.code.searchInclude) != "" || strings.TrimSpace(a.code.searchExclude) != ""
		flatToggleIconButton(c, "File filters", "Show or hide include and exclude globs", filterIcon, 12, 3,
			a.code.searchFiltersOpen || hasFilters, func() {
				a.code.searchFiltersOpen = !a.code.searchFiltersOpen
			})
		flatToggleIconButton(c, "Toggle replace", "Show or hide replace", chevronDownIcon, 12, 3,
			a.code.searchReplaceOpen, func() {
				a.code.searchReplaceOpen = !a.code.searchReplaceOpen
				a.code.searchReplaceError = ""
				a.code.searchReplaceStatus = ""
			})
	})
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
	// Replacement is the final input row, below both optional file filters.
	if a.code.searchReplaceOpen {
		ui.Box(c).Height(1).Background(theme.Border).Margin(2, 0)
		ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
			ui.Icon(c, replaceAllIcon).FontSize(14).TextColor(theme.TextMuted)
			input := ui.TextInput(c, &a.code.searchReplaceQuery).Bind(&a.code.searchReplaceFocus).
				Label(tr("Replace in files")).Placeholder(tr("Replace with…")).Grow(1)
			if input.Submitted() {
				a.requestCodeSearchReplaceAll()
			}
			if a.code.searchReplaceRunning {
				ui.Spinner(c).Label(tr("Replacing matches"))
			}
			a.codeSearchToggleButton(c, "AB", "Preserve case", &a.code.searchPreserveCase, false)
			disabled := a.code.searchReplaceRunning || a.code.searchRunning || a.code.filesLoading ||
				a.code.searchSubmitted == "" || a.code.searchSubmitted != strings.TrimSpace(a.code.searchQuery)
			tooltip := tr("Replace all matches in the selected files")
			// A disabled MyGo anchor suppresses its own tooltip. The enabled,
			// non-interactive wrapper preserves the explanation without
			// making the replacement action itself clickable.
			ui.Row(c).Tooltip(tooltip).Children(func() {
				primaryIconButton(c, "Replace All", "Replace all matches in the selected files", replaceAllIcon,
					disabled, a.requestCodeSearchReplaceAll)
			})
		})
	}
	if a.code.searchReplaceError != "" {
		ui.Text(c, a.code.searchReplaceError).TextColor(theme.Danger).FontSize(theme.FontSize - 1).
			SingleLine().Ellipsis("…").Tooltip(a.code.searchReplaceError)
	}
	if a.code.searchReplaceStatus != "" {
		ui.Text(c, a.code.searchReplaceStatus).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).
			SingleLine().Ellipsis("…").Tooltip(a.code.searchReplaceStatus)
	}
	if a.code.searchError != "" {
		ui.Text(c, a.code.searchError).TextColor(theme.Danger).FontSize(theme.FontSize - 1).SingleLine().Ellipsis("…").Tooltip(a.code.searchError)
	}
	if a.code.filesLoading || (a.code.searchRunning && !a.code.searchLoadingMore) {
		label := tr("Searching files")
		if a.code.filesLoading {
			label = tr("Loading files")
		}
		loadingPane(c, label, label+"…")
		return
	}
	if a.code.searchSubmitted == "" {
		ui.Column(c).Grow(1).Center().Gap(8).Padding(24).Children(func() {
			ui.Icon(c, searchIcon).FontSize(28).TextColor(theme.TextMuted)
			ui.Text(c, tr("Search your workspace")).Bold()
			ui.Text(c, tr("Type to find text across files. Use the options above to refine your search.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).TextAlign(ui.Center).MaxWidth(300)
		})
		return
	}
	if len(a.code.searchResults) == 0 {
		listSearchEmpty(c, "No matches found")
		return
	}
	ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
		matches, files := len(a.code.searchResults), codeSearchResultFileCount(a.code.searchResults)
		label := trf("%d matches in %d files", matches, files)
		if a.code.searchLoadingMore {
			// Counts are only a lower bound while the single completing scan
			// collects matches beyond the initial page.
			label = trf("%d+ matches in %d+ files", matches, files)
		}
		ui.Text(c, label).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
		if a.code.searchLoadingMore {
			ui.Spinner(c).Label(tr("Completing search"))
		}
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

func (a *app) codeSearchOptionButton(c *ui.Context, text, label string, active *bool) {
	a.codeSearchToggleButton(c, text, label, active, true)
}

func (a *app) codeSearchToggleButton(c *ui.Context, text, label string, active *bool, rerun bool) {
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
		if rerun && a.code.searchSubmitted != "" {
			a.beginCodeSearch()
		}
	}
}

func codeSearchResultFileCount(matches []workspaceSearchMatch) int {
	files := make(map[string]struct{})
	for _, match := range matches {
		files[match.Path] = struct{}{}
	}
	return len(files)
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
	matchColumn := match.Column - 1
	matchColumn -= len(match.Text) - len(strings.TrimLeft(match.Text, " \t\r\n"))
	label := fmt.Sprintf("%s:%d:%d: %s", match.Path, match.Line, match.Column, text)
	button := ui.ButtonBase(row.Context).Grow(1).Gap(8).Padding(4, 7, 4, 26).
		Label(tr("Open") + " " + label)
	button.Children(func() {
		ui.Textf(row.Context, "%d", match.Line).Width(30).TextAlign(ui.End).
			TextColor(secondaryRowColor(theme, row)).FontSize(theme.FontSize - 1)
		ui.RichText(row.Context, codeSearchMatchHighlightSpansAt(text, a.code.searchSubmittedOptions, theme, matchColumn)...).
			Grow(1).SingleLine().Ellipsis("…")
	})
	if button.DoubleClicked() {
		a.selectCodeSearchRow(row.Index)
		a.openCodeSearchMatchMode(match, true)
	} else if button.Clicked() {
		a.selectCodeSearchRow(row.Index)
		a.openCodeSearchMatch(match)
	}
}

func codeSearchHighlightSpans(text, query string, caseSensitive bool, theme *ui.Theme) []ui.Span {
	return codeSearchMatchHighlightSpans(text, workspaceSearchOptions{Query: query, CaseSensitive: caseSensitive}, theme)
}

func codeSearchMatchHighlightSpans(text string, options workspaceSearchOptions, theme *ui.Theme) []ui.Span {
	return codeSearchMatchHighlightSpansAt(text, options, theme, -1)
}

// codeSearchMatchHighlightSpansAt distinguishes repeated matches on one line.
// The fallback retains highlighting for synthetic rows and truncated snippets
// whose original source column cannot be mapped directly into the display text.
func codeSearchMatchHighlightSpansAt(text string, options workspaceSearchOptions, theme *ui.Theme, column int) []ui.Span {
	start, end := -1, -1
	if column >= 0 && column <= len(text) {
		start, end = workspaceSearchRangeWithOptions(text[column:], options)
		if start >= 0 {
			start += column
			end += column
		}
	}
	if start < 0 {
		start, end = workspaceSearchRangeWithOptions(text, options)
	}
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
	id         string
	name       string
	custom     bool
	running    bool
	starting   bool
	analyzing  bool
	missing    bool
	installing bool
	failed     bool
	detail     string
}

// codeLanguageServerStatuses includes every live workspace server, not just
// the one associated with the active extension. Profile order keeps the hover
// panel stable as documents open and close.
func (a *app) codeLanguageServerStatuses() []codeLanguageServerStatus {
	statuses := make([]codeLanguageServerStatus, 0, len(a.additionalLanguageServers)+1)
	for _, profile := range languageServerProfilesWithSettings(a.settings.LanguageServers) {
		status := codeLanguageServerStatus{id: profile.ID, name: profile.Name, custom: profile.Custom,
			installing: a.languageServerInstalling[profile.ID]}
		if profile.ID == goLanguageServerID {
			status.running = a.goLanguageServer != nil
			status.starting = a.goLanguageServerStarting
			status.missing = a.goLanguageServerMissing
			status.failed = a.goLanguageServerError != ""
			status.detail = a.goLanguageServerError
		} else if server := a.additionalLanguageServers[profile.ID]; server != nil {
			status.running = server.connection != nil
			status.starting = server.starting
			status.analyzing = server.analyzing
			status.missing = server.missing
			status.failed = server.error != ""
			status.detail = server.error
			if status.detail == "" {
				status.detail = server.statusDetail
			}
		} else {
			continue
		}
		if status.running || status.starting || status.installing || status.missing || status.failed {
			statuses = append(statuses, status)
		}
	}
	return statuses
}

// codeLanguageServerPresentationChanged compares the status data used by the
// Code toolbar and its popover. Internal progress details that leave that
// presentation unchanged do not need a whole-window frame.
func (a *app) codeLanguageServerPresentationChanged(before []codeLanguageServerStatus) bool {
	return a.workspaceTab == sectionCode && !slices.Equal(before, a.codeLanguageServerStatuses())
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
	analyzing := false
	missing := false
	for _, status := range statuses {
		running = running || status.running
		starting = starting || status.starting
		analyzing = analyzing || (status.analyzing && !status.failed)
		missing = missing || status.missing || status.installing
		if status.failed {
			color = theme.Danger
		}
	}
	if color != theme.Danger && !running && starting {
		color = theme.Warning
	}
	if color != theme.Danger && analyzing {
		color = theme.Warning
	}
	if color != theme.Danger && missing {
		color = theme.Warning
	}
	var button ui.Element
	if analyzing {
		button = compactTextButton(ui.ButtonBase(c))
	} else {
		button = compactIconButton(ui.ButtonBase(c))
	}
	button.Label(tr("Language servers")).Tooltip(tr("Show language servers"))
	button.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
			ui.Icon(c, languageServerOnlineIcon).FontSize(16).TextColor(color)
			if analyzing {
				ui.Text(c, tr("Analyzing…")).FontSize(theme.FontSize - 1).TextColor(theme.Warning)
			}
		})
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
				case status.installing:
					label, stateColor = tr("Installing…"), theme.Warning
				case status.missing:
					label, stateColor = tr("Download required"), theme.Warning
				case status.analyzing:
					label, stateColor = tr("Analyzing…"), theme.Warning
				case status.starting:
					label, stateColor = tr("Starting"), theme.Warning
				}
				row := ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(4, 5).
					Label(tr("Language servers") + ": " + status.name).Tooltip(status.detail)
				row.Children(func() {
					ui.Text(c, "●").TextColor(stateColor).FontSize(theme.FontSize - 2)
					ui.Text(c, status.name).Grow(1).SingleLine()
					ui.Text(c, label).TextColor(stateColor).FontSize(theme.FontSize - 1)
					if !status.custom && !status.installing && (status.missing || status.failed) {
						action := tr("Install managed")
						if status.missing {
							action = tr("Download")
						}
						install := compactTextButton(ui.Button(c, action)).Label(action + " " + status.name)
						if install.Clicked() {
							a.installLanguageServer(status.id)
						}
					}
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
	a.codeEditorOutlineShortcuts(pane)
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
			breadcrumb := ""
			if document.state != nil && document.state.Buffer != nil {
				if offset, ok := codeLanguageOffset(document); ok {
					breadcrumb = languageServerSymbolBreadcrumb(document.symbols, document.state.Buffer.String(), offset)
				}
			}
			location := document.path
			if breadcrumb != "" {
				location += "  ›  " + breadcrumb
			}
			ui.Text(c, location).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1).SingleLine().Tooltip(location)
			if len(a.code.back) > 0 || len(a.code.forward) > 0 {
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
			}
			flatIconButton(c, "Outline", "Symbols in the current document", fileBracesIcon, 14, 4, false, func() {
				a.openLanguageSymbols(document)
			})
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
		if document.loading {
			loadingPane(c, "Loading file", "Loading file…")
		}
		return
	}
	a.applyCodeEditorPreferences(document.state)
	a.syncLanguageDocumentFeatures(document)
	a.syncCodeBlame(document)
	settings := effectiveBuiltInEditorPreferences(a.settings)
	source := func() {
		if !settings.EditorMinimap {
			editorView := editor.View(c, document.state).Grow(1).FillWidth().ReadOnly(document.readOnly || document.loading)
			a.keyboardPane(c, "code-editor", editorView).Label(tr("Code editor")).MinWidth(0)
			return
		}
		ui.Row(c).Grow(1).FillWidth().MinWidth(0).Children(func() {
			editorView := editor.View(c, document.state).Grow(1).FillWidth().FillHeight().ReadOnly(document.readOnly || document.loading)
			a.keyboardPane(c, "code-editor", editorView).Label(tr("Code editor")).MinWidth(0)
			a.codeMinimap(c, document)
		})
	}
	content := source
	if settings.EditorStickyScroll {
		content = func() {
			ui.Column(c).Grow(1).Fill().MinWidth(0).Children(func() {
				a.codeStickySymbolHeader(c, document)
				source()
			})
		}
	}
	if !document.markdownPreviewOpen {
		content()
		return
	}
	paneSplitPercent(c, &a.code.markdownPreviewPercent, content, func() {
		a.codeMarkdownPreview(c, document)
	})
}

func (a *app) codeStickySymbolHeader(c *ui.Context, document *codeDocument) {
	if document == nil || document.state == nil || document.state.Buffer == nil {
		return
	}
	first, _ := document.state.VisibleLines()
	if first <= 0 {
		return
	}
	buffer := document.state.Buffer
	if buffer.LineCount() == 0 {
		return
	}
	offset := buffer.Offset(editor.Pos{Line: min(first, buffer.LineCount()-1)})
	crumb := languageServerSymbolBreadcrumb(document.symbols, buffer.String(), offset)
	if crumb == "" {
		return
	}
	ui.Row(c).FillWidth().Padding(4, 12).Background(c.Theme().Surface.Alpha(0.92)).Children(func() {
		ui.Text(c, crumb).FontSize(c.Theme().FontSize - 1).TextColor(c.Theme().TextMuted).SingleLine()
	})
	ui.Box(c).FillWidth().Height(1).Background(c.Theme().Border)
}

func (a *app) codeMinimap(c *ui.Context, document *codeDocument) {
	if document == nil || document.state == nil {
		return
	}
	theme := c.Theme()
	editor.Minimap(c, document.state).Width(86).FillHeight().Background(theme.Surface.Alpha(0.55)).Label(tr("Minimap"))
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

func (a *app) focusCodeReplace() {
	a.focusCodeSearch()
	a.code.searchReplaceOpen = true
	a.code.searchReplaceFocus.Focus()
}
