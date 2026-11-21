package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// focusRestoreTarget stores logical UI identity only. MyGo's Modal restores
// the exact prior control when it still exists; the keyboard pane is the
// fallback when that control disappeared during command execution.
type focusRestoreTarget struct {
	app  *app
	pane string
}

type commandPaletteState struct {
	open            bool
	query           string
	selected        int
	scroll          ui.ScrollState
	search          ui.Handle
	restore         focusRestoreTarget
	recent          []commandID
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
}

// invokeCommand is shared by palette and Command Catalog-backed cards. It
// re-resolves the current tab and command state; no row callback is retained.
func (s *shell) invokeCommand(id commandID) commandInvokeResult {
	if s == nil {
		return commandInvokeResult{Status: commandInvokeUnknown, Reason: "The window is unavailable"}
	}
	ctx := s.commandContext()
	result := s.catalogFor(ctx).Invoke(ctx, id)
	if !result.Accepted() {
		if s.palette.open {
			s.palette.notice = tr(result.Reason)
		}
		return result
	}
	s.rememberCommand(id)
	if s.palette.open {
		s.closeCommandPalette(true)
	}
	return result
}

func (s *shell) commandPaletteCandidates() []commandCandidate {
	ctx := s.commandContext()
	return s.catalogFor(ctx).Search(ctx, s.palette.query, s.palette.recent)
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
	theme := c.Theme()
	candidates := s.commandPaletteCandidates()
	if len(candidates) == 0 {
		s.palette.selected = 0
	} else if s.palette.selected >= len(candidates) {
		s.palette.selected = len(candidates) - 1
	} else if s.palette.selected < 0 {
		s.palette.selected = 0
	}
	move := func(delta int) {
		if len(candidates) == 0 {
			return
		}
		s.palette.selected = (s.palette.selected + delta + len(candidates)) % len(candidates)
		s.palette.revealSelection = true
		s.palette.notice = ""
	}
	invokeSelected := func() {
		if len(candidates) == 0 {
			return
		}
		s.invokeCommand(candidates[s.palette.selected].ID)
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
						ui.Text(c, tr("Search navigation and actions for the current workspace")).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine()
					})
					iconButton(c, "Close Command Palette", "Escape", closeIcon, false, func() { s.closeCommandPalette(true) })
				})
				search := ui.TextInput(c, &s.palette.query).Bind(&s.palette.search).
					Label(tr("Search commands")).Placeholder(tr("Search commands")).Search().AutoFocus()
				// Text fields consume Enter for submission and may use Escape for
				// editing. Scoped command handling makes the palette contract
				// explicit instead of depending on leftover overlay keys.
				s.palette.search.OnShortcut(c, 0, ui.KeyEscape, func() { s.closeCommandPalette(true) })
				s.palette.search.OnShortcut(c, 0, ui.KeyUp, func() { move(-1) })
				s.palette.search.OnShortcut(c, 0, ui.KeyDown, func() { move(1) })
				search.OnSubmit(invokeSelected)
				if search.Changed() {
					s.palette.selected = 0
					s.palette.revealSelection = true
					s.palette.notice = ""
				}
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, tr("Commands")).Bold().Grow(1)
					if query := strings.TrimSpace(s.palette.query); query != "" {
						ui.Badge(c, fmt.Sprintf("%d", len(candidates)))
					} else {
						ui.Text(c, tr("Suggested · Recent · Navigation")).TextColor(theme.TextMuted).
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
						for index, candidate := range candidates {
							index, candidate := index, candidate
							c.Key("command-palette:" + string(candidate.ID))
							s.commandPaletteRow(c, candidate, index == s.palette.selected, s.palette.revealSelection, func() {
								s.palette.selected = index
								s.invokeCommand(candidate.ID)
							})
						}
						s.palette.revealSelection = false
					})
				})
				ui.Divider(c)
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Text(c, tr("↑↓ Navigate · Enter Run · Escape Close")).TextColor(theme.TextMuted).
						FontSize(theme.FontSize - 1).Grow(1)
					if s.palette.notice != "" {
						ui.Text(c, s.palette.notice).TextColor(theme.Warning).FontSize(theme.FontSize - 1).
							MaxWidth(360).SingleLine().Tooltip(s.palette.notice)
					} else {
						ui.Text(c, trf("%d commands", len(candidates))).TextColor(theme.TextMuted).
							FontSize(theme.FontSize - 1)
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
		ui.Icon(c, commandIconSVG(candidate.Icon)).FontSize(16).TextColor(color)
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, candidate.Label).Bold().SingleLine().TextColor(theme.Text)
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
			ui.Text(c, tr(candidate.Group.label())).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted).SingleLine()
			if candidate.Chord != "" {
				ui.Text(c, candidate.Chord).FontSize(theme.FontSize - 2).TextColor(theme.Accent).SingleLine()
			}
		})
	})
	if row.Clicked() && candidate.Enabled {
		invoke()
	}
}
