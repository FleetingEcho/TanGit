package app

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func commandLogEdgeHasFailureTint(tester *ui.Tester, label string) bool {
	node, found := tester.AccessByRole(ui.RoleRegion, label)
	if !found {
		return false
	}
	bounds := node.Bounds
	image := tester.Image()
	for y := max(int(bounds.Y), 0); y < min(int(bounds.Y)+2, image.Bounds().Dy()); y++ {
		for x := max(int(bounds.X+bounds.W/2), 0); x < min(int(bounds.X+bounds.W/2)+2, image.Bounds().Dx()); x++ {
			r, g, b, _ := image.At(x, y).RGBA()
			red, green, blue := uint8(r>>8), uint8(g>>8), uint8(b>>8)
			if red > 80 && red > green+24 && red > blue+12 {
				return true
			}
		}
	}
	return false
}

func TestCommandLogIncludesDatesAndHighlightsFailedRows(t *testing.T) {
	at := time.Date(2026, time.November, 20, 14, 3, 2, 0, time.Local)
	entries := []repository.CommandEntry{{
		At: at, Command: "git push --force-with-lease", Output: "fatal: lease rejected", Failed: true,
	}, {
		At: at.Add(-time.Hour), Command: "git pull", Output: "Already up to date.",
	}}
	a := newApp(fakeOpener{})
	theme := themeForSettings(a.settings, false)
	tester := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		a.commandLogBody(c, entries)
	}, 1100, 640)
	tester.Frame()
	for _, want := range []string{"2026-11-20 14:03:02", "2026-11-20 13:03:02", "Failed", "Succeeded", "fatal: lease rejected"} {
		if !tester.HasText(want) {
			t.Errorf("command log is missing %q: %q", want, tester.Texts())
		}
	}
	copied := formatCommandLog(entries)
	for _, want := range []string{"[2026-11-20 14:03:02] $ git push --force-with-lease", "[2026-11-20 13:03:02] $ git pull"} {
		if !strings.Contains(copied, want) {
			t.Errorf("copied command log is missing %q:\n%s", want, copied)
		}
	}

	if !commandLogEdgeHasFailureTint(tester, "$ git push --force-with-lease") {
		t.Fatal("failed Git command does not have a high-visibility row treatment")
	}
	if commandLogEdgeHasFailureTint(tester, "$ git pull") {
		t.Fatal("successful Git command inherited the failure row treatment")
	}
}

func TestStatusIslandWorkingStateIsHighContrast(t *testing.T) {
	for _, test := range []struct {
		name      string
		action    bool
		customJob bool
	}{
		{name: "repository action", action: true},
		{name: "custom command", customJob: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := menuTestApp(t)
			h.app.actionBusy = test.action
			h.app.customCommandRunning = test.customJob
			h.tester.Frame()
			if !h.tester.HasText("Working…") {
				t.Fatal("working indicator is missing from the status island")
			}
			theme := themeForSettings(h.app.settings, false)
			if !rectHasColor(h.tester, "Working…", theme.Warning) {
				t.Fatal("working indicator is still a low-contrast muted status")
			}
		})
	}
}
