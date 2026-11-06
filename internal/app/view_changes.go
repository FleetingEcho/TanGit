package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// changeRow is one row of the Changes list: a section header, or one of the
// section's files.
type changeRow struct {
	// header is the section's title on a header row, "" on file/directory rows.
	header string
	// count is a header's number of files.
	count int
	// directory is a tree row's full path; depth indents files and folders.
	directory string
	depth     int
	file      repository.File
	// staged says the row is in the staged section.
	staged bool
	// merge says the row is in the merge-changes section; a conflicted file
	// is resolved there rather than staged or unstaged like an ordinary one.
	merge bool
}

func (r changeRow) isHeader() bool    { return r.header != "" }
func (r changeRow) isDirectory() bool { return r.directory != "" }
func (r changeRow) isFile() bool      { return !r.isHeader() && !r.isDirectory() }

func (r changeRow) sideKey() string {
	switch {
	case r.merge:
		return "merge"
	case r.staged:
		return "staged"
	}
	return "unstaged"
}

// diffFile is the file as its section reads it: the staged section shows the
// index's side of a file that is also changed in the working tree, and the
// changes section the working tree's side. A conflicted file is resolved from
// the working tree, which holds the merge Git wrote.
func (r changeRow) diffFile() repository.File {
	file := r.file
	switch {
	case r.merge:
		file.Staged, file.Unstaged = false, true
	case r.staged:
		file.Staged, file.Unstaged = true, false
	default:
		file.Staged, file.Unstaged = false, true
	}
	return file
}

// pathMatches reports whether path contains query, ignoring case: the exact
// file search this list offers. A fuzzy match turns a two-letter query into
// hundreds of rows, which is not what a file filter should do.
func pathMatches(query, path string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(path), strings.ToLower(query))
}

// fileForChangeSide gives each row of a partially staged file the statistics
// of the diff that row opens. The snapshot also keeps their combined total for
// repository-wide operations; it is not the count of either individual side.
func fileForChangeSide(file repository.File, staged bool) repository.File {
	if !file.Staged || !file.Unstaged {
		return file
	}
	if staged {
		file.LinesAdded = file.StagedLinesAdded
		file.LinesDeleted = file.StagedLinesDeleted
	} else {
		file.LinesAdded = file.UnstagedLinesAdded
		file.LinesDeleted = file.UnstagedLinesDeleted
	}
	return file
}

// buildChangeRows lays out the Changes list: the merge changes, then the
// staged and changes sections, each with its header and, while open, its
// files. A conflicted file belongs to the merge changes alone: it is resolved,
// not staged or unstaged. The search filters every section, and ignoreUntracked
// leaves untracked files out of the changes.
func buildChangeRows(files []repository.File, search string, ignoreUntracked, mergeOpen, stagedOpen, changesOpen bool) []changeRow {
	query := strings.TrimSpace(search)
	merge := make([]changeRow, 0, len(files))
	staged := make([]changeRow, 0, len(files))
	changes := make([]changeRow, 0, len(files))
	for _, file := range files {
		if !pathMatches(query, file.Path) && !pathMatches(query, file.PreviousPath) {
			continue
		}
		if file.HasMergeConflicts {
			merge = append(merge, changeRow{file: file, merge: true})
			continue
		}
		if file.Staged {
			staged = append(staged, changeRow{file: fileForChangeSide(file, true), staged: true})
		}
		if file.Unstaged && !(ignoreUntracked && !file.Tracked) {
			changes = append(changes, changeRow{file: fileForChangeSide(file, false)})
		}
	}
	rows := make([]changeRow, 0, len(merge)+len(staged)+len(changes)+3)
	// The section is only there while something is conflicted: a repository
	// between merges has nothing to resolve and no reason to see it.
	if len(merge) > 0 {
		rows = append(rows, changeRow{header: "Merge Changes", count: len(merge), merge: true})
		if mergeOpen {
			rows = append(rows, merge...)
		}
	}
	rows = append(rows, changeRow{header: "Staged Changes", count: len(staged), staged: true})
	if stagedOpen {
		rows = append(rows, staged...)
	}
	rows = append(rows, changeRow{header: "Changes", count: len(changes)})
	if changesOpen {
		rows = append(rows, changes...)
	}
	return rows
}

// buildChangeTreeRows groups each section independently. Directory folds are
// shared across sections, while each file row retains its original staged or
// conflict side and source file.
func buildChangeTreeRows(files []repository.File, search string, ignoreUntracked, mergeOpen, stagedOpen, changesOpen bool, collapsed map[string]bool) []changeRow {
	flat := buildChangeRows(files, search, ignoreUntracked, true, true, true)
	rows := make([]changeRow, 0, len(flat))
	for start := 0; start < len(flat); {
		header := flat[start]
		rows = append(rows, header)
		end := start + 1
		for end < len(flat) && !flat[end].isHeader() {
			end++
		}
		open := changesOpen
		if header.merge {
			open = mergeOpen
		} else if header.staged {
			open = stagedOpen
		}
		if open && end > start+1 {
			section := flat[start+1 : end]
			paths := make([]string, len(section))
			for i, row := range section {
				paths[i] = row.file.Path
			}
			for _, item := range buildFileTreeRows(paths, true, collapsed, fileTreeChangesScope) {
				if item.isDirectory() {
					rows = append(rows, changeRow{
						directory: item.path, depth: item.depth,
						staged: header.staged, merge: header.merge,
					})
					continue
				}
				change := section[item.source]
				change.depth = item.depth
				rows = append(rows, change)
			}
		}
		start = end
	}
	return rows
}

// changeRowsFrom filters the snapshot's files with the list's current search,
// untracked setting and folded sections.
func (a *app) changeRowsFrom(files []repository.File) []changeRow {
	files = a.visibleReviewFiles(files)
	if a.fileTreeMode {
		return buildChangeTreeRows(files, a.fileSearch, a.ignoreUntracked, a.mergeExpanded, a.stagedExpanded, a.changesExpanded, a.fileTreeCollapsed)
	}
	return buildChangeRows(files, a.fileSearch, a.ignoreUntracked, a.mergeExpanded, a.stagedExpanded, a.changesExpanded)
}

// selectedChange is the file row the Changes list has chosen, false when the
// choice is a section header or nothing.
func (a *app) selectedChange() (changeRow, bool) {
	if a.selectedFile < 0 || a.selectedFile >= len(a.changeRows) {
		return changeRow{}, false
	}
	change := a.changeRows[a.selectedFile]
	if !change.isFile() {
		return changeRow{}, false
	}
	return change, true
}

func selectedChangeDirectory(rows []changeRow, selected int) string {
	if selected < 0 || selected >= len(rows) || !rows[selected].isDirectory() {
		return ""
	}
	return rows[selected].directory
}

func changeDirectoryIndex(rows []changeRow, path string) int {
	for i, row := range rows {
		if row.isDirectory() && row.directory == path {
			return i
		}
	}
	return -1
}

// firstChangeFile is the row of the first file in rows, -1 when there is none.
func firstChangeFile(rows []changeRow) int {
	for i, row := range rows {
		if row.isFile() {
			return i
		}
	}
	return -1
}

// changeRowIndex is the row showing path on the staged side when staged is
// true, and on the changes side otherwise, falling back to the other side. It
// is -1 when neither shows the path.
func changeRowIndex(rows []changeRow, path string, staged bool) int {
	other := -1
	for i, row := range rows {
		if !row.isFile() || row.file.Path != path {
			continue
		}
		if row.staged == staged {
			return i
		}
		if other == -1 {
			other = i
		}
	}
	return other
}

// changeKey keys a row of one section list: a section header, or a file and
// the side of the working tree it shows. The local index is enough for the
// header, since a list shows one section's rows only.
func changeKey(rows []changeRow, row int) any {
	if row < 0 || row >= len(rows) {
		return row
	}
	change := rows[row]
	if change.isHeader() {
		return "section:" + change.header
	}
	if change.isDirectory() {
		return "directory\x00" + change.directory + "\x00" + change.sideKey()
	}
	// A file that is both staged and changed shows in both sections.
	return change.file.Path + "\x00" + change.sideKey()
}

func changeLabel(rows []changeRow, row int) string {
	if row < 0 || row >= len(rows) {
		return ""
	}
	change := rows[row]
	if change.isHeader() {
		return change.header
	}
	if change.isDirectory() {
		parts := strings.Split(change.directory, "/")
		return parts[len(parts)-1]
	}
	return change.file.Path
}

// changeSections is the flat Changes rows cut by section, with the index of
// each section's first file: -1 for a section the list does not have, such as
// the merge changes while nothing is conflicted.
type changeSections struct {
	merge, staged, changes []changeRow
	mergeBase              int
	stagedBase             int
	changesBase            int
}

// splitChangeRows cuts the flat Changes rows into their sections. The rows are
// built header-first, so a header starts a section and the rows after it
// belong to it until the next header.
func splitChangeRows(rows []changeRow) changeSections {
	sections := changeSections{mergeBase: -1, stagedBase: -1, changesBase: -1}
	current := -1
	for i, row := range rows {
		if row.isHeader() {
			switch {
			case row.merge:
				sections.mergeBase, current = i+1, 0
			case row.staged:
				sections.stagedBase, current = i+1, 1
			default:
				sections.changesBase, current = i+1, 2
			}
			continue
		}
		switch current {
		case 0:
			sections.merge = append(sections.merge, row)
		case 1:
			sections.staged = append(sections.staged, row)
		case 2:
			sections.changes = append(sections.changes, row)
		}
	}
	return sections
}

// filesView is the Changes section, as VS Code's Source Control view: the
// commit message and its buttons, the file filter, then the staged and
// unstaged files. Each section is its own list, so its header stays at the top
// while its files scroll under it, and the two are set apart by a gap.
func (a *app) filesView(c *ui.Context) {
	theme := c.Theme()
	a.requestReviewedFiles()
	// Keep the choice on its file when a section folds or unfolds under it:
	// the flat index shifts when rows leave or join the list.
	chosen, hadChoice := a.selectedChange()
	directory := selectedChangeDirectory(a.changeRows, a.selectedFile)
	a.changeRows = a.changeRowsFrom(a.snapshot.Files)
	if hadChoice {
		a.selectedFile = changeRowIndex(a.changeRows, chosen.file.Path, chosen.staged)
	} else if directory != "" {
		a.selectedFile = changeDirectoryIndex(a.changeRows, directory)
	}
	sections := splitChangeRows(a.changeRows)
	a.mergeRows, a.stagedRows, a.changesRows = sections.merge, sections.staged, sections.changes
	if a.selectedFile < 0 || a.selectedFile >= len(a.changeRows) || a.changeRows[a.selectedFile].isHeader() {
		a.selectedFile = firstChangeFile(a.changeRows)
	}
	// Show the chosen file in its own section, and nothing chosen in the
	// others, so the accent does not suggest two live rows.
	a.mergeSelected = sectionSelection(a.selectedFile, sections.mergeBase, len(a.mergeRows))
	a.stagedSelected = sectionSelection(a.selectedFile, sections.stagedBase, len(a.stagedRows))
	a.changesSelected = sectionSelection(a.selectedFile, sections.changesBase, len(a.changesRows))
	islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(12)).Children(func() {
		a.changesHeader(c)
		a.commitBox(c)
		a.changesFilter(c)
		a.reviewFilterRow(c)
		if len(a.snapshot.Files) == 0 {
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, tr("Working tree clean")).TextColor(theme.TextMuted)
			})
			return
		}
		if hasConflictedFiles(a.snapshot.Files) {
			a.conflictBanner(c)
		}
		a.changeSectionsBody(c, sections)
	})
}

// changeSectionsBody lays out the sections under the filter: the merge and
// staged lists above the changes, each with a line the user drags to give its
// side more room: one between the merge changes and the staged ones while a
// merge has conflicts, and one between them and the changes. A section with no
// file in it, or one folded shut, leaves the room to the changes: there is
// nothing above them to resize.
func (a *app) changeSectionsBody(c *ui.Context, sections changeSections) {
	mergeShown := len(sections.merge) > 0 && a.mergeExpanded
	stagedShown := len(sections.staged) > 0 && a.stagedExpanded
	if !mergeShown && !stagedShown {
		ui.Column(c).Grow(1).Gap(10).Children(func() {
			a.mergeSectionView(c, sections, false)
			a.stagedSectionView(c, sections, false)
			a.changesSectionView(c, sections, true)
		})
		return
	}
	box := ui.Box(c).Grow(1).Label(tr("Changes sections"))
	box.Children(func() {
		// The sections above the changes never take the room the changes
		// need for their own header and files: with many conflicts and
		// staged files the auto height would otherwise fill the panel and
		// leave the changes section with no list to scroll.
		limit, known := changeTopLimit(c.Theme(), box.Bounds().H, len(sections.changes))
		if a.changesTopAuto {
			a.changesTopHeight = changeTopHeight(c.Theme(), sections, a.mergeExpanded, a.stagedExpanded)
			if known && a.changesTopHeight > limit {
				a.changesTopHeight = limit
			}
		}
		// The split writes a drag's new height into changesTopHeight: the
		// line moves from the auto height it shows, and the app keeps where
		// the user put it after.
		wasAuto := a.changesTopAuto
		before := a.changesTopHeight
		paneSplitVertical(c, &a.changesTopHeight, func() {
			if sections.mergeBase >= 0 {
				a.mergeSectionSplit(c, sections)
				return
			}
			ui.Column(c).Fill().Gap(10).Children(func() {
				a.stagedSectionView(c, sections, stagedShown)
			})
		}, func() {
			a.changesSectionView(c, sections, true)
		}).Fill()
		// A drag cannot push the line past what the changes keep either, so
		// dragging down shows more of the sections above without hiding the
		// changes.
		if known && a.changesTopHeight > limit {
			a.changesTopHeight = limit
		}
		if wasAuto && a.changesTopHeight != before {
			a.changesTopAuto = false
		}
	})
}

// mergeSectionSplit puts the merge changes above the staged ones with a line
// between them, so each keeps its own height and scrolls on its own. The line
// is only there while a merge has conflicts to show.
func (a *app) mergeSectionSplit(c *ui.Context, sections changeSections) {
	// The staged section keeps its header and, while it shows files, its first
	// row: the line stops above them whether it sits at its automatic height
	// or where the user dragged it, so neither drag nor a shorter window cuts
	// a file out of the section under it.
	stagedMin := changeHeaderHeight
	if a.stagedExpanded && len(sections.staged) > 0 {
		stagedMin += changeSectionGap + changeRowHeight
	}
	maxMerge := a.changesTopHeight - changeDividerHeight - stagedMin
	if maxMerge < changeHeaderHeight {
		maxMerge = changeHeaderHeight
	}
	if a.mergeAuto {
		a.mergeHeight = min(changeSectionHeight(c.Theme(), len(sections.merge), 3, a.mergeExpanded), maxMerge)
	}
	wasAuto := a.mergeAuto
	before := a.mergeHeight
	paneSplitVertical(c, &a.mergeHeight, func() {
		a.mergeSectionView(c, sections, true)
	}, func() {
		a.stagedSectionView(c, sections, true)
	}).Fill()
	// A drag writes its height after the layout, so the clamp lands on the
	// next frame, as the line under the sections does.
	if a.mergeHeight > maxMerge {
		a.mergeHeight = maxMerge
	}
	if wasAuto && a.mergeHeight != before {
		a.mergeAuto = false
	}
}

// mergeSectionView is the merge changes section, when the branch has one: it
// is only there while files are conflicted.
func changeSectionFileCount(rows []changeRow) int {
	count := 0
	for _, row := range rows {
		if row.isFile() {
			count++
		}
	}
	return count
}

func (a *app) mergeSectionView(c *ui.Context, sections changeSections, fill bool) {
	if sections.mergeBase < 0 {
		return
	}
	merge := a.changeSectionView(c, "Merge Changes", conflictIcon, changeSectionFileCount(a.mergeRows),
		a.mergeRows, &a.mergeList, sections.mergeBase, a.mergeExpanded, changeSectionMerge, fill)
	if a.keyboardListChanged(&a.mergeList, merge) && a.mergeSelected >= 0 {
		a.selectedFile = sections.mergeBase + a.mergeSelected
	}
}

// stagedSectionView is the staged changes section, which always shows its
// header even with no file.
func (a *app) stagedSectionView(c *ui.Context, sections changeSections, fill bool) {
	staged := a.changeSectionView(c, "Staged Changes", stageIcon, changeSectionFileCount(a.stagedRows),
		a.stagedRows, &a.stagedList, sections.stagedBase, a.stagedExpanded, changeSectionStaged, fill)
	if a.keyboardListChanged(&a.stagedList, staged) && a.stagedSelected >= 0 {
		a.selectedFile = sections.stagedBase + a.stagedSelected
	}
}

// changesSectionView is the changes section, which always shows its header
// even with no file.
func (a *app) changesSectionView(c *ui.Context, sections changeSections, fill bool) {
	changes := a.changeSectionView(c, "Changes", folderIcon, changeSectionFileCount(a.changesRows),
		a.changesRows, &a.fileList, sections.changesBase, a.changesExpanded, changeSectionChanges, fill)
	if a.keyboardListChanged(&a.fileList, changes) && a.changesSelected >= 0 {
		a.selectedFile = sections.changesBase + a.changesSelected
	}
}

// changeRowHeight is the room a file row is given in a section that does not
// fill its pane. It is the list's measured rows rounded up, so a section
// sized by its rows does not clip the last one.
const changeRowHeight float32 = 36

// changeHeaderHeight is the room a section header takes: its title, badge and
// actions, with the padding around them.
const changeHeaderHeight float32 = 34

// changeSectionGap is the room between a section's header and its list, as
// the column that holds them lays it out.
const changeSectionGap float32 = 4

// changeDividerHeight is the room a line between two of the changes sections
// takes: one DIP, as the split lays it out.
const changeDividerHeight float32 = 1

// changeChangesMinHeight is the least room the Changes section keeps in the
// panel: its header and the placeholder when it has no file, or its first two
// files when it has some. The sections above it stop growing there, so the
// changes always have a list of their own to scroll rather than being
// squeezed out by a long merge or staged list.
func changeChangesMinHeight(theme *ui.Theme, files int) float32 {
	if files == 0 {
		// The empty line is a text with padding around it; the text's height
		// follows the theme's font size.
		return changeHeaderHeight + changeSectionGap + theme.Rem(1.4) + 8
	}
	return changeHeaderHeight + 2*changeRowHeight + 6
}

// changeTopLimit is the tallest the merge and staged sections may be above
// the changes in a panel of the given height, and whether the height is known
// yet: the first frame has no panel to measure.
func changeTopLimit(theme *ui.Theme, panel float32, changes int) (float32, bool) {
	if panel <= 0 {
		return 0, false
	}
	limit := panel - changeChangesMinHeight(theme, changes) - changeDividerHeight
	if limit < 0 {
		limit = 0
	}
	return limit, true
}

// changeSectionHeight is the room one section above the changes asks for: its
// header, the gap under it, and its files up to capRows of them, or the line
// it shows in place of files when it has none and is open.
func changeSectionHeight(theme *ui.Theme, files, capRows int, open bool) float32 {
	if !open {
		return changeHeaderHeight
	}
	height := changeHeaderHeight + changeSectionGap
	if files == 0 {
		// The empty line is a text with padding around it; the text's height
		// follows the theme's font size.
		return height + theme.Rem(1.4) + 8
	}
	return height + float32(min(files, capRows))*changeRowHeight
}

// changeTopHeight is the room the merge and staged sections take when the
// line has not been dragged: their headers, the line between them, and their
// files, capped as the sections showed before they could be resized. The
// heights add up to whole rows, so no section is sized to less than the rows
// it draws.
func changeTopHeight(theme *ui.Theme, sections changeSections, mergeOpen, stagedOpen bool) float32 {
	height := changeSectionHeight(theme, len(sections.staged), 5, stagedOpen)
	if len(sections.merge) > 0 {
		height += changeDividerHeight + changeSectionHeight(theme, len(sections.merge), 3, mergeOpen)
	}
	return height
}

// changeSection is which part of the Changes list a section shows, which
// decides its height and the actions its header offers.
type changeSection int

const (
	changeSectionMerge changeSection = iota
	changeSectionStaged
	changeSectionChanges
)

// sectionSelection is the chosen file's index within a section starting at
// base and holding count files, or -1 when the choice is outside it.
func sectionSelection(selected, base, count int) int {
	if base < 0 || selected < base || selected >= base+count {
		return -1
	}
	return selected - base
}

// changeSectionView is one collapsible section of the Changes panel: a header
// that never scrolls, then its files in a list of their own. A section that
// fills takes the room its pane gives it, and its list scrolls under its
// header; one that does not takes the height of its rows, up to a limit.
func (a *app) changeSectionView(c *ui.Context, title string, icon *ui.SVG, count int, rows []changeRow, state *ui.ListState, base int, expanded bool, section changeSection, fill bool) ui.Element {
	var list ui.Element
	column := ui.Column(c).Gap(changeSectionGap)
	if fill {
		column.Grow(1)
	}
	column.Children(func() {
		a.changeHeaderRow(c, title, icon, count, expanded, section)
		if !expanded {
			return
		}
		if len(rows) == 0 {
			ui.Text(c, tr("No files here")).TextColor(c.Theme().TextMuted).
				FontSize(c.Theme().FontSize-1).Padding(4, 8)
			return
		}
		list = a.keyboardList(c, state, len(rows))
		if fill {
			list.Grow(1)
		} else {
			// Enough for a handful of rows: past that the section scrolls
			// under its own header instead of taking the room of the list
			// below it.
			visible := min(len(rows), 5)
			if section == changeSectionMerge {
				visible = min(len(rows), 3)
			}
			list.Height(float32(visible) * changeRowHeight)
		}
		list.Rows(func(row ui.ListRow) {
			a.changeFileRow(row, rows[row.Index], base+row.Index)
		})
	})
	return list
}

// conflictBanner explains an interrupted operation that left conflicts, so
// the resolution actions are findable before Continue can run.
func (a *app) conflictBanner(c *ui.Context) {
	theme := c.Theme()
	conflicts := conflictedFileCount(a.snapshot.Files)
	ui.Row(c).AlignItems(ui.Center).Gap(10).Padding(8, 10).
		Background(theme.Danger.Alpha(0.12)).Border(1, theme.Danger.Alpha(0.5)).
		Radius(theme.Radius).Children(func() {
		ui.Icon(c, conflictIcon).FontSize(16).TextColor(theme.Danger)
		ui.Column(c).Gap(1).Grow(1).Children(func() {
			ui.Textf(c, tr("%d conflict%s to resolve"), conflicts, plural(conflicts)).
				Bold().TextColor(theme.Danger)
			ui.Text(c, tr("Pick a side, then Continue.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine().
				Tooltip(tr("Continue stays disabled until every conflict is staged"))
		})
		if a.snapshot.Operation != "" {
			ui.Badge(c, strings.ToUpper(a.snapshot.Operation))
		}
	})
}

func plural(n int) string {
	if currentLanguage != defaultLanguage {
		return ""
	}
	if n == 1 {
		return ""
	}
	return "s"
}

// changesHeader is the panel's title row: the count of changed files, then
// the actions of the whole section as flat icons that show their face on
// hover, as VS Code's Source Control view shows them.
func (a *app) changesHeader(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, tr("Changes")).FontSize(15).Bold()
		if files := len(a.snapshot.Files); files > 0 {
			ui.Badge(c, fmt.Sprintf("%d", files)).
				Tooltip(trf("%d changed files", files))
		}
		ui.Spacer(c).Grow(1)
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			flatIconButton(c, "Commit staged changes", "Ctrl+Enter", commitIcon, 15, 5, !a.canCommit(), a.beginCommit)
			treeLabel, treeTooltip := "Show files as tree", "Group files by directory"
			if a.fileTreeMode {
				treeLabel, treeTooltip = "Show files as a flat list", "Show every full file path"
			}
			flatToggleIconButton(c, treeLabel, treeTooltip, folderIcon, 15, 5, a.fileTreeMode, func() {
				a.fileTreeMode = !a.fileTreeMode
			})
			flatIconButton(c, "Refresh", "F5", refreshIcon, 15, 5, a.actionBusy, a.beginRefresh)
			flatIconButton(c, "Apply patch…", "Preview and apply a patch or diff file (Ctrl+I)", patchImportIcon, 15, 5,
				a.actionBusy || a.session == nil, a.beginPatchImport)
			flatIconButton(c, "Stash changes…", "Stash the working tree", stashIcon, 15, 5,
				a.actionBusy || len(a.snapshot.Files) == 0, a.openStashForm)
			// The more button wears the dots itself: the toolbar widget's
			// own overflow wears chevrons, which read as a different action.
			more := ui.ButtonBase(c).Padding(5).Radius(theme.Radius)
			more.Label(tr("More actions")).Tooltip(tr("More actions"))
			more.Children(func() {
				ui.Text(c, "⋯").FontSize(16).TextColor(theme.TextMuted)
			})
			more.PopoverMenuButton(func(m *ui.Menu) {
				if m.Item(tr("Apply patch…")).Disabled(a.actionBusy || a.session == nil).Chosen() {
					a.beginPatchImport()
				}
				m.Separator()
				if m.Item(tr("Commit with a description…")).Disabled(a.actionBusy || !hasStagedFiles(a.snapshot.Files)).Chosen() {
					a.showCommitForm = true
				}
				if m.Item(tr("Commit (amend last)…")).Disabled(!a.canCommit()).Chosen() {
					a.commitAmend = true
					a.beginCommit()
				}
				m.Separator()
				if m.Item(tr("Expand all sections")).Chosen() {
					a.mergeExpanded, a.stagedExpanded, a.changesExpanded = true, true, true
				}
				if m.Item(tr("Collapse all sections")).Chosen() {
					a.mergeExpanded, a.stagedExpanded, a.changesExpanded = false, false, false
				}
				m.Separator()
				a.fileTreeMenuItems(m, fileTreeChangesScope, changeFilePaths(a.snapshot.Files))
			})
			if more.Hovered() {
				more.Background(theme.SurfaceHover)
			}
			paneCollapseButton(c, &a.filePaneCollapsed, "Collapse file list")
		})
	})
}

// commitBox is the commit message and its button in one card, under the
// panel's title: the message, then the branch it lands on and the Commit
// button on one line.
func (a *app) commitBox(c *ui.Context) {
	theme := c.Theme()
	on := a.snapshot.Branch
	if on == "" {
		on = "HEAD"
	}
	card := ui.Column(c).Padding(10).Gap(8).
		Background(theme.Surface).Border(1, theme.Border).
		Radius(theme.Radius + 2)
	if card.FocusWithin() {
		card.Border(1, theme.Accent)
	}
	card.Children(func() {
		ui.TextInputBase(c, &a.commitSummary).
			Padding(4, 2).Grow(1).
			Label(tr("Commit message")).
			Placeholder(tr("Commit message")).
			Tooltip(trf("Ctrl+Enter commits on '%s'", on))
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(5).Grow(1).Children(func() {
				ui.Icon(c, branchIcon).FontSize(12).TextColor(theme.TextMuted)
				ui.Text(c, trf("Commit on %s", on)).TextColor(theme.TextMuted).
					FontSize(theme.FontSize - 2).SingleLine().Tooltip(on)
			})
			commit := ui.PrimaryButton(c, "").Label(tr("Commit")).
				Gap(6).Tooltip(tr("Commit the staged changes (Ctrl+Enter)")).
				Disabled(!a.canCommit())
			commit.Children(func() {
				ui.Icon(c, commitIcon).FontSize(13).TextColor(theme.AccentText)
				ui.Text(c, tr("Commit"))
			})
			if commit.Clicked() {
				a.beginCommit()
			}
		})
	})
}

// canCommit reports whether there is a message and staged files to commit.
func (a *app) canCommit() bool {
	return !a.actionBusy && strings.TrimSpace(a.commitSummary) != "" && hasStagedFiles(a.snapshot.Files)
}

// changesFilter is the quick file search and the untracked files toggle.
func (a *app) changesFilter(c *ui.Context) {
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.SearchField(c, &a.fileSearch).Bind(&a.sectionSearch[sectionChanges]).
			Label(tr("Search files")).
			Placeholder(tr("Search files")).
			Grow(1)
		a.searchNavigationButtons(c, strings.TrimSpace(a.fileSearch) != "")
		a.changesFilterPopover(c)
	})
}

// changesFilterPopover keeps the quick filters close to their icon without
// hiding the results behind a modal. Changes apply immediately while the
// popover stays open, and its trigger retains the active-filter indication.
func (a *app) changesFilterPopover(c *ui.Context) {
	anchor := flatToggleIconButton(c, "File filters", "Show file filters", filterIcon, 14, 5,
		a.ignoreUntracked, func() {
			a.changesFiltersOpen = !a.changesFiltersOpen
		})
	anchor.HasPopup(ui.PopupDialog).Expanded(a.changesFiltersOpen)
	ui.Popover(c, anchor, &a.changesFiltersOpen, func() {
		ui.Column(c).Width(240).Gap(10).Padding(8).Children(func() {
			ui.Text(c, tr("File filters")).Bold()
			ui.Checkbox(c, &a.ignoreUntracked, tr("Ignore untracked files")).
				Tooltip(tr("Leave untracked files out of the changes"))
			ui.Row(c).Justify(ui.End).Children(func() {
				clear := compactTextButton(ui.Button(c, tr("Clear filters"))).
					Disabled(!a.ignoreUntracked)
				if clear.Clicked() {
					a.ignoreUntracked = false
				}
			})
		})
	})
}

// reviewFilterRow holds review progress and traversal outside the crowded
// panel header. Its filter is content-current, not a second staging filter.
func (a *app) reviewFilterRow(c *ui.Context) {
	reviewed, total := a.reviewProgress()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, trf("%d/%d reviewed", reviewed, total)).
			FontSize(c.Theme().FontSize - 1).TextColor(c.Theme().TextMuted).
			Tooltip(tr(reviewLocalStatusMessage) + " " + tr("Changed content automatically becomes unreviewed"))
		ui.Spacer(c).Grow(1)
		ui.Checkbox(c, &a.reviewFilter, tr("Unreviewed")).
			Tooltip(tr("Show only files whose current content still needs review"))
		flatIconButton(c, "Next unreviewed", "Go to the next unreviewed file (Shift+R)", chevronDownIcon, 14, 4,
			!a.hasUnreviewedFiles(), a.selectNextUnreviewed)
	})
}

// changeHeaderRow is a section's title row: a click folds or unfolds the
// section, and the badge counts its files. It sits outside the section's list,
// so it stays put while the files scroll.
func (a *app) changeHeaderRow(c *ui.Context, title string, icon *ui.SVG, count int, open bool, section changeSection) {
	theme := c.Theme()
	arrow := "▾"
	if !open {
		arrow = "▸"
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(5, 8).
		Background(theme.Surface).Radius(theme.Radius).Children(func() {
		button := ui.ButtonBase(c).Grow(1).Padding(2, 0)
		clicked := button.Label(tr(title)).Tooltip(tr("Show or hide this section")).Clicked()
		button.Gap(8).Children(func() {
			ui.Text(c, arrow).TextColor(theme.TextMuted).Width(12)
			iconColor := theme.Accent
			if section == changeSectionMerge {
				iconColor = theme.Danger
			}
			ui.Icon(c, icon).FontSize(14).TextColor(iconColor)
			ui.Text(c, tr(title)).Bold()
			ui.Spacer(c).Grow(1)
			ui.Badge(c, fmt.Sprintf("%d", count))
		})
		if clicked {
			switch section {
			case changeSectionMerge:
				a.mergeExpanded = !a.mergeExpanded
			case changeSectionStaged:
				a.stagedExpanded = !a.stagedExpanded
			default:
				a.changesExpanded = !a.changesExpanded
			}
		}
		// The merge changes are resolved file by file, so their header has
		// no action on them all. Each other section carries the action on
		// its own files, as Source Control does: the staged list offers to
		// unstage them all, and the changes list to stage or throw them all
		// away.
		if section == changeSectionMerge {
			return
		}
		if section == changeSectionStaged {
			flatIconButton(c, "Stash staged changes", "Stash the staged files, leaving the rest in the tree", stashIcon, 14, 4,
				a.actionBusy || !hasStagedFiles(a.snapshot.Files), a.stashStagedChanges)
			flatIconButton(c, "Unstage all changes", "Unstage every file", unstageIcon, 14, 4,
				a.actionBusy || !hasStagedFiles(a.snapshot.Files), func() {
					a.beginRepositoryAction("", func(ctx context.Context, session repository.Session) error {
						return session.UnstageAll(ctx)
					}, nil)
				})
			return
		}
		flatIconButton(c, "Stage all changes", "Stage every change", stageIcon, 14, 4,
			a.actionBusy || !hasUnstagedFiles(a.snapshot.Files), func() {
				a.beginRepositoryAction("", func(ctx context.Context, session repository.Session) error {
					return session.StageAll(ctx)
				}, nil)
			})
		flatIconButton(c, "Discard all changes", "Discard every change", revertIcon, 14, 4,
			a.actionBusy || len(a.snapshot.Files) == 0, a.confirmDiscardAllChanges)
	})
}

// changeFileRow is one changed file: its path, the icons that stage, unstage
// or discard it, and its status at the row's end. A conflicted file offers a
// side instead, and staging it marks it resolved. A click on the row opens its
// diff.
func (a *app) changeFileRow(row ui.ListRow, change changeRow, index int) {
	theme := row.Context.Theme()
	if change.isDirectory() {
		a.fileTreeDirectoryRow(row.Context, fileTreeChangesScope, fileTreeRow{
			source: -1, path: change.directory, name: changeLabel([]changeRow{change}, 0), depth: change.depth,
		}, row.Selected(), func() {
			a.selectedFile = index
			a.resetFileDiff()
		})
		return
	}
	file := change.file
	services := row.Context.Services()
	// The room at the end keeps the list's scroll bar, which shows while the
	// pointer is on the row, off the path and the status letter.
	element := ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(5, 8+scrollbarGutter(theme), 5, float32(8+16*change.depth))
	// The row's actions show on the pointer's row, or the chosen one, so the
	// path keeps its room at rest, as in VS Code's Source Control view.
	reveal := element.Hovered() || row.Selected()
	element.Children(func() {
		fileTypeMark(row.Context, file.Path, 13, row.Selected())
		displayPath := file.Path
		if a.fileTreeMode {
			parts := strings.Split(file.Path, "/")
			displayPath = parts[len(parts)-1]
		}
		ui.Column(row.Context).Gap(1).Grow(1).Children(func() {
			ui.Text(row.Context, displayPath).SingleLine().Tooltip(file.Path)
			if file.PreviousPath != "" {
				ui.Textf(row.Context, tr("from %s"), file.PreviousPath).
					TextColor(secondaryRowColor(theme, row)).
					SingleLine()
			}
		})
		if reveal {
			reviewLabel := "Mark reviewed " + file.Path
			reviewIcon := markReviewedIcon
			if a.reviewedFiles[file.Path] {
				reviewLabel = "Mark unreviewed " + file.Path
				reviewIcon = markUnreviewedIcon
			}
			rowIconButton(row.Context, reviewLabel, reviewActionTooltip(), reviewIcon, func() {
				a.setFileReviewed(file, !a.reviewedFiles[file.Path])
			})
		} else if a.reviewedFiles[file.Path] {
			// Badge builds its text before TextColor can be chained onto it. Build
			// this small pill directly so its label gets the contrasting
			// foreground instead of inheriting the row's success green.
			ui.Row(row.Context).Padding(0, theme.Space(1.5)).Radius(999).
				Background(theme.Success).Shrink(0).
				Tooltip(tr(reviewLocalStatusMessage)).Children(func() {
				ui.Text(row.Context, tr("Reviewed")).FontSize(theme.FontSize - 2).
					TextColor(theme.Background).SingleLine()
			})
		}
		if file.HasMergeConflicts {
			if reveal {
				rowIconButton(row.Context, "Use current "+file.Path, "Resolve with the current side", useCurrentIcon, func() {
					a.resolveConflictFile(file, true)
				})
				rowIconButton(row.Context, "Use incoming "+file.Path, "Resolve with the incoming side", useIncomingIcon, func() {
					a.resolveConflictFile(file, false)
				})
				rowIconButton(row.Context, "Mark resolved "+file.Path, "Mark resolved by staging the file as it is", commitIcon, func() {
					a.beginStageFile(file, true)
				})
			} else {
				ui.Badge(row.Context, tr("Conflict")).Background(theme.Danger).TextColor(ui.Hex("#ffffff"))
			}
		} else {
			fileDiffStatCounts(row.Context, file.Path, file.LinesAdded, file.LinesDeleted)
			if reveal {
				if change.staged {
					// A staged row offers only unstage: discarding there would
					// throw away the index and working tree at once, so the
					// action stays in the row's context menu.
					rowIconButton(row.Context, "Unstage "+file.Path, "Unstage this file", unstageIcon, func() {
						a.beginStageFile(file, false)
					})
				} else {
					rowIconButton(row.Context, "Stage "+file.Path, "Stage this file", stageIcon, func() {
						a.beginStageFile(file, true)
					})
					rowIconButton(row.Context, "Discard "+file.Path, "Discard changes…", revertIcon, func() {
						a.confirmDiscardFile(file)
					})
				}
			}
		}
		statusSlot(row.Context, changeStatusLetter(change),
			rowStatusColor(theme, row, statusColor(theme, file)))
	})
	element.PopoverMenu(func(m *ui.Menu) { a.fileRowMenu(m, index, services) })
}

// changeStatusLetter is the one letter a change wears at the row's end. The
// section already says whether the change is staged, so the side's letter of
// Git's two-character code is enough; a file Git does not track yet reads as
// U and a conflict as !, as VS Code does.
func changeStatusLetter(change changeRow) string {
	file := change.file
	if file.HasMergeConflicts {
		return "!"
	}
	status := file.Status
	letter := byte(0)
	switch {
	case change.staged && status != "":
		letter = status[0]
	case !change.staged && len(status) > 1:
		letter = status[1]
	case status != "":
		letter = status[0]
	}
	switch letter {
	case '?':
		return "U"
	case ' ':
		letter = 0
	}
	if letter == 0 {
		switch {
		case file.Added:
			return "A"
		case file.Deleted:
			return "D"
		}
		return "M"
	}
	return string(letter)
}

// resolveConflictFile replaces a conflicted file with one side and stages it,
// which is how Git records that the conflict is resolved.
func (a *app) resolveConflictFile(file repository.File, current bool) {
	if a.actionBusy || a.session == nil {
		return
	}
	side := tr("Use incoming")
	if current {
		side = tr("Use current")
	}
	a.confirmAttention("Resolve conflict", file.Path+"\n"+side, "This replaces the entire conflicted file with the chosen side and stages it. Manual resolutions in the file may be lost.", func() {
		a.beginRepositoryAction(file.Path, func(ctx context.Context, session repository.Session) error {
			return session.ResolveConflict(ctx, file, current)
		}, nil)
	})
}

// beginStageFile stages or unstages one file, whichever row asked for it.
func (a *app) beginStageFile(file repository.File, stage bool) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.beginRepositoryAction(file.Path, func(ctx context.Context, session repository.Session) error {
		if stage {
			return session.Stage(ctx, file)
		}
		return session.Unstage(ctx, file)
	}, nil)
}

// iconButton is a compact panel button showing icon, with label for
// accessibility and tooltip for the pointer.
func iconButton(c *ui.Context, label, tooltip string, icon *ui.SVG, disabled bool, run func()) {
	render := func() {
		button := compactIconButton(ui.Button(c, ""))
		if !disabled {
			button.Tooltip(tr(tooltip))
		}
		clicked := button.Label(tr(label)).Disabled(disabled).Clicked()
		button.Children(func() {
			ui.Icon(c, icon).FontSize(14)
		})
		if clicked {
			run()
		}
	}
	if disabled {
		// Disabled buttons do not receive pointer hover, so the wrapper owns
		// the tooltip that explains why the action is unavailable.
		ui.Row(c).Tooltip(tr(tooltip)).Children(render)
		return
	}
	render()
}

// rowIconButton is the small flat icon button of a file row. Its padding and
// glyph are sized to fit the row's line, so revealing it on hover does not
// change the row's height.
func rowIconButton(c *ui.Context, label, tooltip string, icon *ui.SVG, run func()) {
	button := ui.ButtonBase(c).Padding(2)
	clicked := button.Label(tr(label)).Tooltip(tr(tooltip)).Clicked()
	button.Children(func() {
		ui.Icon(c, icon).FontSize(rowActionIconSize(c.Theme()))
	})
	if clicked {
		run()
	}
}
