package app

import "github.com/egoist/mygo/ui"

// The switch is on when shortcuts are enabled. Keep the stored inverse flag
// for compatibility with existing settings files.
func (a *app) hotkeysSwitch(c *ui.Context) {
	ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Text(c, tr("Keyboard shortcuts")).SingleLine().FontSize(c.Theme().FontSize - 1)
		enabled := !a.settings.DisableShortcuts
		toggle := ui.Switch(c, &enabled)
		toggle.Label(tr("Enable keyboard shortcuts")).Tooltip(tr("Enable keyboard shortcuts")).OnClick(func() {
			a.settings.DisableShortcuts = !a.settings.DisableShortcuts
			a.settingsChanged(c)
			if a.settings.DisableShortcuts {
				a.notify("info", "Keyboard shortcuts disabled", tr("The header icons keep every action reachable."))
			} else {
				a.notify("success", "Keyboard shortcuts enabled", tr("Press ? to show keyboard shortcuts."))
			}
		})
	})
}
