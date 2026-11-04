package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func (a *app) codeView(c *ui.Context) {
	a.applyPendingCodeActions()
	a.ensureCodeFiles()
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
	islandCard(c, ui.Column(c).Grow(1).Gap(10).Padding(12)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Icon(c, fileCodeIcon).FontSize(15).TextColor(theme.Accent)
			ui.Text(c, tr("Editor")).Bold().Grow(1)
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
	a.codeRepositoryRootRow(c, theme)
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
		if button.Clicked() {
			a.selectCodeRow(row.Index)
			a.openCodeFile(item.path, 0)
		}
	})
	if a.keyboardListChanged(&a.code.list, list) {
		a.selectCodeRow(a.code.selectedRow)
	}
	normalizeFileTreeRow(a.code.rows, &a.code.selectedRow, &a.code.selectedSource)
}

// codeRepositoryRootRow makes the otherwise implicit repository root a real
// creation and drop target, including when every file lives in a subfolder.
func (a *app) codeRepositoryRootRow(c *ui.Context, theme *ui.Theme) {
	root := ui.ButtonBase(c).FillWidth().Gap(7).Padding(6, 8).
		Label(tr("Repository root")).Tooltip(a.snapshot.Root)
	root.Children(func() {
		ui.Icon(c, folderOpenIcon).FontSize(13).TextColor(theme.Accent)
		ui.Text(c, tr("Repository root")).Grow(1).SingleLine()
	})
	root.PopoverMenu(func(m *ui.Menu) {
		if m.Item(tr("New File…")).Disabled(a.code.fileOperationBusy).Chosen() {
			a.openCodeFileForm(codeFileFormNewFile, "", "")
		}
		if m.Item(tr("New Folder…")).Disabled(a.code.fileOperationBusy).Chosen() {
			a.openCodeFileForm(codeFileFormNewFolder, "", "")
		}
	})
	if _, over := ui.DragOver[codeExplorerDrag](root); over {
		root.Background(theme.Accent.Alpha(0.16))
	}
	if dropped, ok := ui.Drop[codeExplorerDrag](root); ok && !a.code.fileOperationBusy {
		a.code.pendingMoveSource = dropped.path
		a.code.pendingMoveTarget = ""
	}
}

func (a *app) codeSearchSidebar(c *ui.Context) {
	theme := c.Theme()
	input := ui.SearchField(c, &a.code.searchQuery).Bind(&a.code.searchFocus).
		Label(tr("Search in files")).Placeholder(tr("Search repository content"))
	if input.Submitted() {
		a.beginCodeSearch()
	}
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Toggle(c, &a.code.searchCaseSensitive, tr("Match case"))
		ui.Spacer(c)
		if ui.Button(c, tr("Search")).Clicked() {
			a.beginCodeSearch()
		}
	})
	if a.code.filesLoading || a.code.searchRunning {
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
			ui.Text(c, tr("Search across tracked and non-ignored files")).TextColor(theme.TextMuted).TextAlign(ui.Center)
		})
		return
	}
	if len(a.code.searchResults) == 0 {
		listSearchEmpty(c, "No matches found")
		return
	}
	ui.Text(c, trf("%d matches", len(a.code.searchResults))).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
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
		ui.RichText(row.Context, codeSearchHighlightSpans(text, a.code.searchSubmitted, a.code.searchCaseSensitive, theme)...).
			Grow(1).SingleLine().Ellipsis("…")
	})
	if button.Clicked() {
		a.selectCodeSearchRow(row.Index)
		a.openCodeSearchMatch(match)
	}
}

func codeSearchHighlightSpans(text, query string, caseSensitive bool, theme *ui.Theme) []ui.Span {
	start, end := workspaceSearchRange(text, query, caseSensitive)
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

func (a *app) codeEditorPane(c *ui.Context) {
	theme := c.Theme()
	islandCard(c, ui.Column(c).Grow(1)).Clip().Children(func() {
		if len(a.code.tabs) == 0 {
			a.codeEmptyEditor(c)
			return
		}
		if a.code.activeTab < 0 || a.code.activeTab >= len(a.code.tabs) {
			a.code.activeTab = max(0, min(len(a.code.tabs)-1, a.code.activeTab))
		}
		if a.code.tabs[a.code.activeTab] != a.code.active {
			a.saveCodeEdits()
			a.code.active = a.code.tabs[a.code.activeTab]
		}
		a.codeTabs(c)
		document := a.activeCodeDocument()
		if document == nil {
			a.codeEmptyEditor(c)
			return
		}
		ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(7, 14).Children(func() {
			ui.Text(c, document.path).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1).SingleLine().Tooltip(document.path)
			if document.saveError != "" {
				ui.Text(c, tr("Could not save")+": "+document.saveError).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
			} else if document.state != nil {
				ui.Text(c, tr("Saved automatically")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
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
				tab.Children(func() {
					fileTypeMark(c, path, 12, false)
					ui.Text(c, filepath.Base(path)).SingleLine().MaxWidth(180).Tooltip(path)
					if document != nil && document.saveError != "" {
						ui.Icon(c, conflictIcon).FontSize(12).TextColor(theme.Danger)
					}
					close := compactIconButton(ui.ButtonBase(c)).Label(tr("Close") + " " + path).
						Tooltip(tr("Close tab")).Disabled(a.code.fileOperationBusy)
					close.Children(func() {
						ui.Icon(c, closeIcon).FontSize(11).TextColor(theme.TextMuted)
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
		ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
			ui.Text(c, tr("This file is binary and cannot be edited here.")).TextColor(theme.TextMuted)
		})
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
	editorView := editor.View(c, document.state).Grow(1).FillWidth()
	a.keyboardPane(c, "code-editor", editorView).Label(tr("Code editor"))
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
