package app

import (
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
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
	insights := a.cockpit.insights
	if state.unavailable || insights.heatErr != "" {
		return
	}
	if !insights.heatLoaded {
		a.cockpitCard(c, "heatmap", "Commit activity", historyIcon, func() {
			cockpitLoading(c, tr("Reading the history…"), 130)
		})
		return
	}
	heat := insights.heatmap
	if insights.heatTotal == 0 || len(heat.Weeks) == 0 {
		return
	}
	line := a.cockpit.insights.heatmapLine
	actions := func() {
		if flat := cockpitViewToggle(c, "Show commits as a grid of days", heatGridIcon, !line); flat {
			a.cockpit.insights.heatmapLine = false
		}
		if flat := cockpitViewToggle(c, "Show commits as a line over the weeks", lineChartIcon, line); flat {
			a.cockpit.insights.heatmapLine = true
		}
	}
	a.cockpitCardWithActions(c, "heatmap", "Commit activity", historyIcon, actions, func() {
		theme := c.Theme()
		const cell, gap = 11, 3
		// As many weeks as fit, at least a quarter of a year.
		fit := int((width - 120) / (cell + gap))
		weeks := heat.Weeks
		if shown := min(max(fit, 13), len(weeks)); shown < len(weeks) {
			weeks = weeks[len(weeks)-shown:]
		}
		grid := func() {
			dayNames := []string{tr("Mon"), "", tr("Wed"), "", tr("Fri"), "", ""}
			ui.Row(c).FillWidth().MinWidth(0).Justify(ui.Center).AlignItems(ui.Start).Gap(6).Children(func() {
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
		}
		if line {
			cockpitHeatLine(c, weeks, cell+gap)
		} else {
			grid()
		}
		ui.Row(c).AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
			cockpitChip(c, trf("%s commits in the last year", groupedNumber(heat.Total)), theme.Success, nil)
			cockpitChip(c, trf("%d active days", heat.ActiveDays), theme.TextMuted, nil)
			if heat.LongestStreak > 1 {
				cockpitChip(c, trf("longest streak %d days", heat.LongestStreak), theme.TextMuted, nil)
			}
			if heat.Busiest.Count > 0 {
				cockpitChip(c, trf("busiest day %s · %d commits", heat.Busiest.Date.Format("2006-01-02"), heat.Busiest.Count), theme.TextMuted, nil)
			}
			if line {
				return
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

// cockpitCodeChangesCard shows the lines added and deleted over its own period,
// chosen with the card's time-range button, as a mirrored bar chart. The
// Contributors card's period does not change it.
func (a *app) cockpitCodeChangesCard(c *ui.Context) {
	insights := a.cockpit.insights
	if insights.volumeUnavailable {
		return
	}
	actions := func() {
		if index, changed := cockpitPeriodButton(c, insights.volumePeriod, tr("Code changes period")); changed {
			a.cockpit.insights.volumePeriod = index
			a.loadCockpitCodeVolume()
		}
	}
	a.cockpitCardWithActions(c, "code-changes", "Code changes", diffUnifiedIcon, actions, func() {
		theme := c.Theme()
		switch {
		case insights.volumeErr != "":
			ui.Text(c, tr("Could not read the code changes")).Bold().FontSize(theme.FontSize - 1)
			ui.Text(c, insights.volumeErr).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
			return
		case !insights.volumeLoaded:
			cockpitLoading(c, tr("Reading the history…"), 130)
			return
		}
		volume := insights.volume
		// Six colored squares show the balance; hovering them gives the numbers.
		ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(4, 0).Children(func() {
			commitDiffStatBlocksSized(c, tr("Code changes"), volume.Added, volume.Deleted, 14)
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
		ui.Row(c).FillWidth().MinWidth(0).Grow(1).AlignItems(ui.Stretch).Gap(3).MinHeight(60).Label(tr("Lines added and deleted over the period")).Children(func() {
			for index := range volume.BucketAdded {
				added, deleted := volume.BucketAdded[index], volume.BucketDeleted[index]
				from := time.Unix(volume.BucketStart+int64(index)*volume.BucketSeconds, 0).Local()
				date := from.Format("2006-01-02")
				c.Key(fmt.Sprintf("volume-bar:%d", index))
				ui.Column(c).Gap(1).Width(0).Grow(1).MinWidth(0).Children(func() {
					// An empty period still leaves a faint stub, so the chart is never blank.
					ui.Column(c).FillWidth().Grow(1).Children(func() {
						if added > 0 {
							share := float32(max(4, 100*added/most))
							ui.Box(c).FillWidth().Height(0).Grow(100 - share)
							cockpitVolumeTooltip(c, ui.Box(c).FillWidth().Height(0).Grow(share).Radius(2).
								Background(theme.Success.Alpha(0.75)), date, added, deleted)
						} else {
							ui.Box(c).FillWidth().Height(0).Grow(1)
							cockpitVolumeTooltip(c, ui.Box(c).FillWidth().Height(2).Radius(1).
								Background(theme.Border.Alpha(0.6)), date, added, deleted)
						}
					})
					ui.Column(c).FillWidth().Grow(1).Children(func() {
						if deleted > 0 {
							share := float32(max(4, 100*deleted/most))
							cockpitVolumeTooltip(c, ui.Box(c).FillWidth().Height(0).Grow(share).Radius(2).
								Background(theme.Danger.Alpha(0.75)), date, added, deleted)
							ui.Box(c).FillWidth().Height(0).Grow(100 - share)
						} else {
							cockpitVolumeTooltip(c, ui.Box(c).FillWidth().Height(2).Radius(1).
								Background(theme.Border.Alpha(0.6)), date, added, deleted)
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
	actions := func() {
		if cockpitTextLink(c, tr("All"), tr("Open Branches"), tr("Open the Branches page")) {
			a.setWorkspaceTab(sectionBranches)
		}
	}
	a.cockpitCardWithActions(c, "branch-health", "Branch health", branchesIcon, actions, func() {
		theme := c.Theme()
		switch {
		case insights.healthErr != "":
			ui.Text(c, tr("Could not read the branches")).Bold().FontSize(theme.FontSize - 1)
			ui.Text(c, insights.healthErr).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
			return
		case !insights.healthLoaded:
			cockpitLoading(c, tr("Reading the branches…"), 130)
			return
		}
		health := insights.health
		// Three short tabs: the number and one word. What each group means (and
		// its threshold) is in its tooltip, so the card stays quiet when nothing
		// needs care.
		categories := []struct {
			name    string
			meaning string
			color   ui.Color
		}{
			{tr("Merged"), tr("merged — safe to delete"), theme.Success},
			{tr("Stale"), trf("stale (%d+ days)", branchStaleDays), theme.Warning},
			{tr("Far behind"), trf("far behind (%d+ commits)", branchFarBehindAfter), theme.Danger},
		}
		// A group with nothing in it cannot be chosen, and the list never opens on
		// an empty one while another has branches.
		selected := min(max(insights.healthCategory, 0), len(categories)-1)
		first := -1
		for index := range categories {
			if len(health.Groups[index]) > 0 {
				first = index
				break
			}
		}
		if first < 0 {
			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Icon(c, commitIcon).FontSize(14).TextColor(theme.Success)
				ui.Text(c, tr("All branches are healthy")).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
			})
			return
		}
		if len(health.Groups[selected]) == 0 {
			selected = first
		}
		// Left: the groups as a vertical tab list. Right: the chosen group's
		// branches, each opening in Branches. A row says only what matters for its
		// group: how old, or how far behind.
		now := time.Now()
		ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Stretch).Gap(10).Children(func() {
			ui.Column(c).Width(150).Shrink(0).Gap(2).Label(tr("Branch groups")).Children(func() {
				for index, category := range categories {
					index, category := index, category
					count := len(health.Groups[index])
					c.Key(fmt.Sprintf("branch-group:%d", index))
					active := selected == index
					// Every tab has the same layout: dot, name, then the count at the
					// right edge, so the three line up whatever their state.
					tab := ui.ButtonBase(c).FillWidth().MinWidth(0).Height(32).Gap(8).Padding(0, 10).AlignItems(ui.Center).
						Radius(theme.Radius).Label(fmt.Sprintf("%d %s", count, category.name)).
						Tooltip(category.meaning).Selected(active).Disabled(count == 0)
					switch {
					case active:
						tab.Background(category.color.Alpha(0.14))
					case count > 0 && tab.Hovered():
						tab.Background(theme.SurfaceHover)
					}
					tab.Children(func() {
						dot, label, number := category.color, theme.Text, theme.TextMuted
						if count == 0 {
							dot, label = theme.TextMuted.Alpha(0.35), theme.TextMuted.Alpha(0.6)
						}
						if active {
							number = category.color
						}
						ui.Box(c).Width(8).Height(8).Radius(4).Background(dot).Shrink(0)
						ui.Text(c, category.name).FontSize(theme.FontSize - 1).TextColor(label).SingleLine().Width(0).Grow(1).MinWidth(0)
						ui.Text(c, fmt.Sprintf("%d", count)).FontSize(theme.FontSize - 1).Bold().TextColor(number).SingleLine().Shrink(0)
					})
					if tab.Clicked() && count > 0 {
						a.cockpit.insights.healthCategory = index
					}
				}
			})
			ui.Box(c).Width(1).AlignSelf(ui.Stretch).Background(theme.Border.Alpha(0.6)).Shrink(0)
			// The list scrolls when there are many.
			ui.Scroll(c).Grow(1).Basis(0).MinWidth(0).MaxHeight(158).Children(func() {
				ui.Column(c).FillWidth().MinWidth(0).Gap(0).Children(func() {
					for _, branch := range health.Groups[selected] {
						branch := branch
						c.Key("health-branch:" + branch.Name)
						row := ui.ButtonBase(c).FillWidth().MinWidth(0).Gap(8).Padding(4, 6).AlignItems(ui.Center).
							Radius(theme.Radius).Role(ui.RoleOption).Label(branch.Name)
						if row.Hovered() {
							row.Background(theme.SurfaceHover)
						}
						row.Children(func() {
							ui.Icon(c, branchIcon).FontSize(13).TextColor(theme.TextMuted).Shrink(0)
							ui.Text(c, branch.Name).SingleLine().Width(0).Grow(1).MinWidth(0).Tooltip(branch.Name)
							detail := cockpitBranchAge(branch.LastCommit, now)
							if selected == branchCategoryBehind && branch.Behind > 0 {
								detail = trf("%d behind", branch.Behind)
							}
							ui.Text(c, detail).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted).SingleLine().Shrink(0)
						})
						if row.Clicked() {
							a.revealBranch(branch.Name)
						}
					}
				})
			})
		})
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

// cockpitViewToggle is a small icon button choosing between views of a card;
// the shown one is marked. It returns true when it is pressed.
func cockpitViewToggle(c *ui.Context, label string, icon *ui.SVG, active bool) bool {
	pressed := false
	flatToggleIconButton(c, label, label, icon, 15, 4, active, func() { pressed = true })
	return pressed
}

// cockpitHeatLine draws the commits of each week as an area under a line, one
// point per week, lined up with the month names the grid would have.
func cockpitHeatLine(c *ui.Context, weeks [][7]heatDay, step float32) {
	theme := c.Theme()
	totals := make([]int, len(weeks))
	most := 1
	for index, week := range weeks {
		for _, day := range week {
			totals[index] += day.Count
		}
		most = max(most, totals[index])
	}
	const height = 98
	width := step * float32(len(weeks))
	ui.Row(c).FillWidth().MinWidth(0).Justify(ui.Center).AlignItems(ui.Start).Gap(6).Children(func() {
		ui.Column(c).Width(26).Height(height).Padding(15, 0, 0, 0).Justify(ui.SpaceBetween).Shrink(0).Children(func() {
			ui.Text(c, groupedNumber(most)).FontSize(theme.FontSize - 3).TextColor(theme.TextMuted).SingleLine()
			ui.Text(c, "0").FontSize(theme.FontSize - 3).TextColor(theme.TextMuted).SingleLine()
		})
		ui.Column(c).Gap(3).Children(func() {
			ui.Row(c).Gap(0).Height(12).Children(func() {
				for start := 0; start < len(weeks); {
					month := weeks[start][0].Date.Month()
					end := start
					for end < len(weeks) && weeks[end][0].Date.Month() == month {
						end++
					}
					c.Key(fmt.Sprintf("heat-line-month:%d", start))
					ui.Box(c).Width(float32(end-start) * step).Height(12).Clip().Children(func() {
						if end-start >= 2 {
							ui.Text(c, weeks[start][0].Date.Format("Jan")).FontSize(theme.FontSize - 3).
								TextColor(theme.TextMuted).SingleLine()
						}
					})
					start = end
				}
			})
			plot := ui.Row(c).Width(width).Height(height).Label(tr("Commits per week"))
			plot.Draw(func(p *ui.Painter, r ui.Rect) {
				point := func(index int) (float32, float32) {
					x := r.X + (float32(index)+0.5)*r.W/float32(len(totals))
					y := r.Y + r.H - 3 - (r.H-9)*float32(totals[index])/float32(most)
					return x, y
				}
				for _, share := range []float32{0, 0.5, 1} {
					var grid ui.Path
					y := r.Y + r.H - 3 - (r.H-9)*share
					grid.MoveTo(r.X, y).LineTo(r.X+r.W, y)
					p.StrokePath(&grid, 1, theme.Border.Alpha(0.5))
				}
				var area, stroke ui.Path
				firstX, _ := point(0)
				lastX, _ := point(len(totals) - 1)
				area.MoveTo(firstX, r.Y+r.H-3)
				for index := range totals {
					x, y := point(index)
					area.LineTo(x, y)
					if index == 0 {
						stroke.MoveTo(x, y)
					} else {
						stroke.LineTo(x, y)
					}
				}
				area.LineTo(lastX, r.Y+r.H-3).Close()
				p.FillPath(&area, theme.Accent.Alpha(0.2))
				p.StrokePath(stroke.Join(ui.RoundJoin), 1.75, theme.Accent)
				for index := range totals {
					if totals[index] == most {
						x, y := point(index)
						var dot ui.Path
						p.FillPath(dot.Circle(x, y, 3), theme.Accent)
					}
				}
			})
			plot.Children(func() {
				for index, week := range weeks {
					c.Key(fmt.Sprintf("heat-line-week:%d", index))
					tip := week[0].Date.Format("2006-01-02") + " · " + contributorCountLabel(totals[index])
					ui.Box(c).Width(0).Grow(1).MinWidth(0).FillHeight().Tooltip(tip)
				}
			})
		})
	})
}

// cockpitVolumeTooltip shows a bar's date and its added and deleted lines in
// green and red, as the squares of a commit do.
func cockpitVolumeTooltip(c *ui.Context, anchor ui.Element, date string, added, deleted int) {
	theme := c.Theme()
	ui.TooltipBase(c, anchor, func(tip ui.Element) {
		tip.Padding(6, 9).Radius(5).Background(theme.Surface).Border(1, theme.Border).
			Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.2)).Margin(0, 0, 6, 0).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, date).TextColor(theme.TextMuted).SingleLine()
					ui.Text(c, "+"+groupedNumber(added)).TextColor(theme.Success).SingleLine()
					ui.Text(c, "−"+groupedNumber(deleted)).TextColor(theme.Danger).SingleLine()
				})
			})
	})
}
