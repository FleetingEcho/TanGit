package app

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestBehaviorHotkeySwitchesUseSeparateLeftToRightRows(t *testing.T) {
	a := newApp(fakeOpener{})
	tester := ui.NewTester(a.hotkeysSwitch, 700, 240)
	tester.Frame()
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "settings-behavior-hotkeys", tester)
	}

	keyboard, found := tester.AccessByRole(ui.RoleSwitch, "Enable keyboard shortcuts")
	if !found {
		t.Fatal("keyboard-shortcut switch is missing")
	}
	notifications, found := tester.AccessByRole(ui.RoleSwitch, "Shortcut notifications")
	if !found {
		t.Fatal("shortcut-notification switch is missing")
	}
	if notifications.Bounds.Y <= keyboard.Bounds.Y+keyboard.Bounds.H {
		t.Fatalf("switches still share a row: keyboard=%+v notifications=%+v", keyboard.Bounds, notifications.Bounds)
	}
	for _, test := range []struct {
		label string
		right float32
		top   float32
	}{
		{label: "Enable keyboard shortcuts", right: keyboard.Bounds.X + keyboard.Bounds.W, top: keyboard.Bounds.Y},
		{label: "Shortcut notifications", right: notifications.Bounds.X + notifications.Bounds.W, top: notifications.Bounds.Y},
	} {
		var text ui.AccessInfo
		foundText := false
		for _, node := range tester.AccessAll() {
			if node.Name == test.label && node.Bounds.X > test.right {
				text, foundText = node, true
				break
			}
		}
		if !foundText {
			t.Fatalf("%s text is missing to the right of its switch: %+v", test.label, tester.AccessAll())
		}
		if test.right > text.Bounds.X {
			t.Errorf("%s text starts at %.1f, left of its switch at %.1f", test.label, text.Bounds.X, test.right)
		}
		if text.Bounds.Y < test.top || text.Bounds.Y > test.top+48 {
			t.Errorf("%s label is not beside its own switch: text=%+v switchY=%.1f", test.label, text.Bounds, test.top)
		}
	}

	before := a.settings.DisableShortcutToasts
	if err := tester.Click("Shortcut notifications"); err != nil {
		t.Fatalf("toggle separated notification switch: %v", err)
	}
	if a.settings.DisableShortcutToasts == before || !a.settings.ShortcutToastsConfigured {
		t.Fatalf("separate row changed switch layout but not persistence: before=%v after=%v configured=%v",
			before, a.settings.DisableShortcutToasts, a.settings.ShortcutToastsConfigured)
	}
}
