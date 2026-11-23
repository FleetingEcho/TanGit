package app

import (
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

// TestSelectionAndPaletteScreenshots is opt-in like the other screenshot
// harnesses:
//
//	MYGO_SHOTS=/path/to/shots go test ./internal/app -run TestSelectionAndPaletteScreenshots
func TestSelectionAndPaletteScreenshots(t *testing.T) {
	dir := os.Getenv("MYGO_SHOTS")
	if dir == "" {
		t.Skip("set MYGO_SHOTS to a directory to write screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []struct {
		name string
		dark bool
	}{{"dark", true}, {"light", false}} {
		mode := mode
		t.Run("select-changes-"+mode.name, func(t *testing.T) {
			h, _ := twoHunkChangesApp(t, unstagedTrackedFile())
			h.tester.SetSize(1280, 800)
			h.tester.SetDark(mode.dark)
			h.app.hunkMode = false
			h.app.stopLineSelection()
			saveScreenshot(t, dir, "select-idle-"+mode.name, h.tester)
			h.app.hunkMode = true
			saveScreenshot(t, dir, "select-hunk-"+mode.name, h.tester)
			h.app.startLineSelection()
			clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1})
			clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 5, End: 5})
			saveScreenshot(t, dir, "select-lines-"+mode.name, h.tester)
		})
	}
	makeShell := func(dark bool, w, hgt int) (*shell, *ui.Tester) {
		s, tt, _, _ := newShellShot(t, dark, defaultTheme)
		s.activeApp().workspaceTab = sectionOverview
		tt.SetSize(w, hgt)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		tt.Frame()
		return s, tt
	}
	t.Run("palette-modes", func(t *testing.T) {
		s, tt := makeShell(true, 1280, 800)
		a := s.activeApp()
		a.commandLog = repository.NewCommandLog()
		s.openCommandPalette(nil)
		saveScreenshot(t, dir, "palette-empty-dark", tt)
		s.palette.query = "undo"
		saveScreenshot(t, dir, "palette-search-dark", tt)
		s.palette.query = "@"
		saveScreenshot(t, dir, "palette-branches-dark", tt)
		s.palette.query = "!echo hello; sleep 1; echo done"
		a.snapshot.Root = t.TempDir()
		s.invokeCommand("commands/run-line")
		tt.Frame()
		time.Sleep(300 * time.Millisecond)
		saveScreenshot(t, dir, "palette-shell-running-dark", tt)
		a.cancelCustomCommand()
		time.Sleep(300 * time.Millisecond)
		saveScreenshot(t, dir, "palette-shell-done-dark", tt)
	})
}
