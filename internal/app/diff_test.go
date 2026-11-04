package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// The wrap icon is the answer to long diff lines: it turns wrapping on for
// the diff already on screen and for the diffs read after it, and off again
// the same way. It lives above the diff, beside the layout and expand icons,
// and in the full-window view.
func TestDiffWrapTogglesEveryDiff(t *testing.T) {
	h := menuTestApp(t)
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil })
	if h.app.diffState.Wrap {
		t.Fatal("long lines should scroll sideways until the icon is clicked")
	}

	if err := h.tester.Click("Toggle diff line wrapping"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.diffWrap || !h.app.settings.DiffWrap {
		t.Fatalf("the icon did not turn wrapping on: app=%v stored=%v",
			h.app.diffWrap, h.app.settings.DiffWrap)
	}
	if !h.app.diffState.Wrap {
		t.Fatal("the diff on screen still scrolls sideways")
	}
	state, err := buildDiffState(h.app.diffLayout(), h.app.diffWrap,
		repository.File{Path: "later.go"}, repository.Diff{NewText: "package later\n"})
	if err != nil {
		t.Fatal(err)
	}
	if !state.Wrap {
		t.Fatal("a diff read after the toggle does not wrap")
	}

	if err := h.tester.Click("Toggle diff line wrapping"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.diffWrap || h.app.settings.DiffWrap || h.app.diffState.Wrap {
		t.Fatalf("the second click did not turn wrapping off: app=%v stored=%v state=%v",
			h.app.diffWrap, h.app.settings.DiffWrap, h.app.diffState.Wrap)
	}
}

// The setting does the same as the icon, for a user who finds it in the
// settings modal first.
func TestDiffWrapSettingReachesTheOpenDiff(t *testing.T) {
	a := newApp(fakeOpener{})
	a.settings.DiffWrap = true
	state, err := buildDiffState(a.diffLayout(), false,
		repository.File{Path: "open.go"}, repository.Diff{NewText: "package open\n"})
	if err != nil {
		t.Fatal(err)
	}
	a.diffState = state
	a.applySettingsChange()
	if !a.diffWrap || !a.diffState.Wrap {
		t.Fatalf("the setting did not reach the open diff: app=%v state=%v",
			a.diffWrap, a.diffState.Wrap)
	}
}

// The whole-window diff lists the files of the pane it reads, and choosing one
// reads it there, without closing the diff: the header names the new file and
// the diff below is its own.
func TestWholeWindowDiffSwitchesFiles(t *testing.T) {
	h, _ := historyHarness(t)
	h.waitFor(t, "the commit files", func() bool { return len(h.app.commitFiles) > 0 })
	h.waitFor(t, "the first file's diff", func() bool {
		return h.app.commitDiffState != nil && !h.app.commitDiffLoading
	})
	if err := h.tester.Click("Expand diff"); err != nil {
		t.Fatalf("review the diff in the whole window: %v", err)
	}
	h.tester.Frame()
	if !h.app.diffFullscreen {
		t.Fatal("the expand icon did not open the whole-window diff")
	}
	if len(h.app.diffFullscreenRows) != len(h.app.commitFiles) {
		t.Fatalf("the whole-window diff lists %d files, the commit changed %d",
			len(h.app.diffFullscreenRows), len(h.app.commitFiles))
	}
	if h.app.diffFullscreenFile != 0 || h.app.commitDiffPath != "one.txt" {
		t.Fatalf("the whole-window diff started on row %d, file %q",
			h.app.diffFullscreenFile, h.app.commitDiffPath)
	}

	// The rows sit under the list's heading, a row being the height of a
	// file row at the default theme: a line of text and its two paddings.
	heading, ok := h.tester.Find("Changed files")
	if !ok {
		t.Fatalf("the whole-window diff has no file list: %q", h.texts())
	}
	const rowHeight = 32
	rowCenter := func(i int) float32 {
		return heading.Y + heading.H + 8 + float32(i)*rowHeight + rowHeight/2
	}
	h.tester.ClickAt(heading.X+60, rowCenter(1))
	h.tester.Frame()
	h.waitFor(t, "the second file's diff", func() bool {
		return h.app.commitDiffPath == "two.txt" && h.app.commitDiffState != nil && !h.app.commitDiffLoading
	})
	if h.app.selectedCommitFile != 1 {
		t.Fatalf("choosing the file left row %d chosen", h.app.selectedCommitFile)
	}
	if h.app.diffFullscreenFile != 1 {
		t.Fatalf("the list still marks row %d", h.app.diffFullscreenFile)
	}

	// Escape still closes it, and the pane under it shows the file chosen
	// here, so the two are one selection.
	h.press(t, ui.KeyEscape)
	if h.app.diffFullscreen {
		t.Fatal("Escape left the whole-window diff open")
	}
	if h.app.selectedCommitFile != 1 {
		t.Fatalf("the commit pane lost the file chosen in the diff: row %d", h.app.selectedCommitFile)
	}
}

// The enlarged diff can give its whole width to the patch without losing the
// selected file or the list's remembered width. Its restore rail remains
// usable when the window is narrow, just like the other collapsible panes.
func TestWholeWindowDiffFileListCollapsesAndRestores(t *testing.T) {
	h, _ := historyHarness(t)
	h.waitFor(t, "the commit files", func() bool { return len(h.app.commitFiles) > 0 })
	h.waitFor(t, "the first file's diff", func() bool {
		return h.app.commitDiffState != nil && !h.app.commitDiffLoading
	})
	if err := h.tester.Click("Expand diff"); err != nil {
		t.Fatalf("review the diff in the whole window: %v", err)
	}
	h.tester.Frame()

	width := h.app.diffFullscreenFilesWidth
	diff := h.app.commitDiffState
	if err := h.tester.Click("Collapse changed files"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.diffFullscreenFilesCollapsed {
		t.Fatal("the collapse control did not hide the whole-window file list")
	}
	if h.tester.HasText("Changed files") || !h.tester.HasText("Expand changed files") {
		t.Fatalf("the collapsed list did not leave only its restore rail: %q", h.texts())
	}
	if h.app.commitDiffState != diff || h.app.commitDiffPath != "one.txt" || h.app.selectedCommitFile != 0 {
		t.Fatal("collapsing the file list changed or unloaded the selected diff")
	}

	// A narrow frame must not move the restore action off screen or reload
	// the diff, and restoring must use the width from before the collapse.
	h.tester.SetSize(720, 800)
	h.tester.Frame()
	if err := h.tester.Click("Expand changed files"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.diffFullscreenFilesCollapsed || !h.tester.HasText("Changed files") {
		t.Fatal("the restore rail did not bring the file list back")
	}
	if h.app.diffFullscreenFilesWidth != width {
		t.Fatalf("restoring changed the list width from %v to %v", width, h.app.diffFullscreenFilesWidth)
	}
	if h.app.commitDiffState != diff || h.app.commitDiffPath != "one.txt" || h.app.selectedCommitFile != 0 {
		t.Fatal("restoring the file list changed or unloaded the selected diff")
	}
}

// The line beside the whole-window diff's file list is dragged, so a long path
// gets the room it needs and the diff gets it back.
func TestWholeWindowDiffResizesItsFileList(t *testing.T) {
	h, _ := historyHarness(t)
	h.waitFor(t, "the commit files", func() bool { return len(h.app.commitFiles) > 0 })
	h.waitFor(t, "the first file's diff", func() bool {
		return h.app.commitDiffState != nil && !h.app.commitDiffLoading
	})
	if err := h.tester.Click("Expand diff"); err != nil {
		t.Fatalf("review the diff in the whole window: %v", err)
	}
	h.tester.Frame()

	before := h.app.diffFullscreenFilesWidth
	heading, ok := h.tester.Find("Changed files")
	if !ok {
		t.Fatalf("the whole-window diff has no file list: %q", h.texts())
	}
	// The split starts at the panel's padding, so the line is its width in.
	lineX := islandGap/2 + before
	dragTo(t, h.tester, lineX, heading.Y+120, 80, 0)
	if h.app.diffFullscreenFilesWidth < before+60 {
		t.Fatalf("dragging the line right left the list %v wide, was %v",
			h.app.diffFullscreenFilesWidth, before)
	}
	grown := h.app.diffFullscreenFilesWidth
	dragTo(t, h.tester, islandGap/2+grown, heading.Y+120, -80, 0)
	if h.app.diffFullscreenFilesWidth > grown-60 {
		t.Fatalf("dragging the line left left the list %v wide, was %v",
			h.app.diffFullscreenFilesWidth, grown)
	}
}
