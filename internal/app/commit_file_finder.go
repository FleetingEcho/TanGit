package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// commitFileFinderQuery is the filter in force: empty while the finder is closed.
func (a *app) commitFileFinderQuery() string {
	if !a.commitFileSearchOpen {
		return ""
	}
	return strings.TrimSpace(a.commitFileSearch)
}

// syncCommitFileRows builds the commit's file rows, narrowed by the finder. A
// narrowed tree is rebuilt from the matching paths, and every file row keeps
// its source index so the diff and the actions still target the right file.
func (a *app) syncCommitFileRows(state commitDetailViewState, files []repository.CommitFile) []fileTreeRow {
	query := a.commitFileFinderQuery()
	if query == "" {
		return a.syncFileTreeRows(state.scope, files, *state.fileRows, state.selectedFileRow, state.selectedFile)
	}
	visible := filteredSourceRows(len(files), query, func(index int) string {
		return files[index].Path + "\n" + files[index].PreviousPath
	})
	paths := make([]string, len(visible))
	for index, source := range visible {
		paths[index] = files[source].Path
	}
	rows := a.fileTreeRows(state.scope, paths)
	for index := range rows {
		if !rows[index].isDirectory() {
			rows[index].source = visible[rows[index].source]
		}
	}
	if directory := selectedFileTreeDirectory(*state.fileRows, *state.selectedFileRow); directory != "" {
		*state.selectedFileRow = fileTreeDirectoryIndex(rows, directory)
		*state.selectedFile = -1
	}
	previous := *state.selectedFile
	syncFileTreeRow(rows, state.selectedFileRow, state.selectedFile)
	if *state.selectedFile != previous && *state.selectedFile >= 0 {
		// The old file was filtered out; the first match takes over.
		state.selectFileRow(*state.selectedFileRow)
	}
	return rows
}

// commitFileFinderButton is the header icon that opens or closes the finder.
func (a *app) commitFileFinderButton(c *ui.Context) {
	theme := c.Theme()
	label, tooltip := "Find a file", "Search the changed files by path"
	if a.commitFileSearchOpen {
		label, tooltip = "Close file finder", "Close the file search and show every file"
	}
	button := compactIconButton(ui.Button(c, "")).Label(tr(label)).Tooltip(tr(tooltip)).Checked(a.commitFileSearchOpen)
	color := theme.Text
	if a.commitFileSearchOpen {
		button.Background(theme.Accent.Alpha(0.14))
		color = theme.Accent
	}
	button.Children(func() {
		ui.Icon(c, searchIcon).FontSize(14).TextColor(color)
	})
	if button.Clicked() {
		a.commitFileSearchOpen = !a.commitFileSearchOpen
		if a.commitFileSearchOpen {
			a.commitFileSearchFocus.Focus(c)
		} else {
			a.commitFileSearch = ""
		}
	}
}

// commitFileFinderRow is the search field under the header while the finder is
// open, with the match count and a clear action. Escape closes it.
func (a *app) commitFileFinderRow(c *ui.Context, files []repository.CommitFile) {
	if !a.commitFileSearchOpen {
		return
	}
	theme := c.Theme()
	input := ui.Row(c).AlignItems(ui.Center).Gap(7).Padding(4, 8).MarginEnd(8).
		Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius)
	var field ui.Element
	input.Children(func() {
		ui.Icon(c, searchIcon).FontSize(14).TextColor(theme.TextMuted)
		field = ui.TextInputBase(c, &a.commitFileSearch).Grow(1).MinWidth(0).Bind(&a.commitFileSearchFocus).
			AutoFocus().Label(tr("Find a file")).Placeholder(tr("Find a file…"))
		if strings.TrimSpace(a.commitFileSearch) != "" {
			ui.Text(c, trf("%d / %d", a.commitFileMatchCount(files), len(files))).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			flatIconButton(c, "Clear file search", "Show every file", closeIcon, 11, 3, false, func() {
				a.commitFileSearch = ""
				a.commitFileSearchFocus.Focus(c)
			})
		}
	})
	field.OnTextKey(func(key ui.TextKey) bool {
		if key.Key == ui.KeyEscape && key.Mods == 0 {
			a.commitFileSearchOpen = false
			a.commitFileSearch = ""
			return true
		}
		return false
	})
}

// commitFileMatchCount is the number of files the filter keeps.
func (a *app) commitFileMatchCount(files []repository.CommitFile) int {
	return len(filteredSourceRows(len(files), a.commitFileFinderQuery(), func(index int) string {
		return files[index].Path + "\n" + files[index].PreviousPath
	}))
}

// fullscreenFileFinderQuery is the filter of the whole-window file list.
func (a *app) fullscreenFileFinderQuery() string {
	if !a.fullscreenFileSearchOpen {
		return ""
	}
	return strings.TrimSpace(a.fullscreenFileSearch)
}

func (a *app) closeFullscreenFileFinder() {
	a.fullscreenFileSearchOpen = false
	a.fullscreenFileSearch = ""
}

// fullscreenFileFinderButton is the search icon of the whole-window file list.
func (a *app) fullscreenFileFinderButton(c *ui.Context) {
	theme := c.Theme()
	label, tooltip := "Find a file", "Search the changed files by path"
	if a.fullscreenFileSearchOpen {
		label, tooltip = "Close file finder", "Close the file search and show every file"
	}
	button := compactIconButton(ui.Button(c, "")).Label(tr(label)).Tooltip(tr(tooltip)).Checked(a.fullscreenFileSearchOpen)
	color := theme.Text
	if a.fullscreenFileSearchOpen {
		button.Background(theme.Accent.Alpha(0.14))
		color = theme.Accent
	}
	button.Children(func() {
		ui.Icon(c, searchIcon).FontSize(14).TextColor(color)
	})
	if button.Clicked() {
		if a.fullscreenFileSearchOpen {
			a.closeFullscreenFileFinder()
		} else {
			a.fullscreenFileSearchOpen = true
			a.fullscreenFileSearchFocus.Focus(c)
		}
	}
}

func (a *app) fullscreenFileFinderRow(c *ui.Context, matches, total int) {
	if !a.fullscreenFileSearchOpen {
		return
	}
	theme := c.Theme()
	var field ui.Element
	ui.Row(c).AlignItems(ui.Center).Gap(7).Padding(4, 8).
		Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
		ui.Icon(c, searchIcon).FontSize(14).TextColor(theme.TextMuted)
		field = ui.TextInputBase(c, &a.fullscreenFileSearch).Grow(1).MinWidth(0).Bind(&a.fullscreenFileSearchFocus).
			AutoFocus().Label(tr("Find a file")).Placeholder(tr("Find a file…"))
		if strings.TrimSpace(a.fullscreenFileSearch) != "" {
			ui.Text(c, trf("%d / %d", matches, total)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			flatIconButton(c, "Clear file search", "Show every file", closeIcon, 11, 3, false, func() {
				a.fullscreenFileSearch = ""
				a.fullscreenFileSearchFocus.Focus(c)
			})
		}
	})
	field.OnTextKey(func(key ui.TextKey) bool {
		if key.Key == ui.KeyEscape && key.Mods == 0 {
			a.closeFullscreenFileFinder()
			return true
		}
		return false
	})
}

// fileHistoryVisibleCommits maps the commit list's rows to the commits the
// finder keeps, matching subject, author, and hash.
func (a *app) fileHistoryVisibleCommits() []int {
	query := ""
	if a.fileHistorySearchOpen {
		query = a.fileHistorySearch
	}
	return filteredSourceRows(len(a.fileHistoryCommits), query, func(index int) string {
		commit := a.fileHistoryCommits[index]
		return commit.Subject + "\n" + commit.AuthorName + "\n" + commit.Hash
	})
}

// syncFileHistoryRow places the list's selection on the chosen commit's row,
// or on none while the finder hides it.
func (a *app) syncFileHistoryRow() {
	a.fileHistoryRow = -1
	for row, source := range a.fileHistoryRows {
		if source == a.fileHistorySelected {
			a.fileHistoryRow = row
			return
		}
	}
}

func (a *app) fileHistoryFinderButton(c *ui.Context) {
	theme := c.Theme()
	label, tooltip := "Find a commit", "Search the commits by message, author or hash"
	if a.fileHistorySearchOpen {
		label, tooltip = "Close commit finder", "Close the commit search and show every commit"
	}
	button := compactIconButton(ui.Button(c, "")).Label(tr(label)).Tooltip(tr(tooltip)).Checked(a.fileHistorySearchOpen)
	color := theme.Text
	if a.fileHistorySearchOpen {
		button.Background(theme.Accent.Alpha(0.14))
		color = theme.Accent
	}
	button.Children(func() {
		ui.Icon(c, searchIcon).FontSize(14).TextColor(color)
	})
	if button.Clicked() {
		if a.fileHistorySearchOpen {
			a.fileHistorySearchOpen, a.fileHistorySearch = false, ""
		} else {
			a.fileHistorySearchOpen = true
			a.fileHistorySearchFocus.Focus(c)
		}
	}
}

func (a *app) fileHistoryFinderRow(c *ui.Context, matches, total int) {
	if !a.fileHistorySearchOpen {
		return
	}
	theme := c.Theme()
	var field ui.Element
	ui.Row(c).AlignItems(ui.Center).Gap(7).Padding(4, 8).
		Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
		ui.Icon(c, searchIcon).FontSize(14).TextColor(theme.TextMuted)
		field = ui.TextInputBase(c, &a.fileHistorySearch).Grow(1).MinWidth(0).Bind(&a.fileHistorySearchFocus).
			AutoFocus().Label(tr("Find a commit")).Placeholder(tr("Find a commit…"))
		if strings.TrimSpace(a.fileHistorySearch) != "" {
			ui.Text(c, trf("%d / %d", matches, total)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			flatIconButton(c, "Clear commit search", "Show every commit", closeIcon, 11, 3, false, func() {
				a.fileHistorySearch = ""
				a.fileHistorySearchFocus.Focus(c)
			})
		}
	})
	field.OnTextKey(func(key ui.TextKey) bool {
		if key.Key == ui.KeyEscape && key.Mods == 0 {
			a.fileHistorySearchOpen, a.fileHistorySearch = false, ""
			return true
		}
		return false
	})
}

// fileHistoryVisibleFiles maps the file column's rows to the files the finder keeps.
func (a *app) fileHistoryVisibleFiles() []int {
	query := ""
	if a.fileHistoryFilesSearchOpen {
		query = a.fileHistoryFilesSearch
	}
	return filteredSourceRows(len(a.fileHistoryFiles), query, func(index int) string {
		return a.fileHistoryFiles[index].Path + "\n" + a.fileHistoryFiles[index].PreviousPath
	})
}

func (a *app) syncFileHistoryFileRow() {
	a.fileHistoryFileRow = -1
	for row, source := range a.fileHistoryFileRows {
		if source == a.fileHistorySelectedFile {
			a.fileHistoryFileRow = row
			return
		}
	}
}

func (a *app) fileHistoryFilesFinderButton(c *ui.Context) {
	theme := c.Theme()
	label, tooltip := "Find a file", "Search the changed files by path"
	if a.fileHistoryFilesSearchOpen {
		label, tooltip = "Close file finder", "Close the file search and show every file"
	}
	button := compactIconButton(ui.Button(c, "")).Label(tr(label)).Tooltip(tr(tooltip)).Checked(a.fileHistoryFilesSearchOpen)
	color := theme.Text
	if a.fileHistoryFilesSearchOpen {
		button.Background(theme.Accent.Alpha(0.14))
		color = theme.Accent
	}
	button.Children(func() {
		ui.Icon(c, searchIcon).FontSize(14).TextColor(color)
	})
	if button.Clicked() {
		if a.fileHistoryFilesSearchOpen {
			a.fileHistoryFilesSearchOpen, a.fileHistoryFilesSearch = false, ""
		} else {
			a.fileHistoryFilesSearchOpen = true
			a.fileHistoryFilesSearchFocus.Focus(c)
		}
	}
}

func (a *app) fileHistoryFilesFinderRow(c *ui.Context, matches, total int) {
	if !a.fileHistoryFilesSearchOpen {
		return
	}
	theme := c.Theme()
	var field ui.Element
	ui.Row(c).AlignItems(ui.Center).Gap(7).Padding(4, 8).
		Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
		ui.Icon(c, searchIcon).FontSize(14).TextColor(theme.TextMuted)
		field = ui.TextInputBase(c, &a.fileHistoryFilesSearch).Grow(1).MinWidth(0).Bind(&a.fileHistoryFilesSearchFocus).
			AutoFocus().Label(tr("Find a file")).Placeholder(tr("Find a file…"))
		if strings.TrimSpace(a.fileHistoryFilesSearch) != "" {
			ui.Text(c, trf("%d / %d", matches, total)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			flatIconButton(c, "Clear file search", "Show every file", closeIcon, 11, 3, false, func() {
				a.fileHistoryFilesSearch = ""
				a.fileHistoryFilesSearchFocus.Focus(c)
			})
		}
	})
	field.OnTextKey(func(key ui.TextKey) bool {
		if key.Key == ui.KeyEscape && key.Mods == 0 {
			a.fileHistoryFilesSearchOpen, a.fileHistoryFilesSearch = false, ""
			return true
		}
		return false
	})
}
