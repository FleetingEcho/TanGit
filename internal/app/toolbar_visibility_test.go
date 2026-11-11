package app

import (
	"os"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestToolbarActionsDefaultNormalizeAndPersist(t *testing.T) {
	for _, actions := range [][]toolbarAction{headerToolbarActions, statusToolbarActions} {
		for _, action := range actions {
			if !toolbarActionVisible(nil, action.id) {
				t.Fatalf("%s is hidden by default", action.id)
			}
		}
	}

	got := normalizeSettings(appSettings{
		HiddenHeaderActions: []string{"push", "unknown", "push", "fetch"},
		HiddenStatusActions: []string{"terminal", "unknown", "terminal"},
	})
	if want := []string{"fetch", "push"}; !slices.Equal(got.HiddenHeaderActions, want) {
		t.Fatalf("normalized hidden header actions = %v, want %v", got.HiddenHeaderActions, want)
	}
	if want := []string{"terminal"}; !slices.Equal(got.HiddenStatusActions, want) {
		t.Fatalf("normalized hidden status actions = %v, want %v", got.HiddenStatusActions, want)
	}

	dir := t.TempDir()
	if err := os.WriteFile(settingsStorePath(dir), []byte(`{"theme":"Tokyo Night"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := loadAppSettings(dir)
	if len(legacy.HiddenHeaderActions) != 0 || len(legacy.HiddenStatusActions) != 0 {
		t.Fatalf("old settings hide actions: header=%v status=%v", legacy.HiddenHeaderActions, legacy.HiddenStatusActions)
	}

	got.HiddenHeaderActions = []string{"fetch", "push"}
	if err := saveAppSettings(dir, got); err != nil {
		t.Fatal(err)
	}
	loaded := loadAppSettings(dir)
	if !slices.Equal(loaded.HiddenHeaderActions, got.HiddenHeaderActions) ||
		!slices.Equal(loaded.HiddenStatusActions, got.HiddenStatusActions) {
		t.Fatalf("saved visibility changed: header=%v status=%v", loaded.HiddenHeaderActions, loaded.HiddenStatusActions)
	}
}

func TestToolbarVisibilitySettingsToggleIndependently(t *testing.T) {
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	h.app.openSettingsSection(settingsPageWorkspace)
	h.tester.Frame()

	for _, group := range []struct {
		actions []toolbarAction
		format  string
	}{
		{headerToolbarActions, "Show %s in header"},
		{statusToolbarActions, "Show %s in status bar"},
	} {
		for _, action := range group.actions {
			label := trf(group.format, action.label)
			box, found := h.tester.AccessByRole(ui.RoleCheckBox, label)
			if !found || !box.Checked {
				t.Fatalf("%s must default to visible: found=%v checked=%v", label, found, box.Checked)
			}
		}
	}

	if err := h.tester.Click("Show Pull in header"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Scroll(700, 480, 0, 500)
	h.tester.Frame()
	if err := h.tester.Click("Show Commands in status bar"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !slices.Equal(h.app.settings.HiddenHeaderActions, []string{"pull"}) ||
		!slices.Equal(h.app.settings.HiddenStatusActions, []string{"commands"}) {
		t.Fatalf("independent choices = header %v, status %v", h.app.settings.HiddenHeaderActions, h.app.settings.HiddenStatusActions)
	}
	h.app.closeSettings()
	h.tester.Frame()
	if _, found := h.tester.AccessByRole(ui.RoleButton, "Pull"); found {
		t.Fatal("hidden Pull remains in the header")
	}
	if _, found := h.tester.AccessByRole(ui.RoleButton, "Commands"); found {
		t.Fatal("hidden Commands remains in the status bar")
	}
	for _, label := range []string{"Push", "Refresh", "Pull requests", "Toggle terminal"} {
		if _, found := h.tester.Find(label); !found {
			t.Errorf("an independent choice hid %s", label)
		}
	}
	saved := loadAppSettings(h.app.pathUserData)
	if !slices.Equal(saved.HiddenHeaderActions, []string{"pull"}) ||
		!slices.Equal(saved.HiddenStatusActions, []string{"commands"}) {
		t.Fatalf("visibility did not persist: header=%v status=%v", saved.HiddenHeaderActions, saved.HiddenStatusActions)
	}
}

func TestEveryToolbarActionCanBeHidden(t *testing.T) {
	h := menuTestApp(t)
	headerButton, found := h.tester.Find("Fetch")
	if !found {
		t.Fatal("default header has no Fetch button")
	}
	statusButton, found := h.tester.Find("Toggle terminal")
	if !found {
		t.Fatal("default status bar has no terminal button")
	}
	headerY, statusY := headerButton.Y, statusButton.Y
	h.app.pathUserData = t.TempDir()
	h.app.openSettingsSection(settingsPageWorkspace)
	h.tester.Frame()
	for _, group := range []struct {
		actions []toolbarAction
		format  string
	}{
		{headerToolbarActions, "Show %s in header"},
		{statusToolbarActions, "Show %s in status bar"},
	} {
		if group.format == "Show %s in status bar" {
			h.tester.Scroll(700, 480, 0, 500)
			h.tester.Frame()
		}
		for _, action := range group.actions {
			if err := h.tester.Click(trf(group.format, action.label)); err != nil {
				t.Fatalf("hide %s: %v", action.id, err)
			}
			h.tester.Frame()
		}
	}
	if len(h.app.settings.HiddenHeaderActions) != len(headerToolbarActions) ||
		len(h.app.settings.HiddenStatusActions) != len(statusToolbarActions) {
		t.Fatalf("could not hide every action: header=%v status=%v", h.app.settings.HiddenHeaderActions, h.app.settings.HiddenStatusActions)
	}
	h.app.closeSettings()
	h.tester.Frame()
	for _, action := range headerToolbarActions {
		if button, found := h.tester.Find(action.label); found && button.Y == headerY {
			t.Errorf("hidden header action %s still has a button", action.id)
		}
	}
	for _, action := range statusToolbarActions {
		if button, found := h.tester.Find(action.label); found && button.Y == statusY {
			t.Errorf("hidden status action %s still has a button", action.id)
		}
	}
}
