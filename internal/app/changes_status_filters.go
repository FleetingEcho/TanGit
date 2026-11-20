package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

type changesStatusFilter uint8

const (
	changesStatusAll changesStatusFilter = iota
	changesStatusStaged
	changesStatusUnstaged
	changesStatusTracked
	changesStatusUntracked
)

func changesStatusFilters() []changesStatusFilter {
	return []changesStatusFilter{changesStatusStaged, changesStatusUnstaged, changesStatusTracked, changesStatusUntracked, changesStatusAll}
}

func (f changesStatusFilter) label() string {
	switch f {
	case changesStatusStaged:
		return "Show only staged files"
	case changesStatusUnstaged:
		return "Show only unstaged files"
	case changesStatusTracked:
		return "Show only tracked files"
	case changesStatusUntracked:
		return "Show only untracked files"
	default:
		return "No filter"
	}
}

func changesFileMatchesStatus(file repository.File, filter changesStatusFilter) bool {
	switch filter {
	case changesStatusStaged:
		return file.Staged
	case changesStatusUnstaged:
		return file.Unstaged
	case changesStatusTracked:
		return file.Tracked
	case changesStatusUntracked:
		return !file.Tracked
	default:
		return true
	}
}

// changesFilesWithStatus only changes presentation. Working-tree and index
// mutations always retain all files; a filtered list is never a staging set.
func changesFilesWithStatus(files []repository.File, filter changesStatusFilter) []repository.File {
	if filter == changesStatusAll {
		return files
	}
	visible := make([]repository.File, 0, len(files))
	for _, file := range files {
		if changesFileMatchesStatus(file, filter) {
			visible = append(visible, file)
		}
	}
	return visible
}

func (a *app) changesFileFiltersActive() bool {
	return a.ignoreUntracked || a.review.filter || a.fileStatusFilter != changesStatusAll
}

// changesFilterStateChanged rebuilds rows immediately and protects both
// selection and in-flight diff work. Keep a file on the same side when its
// status allows it; otherwise choose the first visible file during rendering.
func (a *app) changesFilterStateChanged() {
	chosen, hadChoice := a.selectedChange()
	a.changeRows = a.changeRowsFrom(a.snapshot.Files)
	if !hadChoice {
		return
	}
	index := changeRowIndex(a.changeRows, chosen.file.Path, chosen.staged)
	if index < 0 {
		a.selectedFile = -1
		a.resetFileDiff()
		return
	}
	a.selectedFile = index
	updated := a.changeRows[index]
	if updated.staged != chosen.staged || updated.merge != chosen.merge {
		a.resetFileDiff()
	}
}

func (a *app) selectChangesStatusFilter(filter changesStatusFilter) {
	a.fileStatusFilter = filter
	a.changesFilterStateChanged()
	a.changesFiltersOpen = false
}

func (a *app) clearChangesFileFilters() {
	a.fileStatusFilter = changesStatusAll
	a.ignoreUntracked = false
	a.review.filter = false
	a.changesFilterStateChanged()
	a.changesFiltersOpen = false
}

// changesFilterPopover keeps quick filters beside the file search, without a
// modal. A status choice closes the menu; checkboxes remain open so they can
// be combined without reopening it.
func (a *app) changesFilterPopover(c *ui.Context) {
	anchor := flatToggleIconButton(c, "File filters", "Show file filters", filterIcon, 14, 5,
		a.changesFileFiltersActive(), func() {
			a.changesFiltersOpen = !a.changesFiltersOpen
		})
	anchor.HasPopup(ui.PopupDialog).Expanded(a.changesFiltersOpen)
	ui.Popover(c, anchor, &a.changesFiltersOpen, func() {
		ui.Column(c).Width(268).Gap(10).Padding(10).
			Role(ui.RoleDialog).Label(tr("File filters")).Children(func() {
			ui.Text(c, tr("File filters")).Bold().FontSize(c.Theme().FontSize + 1)
			for _, filter := range changesStatusFilters() {
				filter := filter
				label := tr(filter.label())
				active := a.fileStatusFilter == filter
				button := ui.ButtonBase(c).FillWidth().Gap(8).Padding(5, 7).Radius(c.Theme().Radius).
					Label(label).Role(ui.RoleRadio).Checked(active)
				if active {
					button.Background(c.Theme().Accent.Alpha(0.11))
				} else if button.Hovered() {
					button.Background(c.Theme().SurfaceHover)
				}
				button.Children(func() {
					glyph, color := "○", c.Theme().TextMuted
					if active {
						glyph, color = "●", c.Theme().Accent
					}
					ui.Text(c, glyph).FontSize(12).TextColor(color)
					ui.Text(c, label).Grow(1)
				})
				if button.Clicked() {
					a.selectChangesStatusFilter(filter)
				}
			}
			ui.Divider(c)
			ignored := ui.Checkbox(c, &a.ignoreUntracked, tr("Ignore untracked files")).
				Tooltip(tr("Leave untracked files out of the changes"))
			if ignored.Changed() {
				a.changesFilterStateChanged()
			}
			reviewed := ui.Checkbox(c, &a.review.filter, tr("Unreviewed")).
				Tooltip(tr("Show only files whose current content still needs review"))
			if reviewed.Changed() {
				a.changesFilterStateChanged()
			}
			ui.Row(c).Justify(ui.End).Children(func() {
				clear := compactTextButton(ui.Button(c, tr("Clear filters"))).
					Disabled(!a.changesFileFiltersActive())
				if clear.Clicked() {
					a.clearChangesFileFilters()
				}
			})
		})
	})
}

func (a *app) emptyChangesFilterResult(c *ui.Context) {
	message := tr("No files match the current filters")
	if strings.TrimSpace(a.fileSearch) != "" {
		message = tr("No files match your search or filters")
	}
	ui.Column(c).Grow(1).Center().Gap(10).Padding(18).Children(func() {
		ui.Icon(c, filterIcon).FontSize(24).TextColor(c.Theme().TextMuted)
		ui.Text(c, message).TextAlign(ui.Center).Bold()
		if ui.Button(c, tr("Clear filters")).Clicked() {
			a.clearChangesFileFilters()
		}
	})
}
