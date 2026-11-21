package app

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// focusRestoreTarget stores logical UI identity only. MyGo's Modal restores
// the exact prior control when it still exists; the keyboard pane is the
// fallback when that control disappeared during command execution.
type focusRestoreTarget struct {
	app  *app
	pane string
}

// paletteShellRun follows one command started with the ! mode. It outlives
// the palette: closing the window never cancels the command, and reopening it
// shows the command's progress or result until it is dismissed.
type paletteShellRun struct {
	app       *app
	entryID   uint64
	cancelled bool
}

func (r paletteShellRun) active() bool { return r.app != nil && r.entryID != 0 }

// entry finds the run's command-log record; false once the log dropped it.
func (r paletteShellRun) entry() (repository.CommandEntry, bool) {
	if !r.active() {
		return repository.CommandEntry{}, false
	}
	return r.app.commandLog.Entry(r.entryID)
}

type commandPaletteState struct {
	open            bool
	query           string
	selected        int
	scroll          ui.ScrollState
	search          ui.Handle
	restore         focusRestoreTarget
	recent          []commandID
	pinned          []commandID
	shellRun        paletteShellRun
	shellHistory    []string
	historyPos      int
	historyDraft    string
	actionIndex     int
	notice          string
	revealSelection bool
}

func (s *shell) commandContext() commandContext {
	return commandContext{Shell: s, App: s.activeApp()}
}

// openCommandPalette opens only over ordinary application content. Existing
// safety forms and confirmations always keep the top-most interaction scope.
func (s *shell) openCommandPalette(c *ui.Context) {
	if s == nil || s.palette.open {
		return
	}
	a := s.activeApp()
	if a != nil && a.modalOpen() {
		return
	}
	s.palette.open = true
	s.loadPaletteState()
	s.palette.historyPos = -1
	s.palette.actionIndex = 0
	if _, ok := s.palette.shellRun.entry(); !ok {
		s.palette.shellRun = paletteShellRun{}
	}
	s.palette.query = ""
	s.palette.selected = 0
	s.palette.notice = ""
	s.palette.revealSelection = true
	s.palette.scroll.Y = 0
	s.palette.restore = focusRestoreTarget{}
	if a != nil {
		pane := a.keyboardLastPane
		if c != nil {
			pane = a.focusedKeyboardPane(c)
		}
		s.palette.restore = focusRestoreTarget{app: a, pane: pane}
	}
	s.palette.search.CancelFocus()
	s.invalidate()
}

func (s *shell) closeCommandPalette(restore bool) {
	if s == nil || !s.palette.open {
		return
	}
	s.palette.open = false
	if run := s.palette.shellRun; run.app != nil {
		run.app.paletteOutputVisible.Store(false)
	}
	s.palette.query = ""
	s.palette.selected = 0
	s.palette.notice = ""
	s.palette.search.CancelFocus()
	if restore && s.palette.restore.app != nil && !s.palette.restore.app.modalOpen() {
		a := s.palette.restore.app
		if s.activeApp() == a && s.palette.restore.pane != "" {
			// finishKeyboardFrame falls back to the section's primary pane if
			// this identity was removed while the palette was open.
			a.keyboardFocus = s.palette.restore.pane
		}
	}
	s.palette.restore = focusRestoreTarget{}
	s.invalidate()
}

func (s *shell) rememberCommand(id commandID) {
	recent := make([]commandID, 0, 20)
	recent = append(recent, id)
	for _, existing := range s.palette.recent {
		if existing != id && len(recent) < 20 {
			recent = append(recent, existing)
		}
	}
	s.palette.recent = recent
	s.savePaletteState()
}

func commandIDsToStrings(ids []commandID) []string {
	out := make([]string, len(ids))
	for index, id := range ids {
		out[index] = string(id)
	}
	return out
}

// loadPaletteState reads the persisted pins, recents and shell history from
// the active workspace's settings.
func (s *shell) loadPaletteState() {
	a := s.activeApp()
	if a == nil {
		return
	}
	toIDs := func(values []string) []commandID {
		ids := make([]commandID, len(values))
		for index, value := range values {
			ids[index] = commandID(value)
		}
		return ids
	}
	s.palette.pinned = toIDs(a.settings.PinnedCommands)
	s.palette.recent = toIDs(a.settings.RecentCommands)
	s.palette.shellHistory = append([]string(nil), a.settings.ShellHistory...)
}

// savePaletteState stores the pins, recents and shell history beside the
// other preferences so they survive a restart.
func (s *shell) savePaletteState() {
	a := s.activeApp()
	if a == nil {
		return
	}
	a.settings.PinnedCommands = normalizeCommandList(commandIDsToStrings(s.palette.pinned), 50)
	a.settings.RecentCommands = normalizeCommandList(commandIDsToStrings(s.palette.recent), 20)
	a.settings.ShellHistory = normalizeCommandList(s.palette.shellHistory, 50)
	if a.dataDir() != "" {
		a.saveSettings()
	}
}

// rememberShellLine records a ! command line, newest first.
func (s *shell) rememberShellLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	s.palette.shellHistory = normalizeCommandList(append([]string{line}, s.palette.shellHistory...), 50)
	s.palette.historyPos = -1
	s.savePaletteState()
}

// recallShellLine moves through the ! history: older with delta 1, newer with
// -1. Moving past the newest entry restores what was being typed.
func (s *shell) recallShellLine(delta int) {
	history := s.palette.shellHistory
	if len(history) == 0 {
		return
	}
	if s.palette.historyPos < 0 {
		if delta < 0 {
			return
		}
		_, s.palette.historyDraft = commandQueryMode(s.palette.query)
	}
	next := s.palette.historyPos + delta
	switch {
	case next < 0:
		s.palette.historyPos = -1
		s.palette.query = "!" + s.palette.historyDraft
	case next >= len(history):
		return
	default:
		s.palette.historyPos = next
		s.palette.query = "!" + history[next]
	}
	s.palette.selected = 0
}

// invokeCommand is shared by palette and Command Catalog-backed cards. It
// re-resolves the current tab and command state; no row callback is retained.
func (s *shell) invokeCommand(id commandID) commandInvokeResult {
	return s.invokeCommandAction(id, 0)
}

func (s *shell) invokeCommandAction(id commandID, action int) commandInvokeResult {
	if s == nil {
		return commandInvokeResult{Status: commandInvokeUnknown, Reason: "The window is unavailable"}
	}
	ctx := s.commandContext()
	result := s.catalogFor(ctx).InvokeAction(ctx, id, action)
	if !result.Accepted() {
		if s.palette.open {
			s.palette.notice = tr(result.Reason)
		}
		return result
	}
	s.rememberCommand(id)
	if s.palette.open && !result.KeepOpen {
		s.closeCommandPalette(true)
	}
	return result
}

func (s *shell) commandPaletteCandidates() []commandCandidate {
	ctx := s.commandContext()
	return s.catalogFor(ctx).SearchPinned(ctx, s.palette.query, s.palette.recent, s.palette.pinned)
}

// togglePinnedCommand pins or unpins a command for the rest of the session;
// pinned commands lead the empty-query list.
func (s *shell) togglePinnedCommand(id commandID) {
	for index, existing := range s.palette.pinned {
		if existing == id {
			s.palette.pinned = append(s.palette.pinned[:index:index], s.palette.pinned[index+1:]...)
			s.savePaletteState()
			return
		}
	}
	s.palette.pinned = append(s.palette.pinned, id)
	s.savePaletteState()
}

// commandPaletteSection names the empty-query section a candidate belongs to.
func commandPaletteSection(candidate commandCandidate) string {
	switch candidate.emptyRank {
	case -1:
		return "Pinned"
	case 0:
		return "Suggested"
	case 1:
		return "Recent"
	case 2:
		return "Navigation"
	}
	return ""
}

// commandLabelSpans bolds the matched characters of a label. Offsets that do
// not fall on rune boundaries are ignored rather than splitting a character.
func commandLabelSpans(label string, ranges []textRange, color, matched ui.Color) []ui.Span {
	spans := make([]ui.Span, 0, 2*len(ranges)+1)
	position := 0
	for _, r := range ranges {
		if r.Start < position || r.End > len(label) || r.Start >= r.End ||
			!utf8.RuneStart(label[r.Start]) || (r.End < len(label) && !utf8.RuneStart(label[r.End])) {
			continue
		}
		if r.Start > position {
			spans = append(spans, ui.Span{Text: label[position:r.Start], Color: color, Weight: 600})
		}
		spans = append(spans, ui.Span{Text: label[r.Start:r.End], Color: matched, Weight: 800})
		position = r.End
	}
	if position < len(label) {
		spans = append(spans, ui.Span{Text: label[position:], Color: color, Weight: 600})
	}
	return spans
}

func commandIconSVG(icon commandKind) *ui.SVG {
	switch icon {
	case commandIconNavigation:
		return openRepoIcon
	case commandIconFetch:
		return fetchIcon
	case commandIconPull:
		return pullIcon
	case commandIconPush:
		return pushIcon
	case commandIconRefresh:
		return refreshIcon
	case commandIconTerminal:
		return terminalIcon
	case commandIconLog:
		return logIcon
	case commandIconSettings:
		return settingsIcon
	case commandIconHelp:
		return keyboardIcon
	case commandIconCommit:
		return commitIcon
	case commandIconContinue:
		return continueIcon
	case commandIconActivity:
		return historyIcon
	case commandIconAgent:
		return appIcon
	case commandIconCustom:
		return commandIcon
	default:
		return commandIcon
	}
}

func (s *shell) commandPaletteView(c *ui.Context) {
	if !s.palette.open {
		return
	}
	if run := s.palette.shellRun; run.app != nil {
		// The output panel marks itself visible again below if it is drawn.
		run.app.paletteOutputVisible.Store(false)
	}
	theme := c.Theme()
	candidates := s.commandPaletteCandidates()
	if len(candidates) == 0 {
		s.palette.selected = 0
	} else if s.palette.selected >= len(candidates) {
		s.palette.selected = len(candidates) - 1
	} else if s.palette.selected < 0 {
		s.palette.selected = 0
	}
	total := s.catalogFor(s.commandContext()).Total(s.commandContext())
	mode, _ := commandQueryMode(s.palette.query)
	jump := func(delta int) {
		if len(candidates) == 0 {
			return
		}
		s.palette.selected = min(max(s.palette.selected+delta, 0), len(candidates)-1)
		s.palette.actionIndex = 0
		s.palette.revealSelection = true
		s.palette.notice = ""
	}
	pinSelected := func() {
		if len(candidates) == 0 {
			return
		}
		s.togglePinnedCommand(candidates[s.palette.selected].ID)
	}
	scope := ""
	if tab := s.activeTab(); tab != nil {
		scope = tab.title()
	}
	move := func(delta int) {
		if mode == '!' && len(s.palette.shellHistory) > 0 {
			// The ! list holds one row; Up and Down walk the shell history.
			s.recallShellLine(-delta)
			return
		}
		if len(candidates) == 0 {
			return
		}
		s.palette.actionIndex = 0
		s.palette.selected = (s.palette.selected + delta + len(candidates)) % len(candidates)
		s.palette.revealSelection = true
		s.palette.notice = ""
	}
	invokeSelected := func() {
		if len(candidates) == 0 {
			return
		}
		s.invokeCommandAction(candidates[s.palette.selected].ID, s.palette.actionIndex)
	}
	cycleAction := func() {
		if len(candidates) == 0 {
			return
		}
		count := len(candidates[s.palette.selected].ActionLabels)
		if count > 0 {
			s.palette.actionIndex = (s.palette.actionIndex + 1) % (count + 1)
		}
	}
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		back.Children(func() {
			panel := ui.Column(c).Width(720).MaxWidthPercent(94).Height(570).MaxHeightPercent(88).
				Padding(18).Gap(12).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Command Palette"))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Icon(c, searchIcon).FontSize(20).TextColor(theme.Accent)
					ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, tr("Command Palette")).FontSize(19).Bold()
						ui.Text(c, tr("Search navigation and actions for the current workspace")+" · "+cmdShortcutChord()+"+Shift+P").
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine()
					})
					if scope != "" {
						ui.Badge(c, scope).Tooltip(tr("Commands run against this workspace"))
					}
					iconButton(c, "Close Command Palette", "Escape", closeIcon, false, func() { s.closeCommandPalette(true) })
				})
				search := ui.TextInput(c, &s.palette.query).Bind(&s.palette.search).
					Label(tr("Search commands")).Placeholder(tr("Search commands — or type > @ # / !")).Search().AutoFocus()
				// Text fields consume Enter for submission and may use Escape for
				// editing. Scoped command handling makes the palette contract
				// explicit instead of depending on leftover overlay keys.
				s.palette.search.OnShortcut(c, 0, ui.KeyEscape, func() { s.closeCommandPalette(true) })
				s.palette.search.OnShortcut(c, 0, ui.KeyUp, func() { move(-1) })
				s.palette.search.OnShortcut(c, 0, ui.KeyDown, func() { move(1) })
				s.palette.search.OnShortcut(c, 0, ui.KeyPageUp, func() { jump(-8) })
				s.palette.search.OnShortcut(c, 0, ui.KeyPageDown, func() { jump(8) })
				s.palette.search.OnShortcut(c, ui.Ctrl, ui.KeyB, pinSelected)
				s.palette.search.OnShortcut(c, 0, ui.KeyTab, cycleAction)
				search.OnSubmit(invokeSelected)
				if search.Changed() {
					s.palette.historyPos = -1
					s.palette.actionIndex = 0
					s.palette.selected = 0
					s.palette.revealSelection = true
					s.palette.notice = ""
				}
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, tr("Commands")).Bold().Grow(1)
					if query := strings.TrimSpace(s.palette.query); query != "" {
						if label := commandModeLabel(mode); label != "" {
							ui.Text(c, tr(label)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
						}
						ui.Badge(c, fmt.Sprintf("%d", len(candidates)))
					} else {
						ui.Text(c, tr("> commands · @ branches · # commits · / files · ! shell")).TextColor(theme.TextMuted).
							FontSize(theme.FontSize - 1)
					}
				})
				ui.Divider(c)
				ui.Scroll(c).Grow(1).TrackScroll(&s.palette.scroll).Children(func() {
					if len(candidates) == 0 {
						ui.Column(c).Grow(1).Center().Gap(8).Padding(24).Children(func() {
							ui.Icon(c, searchIcon).FontSize(25).TextColor(theme.TextMuted)
							ui.Text(c, tr("No commands match your search")).Bold()
							ui.Text(c, tr("Try an English command name, a translated label, a keyword or a shortcut")).
								TextColor(theme.TextMuted).TextAlign(ui.Center).MaxWidth(420)
						})
						return
					}
					ui.Column(c).FillWidth().Gap(4).Role(ui.RoleListBox).Label(tr("Available commands")).Children(func() {
						sectioned := mode == 0 && strings.TrimSpace(s.palette.query) == ""
						previousSection := ""
						for index, candidate := range candidates {
							index, candidate := index, candidate
							if section := commandPaletteSection(candidate); sectioned && section != "" && section != previousSection {
								previousSection = section
								c.Key("command-palette-section:" + section)
								ui.Text(c, tr(section)).Bold().FontSize(theme.FontSize-2).TextColor(theme.TextMuted).Padding(6, 10, 0, 10)
							}
							c.Key("command-palette:" + string(candidate.ID))
							s.commandPaletteRow(c, candidate, index == s.palette.selected, s.palette.revealSelection, func() {
								s.palette.selected = index
								s.invokeCommand(candidate.ID)
							})
						}
						s.palette.revealSelection = false
					})
				})
				s.paletteShellOutput(c)
				ui.Divider(c)
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Text(c, tr("↑↓ Navigate · PgUp/PgDn Jump · Tab Actions · Enter Run · Ctrl+B Pin · Escape Close")).TextColor(theme.TextMuted).
						FontSize(theme.FontSize - 1).Grow(1)
					if s.palette.notice != "" {
						ui.Text(c, s.palette.notice).TextColor(theme.Warning).FontSize(theme.FontSize - 1).
							MaxWidth(360).SingleLine().Tooltip(s.palette.notice)
					} else {
						count := trf("%d results", len(candidates))
						if mode == 0 || mode == '>' {
							count = trf("%d of %d commands", len(candidates), total)
							if len(candidates) == total {
								count = trf("%d commands", total)
							}
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
			if back.OverlayShortcut(0, ui.KeyPageUp) {
				jump(-8)
			}
			if back.OverlayShortcut(0, ui.KeyPageDown) {
				jump(8)
			}
			if back.OverlayShortcut(0, ui.KeyTab) {
				cycleAction()
			}
			if back.OverlayShortcut(ui.Ctrl, ui.KeyB) {
				pinSelected()
			}
			if back.OverlayShortcut(0, ui.KeyEnter) {
				invokeSelected()
			}
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				s.closeCommandPalette(true)
			}
		})
	})
}

func (s *shell) commandPaletteRow(c *ui.Context, candidate commandCandidate, selected, reveal bool, invoke func()) {
	theme := c.Theme()
	label := candidate.Label
	if !candidate.Enabled && candidate.DisabledReason != "" {
		label += " — " + candidate.DisabledReason
	}
	row := ui.ButtonBase(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(10).
		Padding(8, 10).Radius(theme.Radius).Role(ui.RoleOption).
		Label(label).Selected(selected)
	if selected {
		row.Background(theme.Accent.Alpha(0.12)).Border(1, theme.Accent.Alpha(0.55))
		if reveal {
			row.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollNearest})
		}
	} else if row.Hovered() {
		row.Background(theme.SurfaceHover)
	}
	row.Children(func() {
		color := theme.TextMuted
		if selected {
			color = theme.Accent
		}
		glyph := candidate.Glyph
		if glyph == nil {
			glyph = commandIconSVG(candidate.Icon)
		}
		ui.Icon(c, glyph).FontSize(16).TextColor(color)
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			labelColor := theme.Text
			if !candidate.Enabled {
				labelColor = theme.TextMuted
			}
			if spans := commandLabelSpans(candidate.Label, candidate.MatchRanges, labelColor, theme.Accent); len(candidate.MatchRanges) > 0 && len(spans) > 0 {
				ui.RichText(c, spans...).SingleLine()
			} else {
				ui.Text(c, candidate.Label).Bold().SingleLine().TextColor(labelColor)
			}
			detail := candidate.Detail
			if !candidate.Enabled && candidate.DisabledReason != "" {
				detail = candidate.DisabledReason
			}
			if detail != "" {
				text := ui.Text(c, detail).FontSize(theme.FontSize - 1).SingleLine().MinWidth(0)
				if !candidate.Enabled {
					text.TextColor(theme.Warning)
				} else {
					text.TextColor(theme.TextMuted)
				}
			}
		})
		ui.Column(c).Gap(2).Shrink(0).AlignItems(ui.End).Children(func() {
			if selected && len(candidate.ActionLabels) > 0 {
				hint := tr("Tab: more actions")
				if s.palette.actionIndex > 0 && s.palette.actionIndex <= len(candidate.ActionLabels) {
					hint = tr("Enter: ") + candidate.ActionLabels[s.palette.actionIndex-1]
				}
				ui.Text(c, hint).FontSize(theme.FontSize - 2).TextColor(theme.Warning).SingleLine()
			}
			groupLabel := tr(candidate.Group.label())
			if candidate.Pinned {
				groupLabel = "★ " + groupLabel
			}
			ui.Text(c, groupLabel).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted).SingleLine()
			if candidate.Chord != "" {
				ui.Text(c, candidate.Chord).FontSize(theme.FontSize - 2).TextColor(theme.Accent).SingleLine()
			}
		})
	})
	if row.Clicked() && candidate.Enabled {
		invoke()
	}
}

func commandModeLabel(mode byte) string {
	switch mode {
	case '>':
		return "Commands"
	case '@':
		return "Branches"
	case '#':
		return "Commits"
	case '/':
		return "Changed files"
	case '!':
		return "Shell command"
	}
	return ""
}

// paletteShellOutput shows the ! command's progress and output under the
// list. The command keeps running if the palette is closed; Cancel stops it.
func (s *shell) paletteShellOutput(c *ui.Context) {
	run := s.palette.shellRun
	if !run.active() {
		return
	}
	entry, ok := run.entry()
	if !ok {
		return
	}
	run.app.paletteOutputVisible.Store(true)
	theme := c.Theme()
	ui.Divider(c)
	ui.Column(c).FillWidth().Gap(6).Height(210).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			switch {
			case entry.Running:
				ui.Spinner(c).TextColor(theme.Warning).Label(tr("Working"))
				ui.Text(c, tr("Running")).Bold().TextColor(theme.Warning)
			case run.cancelled:
				ui.Text(c, tr("Cancelled")).Bold().TextColor(theme.Warning)
			case entry.Failed:
				ui.Text(c, tr("Failed")).Bold().TextColor(theme.Danger)
			default:
				ui.Text(c, tr("Succeeded")).Bold().TextColor(theme.Success)
			}
			ui.Text(c, entry.Command).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine().Grow(1).MinWidth(0)
			if entry.Running {
				if ui.Button(c, tr("Cancel command")).Clicked() {
					s.palette.shellRun.cancelled = true
					run.app.cancelCustomCommand()
				}
			} else if ui.Button(c, tr("Dismiss")).Clicked() {
				s.palette.shellRun = paletteShellRun{}
			}
		})
		ui.Scroll(c).Grow(1).Children(func() {
			output := strings.TrimRight(entry.Output, "\n")
			switch {
			case output != "":
				ui.Text(c, output).FontSize(theme.FontSize - 1).TextColor(theme.Text)
			case entry.Running:
				ui.Text(c, tr("Waiting for output… Output appears when the command finishes. You can close this window; the command keeps running until it ends or you cancel it.")).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			default:
				ui.Text(c, tr("The command printed nothing.")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			}
		})
	})
}
