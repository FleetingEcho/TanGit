package app

import (
	"hash/fnv"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
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

// filePathsMemoKey keys the path slice built from one commit/stash files
// slice; the files slices the detail panes hold are replaced wholesale on
// reload, never edited in place, so the slice identity keys the memo.
type filePathsMemoKey struct {
	first  *repository.CommitFile
	length int
}

// pathsForFiles returns the paths of a commit's files, memoized by the files
// slice identity and its length.
func (a *app) pathsForFiles(files []repository.CommitFile) []string {
	var first *repository.CommitFile
	if len(files) > 0 {
		first = &files[0]
	}
	key := filePathsMemoKey{first: first, length: len(files)}
	if cached, ok := a.filePathsMemo[key]; ok {
		return cached
	}
	paths := commitFilePaths(files)
	if a.filePathsMemo == nil || len(a.filePathsMemo) >= 32 {
		a.filePathsMemo = make(map[filePathsMemoKey][]string)
	}
	a.filePathsMemo[key] = paths
	return paths
}

// collapsedSignature names a collapsed-directories map for the rows memo:
// the map is edited in place, so its contents are fingerprinted in key order.
func collapsedSignature(collapsed map[string]bool) uint64 {
	if len(collapsed) == 0 {
		return 0
	}
	keys := make([]string, 0, len(collapsed))
	for key := range collapsed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	h := fnv.New64a()
	for _, key := range keys {
		_, _ = h.Write([]byte(key))
		if collapsed[key] {
			_, _ = h.Write([]byte{1})
		} else {
			_, _ = h.Write([]byte{0})
		}
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

type fileTreeRowsMemoKeyType struct {
	scope     string
	first     *repository.CommitFile
	length    int
	mode      bool
	collapsed uint64
}

// fileTreeRowsFor memoizes the visible tree rows per (scope, files identity,
// layout mode, collapsed signature); idle frames reuse them verbatim.
func (a *app) fileTreeRowsFor(scope string, files []repository.CommitFile, paths []string) []fileTreeRow {
	collapsed := collapsedSignature(a.fileTreeCollapsed)
	var first *repository.CommitFile
	if len(files) > 0 {
		first = &files[0]
	}
	key := fileTreeRowsMemoKeyType{scope: scope, first: first, length: len(files), mode: a.fileTreeMode, collapsed: collapsed}
	if cached, ok := a.fileTreeRowsMemo[key]; ok {
		return cached
	}
	rows := a.fileTreeRows(scope, paths)
	if a.fileTreeRowsMemo == nil || len(a.fileTreeRowsMemo) >= 32 {
		a.fileTreeRowsMemo = make(map[fileTreeRowsMemoKeyType][]fileTreeRow)
	}
	a.fileTreeRowsMemo[key] = rows
	return rows
}

func (a *app) syncFileTreeRows(scope string, files []repository.CommitFile, previous []fileTreeRow, selectedRow, selectedSource *int) []fileTreeRow {
	paths := a.pathsForFiles(files)
	directory := selectedFileTreeDirectory(previous, *selectedRow)
	rows := a.fileTreeRowsFor(scope, files, paths)
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

const treeIndentStep = 12

// treeGuideGroup is the branch of one expanded parent in the actual row
// order. parent and end are inclusive row indexes: end is its last visible
// descendant after folds are applied, not a depth-derived guess.
type treeGuideGroup struct {
	parent, end int
	depth       int
}

// treeGuideLayout keys each real branch by its parent directory's full path.
type treeGuideLayout map[string]treeGuideGroup

// treeGuideGroups extracts the visible tree's skeleton once, before a List
// virtualizes its rows. A parent without visible descendants has no branch.
func treeGuideGroups(rows []fileTreeRow) treeGuideLayout {
	type openGroup struct {
		path  string
		group treeGuideGroup
	}
	groups := make(treeGuideLayout)
	var open []openGroup
	closeGroup := func(group openGroup, end int) {
		if end > group.group.parent {
			group.group.end = end
			groups[group.path] = group.group
		}
	}
	for index, item := range rows {
		depth := max(item.depth, 0)
		for len(open) > 0 && depth <= open[len(open)-1].group.depth {
			last := len(open) - 1
			closeGroup(open[last], index-1)
			open = open[:last]
		}
		if item.isDirectory() && item.path != "" {
			open = append(open, openGroup{
				path:  item.path,
				group: treeGuideGroup{parent: index, depth: depth},
			})
		}
	}
	for len(open) > 0 {
		last := len(open) - 1
		closeGroup(open[last], len(rows)-1)
		open = open[:last]
	}
	return groups
}

// treeGuideSegment is in one row's coordinates. X values are DIPs; Y values
// are fractions of that row's height. Adjacent rows therefore meet at exactly
// the same absolute Y even when their heights differ.
type treeGuideSegment struct {
	X1, Y1, X2, Y2 float32
}

// segments clips the parent's branch intervals to a row that is actually
// built. It also adds the child's horizontal elbow. Root rows never acquire
// an incoming guide; their outgoing rail exists only with visible children.
func (groups treeGuideLayout) segments(item fileTreeRow, row int) []treeGuideSegment {
	if len(groups) == 0 || row < 0 || item.depth < 0 {
		return nil
	}
	depth := min(item.depth, 128)
	var segments []treeGuideSegment
	parts := strings.Split(item.path, "/")
	for level := 0; level < min(depth, len(parts)-1); level++ {
		group, ok := groups[strings.Join(parts[:level+1], "/")]
		if !ok || group.depth != level || row <= group.parent || row > group.end {
			continue
		}
		x := float32(11 + treeIndentStep*level)
		y2 := float32(1)
		if group.end == row {
			y2 = 0.5
		}
		segments = append(segments, treeGuideSegment{X1: x, X2: x, Y2: y2})
		if level == depth-1 {
			// Start beside the vertical's one DIP, sharing an edge rather than
			// blending the elbow's pixel twice when this is the last child.
			segments = append(segments, treeGuideSegment{
				X1: x + 1, Y1: 0.5, X2: x + treeIndentStep/2, Y2: 0.5,
			})
		}
	}
	if item.isDirectory() {
		if group, ok := groups[item.path]; ok && group.parent == row && group.depth == depth {
			x := float32(11 + treeIndentStep*depth)
			segments = append(segments, treeGuideSegment{X1: x, Y1: 0.5, X2: x, Y2: 1})
		}
	}
	return segments
}

// decorate paints only this row's portion of the shared parent branches.
// There is one uniform-alpha stroke at any pixel, and an off-screen parent
// is unnecessary: every built descendant carries the same extracted branch.
func (groups treeGuideLayout) decorate(element ui.Element, item fileTreeRow, row ui.ListRow) ui.Element {
	segments := groups.segments(item, row.Index)
	if len(segments) == 0 {
		return element
	}
	color := treeGuideColor(row.Context.Theme(), row.Selected())
	return element.Draw(func(p *ui.Painter, r ui.Rect) {
		for _, line := range segments {
			if line.X1 == line.X2 {
				height := (line.Y2 - line.Y1) * r.H
				if line.Y1 == 0 && line.Y2 == 0.5 {
					// Fill's lower edge is exclusive. Include the midpoint pixel
					// so the terminal vertical and its elbow share an edge, not
					// merely diagonal corners, without entering the next row.
					height++
				}
				p.Fill(ui.Rect{
					X: r.X + line.X1, Y: r.Y + line.Y1*r.H,
					W: 1, H: height,
				}, color, 0)
				continue
			}
			p.Fill(ui.Rect{
				X: r.X + line.X1, Y: r.Y + line.Y1*r.H,
				W: line.X2 - line.X1, H: 1,
			}, color, 0)
		}
	})
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
