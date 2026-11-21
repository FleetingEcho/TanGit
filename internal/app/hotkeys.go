package app

import "github.com/egoist/mygo/ui"

// The switch is on when shortcuts are enabled. Keep the stored inverse flag
// for compatibility with existing settings files, with no confirmation for
// ordinary preference changes.
func (a *app) hotkeysSwitch(c *ui.Context) {
	enabled := !a.settings.DisableShortcuts
	notifications := !a.settings.DisableShortcutToasts
	changed, notificationsChanged := false, false
	// Give each preference a full switch-then-label row. Sharing one wrapped
	// row made the shortcut and notification toggles visually ambiguous.
	ui.Column(c).Gap(12).Children(func() {
		changed = settingsSwitch(c, "Enable keyboard shortcuts", "Enable keyboard shortcuts", &enabled)
		notificationsChanged = settingsSwitch(c, "Shortcut notifications",
			"Show shortcut acknowledgements while keeping Command Log and operation notifications visible.", &notifications)
	})
	if changed {
		a.settings.DisableShortcuts = !enabled
		a.settingsChanged(c)
		if a.settings.DisableShortcuts {
			a.notify("info", "Keyboard shortcuts disabled", tr("The header icons keep every action reachable."))
		} else {
			a.notify("success", "Keyboard shortcuts enabled", tr("Press ? to show keyboard shortcuts."))
		}
	}
	if notificationsChanged {
		a.settings.DisableShortcutToasts = !notifications
		a.settings.ShortcutToastsConfigured = true
		a.settingsChanged(c)
	}
}
