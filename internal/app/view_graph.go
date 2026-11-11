package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// graphView is the Graph section: every branch's history drawn as lanes,
// newest first, beside the chosen commit's details. A filter narrows the
// lanes to its matches, which is how an older commit is found without
// scrolling the whole graph.
func (a *app) graphView(c *ui.Context) {
	theme := c.Theme()
	// The Graph opens on the branch checked out like History does, and
	// follows a checkout while it holds that default.
	a.keepCommitFilterDefault(sectionGraph)
	commits := a.visibleCommits()
	rows := graphLayout(commits)
	laneCount := graphLaneCount(rows)
	filtered := a.graphFilter.narrows()
	a.collapsibleContentPaneSplitPercent(c, &a.graphListPercent, &a.workspacePaneCollapsed[sectionGraph],
		"Expand list pane", func() {
			islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(12)).Children(func() {
				ui.Column(c).Gap(6).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Icon(c, graphIcon).FontSize(14).TextColor(theme.Accent)
						ui.Text(c, tr("Commit graph")).Bold()
						if filtered {
							if a.graphFilter.loading {
								ui.Spinner(c).Label(tr("Searching commits"))
							} else if a.graphFilter.err == "" {
								ui.Badge(c, fmt.Sprintf("%d", len(commits)))
							}
						} else if len(commits) > 0 {
							ui.Badge(c, fmt.Sprintf("%d", len(commits)))
						}
						ui.Spacer(c).Grow(1)
						a.commitSelectionModeButton(c)
						commitFilterButton(c, "Search and filter the commit graph", filtered, func() {
							a.openCommitFilter(sectionGraph)
						})
						if filtered {
							clearFilterButton(c, "Clear every filter and show all commits", func() {
								a.clearCommitFilter(sectionGraph)
							})
						}
						paneCollapseButton(c, &a.workspacePaneCollapsed[sectionGraph], "Collapse list pane")
					})
					if filtered {
						ui.Row(c).Children(func() {
							ui.Badge(c, commitFilterSummary(a.graphFilter.applied))
						})
					}
				})
				subtitle := "Every branch, newest first"
				if filtered {
					subtitle = "Matching commits, newest first"
				}
				ui.Text(c, tr(subtitle)).
					TextColor(theme.TextMuted).
					FontSize(theme.FontSize - 1)
				a.commitSelectionBar(c)
				if len(commits) == 0 {
					a.commitEmptyView(c, theme, sectionGraph)
					return
				}
				if a.graphFilter.err != "" {
					ui.Text(c, a.graphFilter.err).TextColor(theme.Danger).SingleLine().
						Tooltip(a.graphFilter.err)
				}
				a.keyboardList(c, &a.commitList, len(commits)).Grow(1).Rows(func(row ui.ListRow) {
					commit := commits[row.Index]
					rowGraph := rows[row.Index]
					services := row.Context.Services()
					line := ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(5, 6)
					// The lanes are painted on the row itself, so they run the
					// row's full height and meet the rows above and below.
					line.Draw(func(p *ui.Painter, r ui.Rect) {
						drawGraphRow(p, r, rowGraph, laneCount, row.Selected(), line.Hovered(), theme)
					})
					line.Children(func() {
						// A Box as wide as this row's own lanes, not a Spacer: a
						// Spacer grows, and sizing the box from the busiest row
						// would leave a single-lane commit's text marooned far
						// right of the line it belongs to.
						ui.Box(row.Context).Width(graphRowInset(rowGraph.width, laneCount))
						a.commitSelectionCheckbox(row.Context, commit)
						authorAvatar(row.Context, commit.AuthorName, commit.AuthorEmail, 18)
						ui.Column(row.Context).Gap(1).Grow(1).Children(func() {
							ui.Text(row.Context, commit.Subject).SingleLine()
							ui.Row(row.Context).Gap(8).Children(func() {
								ui.Text(row.Context, commit.ShortHash).TextColor(secondaryRowColor(theme, row))
								ui.Text(row.Context, commit.AuthorName).TextColor(secondaryRowColor(theme, row)).SingleLine()
								if rel := relTime(commit.UnixTimestamp); rel != "" {
									ui.Text(row.Context, rel).TextColor(secondaryRowColor(theme, row)).SingleLine()
								}
							})
						})
					}).PopoverMenu(func(m *ui.Menu) {
						a.selectedCommit = row.Index
						a.commitRowMenu(m, commit, services)
					})
				})
				if a.commitList.AtEnd() {
					a.requestMoreCommits()
				}
				a.commitListFooter(c)
			})
		}, func() {
			a.commitDetailPane(c)
		})
}
