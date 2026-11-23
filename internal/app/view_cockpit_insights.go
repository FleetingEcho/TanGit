package app

import (
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// groupedNumber writes n with thousands separators: 12,345.
func groupedNumber(n int) string {
	text := strconv.Itoa(max(n, -n))
	for index := len(text) - 3; index > 0; index -= 3 {
		text = text[:index] + "," + text[index:]
	}
	if n < 0 {
		text = "-" + text
	}
	return text
}

// heatColor is the shade of one heat-map square.
func heatColor(theme *ui.Theme, level int) ui.Color {
	switch level {
	case 0:
		return theme.Border.Alpha(0.45)
	case 1:
		return theme.Accent.Alpha(0.30)
	case 2:
		return theme.Accent.Alpha(0.50)
	case 3:
		return theme.Accent.Alpha(0.75)
	}
	return theme.Accent
}

// cockpitHeatmapCard draws the commits of the last weeks as one square per day,
// a week to a column, with the busiest day and the longest streak beside it.
func (a *app) cockpitHeatmapCard(c *ui.Context, width float32) {
	state := a.cockpit.contributors
	if state.unavailable || !state.loaded || state.summary.TotalCommits == 0 {
		return
	}
	heat := a.cockpit.insights.heatmap
	if len(heat.Weeks) == 0 {
		return
	}
	a.cockpitCard(c, "heatmap", "Commit activity", historyIcon, func() {
		theme := c.Theme()
		const cell, gap = 11, 3
		// As many weeks as fit, at least a quarter of a year.
		fit := int((width - 120) / (cell + gap))
		weeks := heat.Weeks
		if shown := min(max(fit, 13), len(weeks)); shown < len(weeks) {
			weeks = weeks[len(weeks)-shown:]
		}
		dayNames := []string{tr("Mon"), "", tr("Wed"), "", tr("Fri"), "", ""}
		ui.Row(c).AlignItems(ui.Start).Gap(6).Children(func() {
			ui.Column(c).Gap(gap).Padding(15, 0, 0, 0).Shrink(0).Children(func() {
				for _, name := range dayNames {
					ui.Box(c).Height(cell).Width(26).Children(func() {
						if name != "" {
							ui.Text(c, name).FontSize(theme.FontSize - 3).TextColor(theme.TextMuted).SingleLine()
						}
					})
				}
			})
			ui.Column(c).Gap(3).Children(func() {
				// Month names, each over the weeks that start in it.
				ui.Row(c).Gap(0).Height(12).Children(func() {
					for start := 0; start < len(weeks); {
						month := weeks[start][0].Date.Month()
						end := start
						for end < len(weeks) && weeks[end][0].Date.Month() == month {
							end++
						}
						span := float32(end-start) * (cell + gap)
						c.Key(fmt.Sprintf("heat-month:%d", start))
						ui.Box(c).Width(span).Height(12).Clip().Children(func() {
							if end-start >= 2 {
								ui.Text(c, weeks[start][0].Date.Format("Jan")).FontSize(theme.FontSize - 3).
									TextColor(theme.TextMuted).SingleLine()
							}
						})
						start = end
					}
				})
				ui.Row(c).Gap(gap).Label(tr("Commits per day")).Children(func() {
					for weekIndex, week := range weeks {
						c.Key(fmt.Sprintf("heat-week:%d", weekIndex))
						ui.Column(c).Gap(gap).Children(func() {
							for dayIndex, day := range week {
								c.Key(fmt.Sprintf("heat-day:%d", dayIndex))
								box := ui.Box(c).Width(cell).Height(cell).Radius(2)
								if !day.InRange {
									continue
								}
								box.Background(heatColor(theme, heatLevel(day.Count, heat.Max)))
								box.Tooltip(day.Date.Format("2006-01-02") + " · " + contributorCountLabel(day.Count))
							}
						})
					}
				})
			})
		})
		ui.Row(c).AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
			cockpitChip(c, trf("%s commits in the last year", groupedNumber(heat.Total)), theme.Success, nil)
			cockpitChip(c, trf("%d active days", heat.ActiveDays), theme.TextMuted, nil)
			if heat.LongestStreak > 1 {
				cockpitChip(c, trf("longest streak %d days", heat.LongestStreak), theme.TextMuted, nil)
			}
			if heat.Busiest.Count > 0 {
				cockpitChip(c, trf("busiest day %s · %d commits", heat.Busiest.Date.Format("2006-01-02"), heat.Busiest.Count), theme.TextMuted, nil)
			}
			ui.Spacer(c).Grow(1)
			ui.Text(c, tr("Less")).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
			for level := 0; level <= 4; level++ {
				c.Key(fmt.Sprintf("heat-legend:%d", level))
				ui.Box(c).Width(cell).Height(cell).Radius(2).Background(heatColor(theme, level))
			}
			ui.Text(c, tr("More")).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
		})
	})
}

// cockpitCodeChangesCard shows the lines added and deleted over the period
// chosen in the Contributors card, as a mirrored bar chart.
func (a *app) cockpitCodeChangesCard(c *ui.Context) {
	insights := a.cockpit.insights
	if insights.volumeUnavailable {
		return
	}
	a.cockpitCard(c, "code-changes", "Code changes", diffUnifiedIcon, func() {
		theme := c.Theme()
		switch {
		case insights.volumeErr != "":
			ui.Text(c, tr("Could not read the code changes")).Bold().FontSize(theme.FontSize - 1)
			ui.Text(c, insights.volumeErr).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
			return
		case !insights.volumeLoaded:
			ui.Text(c, tr("Reading the history…")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			return
		}
		volume := insights.volume
		ui.Row(c).AlignItems(ui.End).Gap(14).Children(func() {
			cockpitMetric(c, "+"+groupedNumber(volume.Added), tr("added"), theme.Success)
			cockpitMetric(c, "−"+groupedNumber(volume.Deleted), tr("deleted"), theme.Danger)
		})
		ui.Row(c).AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
			net := volume.Added - volume.Deleted
			sign := "+"
			if net < 0 {
				sign = "−"
			}
			cockpitChip(c, trf("net %s lines", sign+groupedNumber(max(net, -net))), theme.TextMuted, nil)
			cockpitChip(c, contributorCountLabel(volume.Commits), theme.TextMuted, nil)
		})
		most := 1
		for index := range volume.BucketAdded {
			most = max(most, volume.BucketAdded[index], volume.BucketDeleted[index])
		}
		const half = 22
		barWidth := float32(12)
		if len(volume.BucketAdded) > 13 {
			barWidth = 8
		}
		ui.Row(c).Gap(3).Label(tr("Lines added and deleted over the period")).Children(func() {
			for index := range volume.BucketAdded {
				added, deleted := volume.BucketAdded[index], volume.BucketDeleted[index]
				from := time.Unix(volume.BucketStart+int64(index)*volume.BucketSeconds, 0).Local()
				tip := from.Format("2006-01-02") + " · +" + groupedNumber(added) + " −" + groupedNumber(deleted)
				c.Key(fmt.Sprintf("volume-bar:%d", index))
				ui.Column(c).Gap(1).Children(func() {
					ui.Column(c).Height(half).Width(barWidth).Justify(ui.End).Children(func() {
						if added > 0 {
							ui.Box(c).Width(barWidth).Height(float32(max(2, half*added/most))).Radius(2).
								Background(theme.Success.Alpha(0.75)).Tooltip(tip)
						}
					})
					ui.Column(c).Height(half).Width(barWidth).Children(func() {
						if deleted > 0 {
							ui.Box(c).Width(barWidth).Height(float32(max(2, half*deleted/most))).Radius(2).
								Background(theme.Danger.Alpha(0.75)).Tooltip(tip)
						}
					})
				})
			}
		})
		caption := tr("Lines added and deleted over the period")
		if volume.Limited {
			caption += " · " + tr("only the newest commits are counted")
		}
		ui.Text(c, caption).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
	})
}

// cockpitBranchHealthCard lists the branches that may need care: merged ones
// that can go, stale ones and ones far behind the default branch.
func (a *app) cockpitBranchHealthCard(c *ui.Context) {
	insights := a.cockpit.insights
	if insights.healthUnavailable {
		return
	}
	a.cockpitCard(c, "branch-health", "Branch health", branchesIcon, func() {
		theme := c.Theme()
		switch {
		case insights.healthErr != "":
			ui.Text(c, tr("Could not read the branches")).Bold().FontSize(theme.FontSize - 1)
			ui.Text(c, insights.healthErr).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
			return
		case !insights.healthLoaded:
			ui.Text(c, tr("Reading the branches…")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			return
		}
		health := insights.health
		caption := trf("%d remote", health.Remote)
		if health.Default != "" {
			caption += " · " + trf("compared with %s", health.Default)
		}
		cockpitMetric(c, trf("%d local", health.Local), caption, theme.Text)
		categories := []struct {
			label string
			color ui.Color
		}{
			{tr("merged — safe to delete"), theme.Success},
			{trf("stale (%d+ days)", branchStaleDays), theme.Warning},
			{trf("far behind (%d+ commits)", branchFarBehindAfter), theme.Danger},
		}
		selected := insights.healthCategory
		ui.Row(c).AlignItems(ui.Center).Gap(6).Wrap().Label(tr("Branch groups")).Children(func() {
			for index, category := range categories {
				index, category := index, category
				count := len(health.Groups[index])
				label := fmt.Sprintf("%d %s", count, category.label)
				c.Key(fmt.Sprintf("branch-group:%d", index))
				active := selected == index
				button := ui.ButtonBase(c).Padding(2, 9).Radius(999).Border(1, theme.Border).Label(label).Selected(active)
				switch {
				case active:
					button.Background(category.color.Alpha(0.16)).Border(1, category.color.Alpha(0.6))
				case button.Hovered():
					button.Background(theme.SurfaceHover)
				}
				button.Children(func() {
					color := theme.TextMuted
					if count > 0 {
						color = category.color
					}
					ui.Text(c, label).FontSize(theme.FontSize - 1).TextColor(color).SingleLine()
				})
				if button.Clicked() {
					a.cockpit.insights.healthCategory = index
				}
			}
		})
		group := health.Groups[min(max(selected, 0), len(health.Groups)-1)]
		if len(group) == 0 {
			ui.Text(c, tr("Nothing in this group — the branches are in good shape.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		}
		const listed = 6
		now := time.Now()
		for index, branch := range group {
			if index == listed {
				break
			}
			branch := branch
			c.Key("health-branch:" + branch.Name)
			row := ui.ButtonBase(c).FillWidth().MinWidth(0).Gap(8).Padding(2, 4).AlignItems(ui.Center).
				Radius(theme.Radius).Role(ui.RoleOption).Label(branch.Name)
			if row.Hovered() {
				row.Background(theme.SurfaceHover)
			}
			row.Children(func() {
				icon := branchIcon
				if branch.Remote {
					icon = remotesIcon
				}
				ui.Icon(c, icon).FontSize(13).TextColor(theme.TextMuted).Shrink(0)
				ui.Text(c, branch.Name).SingleLine().Width(0).Grow(1).MinWidth(0).Tooltip(branch.Name)
				detail := cockpitBranchAge(branch.LastCommit, now)
				if branch.Behind > 0 {
					detail += " · " + trf("%d behind", branch.Behind)
				}
				ui.Text(c, detail).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted).SingleLine().Shrink(0)
				if selected == branchCategoryMerged && !branch.Remote {
					if local, ok := a.localBranch(branch.Name); ok {
						trash := compactIconButton(ui.Button(c, "")).Center().Label(trf("Delete branch %s", branch.Name)).
							Tooltip(tr("Delete this merged branch…"))
						trash.Children(func() { ui.Icon(c, trashIcon).FontSize(13) })
						if trash.Clicked() {
							a.confirmDeleteBranch(local)
						}
					}
				}
			})
			if row.Clicked() && !branch.Remote {
				a.revealBranch(branch.Name)
			}
		}
		if len(group) > listed {
			ui.Row(c).Gap(7).Wrap().Children(func() {
				if ui.Button(c, trf("%d more — open Branches", len(group)-listed)).Label(tr("Open Branches")).Clicked() {
					a.setWorkspaceTab(sectionBranches)
				}
			})
		}
	})
}

func (a *app) localBranch(name string) (repository.Branch, bool) {
	for _, branch := range a.snapshot.Branches {
		if branch.Name == name {
			return branch, true
		}
	}
	return repository.Branch{}, false
}

// cockpitBranchAge says how long ago a branch's newest commit was.
func cockpitBranchAge(unix int64, now time.Time) string {
	if unix <= 0 {
		return ""
	}
	days := int(now.Sub(time.Unix(unix, 0)).Hours() / 24)
	switch {
	case days < 1:
		return tr("today")
	case days < 60:
		return trf("%d days ago", days)
	case days < 730:
		return trf("%d months ago", days/30)
	}
	return trf("%d years ago", days/365)
}
