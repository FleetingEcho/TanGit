package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

const terminalNameLimit = 80

func (a *app) openTerminalRename(id uint64) {
	_, session := a.terminalSessionByID(id)
	if session == nil {
		return
	}
	a.term.renameID = id
	a.term.renameName = session.name
}

func (a *app) closeTerminalRename() {
	a.term.renameID = 0
	a.term.renameName = ""
}

func (a *app) beginTerminalRename() {
	_, session := a.terminalSessionByID(a.term.renameID)
	if session == nil {
		a.closeTerminalRename()
		return
	}
	name := strings.TrimSpace(a.term.renameName)
	runes := []rune(name)
	if len(runes) > terminalNameLimit {
		name = string(runes[:terminalNameLimit])
	}
	session.name = name
	a.closeTerminalRename()
}

// terminalRenameModal changes only the card's label. The shell process, its
// OSC title and its working directory remain independent of this name.
func (a *app) terminalRenameModal(c *ui.Context) {
	_, session := a.terminalSessionByID(a.term.renameID)
	if session == nil {
		return
	}
	theme := c.Theme()
	defaultSession := *session
	defaultSession.name = ""
	placeholder := terminalSessionLabel(&defaultSession)
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(440).MaxWidthPercent(92).
				Padding(22).Gap(16).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Rename terminal"))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Box(c).Size(34, 34).Radius(9).Center().
						Background(theme.Accent.Alpha(0.14)).Children(func() {
						ui.Icon(c, editIcon).FontSize(18).TextColor(theme.Accent)
					})
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Rename terminal")).FontSize(17).Bold()
						ui.Text(c, tr("Give this terminal a name of its own.")).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close rename form", "Escape", closeIcon, false, a.closeTerminalRename)
				})
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, tr("Terminal name")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					input := ui.TextInput(c, &a.term.renameName).
						Label(tr("Terminal name")).Placeholder(placeholder).AutoFocus()
					if input.Submitted() {
						a.beginTerminalRename()
					}
				})
				ui.Text(c, tr("This only changes the terminal tab label; the shell and its working directory are unchanged.")).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Text(c, trf("Up to %d characters. Leave it empty to show the default name again.", terminalNameLimit)).
						TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
					if ui.Button(c, tr("Cancel")).Clicked() {
						a.closeTerminalRename()
					}
					if ui.PrimaryButton(c, tr("Save name")).Clicked() {
						a.beginTerminalRename()
					}
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeTerminalRename()
			}
		})
	})
}
