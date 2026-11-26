package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// commandLogTimestampLayout is the full local timestamp in copied text and
// row tooltips. Compact rows show the clock, matching the rest of the log UI.
const commandLogTimestampLayout = "2006-01-02 15:04:05"

// commandLogModal is a read-only projection of one repository's canonical
// session logger. Its page size never controls command recording or output.
func (a *app) commandLogModal(c *ui.Context) {
	if !a.commandLogOpen {
		return
	}
	theme := c.Theme()
	entries := a.commandLogVisibleEntries()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(940).MaxWidthPercent(94).HeightPercent(92).MaxHeightPercent(92).
				Padding(20).Gap(12).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Command log")).
				Children(func() {
					a.commandLogHeader(c, len(entries))
					ui.Divider(c)
					a.commandLogTools(c)
					a.commandLogBody(c, entries)
					ui.Divider(c)
					a.commandLogFooter(c, entries)
				})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeCommandLog()
			}
		})
	})
}

// commandLogEntries copies the mutex-protected record before the frame walks
// it. Git and custom-command goroutines may continue recording concurrently.
func (a *app) commandLogEntries() []repository.CommandEntry {
	if a.commandLog == nil {
		return nil
	}
	return a.commandLog.Entries()
}

func (a *app) commandLogHeader(c *ui.Context, count int) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Icon(c, logIcon).FontSize(20).TextColor(theme.Accent)
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, tr("Command log")).FontSize(19).Bold()
			ui.Text(c, tr("Git, hosting and custom commands run in this workspace, with what they printed.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		})
		ui.Badge(c, fmt.Sprintf("%d", count)).Label(tr("Matching commands"))
		iconButton(c, "Close command log", "Escape", closeIcon, false, a.closeCommandLog)
	})
}

// commandLogBody virtualizes visible rows and reveals one exact command ID.
// EndReached is supplied by MyGo only after user scrolling, not initial layout
// or programmatic failure navigation. Expanding a page is in-memory UI work.
func (a *app) commandLogBody(c *ui.Context, entries []repository.CommandEntry) {
	theme := c.Theme()
	reveal := a.commandLogReveal
	a.commandLogReveal = nil
	if len(entries) == 0 {
		message := tr("No commands in this section yet.")
		hint := tr("Your configured and one-off commands appear in Your commands.")
		if !a.commandLogUser {
			hint = tr("Git actions from lazygitGo appear in Background Git commands.")
		}
		hasSectionEntries := false
		origin := repository.CommandOriginGit
		if a.commandLogUser {
			origin = repository.CommandOriginUser
		}
		for _, entry := range a.commandLogEntries() {
			if entry.Origin == origin {
				hasSectionEntries = true
				break
			}
		}
		if hasSectionEntries && (strings.TrimSpace(a.commandLogQuery) != "" || a.commandLogStatus != "" || a.commandLogSource != "" ||
			(a.commandLogRange != "" && a.commandLogRange != commandLogRangeAll)) {
			message = tr("No commands match your search.")
			hint = tr("Try a different search, status, source or time range.")
		}
		ui.Column(c).Grow(1).Center().Gap(8).Padding(30).Children(func() {
			ui.Icon(c, logIcon).FontSize(26).TextColor(theme.TextMuted)
			ui.Text(c, message).Bold()
			ui.Text(c, hint).TextColor(theme.TextMuted).TextAlign(ui.Center).MaxWidth(360)
			if ui.Button(c, tr("Clear filters")).Clicked() {
				a.clearCommandLogFilters()
			}
		})
		return
	}

	limit := a.commandLogLimit
	if limit < commandLogPageSize {
		limit = commandLogPageSize
	}
	var revealRow int
	if reveal != nil {
		for index := len(entries) - 1; index >= 0; index-- {
			if entries[index].ID() == reveal.id && (reveal.log == nil || reveal.log == a.commandLog) {
				revealRow = len(entries) - 1 - index
				if limit < revealRow+1 {
					limit = revealRow + 1
				}
				break
			}
		}
	}
	if limit > len(entries) {
		limit = len(entries)
	}
	a.commandLogLimit = limit
	a.commandLogList.Key = func(row int) any {
		if id := entries[len(entries)-1-row].ID(); id != 0 {
			return id
		}
		// Directly constructed presentation fixtures have no logger identity.
		// Real records always have a nonzero monotonically increasing ID.
		return fmt.Sprintf("untracked-row-%d", row)
	}
	a.commandLogList.KeepOffset = false // anchor to command ID when newer entries arrive
	a.commandLogList.Label = func(row int) string {
		// The virtual row and its nested failure-highlight region need distinct
		// names for assistive technology and exact-region geometry tests.
		return commandLogEntryText(entries[len(entries)-1-row])
	}
	a.commandLogList.NearEnd = 0
	a.commandLogList.EndReached = func(last int) {
		if a.commandLogLimit >= len(entries) {
			return
		}
		a.commandLogLimit = min(a.commandLogLimit+commandLogPageSize, len(entries))
		c.Invalidate()
	}
	list := ui.List(c, &a.commandLogList, limit).
		Grow(1).FillWidth().KeepScrollbars(true).TrackScroll(&a.commandLogScroll).
		Label(tr("Command history"))
	list.Rows(func(row ui.ListRow) {
		index := len(entries) - 1 - row.Index
		entry := entries[index]
		ui.Column(row.Context).FillWidth().Children(func() {
			a.commandLogEntry(row.Context, entry, reveal != nil && entry.ID() == reveal.id)
			ui.Divider(row.Context)
		})
	})
	if reveal != nil && revealRow > 0 {
		// Count is already installed by List, so this is a same-layout request.
		a.commandLogList.ScrollIntoView(revealRow)
	}
}

func commandLogEntryStatusStyle(c *ui.Context, entry repository.CommandEntry) (string, ui.Color, *ui.SVG) {
	theme := c.Theme()
	switch commandLogEntryStatus(entry) {
	case commandLogStatusFailed:
		return tr("Failed"), theme.Danger, conflictIcon
	case commandLogStatusRunning:
		return tr("Running"), theme.Warning, refreshIcon
	default:
		return tr("Succeeded"), theme.Success, commandSuccessIcon
	}
}

func (a *app) commandLogEntrySourceText(entry repository.CommandEntry) string {
	repositoryName := repoName(a.location.String())
	if repositoryName == "" {
		repositoryName = repoName(a.snapshot.Root)
	}
	if repositoryName == "" {
		repositoryName = tr("Current repository")
	}
	return commandLogSourceLabel(commandLogEntrySource(entry)) + " · " + tr("Session") + " · " + repositoryName
}

func (a *app) commandLogEntry(c *ui.Context, entry repository.CommandEntry, reveal ...bool) {
	theme := c.Theme()
	status, statusColor, statusIcon := commandLogEntryStatusStyle(c, entry)
	container := ui.Column(c).FillWidth().Gap(4).Padding(8, 6).Selectable().
		Role(ui.RoleRegion).Label(commandLogEntryText(entry))
	if entry.Failed {
		container = container.Background(theme.Danger.Alpha(0.14)).
			Border(1, theme.Danger.Alpha(0.55)).Radius(theme.Radius - 1)
	}
	if len(reveal) > 0 && reveal[0] {
		container = container.Border(2, theme.Accent)
	}
	container.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(10).MinWidth(0).Children(func() {
			// The adjacent badge owns the accessible status name; announcing
			// the same status again for this decorative icon would be redundant.
			ui.Icon(c, statusIcon).FontSize(16).TextColor(statusColor).Tooltip(status)
			ui.Column(c).Gap(4).Grow(1).MinWidth(0).Children(func() {
				commandLogHighlighted(c, commandLogEntryText(entry), a.commandLogQuery, theme.Text).
					SingleLine().Ellipsis("…").Tooltip(commandLogEntryText(entry))
			})
			ui.Badge(c, status).Background(statusColor.Alpha(0.14)).TextColor(statusColor).
				FontSize(theme.FontSize - 2).Tooltip(tr("Command status"))
			// Only a command that took a noticeable time shows how long: it is
			// what tells a slow repository from a slow command.
			if entry.Duration >= time.Second {
				ui.Text(c, commandLogDuration(entry.Duration)).TextColor(theme.Warning).
					FontSize(theme.FontSize - 1).Tooltip(tr("How long the command ran"))
			}
			if !entry.At.IsZero() {
				absolute := entry.At.Local().Format(commandLogTimestampLayout)
				ui.Text(c, entry.At.Local().Format("15:04")).TextColor(theme.TextMuted).
					FontSize(theme.FontSize - 1).Tooltip(absolute).Label(absolute)
			}
			a.commandLogEntryMenu(c, entry)
		})
		ui.Row(c).Padding(0, 26).FillWidth().Children(func() {
			ui.Text(c, a.commandLogEntrySourceText(entry)).TextColor(theme.TextMuted).
				FontSize(theme.FontSize - 2).SingleLine().Ellipsis("…").
				Tooltip(commandLogSourceLabel(commandLogEntrySource(entry)) + " · " + tr("Session"))
		})
		expanded := a.commandLogOutputExpanded(entry)
		output := strings.TrimRight(entry.Output, "\n")
		if output != "" && expanded {
			ui.Box(c).FillWidth().Padding(7, 9).Margin(4, 0, 0, 26).Radius(theme.Radius-1).
				Background(theme.Surface).Border(1, theme.Border).Children(func() {
				for line := range strings.SplitSeq(output, "\n") {
					commandLogHighlighted(c, line, a.commandLogQuery, theme.TextMuted)
				}
			})
		}
	})
}

func (a *app) commandLogOutputExpanded(entry repository.CommandEntry) bool {
	if a.commandLogCollapsed[entry.ID()] {
		return false
	}
	if entry.Failed || a.commandLogExpanded[entry.ID()] {
		return true
	}
	// Any nonempty search makes matching output available too, including a
	// command whose output matched but whose command text did not.
	return strings.TrimSpace(a.commandLogQuery) != ""
}

func (a *app) commandLogEntryMenu(c *ui.Context, entry repository.CommandEntry) {
	hasOutput := strings.TrimSpace(entry.Output) != ""
	button := ui.ButtonBase(c).Padding(4).Radius(c.Theme().Radius).
		Label(tr("Command actions")).Tooltip(tr("Copy or inspect this command"))
	if button.Hovered() {
		button.Background(c.Theme().SurfaceHover)
	}
	button.PopoverMenuButton(func(menu *ui.Menu) {
		if hasOutput {
			label := tr("Show output")
			if a.commandLogOutputExpanded(entry) {
				label = tr("Hide output")
			}
			if menu.Item(label).Chosen() {
				if a.commandLogCollapsed == nil {
					a.commandLogCollapsed = make(map[uint64]bool)
				}
				if a.commandLogOutputExpanded(entry) {
					a.commandLogCollapsed[entry.ID()] = true
				} else {
					delete(a.commandLogCollapsed, entry.ID())
					if a.commandLogExpanded == nil {
						a.commandLogExpanded = make(map[uint64]bool)
					}
					a.commandLogExpanded[entry.ID()] = true
				}
			}
		}
		if menu.Item(tr("Copy command")).Chosen() {
			c.Services().WriteClipboard(commandLogEntryText(entry))
		}
		if hasOutput && menu.Item(tr("Copy output")).Chosen() {
			c.Services().WriteClipboard(entry.Output)
		}
	})
	button.Children(func() {
		ui.Icon(c, moreIcon).FontSize(13).TextColor(c.Theme().TextMuted)
	})
}

func (a *app) commandLogFooter(c *ui.Context, entries []repository.CommandEntry) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
		shown := min(a.commandLogLimit, len(entries))
		count := commandCount(len(entries))
		if shown < len(entries) {
			count = trf("Showing %d of %d commands", shown, len(entries))
		}
		ui.Text(c, count).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		if shown < len(entries) {
			ui.Text(c, tr("Scroll down to load more")).TextColor(theme.TextMuted).
				FontSize(theme.FontSize - 1).Grow(1)
		} else {
			ui.Box(c).Grow(1)
		}
		if ui.Button(c, tr("Copy log")).Disabled(len(entries) == 0).
			Tooltip(tr("Copy every command and its output")).Clicked() {
			c.Services().WriteClipboard(formatCommandLog(entries))
			a.notify("success", "Copied command log", trf("%d commands", len(entries)))
		}
		if ui.Button(c, tr("Clear")).Disabled(len(entries) == 0).Clicked() {
			a.clearCommandLog()
			c.Invalidate()
		}
		if ui.PrimaryButton(c, tr("Done")).Clicked() {
			a.closeCommandLog()
		}
	})
}

func commandCount(count int) string {
	if count == 1 {
		return tr("1 command")
	}
	return trf("%d commands", count)
}

// formatCommandLog includes full local timestamps as well as the complete
// output even when that output is collapsed in a compact row.
func formatCommandLog(entries []repository.CommandEntry) string {
	var text strings.Builder
	for _, entry := range entries {
		if !entry.At.IsZero() {
			fmt.Fprintf(&text, "[%s] ", entry.At.Format(commandLogTimestampLayout))
		}
		fmt.Fprintf(&text, "%s", commandLogEntryText(entry))
		if entry.Duration >= time.Second {
			fmt.Fprintf(&text, " (%s)", commandLogDuration(entry.Duration))
		}
		text.WriteString("\n")
		if output := strings.TrimRight(entry.Output, "\n"); output != "" {
			text.WriteString(output)
			text.WriteString("\n")
		}
	}
	return text.String()
}

// commandLogDuration writes a duration the way the log shows it: seconds with
// one decimal under a minute, minutes and seconds beyond.
func commandLogDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}
