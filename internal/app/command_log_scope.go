package app

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// commandLogVisibleEntries gives one section at a time, then narrows it by a
// case-insensitive command-and-output query. Origin comes from the logger,
// not the executable name: a user-typed `git status` remains user work even
// though the app also runs that command on its own.
func (a *app) commandLogVisibleEntries() []repository.CommandEntry {
	entries := a.commandLogEntries()
	query := strings.ToLower(strings.TrimSpace(a.commandLogQuery))
	origin := repository.CommandOriginGit
	if a.commandLogUser {
		origin = repository.CommandOriginUser
	}
	visible := make([]repository.CommandEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Origin != origin {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(entry.Command+"\n"+entry.Output), query) {
			continue
		}
		visible = append(visible, entry)
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
	ui.Row(c).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
		commandLogScopeButton(c, "Your commands", "Show commands you chose to run", user,
			a.commandLogUser, func() { a.commandLogUser = true })
		commandLogScopeButton(c, "Background Git commands", "Show Git and hosting actions from TanGit", git,
			!a.commandLogUser, func() { a.commandLogUser = false })
	})
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, searchIcon).FontSize(14).TextColor(c.Theme().TextMuted)
		ui.TextInput(c, &a.commandLogQuery).Label(tr("Search commands")).
			Placeholder(tr("Command or output")).Grow(1).AutoFocus()
		if strings.TrimSpace(a.commandLogQuery) != "" {
			flatIconButton(c, "Clear search", "Clear the command log search", closeIcon, 14, 4, false, func() {
				a.commandLogQuery = ""
			})
		}
	})
}

func commandLogScopeButton(c *ui.Context, title, tooltip string, count int, active bool, run func()) {
	label := tr(title)
	var button ui.Element
	if active {
		button = ui.PrimaryButton(c, "")
	} else {
		button = ui.Button(c, "")
	}
	button = compactTextButton(button).Label(label).Tooltip(tr(tooltip)).Gap(6).Children(func() {
		ui.Text(c, label)
		ui.Badge(c, strconv.Itoa(count))
	})
	if button.Clicked() {
		run()
	}
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
func commandLogHighlighted(c *ui.Context, text, query string, color ui.Color) {
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
	ui.RichText(c, spans...).FontSize(theme.FontSize - 1)
}
