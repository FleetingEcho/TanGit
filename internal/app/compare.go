package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

// comparisonState owns the page's drafts, applied read options, and results.
// Applied values never alias the editable form: a background read keeps the
// refs and options it started with even when the user changes the next one.
type comparisonState struct {
	from    string
	to      string
	context string
	options repository.DiffOptions

	appliedFrom    string
	appliedTo      string
	appliedOptions repository.DiffOptions
	files          []repository.CommitFile
	filesLoading   bool
	filesError     string

	fileSearch  string
	fileRows    []fileTreeRow
	selected    int
	selectedRow int
	list        ui.ListState
	listWidth   float32

	diffKey          string
	diffPatch        string
	diffState        *editor.DiffState
	diffLoading      bool
	diffError        string
	wholeDiffLoading bool
	token            uint64
}

// openCompare switches to the comparison page without disturbing another
// section's selection or diff. The latest commit is a useful default only when
// it has a parent; a root-only repository asks for its starting ref explicitly.
func (a *app) openCompare() {
	if a.state != stateReady || a.session == nil {
		return
	}
	if !a.sectionVisible(sectionCompare) {
		a.settings.VisibleSections = append(a.settings.VisibleSections, workspaceSectionIDs[sectionCompare])
		a.settings.VisibleSections = normalizeVisibleSections(a.settings.VisibleSections)
	}
	a.setWorkspaceTab(sectionCompare)
	if a.compare.context == "" {
		a.compare.context = "3"
	}
	if a.compare.from == "" && a.compare.to == "" && len(a.snapshot.Commits) > 0 {
		a.compare.to = "HEAD"
		if len(a.snapshot.Commits[0].Parents) > 0 {
			a.compare.from = "HEAD~1"
		}
	}
}

// closeCompare rejects anything the comparison page's reads still have in
// flight. Completed results remain so a new comparison can reuse its options.
func (a *app) closeCompare() {
	a.compare.token++
	a.compare.filesLoading = false
	a.compare.diffLoading = false
	a.compare.wholeDiffLoading = false
	a.compare.diffKey = ""
}

// resetComparison drops every ref and result belonging to the old repository.
func (a *app) resetComparison() {
	a.closeCompare()
	a.compare.from = ""
	a.compare.to = ""
	a.compare.context = "3"
	a.compare.options = repository.DiffOptions{ContextLines: 3}
	a.compare.appliedFrom = ""
	a.compare.appliedTo = ""
	a.compare.appliedOptions = repository.DiffOptions{}
	a.compare.files = nil
	a.compare.filesError = ""
	a.compare.fileSearch = ""
	a.compare.fileRows = nil
	a.compare.selected = -1
	a.compare.selectedRow = -1
	a.compare.diffKey = ""
	a.compare.diffPatch = ""
	a.compare.diffState = nil
	a.compare.diffError = ""
	a.compare.wholeDiffLoading = false
}

// resetComparePage restores the initial controls without reloading results.
func (a *app) resetComparePage() {
	a.resetComparison()
	a.openCompare()
}

// beginCompare applies the current drafts and starts a new file-list read.
// Changing either refs or an option invalidates both the list and file diff.
func (a *app) beginCompare() {
	if a.session == nil || a.compare.filesLoading {
		return
	}
	from, to := strings.TrimSpace(a.compare.from), strings.TrimSpace(a.compare.to)
	if from == "" || to == "" {
		a.compare.filesError = tr("Choose both refs to compare.")
		return
	}
	contextLines, err := strconv.Atoi(a.compare.context)
	if err != nil || contextLines < 0 {
		a.compare.filesError = tr("Choose a valid diff context.")
		return
	}
	options := repository.DiffOptions{
		IgnoreWhitespace: a.compare.options.IgnoreWhitespace,
		ContextLines:     contextLines,
	}
	a.compare.from, a.compare.to = from, to
	a.compare.options = options
	a.compare.appliedFrom, a.compare.appliedTo = from, to
	a.compare.appliedOptions = options
	a.compare.files = nil
	a.compare.filesLoading = true
	a.compare.filesError = ""
	a.compare.selected = -1
	a.compare.selectedRow = -1
	a.compare.diffKey = ""
	a.compare.diffPatch = ""
	a.compare.diffState = nil
	a.compare.diffLoading = false
	a.compare.diffError = ""
	a.compare.wholeDiffLoading = false
	a.compare.token++

	session := a.session
	generation := a.generation
	token := a.compare.token
	go func() {
		defer a.recoverBackground()
		files, err := session.CompareFiles(context.Background(), from, to, options)
		a.update(func() {
			if generation != a.generation || token != a.compare.token {
				return
			}
			a.compare.filesLoading = false
			if err != nil {
				a.compare.filesError = safeError(err)
				return
			}
			if files == nil {
				files = []repository.CommitFile{}
			}
			a.compare.files = files
			if len(files) > 0 {
				a.compare.selected = 0
			}
		})
	}()
}

// refreshAppliedCompare reloads after an option change only once a comparison
// has been applied; editing a fresh form does not start reads on every click.
func (a *app) refreshAppliedCompare() {
	if a.compare.appliedFrom != "" && !a.compare.filesLoading {
		a.beginCompare()
		return
	}
	if a.compare.appliedFrom != "" {
		// A new choice supersedes the read in flight. Allow the replacement.
		a.compare.filesLoading = false
		a.beginCompare()
	}
}

// swapCompare reverses the drafts and immediately reverses an applied result.
func (a *app) swapCompare() {
	a.compare.from, a.compare.to = a.compare.to, a.compare.from
	if a.compare.appliedFrom != "" {
		if a.compare.filesLoading {
			a.compare.filesLoading = false
		}
		a.beginCompare()
	}
}

// syncCompareFileRows filters without losing the source index Git actions use,
// then adds the shared directory hierarchy. Filtering or changing modes moves
// the selection to the first visible file when its previous file is hidden.
func (a *app) syncCompareFileRows() []fileTreeRow {
	previous := a.compare.selected
	visible := filteredSourceRows(len(a.compare.files), a.compare.fileSearch, func(index int) string {
		file := a.compare.files[index]
		return file.Path + "\n" + file.PreviousPath
	})
	paths := make([]string, len(visible))
	for index, source := range visible {
		paths[index] = a.compare.files[source].Path
	}
	rows := a.fileTreeRows(fileTreeCompareScope, paths)
	for index := range rows {
		if !rows[index].isDirectory() {
			rows[index].source = visible[rows[index].source]
		}
	}
	if directory := selectedFileTreeDirectory(a.compare.fileRows, a.compare.selectedRow); directory != "" {
		a.compare.selectedRow = fileTreeDirectoryIndex(rows, directory)
		a.compare.selected = -1
		if a.compare.selectedRow < 0 {
			syncFileTreeRow(rows, &a.compare.selectedRow, &a.compare.selected)
		}
	} else {
		syncFileTreeRow(rows, &a.compare.selectedRow, &a.compare.selected)
	}
	if a.compare.selected != previous {
		a.clearCompareDiff()
	}
	return rows
}

// selectCompareFileRow separates visible tree rows from their source files. A
// directory has no diff, so choosing one also drops any old file patch.
func (a *app) selectCompareFileRow(row int) {
	previous := a.compare.selected
	selectFileTreeRow(a.compare.fileRows, row, &a.compare.selectedRow, &a.compare.selected)
	if a.compare.selected != previous || a.compare.selected < 0 {
		a.clearCompareDiff()
	}
}

func (a *app) clearCompareDiff() {
	a.compare.diffKey = ""
	a.compare.diffPatch = ""
	a.compare.diffState = nil
	a.compare.diffLoading = false
	a.compare.diffError = ""
}

// selectCompareSource also serves the full-window file list, where changing a
// file must start its read even though the ordinary page is behind the overlay.
func (a *app) selectCompareSource(source int) {
	previous := a.compare.selected
	a.compare.selected = source
	a.compare.fileRows = a.syncCompareFileRows()
	if a.compare.selected != previous || a.compare.selected < 0 {
		a.clearCompareDiff()
	}
	a.requestCompareFileDiff()
}

// requestCompareFileDiff reads the selected file with exactly the options that
// produced its list. diffKey rejects a slower response for a previous file;
// token and generation reject responses for a previous comparison or repo.
func (a *app) requestCompareFileDiff() {
	if a.compare.filesLoading || a.compare.filesError != "" ||
		a.compare.selected < 0 || a.compare.selected >= len(a.compare.files) {
		return
	}
	file := a.compare.files[a.compare.selected]
	if a.compare.diffKey == file.Path {
		return
	}
	a.compare.diffKey = file.Path
	a.compare.diffPatch = ""
	a.compare.diffState = nil
	a.compare.diffLoading = true
	a.compare.diffError = ""

	session := a.session
	generation := a.generation
	token := a.compare.token
	from, to := a.compare.appliedFrom, a.compare.appliedTo
	options := a.compare.appliedOptions
	layout, wrap := a.diffLayout(), a.diffWrap
	go func() {
		defer a.recoverBackground()
		diff, err := session.CompareFileDiff(context.Background(), from, to, file, options)
		var state *editor.DiffState
		if err == nil && (diff.Patch != "" || diff.OldText != "" || diff.NewText != "") {
			state, err = buildDiffState(layout, wrap, repository.File{Path: file.Path}, diff, options)
		}
		a.update(func() {
			if generation != a.generation || token != a.compare.token ||
				a.compare.diffKey != file.Path {
				return
			}
			a.compare.diffLoading = false
			if err != nil {
				a.compare.diffError = safeError(err)
				return
			}
			a.compare.diffPatch = diff.Patch
			a.compare.diffState = state
		})
	}()
}

// comparePage puts display options beside the title and both refs in one
// compact action row, leaving the rest of the workspace to files and diffs.
const compareDiffActionSize float32 = 22

// compareDiffActionButton is deliberately smaller than the ordinary diff
// toolbar: these actions sit beside the file title rather than on their own row.
func compareDiffActionButton(c *ui.Context, label, tooltip string, icon *ui.SVG, active, disabled bool, run func()) {
	var button ui.Element
	if active {
		button = ui.PrimaryButton(c, "")
	} else {
		button = ui.Button(c, "")
	}
	clicked := button.Size(compareDiffActionSize, compareDiffActionSize).Padding(0).Center().
		Label(tr(label)).Tooltip(tr(tooltip)).Disabled(disabled).Clicked()
	button.Children(func() {
		ui.Icon(c, icon).FontSize(12)
	})
	if clicked {
		run()
	}
}

// compareDiffViewOptions keeps the patch-read and presentation actions in one
// compact title-bar group, followed by download and fullscreen.
func (a *app) compareDiffViewOptions(c *ui.Context, fullscreen bool) {
	ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
		compareDiffActionButton(c, "Toggle whitespace-only diff changes", "Alt+W · Hide or show whitespace-only changes",
			diffWhitespaceIcon, a.compare.options.IgnoreWhitespace, false, a.toggleDiffWhitespace)
		contextButton := ui.Button(c, "").Size(compareDiffActionSize, compareDiffActionSize).Padding(0).Center()
		contextClicked := contextButton.Label(tr("Change diff context lines")).
			Tooltip(tr("Alt+U · Cycle through 0, 1, 3, 5, and 10 context lines")).Clicked()
		contextButton.Children(func() {
			ui.Row(c).Gap(1).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, diffContextIcon).FontSize(10)
				ui.Text(c, strconv.Itoa(a.compare.options.ContextLines)).FontSize(c.Theme().FontSize - 5)
			})
		})
		if contextClicked {
			a.cycleDiffContext()
		}

		layoutTooltip, layoutIcon := "Switch to side-by-side diff", diffSplitIcon
		if a.diffSplit {
			layoutTooltip, layoutIcon = "Switch to unified diff", diffUnifiedIcon
		}
		compareDiffActionButton(c, "Toggle diff layout", layoutTooltip, layoutIcon, false, false, a.toggleDiffLayout)

		wrapTooltip := "Long lines scroll sideways — click to wrap them"
		if a.diffWrap {
			wrapTooltip = "Long lines are wrapped — click to scroll them sideways"
		}
		compareDiffActionButton(c, "Toggle diff line wrapping", wrapTooltip, diffWrapIcon, a.diffWrap, false, a.toggleDiffWrap)

		shown, ok := a.activeDiff()
		compareDiffActionButton(c, "Save diff", "Save this diff to a file…", downloadIcon,
			false, !ok || shown.patch == "", a.exportDiff)
		if fullscreen {
			compareDiffActionButton(c, "Restore diff view", "Restore diff view", shrinkIcon, false, false, a.closeDiffFullscreen)
		} else {
			compareDiffActionButton(c, "Expand diff", "Review this diff in the whole window", expandIcon,
				false, !ok, a.openDiffFullscreen)
		}
	})
}

const (
	compareControlHeight    float32 = 32
	comparePaneDividerInset float32 = 8
)

func compareHeaderIconButton(c *ui.Context, label string, icon *ui.SVG, disabled bool, run func()) {
	button := ui.Button(c, "").Size(compareControlHeight, compareControlHeight).Padding(0).Center()
	clicked := button.Label(tr(label)).Tooltip(tr(label)).Disabled(disabled).Clicked()
	button.Children(func() {
		ui.Icon(c, icon).FontSize(14)
	})
	if clicked {
		run()
	}
}

func (a *app) comparePage(c *ui.Context) {
	theme := c.Theme()
	islandCard(c, ui.Column(c).Grow(1)).Children(func() {
		ui.Column(c).Gap(8).Padding(10, 12).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Icon(c, compareIcon).FontSize(16).TextColor(theme.Accent)
				ui.Text(c, tr("Compare refs")).FontSize(16).Bold()
			})
			ui.Row(c).AlignItems(ui.End).Gap(8).Children(func() {
				from := a.compareRefInput(c, "From ref", "main or HEAD~1", &a.compare.from).Grow(1)
				to := a.compareRefInput(c, "To ref", "feature or HEAD", &a.compare.to).Grow(1)
				compare := compactTextButton(ui.PrimaryButton(c, tr("Compare"))).Height(compareControlHeight).
					Tooltip(tr("Read the changed files and their diffs")).
					Disabled(strings.TrimSpace(a.compare.from) == "" || strings.TrimSpace(a.compare.to) == "" || a.compare.filesLoading)
				if compare.Clicked() || from.Submitted() || to.Submitted() {
					a.beginCompare()
				}
				compareHeaderIconButton(c, "Refresh comparison", refreshIcon,
					a.compare.appliedFrom == "" || a.compare.filesLoading, a.refreshAppliedCompare)
				compareHeaderIconButton(c, "Swap refs", swapIcon,
					strings.TrimSpace(a.compare.from) == "" || strings.TrimSpace(a.compare.to) == "", a.swapCompare)
				compareHeaderIconButton(c, "Cancel comparison", closeIcon, false, a.resetComparePage)
			})
		})
		ui.Divider(c).FillWidth()
		ui.Column(c).Grow(1).Padding(10, 12).
			Role(ui.RoleRegion).Label(tr("Comparison results")).Children(func() {
			a.compareResults(c, theme)
		})
	})
}

// compareRefInput keeps revisions editable while offering known branches and
// tags. Unlike a closed combobox, an unmatched value is a valid Git revision.
func (a *app) compareRefInput(c *ui.Context, label, placeholder string, value *string) ui.Element {
	theme := c.Theme()
	combo := ui.ComboboxBase(c, value)
	input := combo.Input.Grow(1).Height(compareControlHeight).Padding(7, 10).
		Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).
		Label(tr(label)).Placeholder(tr(placeholder)).AutoComplete(ui.AutoCompleteList)
	combo.Popup(func(panel ui.Element) {
		panel.Padding(4).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius)
		choices := a.compareRefChoices(*value, combo.Filtering())
		filterPopupList(c, theme, func() {
			if len(choices) == 0 {
				ui.Row(c).Padding(7, 10).Children(func() {
					ui.Text(c, tr("No matches")).TextColor(theme.TextMuted)
				})
				return
			}
			for _, ref := range choices {
				item := combo.Item(ref).Padding(7, 10)
				if item.Highlighted() {
					item.Background(theme.SurfaceHover)
				}
				item.Children(func() { ui.Text(c, ref).SingleLine().Tooltip(ref) })
			}
		})
	})
	if ref, ok := combo.Chosen(); ok {
		*value = ref
	}
	return input
}

// compareRefChoices lists familiar revisions on click and narrows them while
// the user types. Keep the list bounded for repositories with many refs.
func (a *app) compareRefChoices(typed string, filtering bool) []string {
	choices := make([]string, 0, 1+len(a.snapshot.Branches)+len(a.snapshot.Tags))
	seen := make(map[string]bool)
	add := func(ref string) {
		if ref != "" && !seen[ref] {
			seen[ref] = true
			choices = append(choices, ref)
		}
	}
	add("HEAD")
	add("HEAD~1")
	for _, branch := range a.snapshot.Branches {
		add(branch.Name)
	}
	for _, remote := range a.snapshot.Remotes {
		for _, branch := range remote.Branches {
			name := branch.RemoteName
			if name == "" {
				name = remote.Name
			}
			if name != "" && branch.Name != "" {
				add(name + "/" + branch.Name)
			}
		}
	}
	for _, tag := range a.snapshot.Tags {
		add(tag.Name)
	}
	if filtering {
		needle := strings.ToLower(strings.TrimSpace(typed))
		if needle != "" {
			matched := choices[:0]
			for _, ref := range choices {
				if strings.Contains(strings.ToLower(ref), needle) {
					matched = append(matched, ref)
				}
			}
			choices = matched
		}
	}
	if len(choices) > 200 {
		choices = choices[:200]
	}
	return choices
}

func (a *app) compareResults(c *ui.Context, theme *ui.Theme) {
	switch {
	case a.compare.filesLoading:
		ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
			ui.Spinner(c).Label(tr("Loading comparison"))
			ui.Text(c, tr("Loading comparison…")).TextColor(theme.TextMuted)
		})
		return
	case a.compare.filesError != "":
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			ui.Text(c, tr("The comparison could not be read.")).Bold()
			ui.Text(c, a.compare.filesError).TextColor(theme.Danger).TextAlign(ui.Center)
			if ui.Button(c, tr("Try again")).Clicked() {
				a.beginCompare()
			}
		})
		return
	case a.compare.files == nil:
		ui.Column(c).Grow(1).Center().Gap(6).Children(func() {
			ui.Text(c, tr("Choose two refs, then Compare.")).Bold()
			ui.Text(c, tr("Use branch names, tags, commit hashes, or revisions such as HEAD~3.")).
				TextColor(theme.TextMuted)
		})
		return
	case len(a.compare.files) == 0:
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, tr("No changes between these refs.")).TextColor(theme.TextMuted)
		})
		return
	}

	a.compare.fileRows = a.syncCompareFileRows()
	a.collapsibleContentPaneSplit(c, &a.compare.listWidth, &a.workspacePaneCollapsed[sectionCompare],
		"Expand list pane", func() {
			ui.Column(c).Grow(1).FillHeight().Gap(7).Padding(0, comparePaneDividerInset, 0, 0).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
					paneTitle(c, folderIcon, "Changed files")
					ui.Spacer(c).Grow(1)
					ui.Badge(c, strconv.Itoa(len(a.compare.files)))
					a.fileTreeModeButton(c)
					paneCollapseButton(c, &a.workspacePaneCollapsed[sectionCompare], "Collapse list pane")
				})
				ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
					ui.SearchField(c, &a.compare.fileSearch).Bind(&a.sectionSearch[sectionCompare]).
						Label(tr("Search files")).Placeholder(tr("Search files")).Grow(1)
					a.searchNavigationButtons(c, strings.TrimSpace(a.compare.fileSearch) != "")
					a.exportCompareDiffButton(c)
				})
				if len(a.compare.fileRows) == 0 {
					ui.Column(c).Grow(1).Center().Children(func() {
						ui.Text(c, tr("No files match this filter")).TextColor(theme.TextMuted)
					})
					return
				}
				list := a.keyboardList(c, &a.compare.list, len(a.compare.fileRows)).Grow(1).Rows(func(row ui.ListRow) {
					item := a.compare.fileRows[row.Index]
					if item.isDirectory() {
						a.fileTreeDirectoryRow(row.Context, fileTreeCompareScope, item, row.Selected(), func() {
							a.selectCompareFileRow(row.Index)
						})
						return
					}
					file := a.compare.files[item.source]
					ui.Row(row.Context).Gap(8).AlignItems(ui.Center).
						Padding(6, 8, 6, float32(8+16*item.depth)).Children(func() {
						fileTypeMark(row.Context, file.Path, 13, row.Selected())
						ui.Text(row.Context, item.name).Grow(1).SingleLine().Tooltip(file.Path)
						fileDiffStatCounts(row.Context, file.Path, file.LinesAdded, file.LinesDeleted)
						statusSlot(row.Context, statusLetter(file.Status), rowStatusColor(theme, row, theme.Warning))
					})
				})
				if a.keyboardListChanged(&a.compare.list, list) {
					a.selectCompareFileRow(a.compare.selectedRow)
				}
				normalizeFileTreeRow(a.compare.fileRows, &a.compare.selectedRow, &a.compare.selected)
			})
		}, func() {
			ui.Column(c).Grow(1).FillHeight().Gap(7).Padding(0, 0, 0, comparePaneDividerInset).Children(func() {
				if a.compare.selected < 0 || a.compare.selected >= len(a.compare.files) {
					ui.Column(c).Grow(1).Center().Children(func() {
						ui.Text(c, tr("Select a file to see its diff.")).TextColor(theme.TextMuted)
					})
					return
				}
				file := a.compare.files[a.compare.selected]
				ui.Row(c).AlignItems(ui.Center).Gap(8).
					Role(ui.RoleRegion).Label(tr("File diff")).Children(func() {
					ui.Text(c, file.Path).Bold().SingleLine().Tooltip(file.Path).Grow(1)
					a.compareDiffViewOptions(c, false)
				})
				a.requestCompareFileDiff()
				switch {
				case a.compare.diffLoading:
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Spinner(c).Label(tr("Loading diff"))
						ui.Text(c, tr("Loading diff…")).TextColor(theme.TextMuted)
					})
				case a.compare.diffError != "":
					ui.Text(c, a.compare.diffError).TextColor(theme.Danger).Selectable()
				case a.compare.diffState != nil:
					a.keyboardDiffViewer(c, a.compare.diffState)
				default:
					ui.Column(c).Grow(1).Center().Children(func() {
						ui.Text(c, tr("No textual diff")).TextColor(theme.TextMuted)
					})
				}
			})
		})
}
