package app

import (
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

const (
	fileTreeChangesScope = "changes"
	fileTreeCommitScope  = "commit"
	fileTreeBranchScope  = "branch-commit"
	fileTreeStashScope   = "stash"
)

func changeFilePaths(files []repository.File) []string {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.Path
	}
	return paths
}

func commitFilePaths(files []repository.CommitFile) []string {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.Path
	}
	return paths
}

// fileTreeRow is either a file's source row or a directory. source is -1 for
// a directory, which keeps file-only actions from ever treating it as a file.
type fileTreeRow struct {
	source int
	path   string
	name   string
	depth  int
}

func (r fileTreeRow) isDirectory() bool { return r.source < 0 }

func fileTreeCollapseKey(scope, path string) string { return scope + "\x00" + path }

// buildFileTreeRows keeps flat mode in repository order. Tree mode sorts by
// path, inserts each visible directory once, and retains every file's source
// index so collapsing or reordering rows cannot change an action's target.
func buildFileTreeRows(paths []string, tree bool, collapsed map[string]bool, scope string) []fileTreeRow {
	rows := make([]fileTreeRow, 0, len(paths))
	if !tree {
		for source, path := range paths {
			rows = append(rows, fileTreeRow{source: source, path: path, name: path})
		}
		return rows
	}

	ordered := make([]int, len(paths))
	for source := range paths {
		ordered[source] = source
	}
	sort.SliceStable(ordered, func(i, j int) bool { return paths[ordered[i]] < paths[ordered[j]] })
	seenDirectories := make(map[string]bool)
	for _, source := range ordered {
		path := paths[source]
		parts := strings.Split(path, "/")
		hidden := false
		for depth := 0; depth < len(parts)-1; depth++ {
			directory := strings.Join(parts[:depth+1], "/")
			if !seenDirectories[directory] {
				seenDirectories[directory] = true
				if !hidden {
					rows = append(rows, fileTreeRow{
						source: -1, path: directory, name: parts[depth], depth: depth,
					})
				}
			}
			if collapsed[fileTreeCollapseKey(scope, directory)] {
				hidden = true
			}
		}
		if !hidden {
			rows = append(rows, fileTreeRow{
				source: source, path: path, name: parts[len(parts)-1], depth: len(parts) - 1,
			})
		}
	}
	return rows
}

func firstFileTreeFile(rows []fileTreeRow) int {
	for row, item := range rows {
		if !item.isDirectory() {
			return row
		}
	}
	return -1
}

func fileTreeDirectoryIndex(rows []fileTreeRow, path string) int {
	if path == "" {
		return -1
	}
	for row, item := range rows {
		if item.isDirectory() && item.path == path {
			return row
		}
	}
	return -1
}

func selectedFileTreeDirectory(rows []fileTreeRow, row int) string {
	if row < 0 || row >= len(rows) || !rows[row].isDirectory() {
		return ""
	}
	return rows[row].path
}

func selectFileTreeRow(rows []fileTreeRow, row int, selectedRow, selectedSource *int) {
	*selectedRow, *selectedSource = -1, -1
	if row < 0 || row >= len(rows) {
		return
	}
	*selectedRow, *selectedSource = row, rows[row].source
}

// syncFileTreeRow follows source-file selection after a reload or mode swap.
// A directory selection remains a directory; an absent or newly loaded file
// falls back to the first visible file, never to a directory action target.
func syncFileTreeRow(rows []fileTreeRow, selectedRow, selectedSource *int) {
	if *selectedSource >= 0 {
		for row, item := range rows {
			if item.source == *selectedSource {
				*selectedRow = row
				return
			}
		}
	}
	if *selectedSource < 0 && *selectedRow >= 0 && *selectedRow < len(rows) && rows[*selectedRow].isDirectory() {
		return
	}
	selectFileTreeRow(rows, firstFileTreeFile(rows), selectedRow, selectedSource)
}

func normalizeFileTreeRow(rows []fileTreeRow, selectedRow, selectedSource *int) {
	if len(rows) > 0 && (*selectedRow < 0 || *selectedRow >= len(rows)) {
		selectFileTreeRow(rows, firstFileTreeFile(rows), selectedRow, selectedSource)
	}
}

func (a *app) syncFileTreeRows(scope string, paths []string, previous []fileTreeRow, selectedRow, selectedSource *int) []fileTreeRow {
	directory := selectedFileTreeDirectory(previous, *selectedRow)
	rows := a.fileTreeRows(scope, paths)
	if directory != "" {
		*selectedRow = fileTreeDirectoryIndex(rows, directory)
		*selectedSource = -1
	}
	syncFileTreeRow(rows, selectedRow, selectedSource)
	return rows
}

func fileTreeKey(rows []fileTreeRow, row int) any {
	if row < 0 || row >= len(rows) {
		return row
	}
	item := rows[row]
	if item.isDirectory() {
		return "directory\x00" + item.path
	}
	return "file\x00" + item.path
}

func fileTreeLabel(rows []fileTreeRow, row int) string {
	if row < 0 || row >= len(rows) {
		return ""
	}
	return rows[row].name
}

func fileTreeDirectories(paths []string) []string {
	seen := make(map[string]bool)
	var directories []string
	for _, path := range paths {
		parts := strings.Split(path, "/")
		for depth := 0; depth < len(parts)-1; depth++ {
			directory := strings.Join(parts[:depth+1], "/")
			if !seen[directory] {
				seen[directory] = true
				directories = append(directories, directory)
			}
		}
	}
	return directories
}

func (a *app) fileTreeRows(scope string, paths []string) []fileTreeRow {
	return buildFileTreeRows(paths, a.fileTreeMode, a.fileTreeCollapsed, scope)
}

func (a *app) toggleFileTreeDirectory(scope, path string) {
	if a.fileTreeCollapsed == nil {
		a.fileTreeCollapsed = make(map[string]bool)
	}
	key := fileTreeCollapseKey(scope, path)
	a.fileTreeCollapsed[key] = !a.fileTreeCollapsed[key]
}

func (a *app) setAllFileTreeDirectories(scope string, paths []string, collapse bool) {
	if a.fileTreeCollapsed == nil {
		a.fileTreeCollapsed = make(map[string]bool)
	}
	for _, directory := range fileTreeDirectories(paths) {
		a.fileTreeCollapsed[fileTreeCollapseKey(scope, directory)] = collapse
	}
}

// fileTreeButtons offers the same flat/tree and directory controls in every
// file pane. The controls act on view state only; no Git operation is involved.
func (a *app) fileTreeButtons(c *ui.Context, scope string, paths []string) {
	label, tooltip := "Show files as tree", "Group files by directory"
	if a.fileTreeMode {
		label, tooltip = "Show files as a flat list", "Show every full file path"
	}
	iconButton(c, label, tooltip, folderIcon, false, func() {
		a.fileTreeMode = !a.fileTreeMode
	})
	if !a.fileTreeMode {
		return
	}
	iconButton(c, "Expand all directories", "Show files in every directory", chevronDownIcon, false, func() {
		a.setAllFileTreeDirectories(scope, paths, false)
	})
	iconButton(c, "Collapse all directories", "Show only top-level files and directories", chevronUpIcon, false, func() {
		a.setAllFileTreeDirectories(scope, paths, true)
	})
}

func (a *app) fileTreeMenuItems(m *ui.Menu, scope string, paths []string) {
	label := "Show files as tree"
	if a.fileTreeMode {
		label = "Show files as a flat list"
	}
	if m.Item(tr(label)).Chosen() {
		a.fileTreeMode = !a.fileTreeMode
	}
	if !a.fileTreeMode {
		return
	}
	if m.Item(tr("Expand all directories")).Chosen() {
		a.setAllFileTreeDirectories(scope, paths, false)
	}
	if m.Item(tr("Collapse all directories")).Chosen() {
		a.setAllFileTreeDirectories(scope, paths, true)
	}
}

func (a *app) fileTreeDirectoryRow(c *ui.Context, scope string, item fileTreeRow, selected bool, selectRow func()) {
	theme := c.Theme()
	arrow := "▾"
	action := "Collapse directory"
	if a.fileTreeCollapsed[fileTreeCollapseKey(scope, item.path)] {
		arrow = "▸"
		action = "Expand directory"
	}
	ui.Row(c).AlignItems(ui.Center).Padding(6, 8, 6, float32(8+16*item.depth)).Children(func() {
		button := ui.ButtonBase(c).Grow(1).Gap(7).Padding(0)
		clicked := button.Label(tr(action) + " " + item.path).Tooltip(item.path).Clicked()
		button.Children(func() {
			arrowColor := theme.TextMuted
			folderColor := theme.Accent
			if selected {
				// The row's selected face is Accent, so Accent icons disappear
				// into it. Match the contrasting text painted on that face.
				arrowColor = theme.AccentText
				folderColor = theme.AccentText
			}
			ui.Text(c, arrow).TextColor(arrowColor).Width(12)
			ui.Icon(c, folderIcon).FontSize(13).TextColor(folderColor)
			ui.Text(c, item.name).Grow(1).SingleLine().Tooltip(item.path)
		})
		if clicked {
			// The fold button consumes the pointer event before the surrounding
			// list can select its row. Select explicitly so a directory never
			// leaves a stale file diff or action target behind.
			selectRow()
			a.toggleFileTreeDirectory(scope, item.path)
		}
	})
}
