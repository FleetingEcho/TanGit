package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func showBackgroundFetchInterval(t *testing.T, h *harness) {
	t.Helper()
	for range 12 {
		bounds, found := h.tester.Find("Background fetch interval")
		if found && bounds.W > 0 && bounds.H > 0 && bounds.Y >= 0 && bounds.Y+bounds.H <= 800 {
			return
		}
		delta := float32(260)
		if found && bounds.Y < 0 {
			delta = -260
		}
		h.tester.Scroll(700, 400, 0, delta)
		h.tester.Frame()
	}
	t.Fatalf("could not scroll Background fetch interval into view: %v", h.tester.Texts())
}

func TestBackgroundFetchSelectorChoosesPreset(t *testing.T) {
	h := menuTestApp(t)
	h.app.openSettingsSection(settingsPageBehavior)
	h.tester.Frame()
	showBackgroundFetchInterval(t, h)
	if err := h.tester.Click("Background fetch interval"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("30 min"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.settings.AutoFetchInterval != 30 {
		t.Fatalf("interval = %d, want 30; selector = %d", h.app.settings.AutoFetchInterval, h.app.settingsUI.autoFetchChoice)
	}
}

func TestBackgroundFetchSelectorAcceptsAndPersistsCustomMinutes(t *testing.T) {
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	h.app.openSettingsSection(settingsPageBehavior)
	h.tester.Frame()
	showBackgroundFetchInterval(t, h)

	if !h.tester.HasText("5 min") {
		t.Fatalf("the current interval is missing from the selector: %v", h.tester.Texts())
	}
	for _, label := range []string{"1 min", "15 min", "30 min", "60 min"} {
		if h.tester.HasText(label) {
			t.Fatalf("preset %q is still listed outside the selector", label)
		}
	}

	if err := h.tester.Click("Background fetch interval"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	for _, label := range []string{"1 min", "5 min", "15 min", "30 min", "60 min"} {
		if !h.tester.HasText(label) {
			t.Errorf("the selector lost preset %q", label)
		}
	}
	if err := h.tester.Click("Custom…"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.settingsUI.autoFetchChoice != backgroundFetchCustomChoice {
		t.Fatal("Custom did not enable the numeric interval editor")
	}
	if _, found := h.tester.Find("Custom fetch interval in minutes"); !found {
		t.Fatalf("the custom minutes editor is missing: %v", h.tester.Texts())
	}
	// The Behavior page is intentionally scrollable; changing the selector can
	// reveal its editor below other preferences, so bring it into view first.
	h.tester.Scroll(700, 400, 0, 240)
	h.tester.Frame()
	if err := h.tester.Click("Custom fetch interval in minutes"); err != nil {
		t.Fatal(err)
	}
	h.tester.Key(ui.Ctrl, ui.KeyA)
	h.tester.Type("12")
	h.tester.Frame()
	if h.app.settings.AutoFetchInterval != 12 || !h.app.settings.AutoFetchConfigured {
		t.Fatalf("custom interval = %d, configured = %v", h.app.settings.AutoFetchInterval, h.app.settings.AutoFetchConfigured)
	}
	if saved := loadAppSettings(h.app.pathUserData); saved.AutoFetchInterval != 12 {
		t.Fatalf("persisted custom interval = %d, want 12", saved.AutoFetchInterval)
	}

	for _, invalid := range []string{"0", "1441", "not-a-number"} {
		h.tester.Key(ui.Ctrl, ui.KeyA)
		h.tester.Type(invalid)
		h.tester.Frame()
		if h.app.settings.AutoFetchInterval != 12 {
			t.Fatalf("invalid input %q changed the interval to %d", invalid, h.app.settings.AutoFetchInterval)
		}
	}

	h.app.closeSettings()
	h.app.openSettingsSection(settingsPageBehavior)
	h.tester.Frame()
	showBackgroundFetchInterval(t, h)
	if h.app.settingsUI.autoFetchChoice != backgroundFetchCustomChoice || !h.tester.HasText("Custom…") {
		t.Fatal("an existing custom interval was not restored as Custom")
	}
	if err := h.tester.Click("Background fetch interval"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("30 min"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.settingsUI.autoFetchChoice == backgroundFetchCustomChoice || h.app.settings.AutoFetchInterval != 30 {
		t.Fatalf("preset selection left custom=%v interval=%d", h.app.settingsUI.autoFetchChoice == backgroundFetchCustomChoice, h.app.settings.AutoFetchInterval)
	}
	if _, found := h.tester.Find("Custom fetch interval in minutes"); found {
		t.Fatal("choosing a preset left the custom editor visible")
	}
}
