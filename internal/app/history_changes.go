package app

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

const historyChangesLabel = "Search code changes"

type historyChangesState struct {
	open         bool
	focusPending bool
	focus        ui.Handle
	draft        repository.HistoryChangeQuery
	applied      repository.HistoryChangeQuery
	page         repository.HistoryChangePage
	previous     []repository.HistoryChangeCursor
	started      bool
	loading      bool
	stopped      string
	err          string
	cancel       context.CancelFunc
	token        uint64
	showFilters  bool
	filterDraft  repository.HistoryChangeQuery
	scroll       ui.ScrollState
}

func historyChangesCommandState(ctx commandContext) commandState {
	state := repositoryStateAllowBusy(ctx)
	if state.Enabled && !ctx.App.historyChangesAvailable() {
		state.Enabled = false
		state.DisabledReason = "Code change search requires the History view"
	}
	return state
}

func parseHistoryChangesPalette(text string) (repository.HistoryChangeQuery, bool) {
	words := splitSearchWords(text)
	if len(words) == 0 || !strings.HasPrefix(strings.ToLower(words[0]), "changed:") {
		return repository.HistoryChangeQuery{}, false
	}
	query := repository.HistoryChangeQuery{Text: words[0][len("changed:"):]}
	for _, word := range words[1:] {
		switch lower := strings.ToLower(word); {
		case strings.HasPrefix(lower, "branch:"):
			query.Ref = word[len("branch:"):]
		case strings.HasPrefix(lower, "path:"):
			query.Path = word[len("path:"):]
		}
	}
	return query, true
}

func (a *app) historyChangesAvailable() bool {
	_, ok := a.session.(repository.HistoryChangeSession)
	return ok && a.sectionVisible(sectionHistory)
}

func (a *app) openHistoryChanges(query *repository.HistoryChangeQuery) {
	if !a.historyChangesAvailable() {
		return
	}
	a.closeCommitFilter()
	if query != nil {
		a.resetHistoryChanges()
		a.historyChanges.draft = *query
	}
	a.historyChanges.open = true
	a.historyChanges.focusPending = true
}

func (a *app) closeHistoryChanges() {
	a.historyChanges.open = false
	a.historyChanges.showFilters = false
	if a.historyChanges.cancel != nil {
		a.historyChanges.cancel()
	}
}

func (a *app) resetHistoryChanges() {
	a.closeHistoryChanges()
	a.historyChanges = historyChangesState{token: a.historyChanges.token + 1}
}

func (a *app) startHistoryChanges() {
	search := &a.historyChanges
	if search.loading || search.draft.Text == "" {
		return
	}
	search.applied = search.draft
	if a.recordHistorySearch != nil {
		a.recordHistorySearch(search.applied)
	}
	search.showFilters = false
	search.previous = nil
	a.readHistoryChanges(repository.HistoryChangeCursor{})
}

func (a *app) readHistoryChanges(cursor repository.HistoryChangeCursor) {
	session, ok := a.session.(repository.HistoryChangeSession)
	if !ok {
		return
	}
	search := &a.historyChanges
	search.token++
	token := search.token
	query := search.applied
	query.Ref = strings.TrimSpace(query.Ref)
	if !query.Until.IsZero() {
		query.Until = query.Until.Truncate(time.Minute).Add(time.Minute - time.Second)
	}
	search.page = repository.HistoryChangePage{}
	search.started, search.loading = true, true
	search.err, search.stopped = "", ""
	search.scroll.Y = 0
	ctx, cancel := context.WithTimeout(a.repositoryWork.Context(), 15*time.Second)
	search.cancel = cancel

	publish := func(page repository.HistoryChangePage, finished bool, err error) {
		a.updateChanged(func() bool {
			if search.token != token {
				return false
			}
			search.page = page
			if finished {
				search.loading = false
				search.cancel = nil
				switch {
				case errors.Is(err, context.Canceled):
					search.stopped = "Search stopped. Results are partial."
				case errors.Is(err, context.DeadlineExceeded):
					search.stopped = "Search timed out. Results are partial."
				case err != nil:
					search.err = safeError(err)
				}
			}
			return search.open
		})
	}
	go func() {
		defer a.recoverBackground()
		defer cancel()
		page, err := session.SearchHistoryChanges(ctx, query, cursor, func(progress repository.HistoryChangePage) {
			publish(progress, false, nil)
		})
		publish(page, true, err)
	}()
}

func (a *app) historyChangesModal(c *ui.Context) {
	search := &a.historyChanges
	if !search.open {
		return
	}
	if search.focusPending {
		search.focus.Focus(c)
		search.focusPending = false
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		if !search.showFilters {
			back.OnShortcut(ui.Ctrl, ui.KeyEnter, a.startHistoryChanges)
			back.OnShortcut(ui.Cmd, ui.KeyEnter, a.startHistoryChanges)
		}
		back.Children(func() {
			panel := ui.Column(c).Width(720).MaxWidthPercent(94).Height(460).MaxHeightPercent(94).
				Padding(18).Gap(10).Background(theme.Background).Border(1, theme.Border).
				Radius(theme.Radius + 4).Role(ui.RoleDialog).Label(tr(historyChangesLabel))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, tr(historyChangesLabel)).Grow(1).FontSize(19).Bold()
					iconButton(c, "Close", "Escape", closeIcon, false, a.closeHistoryChanges)
				})
				a.historyChangesInput(c)
				a.historyChangesScope(c)
				ui.Divider(c)
				ui.Scroll(c).Grow(1).TrackScroll(&search.scroll).Children(func() {
					ui.Column(c).FillWidth().Gap(6).Children(func() {
						a.historyChangesResults(c)
					})
				})
				a.historyChangesFooter(c)
			})
			if !search.showFilters && (panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape)) {
				a.closeHistoryChanges()
			}
		})
	})
	if search.showFilters {
		a.historyChangesFilterModal(c)
	}
}

func (a *app) historyChangesInput(c *ui.Context) {
	search := &a.historyChanges
	draft := &search.draft
	const textLabel = "Text to find in changes"
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(8).Children(func() {
		if strings.Contains(draft.Text, "\n") {
			ui.TextArea(c, &draft.Text).Bind(&search.focus).Grow(1).MinWidth(0).Height(62).Label(tr(textLabel))
		} else {
			field := ui.TextInput(c, &draft.Text).Bind(&search.focus).Grow(1).MinWidth(0).Height(36).
				Label(tr(textLabel)).Placeholder(tr(textLabel))
			if field.Submitted() {
				a.startHistoryChanges()
			}
		}
		label, icon, action := "Search", searchIcon, a.startHistoryChanges
		if search.loading {
			label, icon, action = "Stop search", closeIcon, search.cancel
		}
		ui.PrimaryButton(c, "").Size(40, 36).Padding(0).Center().
			Label(tr(label)).Tooltip(tr(label)).Disabled(!search.loading && draft.Text == "").
			OnClick(action).Children(func() { ui.Icon(c, icon).FontSize(17) })
	})
}

func (a *app) historyChangesScope(c *ui.Context) {
	search := &a.historyChanges
	draft := &search.draft
	theme := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
		ui.Row(c).Gap(2).Padding(2).Radius(theme.Radius).Background(theme.Surface).Children(func() {
			for _, option := range []struct {
				label string
				count bool
			}{
				{label: "Changed lines"},
				{label: "Occurrence count", count: true},
			} {
				c.Key(option.label)
				active := draft.Occurrences == option.count
				button := compactTextButton(ui.ButtonBase(c)).Label(tr(option.label)).Checked(active).
					Radius(theme.Radius)
				if active {
					button.Background(theme.Accent.Alpha(0.18))
				} else if button.Hovered() {
					button.Background(theme.SurfaceHover)
				}
				if option.count {
					button.Tooltip(tr("Find commits where the text became more or less frequent in a file."))
				}
				button.OnClick(func() { draft.Occurrences = option.count }).
					Children(func() { ui.Text(c, tr(option.label)).SingleLine() })
			}
		})
		ui.Spacer(c).Grow(1)
		active := draft.AllBranches || draft.Path != "" || draft.Regex ||
			(draft.Ref != "" && draft.Ref != "HEAD") || !draft.Since.IsZero() || !draft.Until.IsZero()
		button := compactIconButton(ui.ButtonBase(c)).Label(tr("Search filters")).Tooltip(tr("Search filters")).
			Radius(theme.Radius).Checked(search.showFilters).Disabled(search.loading)
		if active {
			button.Background(theme.Accent.Alpha(0.18))
		} else if button.Hovered() {
			button.Background(theme.SurfaceHover)
		} else {
			button.Background(theme.Surface)
		}
		button.OnClick(func() {
			search.filterDraft = search.draft
			search.showFilters = true
		}).Children(func() { ui.Icon(c, filterIcon).FontSize(16) })
	})
}

func (a *app) historyChangesFilterModal(c *ui.Context) {
	search := &a.historyChanges
	draft := &search.filterDraft
	theme := c.Theme()
	closeFilters := func() { search.showFilters = false }
	applyFilters := func() {
		search.draft = *draft
		closeFilters()
		if search.started && search.draft != search.applied {
			a.startHistoryChanges()
		}
	}
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().
			Background(ui.RGBA(0, 0, 0, 0.48)).Modal()
		back.Children(func() {
			panel := ui.Column(c).Width(470).MaxWidthPercent(94).MaxHeightPercent(94).
				Padding(18).Gap(12).Background(theme.Background).Border(1, theme.Border).
				Radius(theme.Radius + 4).Role(ui.RoleDialog).Label(tr("Search filters"))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, tr("Search filters")).Grow(1).FontSize(19).Bold()
					iconButton(c, "Close", "Escape", closeIcon, false, closeFilters)
				})
				ui.Scroll(c).MaxHeight(330).Children(func() {
					ui.Column(c).FillWidth().Gap(12).Children(func() {
						ui.Column(c).FillWidth().Gap(5).Children(func() {
							ui.Text(c, tr("File or folder")).Bold()
							ui.TextInput(c, &draft.Path).FillWidth().AutoFocus().
								Label(tr("File or folder")).Placeholder(tr("Whole repository"))
						})
						ui.Column(c).FillWidth().Gap(5).Children(func() {
							ui.Text(c, tr("Branch, tag or range")).Bold()
							ui.TextInput(c, &draft.Ref).FillWidth().Disabled(draft.AllBranches).
								Label(tr("Branch, tag or range")).Placeholder(tr("HEAD or v1.0..v2.0"))
						})
						ui.Row(c).Gap(14).AlignItems(ui.Center).Wrap().Children(func() {
							settingsSwitchControl(c, "All branches", "", &draft.AllBranches)
							settingsSwitchControl(c, "Regular expression", "", &draft.Regex)
						})
						ui.Column(c).FillWidth().Gap(6).Children(func() {
							commitFilterTimeBound(c, tr("From"), &draft.Since)
							commitFilterTimeBound(c, tr("To"), &draft.Until)
						})
					})
				})
				ui.Divider(c)
				ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
					ui.Button(c, tr("Cancel")).OnClick(closeFilters)
					ui.PrimaryButton(c, tr("Apply filters")).OnClick(applyFilters)
				})
			})
			if back.OverlayShortcut(0, ui.KeyEscape) || panel.PressedOutside() {
				closeFilters()
			}
		})
	})
}

func (a *app) historyChangesResults(c *ui.Context) {
	search := &a.historyChanges
	if !search.started {
		return
	}
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(0, 0, 3, 0).Children(func() {
		if search.loading {
			ui.Spinner(c).Label(tr("Searching Git…"))
			ui.Text(c, tr("Searching Git…")).Bold().Grow(1)
		} else {
			label := "Search results"
			if search.draft != search.applied {
				label = "Previous results"
			}
			ui.Text(c, tr(label)).Bold().Grow(1)
		}
		ui.Text(c, trf("%d checked · %d found", search.page.Scanned, len(search.page.Hits))).TextColor(theme.TextMuted)
	})
	if search.stopped != "" {
		ui.Text(c, tr(search.stopped)).TextColor(theme.TextMuted)
	}
	if search.err != "" {
		ui.Text(c, search.err).TextColor(theme.Danger)
	}
	if !search.loading && search.stopped == "" && search.err == "" && len(search.page.Hits) == 0 {
		ui.Text(c, tr("No matches in this batch.")).TextColor(theme.TextMuted)
	}
	for _, hit := range search.page.Hits {
		c.Key(hit.Commit.Hash)
		row := ui.ButtonBase(c).FillWidth().Padding(8, 10).Gap(10).Radius(theme.Radius).AlignItems(ui.Center).
			Label(hit.Commit.ShortHash + " " + hit.Commit.Subject + " " + hit.Path).
			OnClick(func() { a.openHistoryChangeHit(hit) })
		if row.Hovered() {
			row.Background(theme.SurfaceHover)
		} else {
			row.Background(theme.Surface)
		}
		row.Children(func() {
			ui.Icon(c, fileCodeIcon).FontSize(16).TextColor(theme.TextMuted)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, hit.Commit.Subject).Bold().SingleLine().Tooltip(hit.Commit.Subject)
				ui.Text(c, hit.Path).SingleLine().Tooltip(hit.Path).TextColor(theme.TextMuted)
			})
			ui.Column(c).AlignItems(ui.End).Gap(2).Children(func() {
				ui.Text(c, hit.Commit.ShortHash).TextColor(theme.TextMuted).SingleLine()
				if when := relTime(hit.Commit.UnixTimestamp); when != "" {
					ui.Text(c, when).TextColor(theme.TextMuted).SingleLine()
				}
			})
			ui.Icon(c, chevronRightIcon).FontSize(13).TextColor(theme.TextMuted)
		})
	}
}

func (a *app) historyChangesFooter(c *ui.Context) {
	search := &a.historyChanges
	if search.loading || !search.started || search.draft != search.applied ||
		(len(search.previous) == 0 && (!search.page.More || search.err != "")) {
		return
	}
	ui.Row(c).Wrap().Justify(ui.End).AlignItems(ui.Center).Gap(8).Children(func() {
		if count := len(search.previous); count > 0 {
			ui.Button(c, tr("Previous batch")).OnClick(func() {
				cursor := search.previous[count-1]
				search.previous = search.previous[:count-1]
				a.readHistoryChanges(cursor)
			})
		}
		if search.page.More && search.err == "" {
			label := "Continue into older history"
			if search.stopped != "" {
				label = "Resume search"
			}
			ui.Button(c, tr(label)).OnClick(func() {
				if search.page.Scanned > 0 {
					search.previous = append(search.previous, search.page.Start)
				}
				a.readHistoryChanges(search.page.Next)
			})
		}
	})
}

func (a *app) openHistoryChangeHit(hit repository.HistoryChangeHit) {
	query := a.historyChanges.applied
	a.closeHistoryChanges()
	a.showCommitInHistory(hit.Commit.Hash)
	a.resetCommitDetails()
	a.historyDiffTarget = &historyDiffTarget{hash: hit.Commit.Hash, path: hit.Path, change: &query}
}

// A multiline literal can span changed and context lines. Use its first
// changed line when available and still reveal the file for binary matches.
func scrollToHistoryChange(state *editor.DiffState, query repository.HistoryChangeQuery) {
	match := func(line string) bool {
		return strings.Contains(line, query.Text)
	}
	if query.Regex {
		pattern, err := regexp.CompilePOSIX(query.Text)
		if err != nil {
			return
		}
		match = pattern.MatchString
	} else if parts := strings.Split(query.Text, "\n"); len(parts) > 1 {
		match = func(line string) bool {
			for _, part := range parts {
				if part != "" && strings.Contains(line, part) {
					return true
				}
			}
			return false
		}
	}
	for _, hunk := range state.Hunks() {
		for line := hunk.OldStart; line < hunk.OldEnd; line++ {
			if match(state.Old.Line(line)) {
				state.SetScrollY(state.LineTop(editor.DiffOld, line))
				return
			}
		}
		for line := hunk.NewStart; line < hunk.NewEnd; line++ {
			if match(state.New.Line(line)) {
				state.SetScrollY(state.LineTop(editor.DiffNew, line))
				return
			}
		}
	}
}
