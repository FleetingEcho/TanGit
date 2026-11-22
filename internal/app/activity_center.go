package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// The Activity Center is the one place that lists what has happened across
// every open repository: commands, Safety Journal records, Agent task
// activity, transfers and notices. It only reads the existing activity
// projection; opening a row goes through activityFeed.Open, the same
// stable-ID navigation the Overview uses.

const activityCenterResultLimit = 200

type activityCenterState struct {
	open    bool
	query   string
	search  ui.Handle
	scroll  ui.ScrollState
	reveal  bool
	notice  string
	restore focusRestoreTarget

	selected int
	// sources limits the list to some activity sources; empty means all.
	sources map[activitySource]bool
	// attention limits the list to warnings and errors.
	attention bool
	// repository limits the list to one open repository's location; "" is all.
	repository string
	// view is the feed of activity or the list of contributors.
	view         int
	contributors contributorsState
}

var activityCenterSources = []activitySource{
	activitySourceCommand, activitySourceJournal, activitySourceAgent, activitySourceTransfer, activitySourceNotice,
}

// activityInputs gathers the activity of every open repository tab.
func (s *shell) activityInputs() activityInputs {
	var inputs activityInputs
	for _, tab := range s.tabs {
		if tab == nil {
			continue
		}
		if input, ok := tab.app.activityInput(); ok {
			inputs.Apps = append(inputs.Apps, input)
		}
	}
	return inputs
}

func (s *shell) activityCenterQuery() activityQuery {
	query := activityQuery{
		Repository: s.activity.repository, Text: s.activity.query, Limit: activityCenterResultLimit,
	}
	if len(s.activity.sources) > 0 {
		query.Sources = make(map[activitySource]bool, len(s.activity.sources))
		for source := range s.activity.sources {
			query.Sources[source] = true
		}
	}
	if s.activity.attention {
		query.Severities = map[activitySeverity]bool{activitySeverityWarning: true, activitySeverityError: true}
	}
	return query
}

func (s *shell) activityCenterItems() []activityItem {
	return (activityFeed{}).Project(s.activityInputs(), s.activityCenterQuery())
}

// activityRepositories lists the open repositories by their tab title, keyed
// by the location activity targets use.
func (s *shell) activityRepositories() []struct{ location, title string } {
	var repositories []struct{ location, title string }
	seen := map[string]bool{}
	for _, tab := range s.tabs {
		if tab == nil || tab.app == nil {
			continue
		}
		location := activityLocation(tab.app)
		if location == "" || seen[location] {
			continue
		}
		if _, ok := tab.app.activityInput(); !ok {
			continue
		}
		seen[location] = true
		repositories = append(repositories, struct{ location, title string }{location, tab.title()})
	}
	return repositories
}

func (s *shell) openActivityCenter() { s.openActivityCenterView(activityViewFeed) }

// openActivityCenterView opens the center on its activity feed or its list of
// contributors.
func (s *shell) openActivityCenterView(view int) {
	if s == nil || s.activity.open {
		return
	}
	a := s.activeApp()
	if a != nil && a.modalOpen() {
		return
	}
	if s.palette.open {
		s.closeCommandPalette(false)
	}
	s.activity = activityCenterState{open: true, reveal: true, view: view}
	if a != nil {
		s.activity.restore = focusRestoreTarget{app: a, pane: a.keyboardLastPane}
	}
	s.activity.search.CancelFocus()
	s.invalidate()
}

func (s *shell) closeActivityCenter(restore bool) {
	if s == nil || !s.activity.open {
		return
	}
	target := s.activity.restore
	s.activity.open = false
	s.activity.query = ""
	s.activity.notice = ""
	s.activity.restore = focusRestoreTarget{}
	s.activity.search.CancelFocus()
	if restore && target.app != nil && !target.app.modalOpen() && s.activeApp() == target.app && target.pane != "" {
		target.app.keyboardFocus = target.pane
	}
	s.invalidate()
}

// openActivityItem navigates to what a row describes and leaves the center, so
// the destination is what the user sees.
func (s *shell) openActivityItem(item activityItem) {
	result := (activityFeed{}).Open(s.commandContext(), item.Target)
	if !result.Opened {
		s.activity.notice = tr(result.Reason)
		if s.activity.notice == "" {
			s.activity.notice = tr("This activity has no available navigation target")
		}
		return
	}
	s.closeActivityCenter(false)
}

func activitySeverityGlyph(theme *ui.Theme, severity activitySeverity) (*ui.SVG, ui.Color) {
	switch severity {
	case activitySeverityError:
		return conflictIcon, theme.Danger
	case activitySeverityWarning:
		return conflictIcon, theme.Warning
	case activitySeveritySuccess:
		return commandSuccessIcon, theme.Success
	default:
		return historyIcon, theme.TextMuted
	}
}

// activityTimeLabel is the clock time for today's activity and the date with
// the time for older activity.
func activityTimeLabel(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	at = at.Local()
	now = now.Local()
	if at.Year() == now.Year() && at.YearDay() == now.YearDay() {
		return at.Format("15:04")
	}
	return at.Format("Jan 2 15:04")
}

func (s *shell) activityCenterView(c *ui.Context) {
	if !s.activity.open {
		return
	}
	theme := c.Theme()
	contributorsView := s.activity.view == activityViewContributors
	var items []activityItem
	if contributorsView {
		s.ensureContributors()
	} else {
		items = s.activityCenterItems()
	}
	switch {
	case len(items) == 0:
		s.activity.selected = 0
	case s.activity.selected >= len(items):
		s.activity.selected = len(items) - 1
	case s.activity.selected < 0:
		s.activity.selected = 0
	}
	move := func(delta int) {
		if contributorsView {
			s.moveContributor(delta)
			return
		}
		if len(items) == 0 {
			return
		}
		s.activity.selected = (s.activity.selected + delta + len(items)) % len(items)
		s.activity.reveal = true
		s.activity.notice = ""
	}
	openSelected := func() {
		if contributorsView {
			s.toggleSelectedContributor()
			return
		}
		if len(items) > 0 {
			s.openActivityItem(items[s.activity.selected])
		}
	}
	repositories := s.activityRepositories()
	titleOf := func(location string) string {
		for _, repository := range repositories {
			if repository.location == location {
				return repository.title
			}
		}
		return ""
	}
	chip := func(label string, active bool, toggle func()) {
		button := ui.ButtonBase(c).Padding(3, 10).Radius(999).Border(1, theme.Border).
			Label(label).Selected(active)
		switch {
		case active:
			button.Background(theme.Accent.Alpha(0.18)).Border(1, theme.Accent.Alpha(0.6))
		case button.Hovered():
			button.Background(theme.SurfaceHover)
		}
		button.Children(func() {
			color := theme.TextMuted
			if active {
				color = theme.Accent
			}
			ui.Text(c, label).FontSize(theme.FontSize - 1).TextColor(color).SingleLine()
		})
		if button.Clicked() {
			toggle()
			s.activity.selected = 0
			s.activity.reveal = true
		}
	}

	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		back.Children(func() {
			panel := ui.Column(c).Width(820).MaxWidthPercent(94).Height(620).MaxHeightPercent(90).
				Padding(18).Gap(12).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Activity Center"))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Icon(c, historyIcon).FontSize(20).TextColor(theme.Accent)
					ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, tr("Activity Center")).FontSize(19).Bold()
						subtitle := tr("Commands, Safety Journal, Agent tasks, transfers and notices across open repositories")
						if contributorsView {
							subtitle = tr("Who contributed to the active repository, how much, and what they committed")
						}
						ui.Text(c, subtitle).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine()
					})
					iconButton(c, "Close Activity Center", "Escape", closeIcon, false, func() { s.closeActivityCenter(true) })
				})
				ui.Row(c).AlignItems(ui.Center).Gap(6).Label(tr("Activity views")).Children(func() {
					chip(tr("Activity"), !contributorsView, func() { s.activity.view = activityViewFeed })
					chip(tr("Contributors"), contributorsView, func() { s.activity.view = activityViewContributors })
				})
				placeholder := tr("Search activity by title, repository or state")
				if contributorsView {
					placeholder = tr("Search contributors by name or email")
				}
				search := ui.TextInput(c, &s.activity.query).Bind(&s.activity.search).
					Label(tr("Search activity")).Placeholder(placeholder).Search().AutoFocus()
				s.activity.search.OnShortcut(c, 0, ui.KeyEscape, func() { s.closeActivityCenter(true) })
				s.activity.search.OnShortcut(c, 0, ui.KeyUp, func() { move(-1) })
				s.activity.search.OnShortcut(c, 0, ui.KeyDown, func() { move(1) })
				search.OnSubmit(openSelected)
				if search.Changed() {
					s.activity.selected = 0
					s.activity.reveal = true
					s.activity.notice = ""
				}
				if !contributorsView {
					ui.Row(c).AlignItems(ui.Center).Gap(6).Wrap().Label(tr("Activity filters")).Children(func() {
						chip(tr("Needs attention"), s.activity.attention, func() { s.activity.attention = !s.activity.attention })
						for _, source := range activityCenterSources {
							source := source
							chip(activitySourceLabel(source), s.activity.sources[source], func() {
								if s.activity.sources == nil {
									s.activity.sources = map[activitySource]bool{}
								}
								if s.activity.sources[source] {
									delete(s.activity.sources, source)
								} else {
									s.activity.sources[source] = true
								}
							})
						}
					})
				}
				if !contributorsView && len(repositories) > 1 {
					ui.Row(c).AlignItems(ui.Center).Gap(6).Wrap().Label(tr("Repository filter")).Children(func() {
						chip(tr("All repositories"), s.activity.repository == "", func() { s.activity.repository = "" })
						for _, repository := range repositories {
							repository := repository
							chip(repository.title, s.activity.repository == repository.location, func() {
								if s.activity.repository == repository.location {
									s.activity.repository = ""
								} else {
									s.activity.repository = repository.location
								}
							})
						}
					})
				}
				ui.Divider(c)
				ui.Scroll(c).Grow(1).TrackScroll(&s.activity.scroll).Children(func() {
					if contributorsView {
						s.contributorsList(c, theme)
						return
					}
					if len(items) == 0 {
						ui.Column(c).Grow(1).Center().Gap(8).Padding(24).Children(func() {
							ui.Icon(c, historyIcon).FontSize(25).TextColor(theme.TextMuted)
							ui.Text(c, tr("No activity matches")).Bold()
							ui.Text(c, tr("Activity appears here as you run commands, change history, run Agent tasks and transfer files. Clear a filter to see more.")).
								TextColor(theme.TextMuted).TextAlign(ui.Center).MaxWidth(460)
						})
						return
					}
					now := time.Now()
					ui.Column(c).FillWidth().Gap(4).Role(ui.RoleListBox).Label(tr("Activity")).Children(func() {
						for index, item := range items {
							index, item := index, item
							c.Key("activity-center:" + item.ID.String())
							s.activityCenterRow(c, item, titleOf(item.Target.RepositoryID), now,
								index == s.activity.selected, s.activity.reveal, len(repositories) > 1, func() {
									s.activity.selected = index
									s.openActivityItem(item)
								})
						}
						s.activity.reveal = false
					})
				})
				ui.Divider(c)
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Text(c, tr("↑↓ Navigate · Enter Open · Escape Close")).TextColor(theme.TextMuted).
						FontSize(theme.FontSize - 1).Grow(1)
					if s.activity.notice != "" {
						ui.Text(c, s.activity.notice).TextColor(theme.Warning).FontSize(theme.FontSize - 1).
							MaxWidth(420).SingleLine().Tooltip(s.activity.notice)
					} else {
						count := activityCountLabel(len(items))
						if contributorsView {
							count = contributorsCountLabel(len(s.visibleContributors()))
						}
						ui.Text(c, count).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					}
				})
			})
			if back.OverlayShortcut(0, ui.KeyUp) {
				move(-1)
			}
			if back.OverlayShortcut(0, ui.KeyDown) {
				move(1)
			}
			if back.OverlayShortcut(0, ui.KeyEnter) {
				openSelected()
			}
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				s.closeActivityCenter(true)
			}
		})
	})
}

func activityCountLabel(count int) string {
	if count == 1 {
		return tr("1 item")
	}
	if count >= activityCenterResultLimit {
		return fmt.Sprintf(tr("Showing the latest %d items"), count)
	}
	return fmt.Sprintf(tr("%d items"), count)
}

func (s *shell) activityCenterRow(c *ui.Context, item activityItem, repositoryTitle string, now time.Time, selected, reveal, showRepository bool, open func()) {
	theme := c.Theme()
	label := item.Title + " — " + activityStateLabel(item.State)
	row := ui.ButtonBase(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(10).
		Padding(8, 10).Radius(theme.Radius).Role(ui.RoleOption).Label(label).Selected(selected)
	if selected {
		row.Background(theme.Accent.Alpha(0.12)).Border(1, theme.Accent.Alpha(0.55))
		if reveal {
			row.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollNearest})
		}
	} else if row.Hovered() {
		row.Background(theme.SurfaceHover)
	}
	row.Children(func() {
		glyph, color := activitySeverityGlyph(theme, item.Severity)
		ui.Icon(c, glyph).FontSize(16).TextColor(color).Shrink(0)
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, item.Title).Bold().SingleLine().TextColor(theme.Text)
			if item.Description != "" {
				ui.Text(c, item.Description).FontSize(theme.FontSize - 1).SingleLine().MinWidth(0).
					TextColor(theme.TextMuted).Tooltip(item.Description)
			}
			meta := []string{activitySourceLabel(item.Source), activityPersistenceLabel(item.Persistence)}
			if showRepository && repositoryTitle != "" {
				meta = append([]string{repositoryTitle}, meta...)
			}
			ui.Text(c, strings.Join(meta, " · ")).FontSize(theme.FontSize - 2).SingleLine().TextColor(theme.TextMuted)
		})
		ui.Column(c).Gap(2).Shrink(0).AlignItems(ui.End).Children(func() {
			ui.Text(c, activityStateLabel(item.State)).FontSize(theme.FontSize - 2).TextColor(color).SingleLine()
			if when := activityTimeLabel(activityTimestamp(item.OccurredAt, item.UpdatedAt), now); when != "" {
				ui.Text(c, when).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted).SingleLine()
			}
		})
		ui.Icon(c, chevronRightIcon).FontSize(12).TextColor(theme.TextMuted).Shrink(0)
	})
	if row.Clicked() {
		open()
	}
}

func contributorsCountLabel(count int) string {
	if count == 1 {
		return tr("1 contributor")
	}
	return trf("%d contributors", count)
}
