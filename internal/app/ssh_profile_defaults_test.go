package app

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestNewSSHProfileDefaultsAndContentHeight(t *testing.T) {
	a := newApp(fakeOpener{})
	a.settingsLoaded = true
	a.openSSHWorkspacePicker()
	draft := a.sshProfileDraft
	if draft.Username != "root" || draft.AuthMode != sshAuthPassword || draft.DefaultRoot != "/home" || draft.Port != "22" {
		t.Fatalf("new profile defaults=%+v", draft)
	}
	tester := ui.NewTester(a.view, 1280, 1100)
	tester.Frame()
	panel, ok := tester.Find("Open SSH workspace")
	if !ok {
		t.Fatal("SSH modal missing")
	}
	save, ok := tester.Find("Save SSH profile")
	if !ok {
		t.Fatal("save button missing")
	}
	if gap := panel.Y + panel.H - save.Y - save.H; gap > 50 {
		t.Fatalf("SSH modal leaves %v pixels below content", gap)
	}
	if _, ok := tester.Find("SSH password"); !ok {
		t.Fatal("password field missing for default authentication")
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "ssh-profile-content-height", tester)
	}
	small := ui.NewTester(a.view, 800, 500)
	small.Frame()
	bounds, ok := small.Find("Open SSH workspace")
	if !ok || bounds.Y < 0 || bounds.Y+bounds.H > 500 {
		t.Fatalf("small-window modal overflows: %+v", bounds)
	}

}
