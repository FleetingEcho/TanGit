package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// filteredSourceRows returns snapshot indices for visible rows. Lists bind
// their selection to a separate visible-row index; repository actions always
// use the source index, even after a search hides earlier rows.
// filteredCache memoizes one filteredSourceRows call per filter state; idle
// frames hit it instead of rejoining and re-lowering every row.
type filteredCache struct {
	set      bool
	snapshot uint64
	query    string
	length   int
	rows     []int
}

// filteredCached computes the visible rows for one filter, reusing the last
// result while the query, the snapshot seat and the entry count are unchanged.
func filteredCached(cache *filteredCache, snapshot uint64, length int, query string, text func(int) string) []int {
	q := strings.ToLower(strings.TrimSpace(query))
	if cache.set && cache.snapshot == snapshot && cache.query == q && cache.length == length {
		return cache.rows
	}
	rows := filteredSourceRows(length, query, text)
	*cache = filteredCache{set: true, snapshot: snapshot, query: q, length: length, rows: rows}
	return rows
}

func filteredSourceRows(length int, query string, text func(int) string) []int {
	query = strings.ToLower(strings.TrimSpace(query))
	rows := make([]int, 0, length)
	for source := range length {
		if query == "" || strings.Contains(strings.ToLower(text(source)), query) {
			rows = append(rows, source)
		}
	}
	return rows
}

func sourceRow[T any](rows []int, items []T, row int) (T, bool) {
	var zero T
	if row < 0 || row >= len(rows) {
		return zero, false
	}
	source := rows[row]
	if source < 0 || source >= len(items) {
		return zero, false
	}
	return items[source], true
}

// selectFilteredRow moves both indices together. An empty result has no
// source or row selection, so detail panes and actions cannot act on a hidden
// item from before the search.
func selectFilteredRow(rows []int, row int, selectedRow, selectedSource *int) {
	*selectedRow, *selectedSource = -1, -1
	if row >= 0 && row < len(rows) {
		*selectedRow, *selectedSource = row, rows[row]
	}
}

// syncFilteredRow follows snapshot selection restoration by source identity.
// If that item is hidden, the first visible match becomes active instead.
func syncFilteredRow(rows []int, selectedRow, selectedSource *int) {
	for row, source := range rows {
		if source == *selectedSource {
			*selectedRow = row
			return
		}
	}
	selectFilteredRow(rows, 0, selectedRow, selectedSource)
}

// normalizeFilteredRow repairs MyGo's temporary -1 selection after a list's
// keys changed. Non-empty results always have a visible keyboard selection.
func normalizeFilteredRow(rows []int, selectedRow, selectedSource *int) {
	if len(rows) > 0 && (*selectedRow < 0 || *selectedRow >= len(rows)) {
		selectFilteredRow(rows, 0, selectedRow, selectedSource)
	}
}

// listSearchField is the common quick-search row: a slash-focusable input,
// visible/total count, and an explicit clear action. onChange starts the list
// at its first match and closes any inline action whose subject may have moved.
func listSearchField(c *ui.Context, handle *ui.Handle, query *string,
	label, placeholder, clearLabel string, matches, total int, onChange func(), onNavigate ...func(int),
) {
	listSearchFieldTip(c, handle, query, label, placeholder, clearLabel, "", matches, total, onChange, onNavigate...)
}

// listSearchFieldTip is listSearchField with a tooltip on the count, for a list
// whose number is how far it has loaded and not how many entries there are: the
// tooltip says how to reach the rest.
func listSearchFieldTip(c *ui.Context, handle *ui.Handle, query *string,
	label, placeholder, clearLabel, countTip string, matches, total int, onChange func(), onNavigate ...func(int),
) {
	theme := c.Theme()
	badge := func(text string) {
		element := ui.Badge(c, text)
		if countTip != "" {
			element.Tooltip(countTip)
		}
	}
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Icon(c, searchIcon).FontSize(14).TextColor(theme.TextMuted)
		input := ui.TextInput(c, query).Bind(handle).
			Label(tr(label)).Placeholder(tr(placeholder)).Grow(1)
		if input.Changed() {
			onChange()
		}
		if strings.TrimSpace(*query) == "" {
			badge(fmt.Sprintf("%d", total))
			return
		}
		badge(fmt.Sprintf("%d / %d", matches, total))
		if matches > 0 && len(onNavigate) > 0 && onNavigate[0] != nil {
			commitHeaderButton(c, tr(label)+": "+tr("previous result"), "Shift+F3", chevronUpIcon, func() {
				onNavigate[0](-1)
			})
			commitHeaderButton(c, tr(label)+": "+tr("next result"), "F3", chevronDownIcon, func() {
				onNavigate[0](1)
			})
		}
		commitHeaderButton(c, clearLabel, "Show every item", closeIcon, func() {
			*query = ""
			onChange()
		})
	})
}

func listSearchEmpty(c *ui.Context, message string) {
	theme := c.Theme()
	ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
		ui.Icon(c, searchIcon).FontSize(22).TextColor(theme.TextMuted)
		ui.Text(c, tr(message)).TextColor(theme.TextMuted).TextAlign(ui.Center)
	})
}
