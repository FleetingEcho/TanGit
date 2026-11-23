package app

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

const commandLogPageSize = 20

const (
	commandLogStatusAll       = ""
	commandLogStatusSucceeded = "succeeded"
	commandLogStatusFailed    = "failed"
	commandLogStatusRunning   = "running"
)

const (
	commandLogRangeHour       = "1h"
	commandLogRangeThreeHours = "3h"
	commandLogRangeDay        = "24h"
	commandLogRangeThreeDays  = "3d"
	commandLogRangeWeek       = "7d"
	commandLogRangeAll        = "all"
)

// resetCommandLogPaging affects display state only. Canonical logger records
// and completion notifications remain untouched.
func (a *app) resetCommandLogPaging() {
	a.commandLogLimit = commandLogPageSize
	a.commandLogList = ui.ListState{}
	a.commandLogScroll = ui.ScrollState{}
	a.commandLogExpanded = nil
	a.commandLogCollapsed = nil
}

func (a *app) resetCommandLogFilters() {
	a.commandLogStatus = commandLogStatusAll
	a.commandLogSource = ""
	a.commandLogRange = commandLogRangeDay
}

func (a *app) clearCommandLogFilters() {
	a.commandLogQuery = ""
	a.commandLogStatus = commandLogStatusAll
	a.commandLogSource = ""
	a.commandLogRange = commandLogRangeAll
	a.resetCommandLogPaging()
}

func (a *app) commandLogTime() time.Time {
	if a.commandLogClock != nil {
		return a.commandLogClock()
	}
	return time.Now()
}

func commandLogTimeRangeDuration(value string) time.Duration {
	switch value {
	case commandLogRangeHour:
		return time.Hour
	case commandLogRangeThreeHours:
		return 3 * time.Hour
	case commandLogRangeDay:
		return 24 * time.Hour
	case commandLogRangeThreeDays:
		return 3 * 24 * time.Hour
	case commandLogRangeWeek:
		return 7 * 24 * time.Hour
	default:
		return 0
	}
}

func commandLogTimeRangeLabel(value string) string {
	switch value {
	case commandLogRangeHour:
		return tr("Last hour")
	case commandLogRangeThreeHours:
		return tr("Last 3 hours")
	case commandLogRangeDay:
		return tr("Last 24 hours")
	case commandLogRangeThreeDays:
		return tr("Last 3 days")
	case commandLogRangeWeek:
		return tr("Last week")
	default:
		return tr("All time")
	}
}

func commandLogEntryStatus(entry repository.CommandEntry) string {
	if entry.Running {
		return commandLogStatusRunning
	}
	if entry.Failed {
		return commandLogStatusFailed
	}
	return commandLogStatusSucceeded
}

func commandLogStatusLabel(value string) string {
	switch value {
	case commandLogStatusSucceeded:
		return tr("Succeeded")
	case commandLogStatusFailed:
		return tr("Failed")
	case commandLogStatusRunning:
		return tr("Running")
	default:
		return tr("All statuses")
	}
}

// commandLogEntrySource records how the existing command was initiated.
// Source narrows the two existing origin tabs; it is not inferred from an
// executable name because a user-typed git command is still user work.
func commandLogEntrySource(entry repository.CommandEntry) string {
	if entry.InvocationSource == "palette" {
		return "palette"
	}
	if entry.Shortcut != nil || entry.InvocationSource == "keyboard" {
		return "keyboard"
	}
	if entry.Origin == repository.CommandOriginUser {
		return "direct"
	}
	return "background"
}

func commandLogSourceLabel(value string) string {
	switch value {
	case "palette":
		return tr("Command Palette")
	case "keyboard":
		return tr("Keyboard")
	case "direct":
		return tr("Your commands")
	case "background":
		return tr("lazygitGo actions")
	default:
		return tr("All sources")
	}
}

func commandLogSourceOrigin(source string, fallback repository.CommandOrigin) repository.CommandOrigin {
	switch source {
	case "palette", "keyboard", "direct":
		return repository.CommandOriginUser
	case "background":
		return repository.CommandOriginGit
	default:
		return fallback
	}
}

func (a *app) selectCommandLogSource(value string) {
	a.commandLogSource = value
	a.commandLogUser = commandLogSourceOrigin(value, repository.CommandOriginGit) == repository.CommandOriginUser
}

func commandLogMatchesFilters(entry repository.CommandEntry, origin repository.CommandOrigin, query, status, source, timeRange string, now time.Time) bool {
	origin = commandLogSourceOrigin(source, origin)
	if entry.Origin != origin {
		return false
	}
	if query != "" && !strings.Contains(strings.ToLower(commandLogEntrySearchText(entry)), strings.ToLower(query)) {
		return false
	}
	if status != commandLogStatusAll && commandLogEntryStatus(entry) != status {
		return false
	}
	if source != "" && commandLogEntrySource(entry) != source {
		return false
	}
	if duration := commandLogTimeRangeDuration(timeRange); duration > 0 {
		return !entry.At.IsZero() && !entry.At.Before(now.Add(-duration)) && !entry.At.After(now)
	}
	return true
}

// commandLogVisibleEntries filters the canonical records without fetching or
// copying command output again. Rendering limits the returned matches to the
// current page; filters and Copy log still see every matching entry.
func (a *app) commandLogVisibleEntries() []repository.CommandEntry {
	entries := a.commandLogEntries()
	query := strings.TrimSpace(a.commandLogQuery)
	origin := repository.CommandOriginGit
	if a.commandLogUser {
		origin = repository.CommandOriginUser
	}
	visible := make([]repository.CommandEntry, 0, len(entries))
	now := a.commandLogTime()
	for _, entry := range entries {
		if commandLogMatchesFilters(entry, origin, query, a.commandLogStatus, a.commandLogSource, a.commandLogRange, now) {
			visible = append(visible, entry)
		}
	}
	return visible
}

// commandLogTools presents independent sections and the search field rather
// than hiding custom commands in a single undifferentiated Git stream.
func (a *app) commandLogTools(c *ui.Context) {
	user, git := 0, 0
	for _, entry := range a.commandLogEntries() {
		if entry.Origin == repository.CommandOriginUser {
			user++
		} else {
			git++
		}
	}
	section := 0
	if a.commandLogUser {
		section = 1
	}
	tabs := ui.Tabs(c, &section,
		fmt.Sprintf("%s (%d)", tr("Background Git commands"), git),
		fmt.Sprintf("%s (%d)", tr("Your commands"), user))
	if tabs.Changed() {
		a.commandLogUser = section == 1
		a.commandLogSource = ""
		a.resetCommandLogPaging()
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, searchIcon).FontSize(14).TextColor(c.Theme().TextMuted)
		search := ui.TextInput(c, &a.commandLogQuery).Label(tr("Search commands")).
			Placeholder(tr("Command or output")).Grow(1).AutoFocus()
		if search.Changed() {
			a.resetCommandLogPaging()
		}
		if strings.TrimSpace(a.commandLogQuery) != "" {
			flatIconButton(c, "Clear search", "Clear the command log search", closeIcon, 14, 4, false, func() {
				a.commandLogQuery = ""
				a.resetCommandLogPaging()
			})
		}
	})
	statusOptions := []commandLogFilterOption{
		{commandLogStatusAll, tr("All statuses")},
		{commandLogStatusSucceeded, tr("Succeeded")},
		{commandLogStatusFailed, tr("Failed")},
		{commandLogStatusRunning, tr("Running")},
	}
	sourceOptions := []commandLogFilterOption{
		{"", tr("All sources")}, {"palette", tr("Command Palette")},
		{"keyboard", tr("Keyboard")}, {"direct", tr("Your commands")},
		{"background", tr("Background Git commands")},
	}
	timeOptions := []commandLogFilterOption{
		{commandLogRangeHour, tr("Last hour")},
		{commandLogRangeThreeHours, tr("Last 3 hours")},
		{commandLogRangeDay, tr("Last 24 hours")},
		{commandLogRangeThreeDays, tr("Last 3 days")},
		{commandLogRangeWeek, tr("Last week")},
		{commandLogRangeAll, tr("All time")},
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
		a.commandLogFilterMenu(c, "Status filter", commandLogStatusLabel(a.commandLogStatus), statusOptions, a.commandLogStatus, func(value string) {
			a.commandLogStatus = value
		})
		a.commandLogFilterMenu(c, "Source filter", commandLogSourceLabel(a.commandLogSource), sourceOptions, a.commandLogSource, a.selectCommandLogSource)
		a.commandLogFilterMenu(c, "Time range filter", commandLogTimeRangeLabel(a.commandLogRange), timeOptions, a.commandLogRange, func(value string) {
			a.commandLogRange = value
		})
		filtered := strings.TrimSpace(a.commandLogQuery) != "" || a.commandLogStatus != "" || a.commandLogSource != "" ||
			(a.commandLogRange != "" && a.commandLogRange != commandLogRangeAll)
		if ui.Button(c, tr("Clear filters")).Disabled(!filtered).Clicked() {
			a.clearCommandLogFilters()
		}
	})
}

type commandLogFilterOption struct {
	value string
	label string
}

func (a *app) commandLogFilterMenu(c *ui.Context, name, label string, options []commandLogFilterOption, selected string, selectOption func(string)) {
	button := ui.Button(c, label+" ˅").Label(tr(name)).Tooltip(tr("Choose a filter"))
	button.PopoverMenuButton(func(menu *ui.Menu) {
		for _, option := range options {
			option := option
			if menu.Item(option.label).Checked(option.value == selected).Chosen() {
				selectOption(option.value)
				a.resetCommandLogPaging()
			}
		}
	})
}

func commandLogEntryText(entry repository.CommandEntry) string {
	if entry.InvocationSource == "palette" {
		// A palette entry is an accepted application action, not a shell line.
		// Any real Git/custom process it starts has its own $-prefixed record.
		return entry.Command
	}
	if entry.Shortcut != nil {
		return fmt.Sprintf("[%s] %s", entry.Shortcut.Chord, entry.Command)
	}
	return "$ " + entry.Command
}

func commandLogEntrySearchText(entry repository.CommandEntry) string {
	text := entry.Command + "\n" + entry.Output
	if entry.Shortcut != nil {
		text = entry.Shortcut.Chord + "\n" + entry.Shortcut.ID + "\n" + text
	}
	return text
}

// highlightPart is one chunk of rendered command output. Parts preserve order
// so rendering behaves exactly like the unbroken string when the query clears.
type highlightPart struct {
	text    string
	matched bool
}

func commandHighlightParts(text, query string) []highlightPart {
	if query == "" || text == "" {
		return []highlightPart{{text: text}}
	}
	parts := make([]highlightPart, 0, 4)
	for text != "" {
		start, end, found := nextCommandHighlightMatch(text, query)
		if !found {
			parts = append(parts, highlightPart{text: text})
			break
		}
		if start > 0 {
			parts = append(parts, highlightPart{text: text[:start]})
		}
		parts = append(parts, highlightPart{text: text[start:end], matched: true})
		text = text[end:]
	}
	return parts
}

// nextCommandHighlightMatch returns byte boundaries of a Unicode-safe,
// case-insensitive match. Query runes are counted in the original string,
// never in its lowercased bytes; case folding can change UTF-8 byte lengths.
func nextCommandHighlightMatch(text, query string) (int, int, bool) {
	if query == "" {
		return 0, 0, false
	}
	queryRunes := utf8.RuneCountInString(query)
	for start := range text {
		end, count := start, 0
		for end < len(text) && count < queryRunes {
			_, size := utf8.DecodeRuneInString(text[end:])
			end += size
			count++
		}
		if count == queryRunes && strings.EqualFold(text[start:end], query) {
			return start, end, true
		}
	}
	return 0, 0, false
}

// commandLogHighlighted builds ONE rich-text element for a logical line.
// Spans change only paint: copying the text yields exactly what the command
// printed, with no extra newlines between highlighted fragments.
func commandLogHighlighted(c *ui.Context, text, query string, color ui.Color) ui.Element {
	theme := c.Theme()
	spans := make([]ui.Span, 0, 4)
	for _, part := range commandHighlightParts(text, strings.TrimSpace(query)) {
		span := ui.Span{Text: part.text, Color: color}
		if part.matched {
			span.Background = theme.Accent.Alpha(0.28)
			span.Color = theme.Text
			span.Weight = 600
		}
		spans = append(spans, span)
	}
	return ui.RichText(c, spans...).FontSize(theme.FontSize - 1)
}
