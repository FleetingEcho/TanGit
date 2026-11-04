package app

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// The app always has a version to show, even when it runs from `go run` and
// the build carried none.
func TestCurrentAppVersionIsNeverEmpty(t *testing.T) {
	if version := currentAppVersion(); strings.TrimSpace(version) == "" {
		t.Fatal("the running build has no version to show")
	}
	if appVersionFallback == "" {
		t.Fatal("the fallback version is empty")
	}
}

// Settings shows the running version, the updater's latest result, a manual
// check, and the fallback link to GitHub releases.
func TestUpdateSettingsShowsStatusAndChecksForUpdates(t *testing.T) {
	h := menuTestApp(t)
	checks := 0
	h.app.checkForUpdates = func() { checks++ }
	h.app.updateInfo = func() appUpdateInfo {
		return appUpdateInfo{Enabled: true, LastCheck: time.Unix(timeNow(), 0)}
	}
	h.tester.Key(ui.Ctrl, ui.KeyComma)
	h.tester.Frame()
	if err := h.tester.Click("Updates settings"); err != nil {
		t.Fatalf("open Updates settings: %v", err)
	}
	h.tester.Frame()

	if !h.tester.HasText("Open GitHub releases") {
		t.Fatalf("the settings have no releases button: %v", h.texts())
	}
	if !h.tester.HasText(currentAppVersion()) {
		t.Fatalf("the settings do not show the running version: %v", h.texts())
	}
	if !h.tester.HasText("Up to date · checked just now") {
		t.Fatalf("the settings do not show the successful check: %v", h.texts())
	}
	if !h.tester.HasText("After an update installs, restart TanGit to use the new version.") {
		t.Fatalf("the settings do not explain when the update takes effect: %v", h.texts())
	}
	if err := h.tester.Click("Check for updates"); err != nil {
		t.Fatal(err)
	}
	if checks != 1 {
		t.Fatalf("the manual updater ran %d times, want 1", checks)
	}
}

func TestUpdateSettingsAdvertisesAvailableVersion(t *testing.T) {
	h := menuTestApp(t)
	h.app.updateInfo = func() appUpdateInfo {
		return appUpdateInfo{Enabled: true, AvailableVersion: "1.1.0"}
	}
	h.tester.Key(ui.Ctrl, ui.KeyComma)
	h.tester.Frame()
	if err := h.tester.Click("Updates settings"); err != nil {
		t.Fatalf("open Updates settings: %v", err)
	}
	h.tester.Frame()

	if !h.tester.HasText("Version 1.1.0 is available") {
		t.Fatalf("the settings do not advertise the update: %v", h.texts())
	}
}
