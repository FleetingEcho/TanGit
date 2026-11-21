package app

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestShortcutChangeNotificationStacksTitleAndDescription(t *testing.T) {
	h := menuTestApp(t)
	h.tester.SetSize(720, 720)
	h.app.openSettings()
	h.tester.Frame()
	if err := h.tester.Click("Behavior settings"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Enable keyboard shortcuts"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()

	title, found := h.tester.Find("Keyboard shortcuts disabled")
	if !found {
		t.Fatalf("shortcut-change notification title is missing: %q", h.texts())
	}
	description, found := h.tester.Find("The header icons keep every action reachable.")
	if !found {
		t.Fatalf("shortcut-change notification description is missing: %q", h.texts())
	}
	dismiss, found := h.tester.AccessByRole(ui.RoleButton, "Dismiss notification")
	if !found {
		t.Fatal("shortcut-change notification dismiss button is missing")
	}
	t.Logf("title=%+v description=%+v dismiss=%+v", title, description, dismiss.Bounds)
	if description.Y < title.Y+title.H {
		t.Errorf("description is not on the line below the title: title=%+v description=%+v", title, description)
	}
	if description.W < 180 {
		t.Errorf("description collapsed into a narrow column: title=%+v description=%+v", title, description)
	}
	if description.X+description.W > dismiss.Bounds.X {
		t.Errorf("description reaches into the dismiss control: description=%+v dismiss=%+v", description, dismiss.Bounds)
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "shortcut-change-notification", h.tester)
	}
}
