package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// confirmRequest is a question an operation asks before it runs:
// what it is about to do, why, and the label of the button that does it.
type confirmRequest struct {
	title        string
	message      string
	detail       string
	confirmLabel string
	destructive  bool
	run          func()
}

// askConfirm opens the app's own confirmation dialog. It replaces the
// system's message box, so the question takes the window's theme and stays
// inside the app, as every other dialog does.
func (a *app) askConfirm(request confirmRequest) {
	if a.confirm != nil {
		return
	}
	if strings.TrimSpace(request.confirmLabel) == "" {
		request.confirmLabel = "Confirm"
	}
	request.title = tr(request.title)
	request.message = tr(request.message)
	request.detail = tr(request.detail)
	request.confirmLabel = tr(request.confirmLabel)
	run := request.run
	generation, root, branch := a.generation, a.snapshot.Root, a.snapshot.Branch
	request.run = func() {
		if a.generation != generation || a.snapshot.Root != root || a.snapshot.Branch != branch {
			return
		}
		if run != nil {
			run()
		}
	}
	a.confirm = &request
}

func (a *app) cancelConfirm() {
	a.confirm = nil
}

// confirmModal draws the pending question over the window. Escape and a
// press outside cancel it; only the named button runs the action.
func (a *app) confirmModal(c *ui.Context) {
	if a.confirm == nil {
		return
	}
	request := a.confirm
	theme := c.Theme()
	icon, color := confirmIcon(theme, request.destructive)
	ui.Overlay(c, func() {
		c.Key("confirm-modal")
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		back.Children(func() {
			panel := ui.Column(c).Width(470).MaxWidthPercent(92).
				Padding(22).Gap(14).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(request.title).
				Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
						ui.Box(c).Size(34, 34).Radius(9).Center().
							Background(color.Alpha(0.14)).Children(func() {
							ui.Icon(c, icon).FontSize(18).TextColor(color)
						})
						ui.Text(c, request.title).FontSize(17).Bold().Grow(1)
					})
					ui.Text(c, request.message).FontSize(15)
					if request.detail != "" {
						ui.Text(c, request.detail).TextColor(theme.TextMuted)
					}
					ui.Row(c).Justify(ui.End).Gap(8).Padding(4, 0).Children(func() {
						if ui.Button(c, tr("Cancel")).Clicked() {
							a.cancelConfirm()
						}
						button := ui.Button(c, request.confirmLabel)
						if request.destructive {
							button.Background(theme.Danger).TextColor(ui.Hex("#ffffff"))
						}
						if button.Clicked() {
							a.confirm = nil
							if request.run != nil {
								request.run()
							}
						}
					})
				})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.cancelConfirm()
			}
		})
	})
}

// confirmIcon picks the warning triangle for a destructive question and a
// question mark for one that only changes history.
func confirmIcon(theme *ui.Theme, destructive bool) (*ui.SVG, ui.Color) {
	if destructive {
		return conflictIcon, theme.Danger
	}
	return questionIcon, theme.Accent
}
