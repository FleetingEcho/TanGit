package app

import (
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func activityCenterShell(t *testing.T) (*shell, *ui.Tester) {
	t.Helper()
	s, tt, _, _ := newShellShot(t, true, defaultTheme)
	a := s.activeApp()
	// Not the Overview: its recent-activity card would print the same rows.
	a.workspaceTab = sectionHistory
	if a.commandLog == nil {
		a.commandLog = repository.NewCommandLog()
	}
	a.commandLog.Record("git push --dry-run", "remote: authentication failed", true)
	a.commandLog.Record("git status", "clean", false)
	a.recordActivityNotice(activitySeverityInfo, "Workspace restored", "Pinned tabs were restored.", activityTarget{})
	tt.SetSize(1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	return s, tt
}

// hasRow reports whether the list has a row labelled with the given prefix. The
// page behind the overlay also prints these titles, so only row labels count.
func hasRow(tt *ui.Tester, prefix string) bool {
	for _, text := range tt.Texts() {
		if strings.HasPrefix(text, prefix+" — ") {
			return true
		}
	}
	return false
}

func TestActivityCenterListsFiltersAndOpens(t *testing.T) {
	s, tt := activityCenterShell(t)
	s.openActivityCenter()
	tt.Frame()
	if !tt.HasText("Activity Center") {
		t.Fatal("the Activity Center did not open")
	}
	for _, title := range []string{"git push --dry-run", "git status", "Workspace restored"} {
		if !hasRow(tt, title) {
			t.Fatalf("%q has no row in the Activity Center: %q", title, tt.Texts())
		}
	}

	// Needs attention keeps only the failed command.
	if err := tt.Click("Needs attention"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !hasRow(tt, "git push --dry-run") || hasRow(tt, "git status") || hasRow(tt, "Workspace restored") {
		t.Fatalf("attention filter kept the wrong rows: %q", tt.Texts())
	}
	if err := tt.Click("Needs attention"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	// A source chip narrows to that source.
	if err := tt.Click("Notices"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !hasRow(tt, "Workspace restored") || hasRow(tt, "git status") {
		t.Fatalf("source filter kept the wrong rows: %q", tt.Texts())
	}
	if err := tt.Click("Notices"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	// Opening a command row goes to the command log and leaves the center.
	if err := tt.Click("git push --dry-run — Failed"); err != nil {
		t.Fatalf("open the failed command: %v", err)
	}
	tt.Frame()
	tt.Frame()
	if s.activity.open {
		t.Fatal("the Activity Center stayed open after opening a row")
	}
	if !s.activeApp().commandLogOpen {
		t.Fatal("opening a command activity did not show the command log")
	}
}

func TestActivityCenterKeyboardAndShortcutGuards(t *testing.T) {
	s, tt := activityCenterShell(t)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyA)
	tt.Frame()
	if !s.activity.open {
		t.Fatal("the shortcut did not open the Activity Center")
	}
	// The palette must not open over it.
	s.openCommandPalette(nil)
	if s.palette.open {
		t.Fatal("the command palette opened over the Activity Center")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.activity.open {
		t.Fatal("Escape did not close the Activity Center")
	}
}

func TestActivityCenterIsInTheCommandPalette(t *testing.T) {
	s, _ := activityCenterShell(t)
	ctx := s.commandContext()
	candidate, ok := catalogCandidate(s.catalogFor(ctx).Search(ctx, "activity center", nil), "activity/open")
	if !ok || !candidate.Enabled {
		t.Fatalf("palette offers no Activity Center command: %+v", candidate)
	}
	s.invokeCommand("activity/open")
	if !s.activity.open {
		t.Fatal("the palette command did not open the Activity Center")
	}
}

func TestActivityCenterScreenshots(t *testing.T) {
	dir := os.Getenv("MYGO_SHOTS")
	if dir == "" {
		t.Skip("set MYGO_SHOTS to a directory to write screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, tt := activityCenterShell(t)
	s.openActivityCenter()
	saveScreenshot(t, dir, "activity-center-dark", tt)
	s.activity.attention = true
	saveScreenshot(t, dir, "activity-center-attention-dark", tt)
}
