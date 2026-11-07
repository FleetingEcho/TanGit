package app

import (
	"fmt"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// openTerminalSearch reveals and focuses only the active shell's find bar.
// The handle lives in the session, so a split or a drag cannot redirect focus.
func (a *app) openTerminalSearch() {
	session := a.activeTerminal()
	if session == nil || session.pane == nil {
		return
	}
	session.searchOpen = true
	session.searchHandle.Focus()
}

// closeActiveTerminalSearch ends only the active search and gives the keyboard
// back to its shell. Other cards retain their queries and highlighted matches.
func (a *app) closeActiveTerminalSearch() {
	session := a.activeTerminal()
	if session == nil || !session.searchOpen {
		return
	}
	a.closeTerminalSearch(session.id)
}

func (a *app) closeTerminalSearch(id uint64) {
	index, session := a.terminalSessionByID(id)
	if session == nil || !session.searchOpen {
		return
	}
	a.endTerminalSearch(session)
	a.term.active = index
	if handle := a.keyboardHandles[fmt.Sprintf("terminal-%d", id)]; handle != nil {
		handle.Focus()
	}
}

func (a *app) endTerminalSearch(session *terminalSessionState) {
	if session.pane != nil {
		session.pane.EndSearch()
	}
	session.searchOpen = false
	session.searchText = ""
	session.searchStatus = terminal.SearchStatus{}
	session.searchHandle.CancelFocus()
}

func (a *app) navigateTerminalSearch(direction int) {
	session := a.activeTerminal()
	if session == nil || session.pane == nil || !session.searchOpen {
		return
	}
	if direction < 0 {
		session.pane.SearchPrevious()
	} else {
		session.pane.SearchNext()
	}
}

// terminalSearchView draws one session's find controls above its terminal.
// It reacquires the real session by stable ID because the card intentionally
// renders from a snapshot while a drag queues its move for the next build.
func (a *app) terminalSearchView(c *ui.Context, id uint64) {
	_, session := a.terminalSessionByID(id)
	if session == nil || !session.searchOpen || session.pane == nil {
		return
	}
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(2, 4).Label(tr("Terminal search")).Children(func() {
		ui.Icon(c, searchIcon).FontSize(14).TextColor(theme.TextMuted)
		input := ui.TextInput(c, &session.searchText).Bind(&session.searchHandle).
			Label(tr("Search terminal scrollback")).Placeholder(tr("Search scrollback")).Grow(1).AutoFocus()
		if input.Changed() {
			session.pane.Search(session.searchText)
		}
		if input.Submitted() {
			session.pane.SearchNext()
		}
		input.OnShortcut(0, ui.KeyEscape, func() { a.closeTerminalSearch(id) })
		status := session.searchStatus
		switch {
		case session.searchText == "":
		case status.Searching && status.Total == 0:
			ui.Text(c, tr("Searching…")).TextColor(theme.TextMuted).SingleLine()
		case status.Total == 0:
			ui.Text(c, tr("No matches")).TextColor(theme.TextMuted).SingleLine()
		default:
			ui.Badge(c, fmt.Sprintf("%d / %d", status.Selected, status.Total))
		}
		disabled := status.Total == 0
		flatIconButton(c, "Previous terminal search result", "Shift+F3", chevronUpIcon, 13, 3, disabled, func() {
			session.pane.SearchPrevious()
		})
		flatIconButton(c, "Next terminal search result", "F3 / Enter", chevronDownIcon, 13, 3, disabled, func() {
			session.pane.SearchNext()
		})
		flatIconButton(c, "Close terminal search", "Escape", closeIcon, 13, 3, false, func() {
			a.closeTerminalSearch(id)
		})
	})
}
