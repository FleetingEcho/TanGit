package app

import "github.com/egoist/mygo/ui"

// closeBehaviorSetting lets the user choose what the window's close button does:
// keep lazygitGo running in the tray, or quit. The stored flag is the quit
// choice, so existing settings files keep the tray behavior.
func (a *app) closeBehaviorSetting(c *ui.Context) {
	theme := c.Theme()
	keep := !a.settings.QuitOnClose
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, tr("Closing the window")).Bold()
		changed := settingsSwitch(c, "Keep running in the tray when the window is closed",
			"Turn this off to quit lazygitGo when you click the close button.", &keep)
		if changed {
			a.settings.QuitOnClose = !keep
			a.settingsChanged(c)
		}
		note := "The close button hides the window. Open it again from the tray icon, or choose Quit there."
		if !keep {
			note = "The close button quits lazygitGo."
		} else if !trayReady {
			note = "This system has no tray, so the close button always quits."
		}
		ui.Text(c, tr(note)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).MaxWidth(480)
	})
}
