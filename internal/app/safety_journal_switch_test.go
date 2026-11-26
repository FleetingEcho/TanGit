package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// restoreSafetyJournal selects the default for this test and restores it
// afterward, so no other test inherits an explicit choice.
func restoreSafetyJournal(t *testing.T) {
	t.Helper()
	repository.SetSafetyJournalEnabled(false)
	t.Cleanup(func() { repository.SetSafetyJournalEnabled(false) })
}

// The Recovery & checkpoints dialog starts with the journal off, and its
// notice and switch turn the journal on and off while saving each choice.
func TestRecoveryDialogSwitchTurnsTheSafetyJournalOnAndOff(t *testing.T) {
	restoreSafetyJournal(t)
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.app.pathUserData = t.TempDir()
	h.app.setWorkspaceTab(sectionStash)
	h.tester.Frame()
	openRecoveryForTest(t, h)

	if h.app.settings.EnableSafetyJournal || repository.SafetyJournalEnabled() {
		t.Fatal("the safety journal should start off")
	}
	if !h.tester.HasText("Turn on") {
		t.Fatalf("the off notice is missing while the journal is off: %q", h.texts())
	}
	if err := h.tester.Click("Turn the safety journal on"); err != nil {
		t.Fatalf("turn the journal on from the notice: %v; texts %q", err, h.texts())
	}
	h.tester.Frame()
	if !h.app.settings.EnableSafetyJournal || !repository.SafetyJournalEnabled() {
		t.Fatalf("the notice's button did not turn the journal on: setting on=%v enabled=%v",
			h.app.settings.EnableSafetyJournal, repository.SafetyJournalEnabled())
	}
	if saved := loadAppSettings(h.app.pathUserData); !saved.EnableSafetyJournal {
		t.Fatal("turning the journal on was not saved")
	}
	if h.tester.HasText("The safety journal is off. New actions are not backed up and cannot be undone.") {
		t.Fatal("the off notice stayed after turning the journal on")
	}

	if err := h.tester.Click("Safety journal"); err != nil {
		t.Fatalf("turn the journal back off from the switch: %v; texts %q", err, h.texts())
	}
	h.tester.Frame()
	if h.app.settings.EnableSafetyJournal || repository.SafetyJournalEnabled() {
		t.Fatal("the switch did not turn the journal back off")
	}
	if saved := loadAppSettings(h.app.pathUserData); saved.EnableSafetyJournal {
		t.Fatal("turning the journal off was not saved")
	}
	if !h.tester.HasText("The safety journal is off. New actions are not backed up and cannot be undone.") {
		t.Fatalf("the dialog does not say the journal is off: %q", h.texts())
	}
}

// Settings has the same opt-in switch, on the Behavior page.
func TestBehaviorSettingsCanTurnTheSafetyJournalOn(t *testing.T) {
	restoreSafetyJournal(t)
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	h.app.openSettingsSection(settingsPageBehavior)
	h.tester.Frame()
	for i := 0; i < 12 && !h.tester.HasText("Safety journal"); i++ {
		h.tester.Scroll(700, 400, 0, -240)
		h.tester.Frame()
	}
	if !h.tester.HasText("Safety journal") {
		t.Fatalf("the Behavior page has no Safety journal switch: %q", h.texts())
	}
	if h.app.settings.EnableSafetyJournal {
		t.Fatal("the Settings switch should start off")
	}
	if err := h.tester.Click("Safety journal"); err != nil {
		t.Fatalf("switch the journal on in Settings: %v", err)
	}
	h.tester.Frame()
	if !h.app.settings.EnableSafetyJournal || !repository.SafetyJournalEnabled() {
		t.Fatalf("the Settings switch did not turn the journal on: setting on=%v enabled=%v",
			h.app.settings.EnableSafetyJournal, repository.SafetyJournalEnabled())
	}
	if saved := loadAppSettings(h.app.pathUserData); !saved.EnableSafetyJournal {
		t.Fatal("the Settings choice was not saved")
	}
}

// The journal's tabs are real tabs, and an entry shows what ran and when
// without repeating the repository's path on every row.
func TestRecoveryDialogUsesTabsAndCleanJournalRows(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.app.setWorkspaceTab(sectionStash)
	h.tester.Frame()
	h.app.snapshot.Operations = []repository.OperationRecord{
		recoverableJournalRecord("row-one", "Stash WIP on main"),
	}
	h.app.snapshot.Operations[0].Branch = "main"
	h.app.snapshot.Operations[0].Worktree = h.app.snapshot.Root
	openRecoveryForTest(t, h)
	if _, found := h.tester.AccessByRole(ui.RoleTab, "Safety Journal"); !found {
		t.Fatalf("the Safety Journal is not a tab: %q", h.texts())
	}
	if err := h.tester.Click("Safety Journal"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.savedWorkPanel != 1 {
		t.Fatalf("the Safety Journal tab did not select the journal: panel %d", h.app.savedWorkPanel)
	}
	// A row's detail line reads "time · branch"; it must not trail the path.
	for _, text := range h.texts() {
		if strings.Contains(text, " · main") && strings.Contains(text, h.app.snapshot.Root) {
			t.Fatalf("a row repeats the repository path %q: %q", h.app.snapshot.Root, text)
		}
	}
	if !hasTextContaining(h.texts(), " · main") {
		t.Fatalf("the journal row shows no time and branch: %q", h.texts())
	}
	if !h.tester.HasText("Stash WIP on main") {
		t.Fatalf("the journal row is missing: %q", h.texts())
	}
}

func hasTextContaining(texts []string, part string) bool {
	for _, text := range texts {
		if strings.Contains(text, part) {
			return true
		}
	}
	return false
}
