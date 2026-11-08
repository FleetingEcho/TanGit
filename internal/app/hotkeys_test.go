package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestModalHotkeysSwitchDisablesAndRestoresShortcuts(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.pathUserData = t.TempDir()
	h.app.openKeybindings()
	h.tester.Frame()
	if err := h.tester.Click("Enable keyboard shortcuts"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.settings.DisableShortcuts {
		t.Fatal("switch did not disable hotkeys")
	}
	if !loadAppSettings(h.app.pathUserData).DisableShortcuts {
		t.Fatal("disabled preference was not saved")
	}
	if err := h.tester.Click("Done"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	before := len(calls.all())
	h.tester.Key(0, ui.KeyD)
	h.tester.Key(0, ui.Key3)
	h.tester.Key(0, ui.KeyF1)
	h.tester.Frame()
	h.tester.Frame()
	if h.app.confirm != nil || h.app.keybindingsOpen || h.app.workspaceTab != sectionChanges || len(calls.all()) != before {
		t.Fatal("disabled hotkeys still trigger actions")
	}
	// The mouse remains available, including the switch used to recover.
	if err := h.tester.Click("Keyboard shortcuts"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.keybindingsOpen {
		t.Fatal("mouse controls stopped working")
	}
	if err := h.tester.Click("Enable keyboard shortcuts"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.settings.DisableShortcuts {
		t.Fatal("switch did not restore hotkeys")
	}
	if loadAppSettings(h.app.pathUserData).DisableShortcuts {
		t.Fatal("enabled preference was not saved")
	}
	if err := h.tester.Click("Done"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Key(0, ui.Key3)
	h.tester.Frame()
	if h.app.workspaceTab != sectionGraph {
		t.Fatal("re-enabled shortcuts did not take effect")
	}
}

func TestHotkeysSwitchNotifiesBothStates(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.pathUserData = t.TempDir()
	h.app.openKeybindings()
	h.tester.Frame()
	for _, title := range []string{"Keyboard shortcuts disabled", "Keyboard shortcuts enabled"} {
		if err := h.tester.Click("Enable keyboard shortcuts"); err != nil {
			t.Fatal(err)
		}
		h.tester.Frame()
		h.tester.Frame()
		if !h.tester.HasText(title) {
			t.Fatalf("missing notification %q", title)
		}
	}
}
