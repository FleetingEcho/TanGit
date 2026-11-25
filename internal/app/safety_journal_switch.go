package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// setSafetyJournal turns the recovery point that precedes most actions on or
// off, and saves the choice. With it off, actions run without being backed up
// or listed for Undo.
func (a *app) setSafetyJournal(enabled bool) {
	a.settings.DisableSafetyJournal = !enabled
	a.applySettingsChange()
}

// safetyJournalEnabled is the window's current choice.
func (a *app) safetyJournalEnabled() bool {
	return !a.settings.DisableSafetyJournal
}

// safetyJournalSetting is the Behavior page's switch for the journal.
func (a *app) safetyJournalSetting(c *ui.Context) {
	theme := c.Theme()
	enabled := a.safetyJournalEnabled()
	ui.Column(c).Gap(6).Children(func() {
		if settingsSwitch(c, "Safety journal", "Save a recovery point before actions so they can be undone", &enabled) {
			a.setSafetyJournal(enabled)
			c.Invalidate()
		}
		ui.Text(c, tr("Actions run faster while it is off, but stash, discard, checkout, merge and similar actions are not saved and cannot be undone. Rebase, reset and patch edits still keep a recovery point.")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).MaxWidth(480)
	})
}

// safetyJournalToggle is the switch on the Recovery & checkpoints bottom bar,
// switch first and label after, as the Settings switches read. It turns the
// journal off or on from the place that shows it.
func (a *app) safetyJournalToggle(c *ui.Context) {
	enabled := a.safetyJournalEnabled()
	tooltip := "Turn the safety journal off"
	if !enabled {
		tooltip = "Turn the safety journal on"
	}
	ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
		if settingsSwitchControl(c, "Safety journal", tooltip, &enabled) {
			a.setSafetyJournal(enabled)
		}
	})
}

// safetyJournalOffNotice says plainly that nothing is being backed up, with a
// button to turn the journal back on.
func (a *app) safetyJournalOffNotice(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Padding(9, 12).
		Background(theme.Warning.Alpha(0.12)).Border(1, theme.Warning.Alpha(0.45)).Radius(theme.Radius).Children(func() {
		ui.Icon(c, softResetIcon).FontSize(15).TextColor(theme.Warning)
		ui.Text(c, tr("The safety journal is off. New actions are not backed up and cannot be undone.")).
			FontSize(theme.FontSize - 1).Grow(1).MinWidth(0)
		if compactTextButton(ui.Button(c, tr("Turn on"))).Label(tr("Turn the safety journal on")).Clicked() {
			a.setSafetyJournal(true)
		}
	})
}

// journalStatusColor is the dot that leads a journal card: what the entry
// means at a glance.
func journalStatusColor(theme *ui.Theme, record repository.OperationRecord) ui.Color {
	switch {
	case record.CanUndo():
		return theme.Success
	case record.Status == repository.OperationFailed:
		return theme.Danger
	case record.Status == repository.OperationInterrupted || record.Status == repository.OperationPending ||
		record.UndoStartedAt != 0:
		return theme.Warning
	}
	return theme.TextMuted
}
