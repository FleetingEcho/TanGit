package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func (a *app) visibleReflog() []int {
	return filteredSourceRows(len(a.snapshot.Reflog), a.reflogFilter, func(source int) string {
		entry := a.snapshot.Reflog[source]
		return strings.Join([]string{entry.Subject, entry.Hash, entry.ShortHash,
			entry.AuthorName, entry.AuthorEmail}, "\x00")
	})
}

func (a *app) selectReflogRow(row int) {
	selectFilteredRow(a.visibleReflog(), row, &a.selectedReflogRow, &a.selectedReflog)
}

func (a *app) syncReflogRow() {
	syncFilteredRow(a.visibleReflog(), &a.selectedReflogRow, &a.selectedReflog)
}

func (a *app) reflogRowMenu(m *ui.Menu, entry repository.Commit, services ui.Services) {
	if m.Item(tr("Copy commit hash")).Chosen() {
		a.notifyCopied(services, "commit hash", entry.Hash)
	}
	m.Separator()
	if m.Item(tr("Checkout commit (detached)…")).Disabled(a.actionBusy).Chosen() {
		a.confirmCheckoutCommit(entry)
	}
	if m.Item(tr("Create a branch here…")).Disabled(a.actionBusy).Chosen() {
		a.openCommitBranchForm(entry)
	}
	m.Separator()
	if m.Item(tr("Soft reset here…")).Disabled(a.actionBusy).Chosen() {
		a.confirmResetCommit(entry, repository.ResetSoft)
	}
	if m.Item(tr("Mixed reset here…")).Disabled(a.actionBusy).Chosen() {
		a.confirmResetCommit(entry, repository.ResetMixed)
	}
	if m.Item(tr("Hard reset here…")).Disabled(a.actionBusy).Chosen() {
		a.confirmResetCommit(entry, repository.ResetHard)
	}
}

func (a *app) reflogActionIcons(c *ui.Context, entry repository.Commit, services ui.Services) {
	button := func(label, tooltip string, icon *ui.SVG, needsSession bool, run func()) {
		disabled := a.actionBusy
		if needsSession {
			disabled = disabled || a.session == nil
		}
		control := ui.ButtonBase(c).Padding(3).Label(tr(label)).Tooltip(tr(tooltip)).
			Disabled(disabled)
		control.Children(func() { ui.Icon(c, icon).FontSize(14) })
		if control.Clicked() {
			run()
		}
	}
	button("Copy reflog commit hash", "Copy the full commit hash", copyIcon, false, func() {
		a.notifyCopied(services, "commit hash", entry.Hash)
	})
	button("Checkout reflog commit", "Check out this commit in detached HEAD mode", historyIcon, true, func() {
		a.confirmCheckoutCommit(entry)
	})
	button("Create branch from reflog", "Create and check out a branch at this commit", branchIcon, true, func() {
		a.openCommitBranchForm(entry)
	})
	button("Mixed reset to reflog commit", "Move the current branch here and keep changes unstaged", softResetIcon, true, func() {
		a.confirmResetCommit(entry, repository.ResetMixed)
	})
}

func (a *app) reflogView(c *ui.Context) {
	theme := c.Theme()
	a.collapsibleContentPaneSplit(c, &a.filePaneWidth, &a.workspacePaneCollapsed[sectionReflog],
		"Expand list pane", func() {
			islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(12)).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Children(func() {
					paneTitle(c, reflogIcon, "Recent reference movements")
					ui.Spacer(c).Grow(1)
					paneCollapseButton(c, &a.workspacePaneCollapsed[sectionReflog], "Collapse list pane")
				})
				if len(a.snapshot.Reflog) == 0 {
					ui.Column(c).Grow(1).Center().Children(func() {
						ui.Text(c, tr("No reflog entries")).TextColor(theme.TextMuted)
					})
					return
				}
				visible := a.visibleReflog()
				listSearchField(c, &a.sectionSearch[sectionReflog], &a.reflogFilter,
					"Search reflog", "Filter by operation, hash, or author", "Clear reflog search",
					len(visible), len(a.snapshot.Reflog), func() { a.selectReflogRow(0) })
				visible = a.visibleReflog()
				if len(visible) == 0 {
					listSearchEmpty(c, "No reflog entries match this search")
					return
				}
				list := ui.List(c, &a.reflogList, len(visible)).Grow(1).Rows(func(row ui.ListRow) {
					entry, _ := sourceRow(visible, a.snapshot.Reflog, row.Index)
					services := row.Context.Services()
					ui.Column(row.Context).Gap(2).Padding(7, 8).Children(func() {
						ui.Text(row.Context, entry.Subject)
						ui.Row(row.Context).Gap(8).Children(func() {
							ui.Text(row.Context, entry.ShortHash).TextColor(secondaryRowColor(theme, row))
							if rel := relTime(entry.UnixTimestamp); rel != "" {
								ui.Text(row.Context, rel).TextColor(secondaryRowColor(theme, row))
							}
						})
					}).PopoverMenu(func(m *ui.Menu) { a.reflogRowMenu(m, entry, services) })
				})
				if list.Changed() {
					a.selectReflogRow(a.selectedReflogRow)
				}
				normalizeFilteredRow(visible, &a.selectedReflogRow, &a.selectedReflog)
			})
		}, func() {
			if a.selectedReflog < 0 || a.selectedReflog >= len(a.snapshot.Reflog) {
				a.commitDetailCard(c, nil, "Select a reflog entry", nil)
				return
			}
			entry := a.snapshot.Reflog[a.selectedReflog]
			a.commitDetailCard(c, &entry, "Select a reflog entry", func(c *ui.Context, entry repository.Commit) {
				a.reflogActionIcons(c, entry, c.Services())
			})
		})
}
