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
	fileTreeCompareScope = "compare"
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
	return buildFileTreeRowsWithDirectories(paths, nil, tree, collapsed, scope)
}

// buildFileTreeRowsWithDirectories also admits explicitly known empty
// directories. Git does not index those, but they still belong in the Editor.
func buildFileTreeRowsWithDirectories(paths, directories []string, tree bool, collapsed map[string]bool, scope string) []fileTreeRow {
	rows := make([]fileTreeRow, 0, len(paths)+len(directories))
	if !tree {
		for _, directory := range directories {
			if directory != "" {
				rows = append(rows, fileTreeRow{source: -1, path: directory, name: directory})
			}
		}
		for source, path := range paths {
			rows = append(rows, fileTreeRow{source: source, path: path, name: path})
		}
		return rows
	}

	type entry struct {
		path      string
		parts     []string
		source    int
		directory bool
	}
	ordered := make([]entry, 0, len(paths)+len(directories))
	for source, path := range paths {
		ordered = append(ordered, entry{path: path, parts: strings.Split(path, "/"), source: source})
	}
	for _, directory := range directories {
		ordered = append(ordered, entry{path: directory, parts: strings.Split(directory, "/"), source: -1, directory: true})
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.path == right.path {
			return left.directory && !right.directory
		}
		// Compare the first differing component at its actual tree level. A
		// component with descendants is a directory even when the entry itself
		// is a file, so sibling folders sort before sibling files at every depth.
		for depth := 0; depth < min(len(left.parts), len(right.parts)); depth++ {
			if left.parts[depth] == right.parts[depth] {
				continue
			}
			leftDirectory := left.directory || depth < len(left.parts)-1
			rightDirectory := right.directory || depth < len(right.parts)-1
			if leftDirectory != rightDirectory {
				return leftDirectory
			}
			return left.parts[depth] < right.parts[depth]
		}
		return len(left.parts) < len(right.parts)
	})
	seenDirectories := make(map[string]bool)
	for _, entry := range ordered {
		path := entry.path
		source := entry.source
		parts := entry.parts
		hidden := false
		directoryDepth := len(parts) - 1
		if entry.directory {
			directoryDepth = len(parts)
		}
		for depth := 0; depth < directoryDepth; depth++ {
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
		if !entry.directory && !hidden {
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

// fileTreeButtons offers flat/tree grouping and bulk folds in most file panes.
// Commit details and Compare use fileTreeModeButton alone, keeping their
// per-directory fold arrows without the two bulk-fold controls.
func (a *app) fileTreeButtons(c *ui.Context, scope string, paths []string) {
	a.fileTreeModeButton(c)
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

func (a *app) fileTreeModeButton(c *ui.Context) ui.Element {
	label, tooltip := "Show files as tree", "Group files by directory"
	if a.fileTreeMode {
		label, tooltip = "Show files as a flat list", "Show every full file path"
	}
	button := compactIconButton(ui.Button(c, "")).Label(tr(label)).Tooltip(tr(tooltip)).Checked(a.fileTreeMode)
	color := c.Theme().Text
	if a.fileTreeMode {
		button.Background(c.Theme().Accent.Alpha(0.14))
		color = c.Theme().Accent
	}
	button.Children(func() {
		ui.Icon(c, folderIcon).FontSize(14).TextColor(color)
	})
	if button.Clicked() {
		a.fileTreeMode = !a.fileTreeMode
	}
	return button
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

func (a *app) fileTreeDirectoryRow(c *ui.Context, scope string, item fileTreeRow, selected bool, selectRow func()) ui.Element {
	theme := c.Theme()
	arrow := "▾"
	action := "Collapse directory"
	if a.fileTreeCollapsed[fileTreeCollapseKey(scope, item.path)] {
		arrow = "▸"
		action = "Expand directory"
	}
	row := ui.ButtonBase(c).Grow(1).Gap(7).AlignItems(ui.Center).
		Padding(6, 8, 6, float32(8+treeIndentStep*item.depth)).
		Label(tr(action) + " " + item.path).Tooltip(item.path)
	if item.depth > 0 {
		row.Draw(func(p *ui.Painter, r ui.Rect) {
			paintTreeDepthGuides(p, item, theme, selected, r)
		})
	}
	row.Children(func() {
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
	if row.Clicked() {
		// The fold button consumes the pointer event before the surrounding
		// list can select its row. Select explicitly so a directory never
		// leaves a stale file diff or action target behind.
		selectRow()
		a.toggleFileTreeDirectory(scope, item.path)
	}
	return row
}

// treeGuideSegment is one parent rail and the elbow connecting it toward the
// current directory. Root folders have no parent and therefore no such chrome.
type treeGuideSegment struct {
	vertical, horizontal struct{ X1, Y1, X2, Y2 float32 }
}

const treeIndentStep = 12

// treeIndentSegments computes a consistent grid of vertical guides. Child
// rows keep their native padding and icons; repaint only the rail layout.
func treeIndentSegments(depth int) []treeGuideSegment {
	if depth <= 0 {
		return nil
	}
	// Git paths originate in this repository, but cap malformed/degenerate
	// groups as a precaution for display-only state supplied by an editor.
	depth = min(depth, 128)
	guides := make([]treeGuideSegment, 0, depth+1)
	for level := 0; level < depth; level++ {
		var segment treeGuideSegment
		segment.vertical.X1 = 12 + treeIndentStep*float32(level)
		segment.vertical.Y1 = 0
		segment.vertical.X2 = segment.vertical.X1
		segment.vertical.Y2 = 64
		guides = append(guides, segment)
	}
	x := 12 + treeIndentStep*float32(depth-1)
	var segment treeGuideSegment
	segment.vertical.X1 = x
	segment.vertical.Y1 = 0.5
	segment.vertical.X2 = x + treeIndentStep/2
	segment.horizontal.Y2 = 64
	guides = append(guides, treeGuideSegment{horizontal: struct{ X1, Y1, X2, Y2 float32 }{
		X1: x, Y1: 0.5, X2: x + treeIndentStep/2, Y2: 0.5,
	}})
	return guides
}

func paintTreeDepthGuides(p *ui.Painter, item fileTreeRow, theme *ui.Theme, selected bool, r ui.Rect) {
	color := treeGuideColor(theme, selected)
	for _, guide := range treeIndentSegments(item.depth) {
		line := guide.vertical
		p.Fill(ui.Rect{
			X: r.X + line.X1,
			Y: r.Y + line.Y1,
			W: 1,
			H: line.Y2 - line.Y1,
		}, color, 0)
	}
}

func treeGuideColor(theme *ui.Theme, selected bool) ui.Color {
	if theme == nil {
		theme = ui.DarkTheme()
	}
	if selected {
		return theme.AccentText.Alpha(0.62)
	}
	return theme.TextMuted.Alpha(0.34)
}
