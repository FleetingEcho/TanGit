package app

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

func TestChangesFileCountsAreAlwaysVisibleInGreenAndRed(t *testing.T) {
	h, _ := changesTestApp(t, []repository.File{
		{Path: "selected.txt", Status: " M", Unstaged: true, Tracked: true, LinesAdded: 2, LinesDeleted: 1},
		{Path: "work.txt", Status: " M", Unstaged: true, Tracked: true, LinesAdded: 100, LinesDeleted: 10},
		{Path: "new.txt", Status: "??", Unstaged: true},
	})
	row, ok := h.tester.Find("work.txt")
	if !ok {
		t.Fatalf("the changes list has no work.txt: %q", h.texts())
	}
	for _, label := range []string{"+100", "−10", "+0", "−0"} {
		if _, ok := h.tester.Find(label); !ok {
			t.Fatalf("an unhovered file has no persistent %q count: %q", label, h.texts())
		}
	}
	theme := themeForSettings(h.app.settings, true)
	if !rectHasColor(h.tester, "+100", theme.Success) {
		t.Error("the additions are not drawn in the theme's success color")
	}
	if !rectHasColor(h.tester, "−10", theme.Danger) {
		t.Error("the deletions are not drawn in the theme's danger color")
	}
	stats, ok := h.tester.Find("Diff statistics for work.txt: 100 additions, 10 deletions")
	if !ok || stats.X <= row.X || stats.W <= 0 {
		t.Fatalf("the file has no right-side numeric statistics: %+v, found=%v", stats, ok)
	}

	// Hovering reveals actions, but must not be what reveals or hides counts.
	h.tester.Move(row.X+row.W/2, row.Y+row.H/2)
	h.tester.Frame()
	h.tester.Move(2, 2)
	h.tester.Frame()
	for _, label := range []string{"+100", "−10"} {
		if !h.tester.HasText(label) {
			t.Fatalf("leaving the row hid its persistent %q count", label)
		}
	}
}

func TestUnstageAllRefreshesTrackedLineCounts(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init", "--initial-branch=main")
	runGitIn(t, dir, "config", "user.name", "Desktop Test")
	runGitIn(t, dir, "config", "user.email", "desktop@example.com")
	path := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "tracked.txt")
	runGitIn(t, dir, "commit", "-m", "initial")
	if err := os.WriteFile(path, []byte("one\nTWO\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "tracked.txt")

	a := newApp(repository.Opener{})
	a.pathUserData = t.TempDir()
	a.workspaceTab = sectionChanges
	updated := newUpdates()
	a.update = updated.update
	a.path = dir
	h := &harness{app: a, updates: updated, tester: ui.NewTester(a.view, 1280, 800)}
	t.Cleanup(a.shutdown)
	openFixture(t, h.tester)
	h.settle(t)
	h.waitFor(t, "the staged file's statistics", func() bool {
		return len(a.snapshot.Files) == 1 && a.snapshot.Files[0].Staged &&
			a.snapshot.Files[0].LinesAdded == 2 && a.snapshot.Files[0].LinesDeleted == 1
	})
	if err := h.tester.Click("Unstage all changes"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the unstaged snapshot and its refreshed statistics", func() bool {
		return !a.actionBusy && len(a.snapshot.Files) == 1 &&
			!a.snapshot.Files[0].Staged && a.snapshot.Files[0].Unstaged &&
			a.snapshot.Files[0].LinesAdded == 2 && a.snapshot.Files[0].LinesDeleted == 1
	})
	h.tester.Frame()
	for _, label := range []string{"+2", "−1"} {
		if !h.tester.HasText(label) {
			t.Fatalf("unstage all did not show the tracked file's %q count: %q", label, h.texts())
		}
	}
	h.waitFor(t, "the selected file's refreshed unstaged diff", func() bool {
		return a.diffState != nil && !a.diffLoading && a.diffKind == "Unstaged"
	})
}

func TestPartialStageRefreshesRemainingUnstagedLineCounts(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init", "--initial-branch=main")
	runGitIn(t, dir, "config", "user.name", "Desktop Test")
	runGitIn(t, dir, "config", "user.email", "desktop@example.com")
	path := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "tracked.txt")
	runGitIn(t, dir, "commit", "-m", "initial")
	if err := os.WriteFile(path, []byte("one\nTWO\nthree\nfour\nfive\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := newApp(repository.Opener{})
	a.pathUserData = t.TempDir()
	a.workspaceTab = sectionChanges
	updated := newUpdates()
	a.update = updated.update
	a.path = dir
	h := &harness{app: a, updates: updated, tester: ui.NewTester(a.view, 1280, 800)}
	t.Cleanup(a.shutdown)
	openFixture(t, h.tester)
	h.settle(t)
	h.waitFor(t, "the original unstaged diff", func() bool {
		return a.diffState != nil && !a.diffLoading && len(a.snapshot.Files) == 1 &&
			a.snapshot.Files[0].LinesAdded == 3 && a.snapshot.Files[0].LinesDeleted == 1
	})

	// Stage only the replacement, leaving the two added lines unstaged.
	a.diffState.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1}, true)
	h.tester.Frame()
	if err := h.tester.Click("Stage selected lines"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the partially staged snapshot", func() bool {
		return !a.actionBusy && len(a.snapshot.Files) == 1 &&
			a.snapshot.Files[0].Staged && a.snapshot.Files[0].Unstaged
	})
	h.waitFor(t, "the remaining unstaged line counts", func() bool {
		h.tester.Frame()
		return h.tester.HasText("Diff statistics for tracked.txt: 2 additions, 0 deletions") &&
			h.tester.HasText("Diff statistics for tracked.txt: 1 additions, 1 deletions")
	})
}

func TestFileInfoCountsAreGreenAndRed(t *testing.T) {
	h, _ := changesTestApp(t, []repository.File{
		{Path: "new.c", Status: "??", Unstaged: true, LinesAdded: 154},
	})
	if h.tester.HasText("+154 −0") {
		t.Fatal("the file info still draws its counts as one muted text element")
	}
	if !h.tester.HasText("Open file in editor") {
		t.Fatalf("the selected file's details did not render: %q", h.texts())
	}
	theme := themeForSettings(h.app.settings, true)
	if !rectHasColor(h.tester, "+154", theme.Success) {
		t.Error("the file-info additions are not drawn in the theme's success color")
	}
	if !rectHasColor(h.tester, "−0", theme.Danger) {
		t.Error("the file-info deletions are not drawn in the theme's danger color")
	}
}

func TestCommitFileRowsCarryTheirChangeCounts(t *testing.T) {
	h, _ := historyHarness(t)
	h.waitFor(t, "the commit files", func() bool { return len(h.app.commitFiles) > 0 })
	path, ok := h.tester.Find("one.txt")
	if !ok {
		t.Fatalf("the commit has no one.txt row: %q", h.texts())
	}
	for _, label := range []string{"+8", "−2"} {
		if !h.tester.HasText(label) {
			t.Fatalf("the commit file has no %q count: %q", label, h.texts())
		}
	}
	stats, ok := h.tester.Find("Diff statistics for one.txt: 8 additions, 2 deletions")
	if !ok || stats.X <= path.X || stats.W <= 0 {
		t.Fatalf("the numeric statistics sit at %+v beside the path at %+v", stats, path)
	}
}

func rectHasColor(tester *ui.Tester, label string, want ui.Color) bool {
	rect, ok := tester.Find(label)
	if !ok {
		return false
	}
	want = want.SRGB()
	ink := color.RGBA{R: want.R, G: want.G, B: want.B, A: 255}
	image := tester.Image()
	for y := max(int(rect.Y), 0); y < min(int(rect.Y+rect.H), image.Bounds().Dy()); y++ {
		for x := max(int(rect.X), 0); x < min(int(rect.X+rect.W), image.Bounds().Dx()); x++ {
			got := color.RGBAModel.Convert(image.At(x, y)).(color.RGBA)
			// Small text is antialiased against the row, so even a stroke's
			// centre can be a blend rather than the exact palette value.
			if colorChannelDelta(got.R, ink.R) <= 48 &&
				colorChannelDelta(got.G, ink.G) <= 48 &&
				colorChannelDelta(got.B, ink.B) <= 48 {
				return true
			}
		}
	}
	return false
}

func colorChannelDelta(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}
