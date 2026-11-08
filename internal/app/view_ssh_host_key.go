package app

import "github.com/egoist/mygo/ui"

// sshHostKeyModal is shown only for a previously unknown key. Changed keys are
// rejected by the transport and never reach this acceptance flow.
func (a *app) sshHostKeyModal(c *ui.Context) {
	prompt := a.sshHostKeyPrompt
	if prompt == nil {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.42)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(560).MaxWidthPercent(92).Padding(24).Gap(15).
				Background(theme.Background).Border(1, theme.Warning).Radius(theme.Radius + 8).
				Role(ui.RoleDialog).Label(tr("Verify SSH host key"))
			panel.Children(func() {
				ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(38, 38).Radius(9).Center().Background(theme.Warning.Alpha(0.14)).Children(func() {
						ui.Icon(c, conflictIcon).FontSize(20).TextColor(theme.Warning)
					})
					ui.Column(c).Gap(3).Grow(1).Children(func() {
						ui.Text(c, tr("Verify SSH host key")).FontSize(19).Bold()
						ui.Text(c, tr("This host is not in your known hosts yet")).TextColor(theme.TextMuted)
					})
				})
				ui.Text(c, prompt.info.Address).Bold().SingleLine().Tooltip(prompt.info.Address)
				ui.Column(c).Gap(5).Padding(12).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
					ui.Text(c, tr("SHA-256 fingerprint")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					ui.Text(c, prompt.info.FingerprintSHA256).SingleLine().Tooltip(prompt.info.FingerprintSHA256)
				})
				ui.Text(c, tr("Verify this fingerprint through a trusted channel before connecting. A different key for a known host is always rejected.")).
					TextColor(theme.TextMuted)
				ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
					if ui.Button(c, tr("Cancel connection")).Clicked() {
						a.respondSSHHostKey(false)
					}
					if ui.PrimaryButton(c, tr("Trust new host")).Clicked() {
						a.respondSSHHostKey(true)
					}
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.respondSSHHostKey(false)
			}
		})
	})
}
