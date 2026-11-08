package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

// unstagedTrackedFile is a file changed only in the working tree, so the
// Changes pane shows its unstaged diff and offers to stage a selection of it.
func unstagedTrackedFile() []repository.File {
	return []repository.File{{Path: "changed.txt", Status: " M", Unstaged: true, Tracked: true}}
}

// findCall is the first call the recorder holds that starts with prefix.
func findCall(calls *callRecorder, prefix string) string {
	for _, call := range calls.all() {
		if strings.HasPrefix(call, prefix) {
			return call
		}
	}
	return ""
}

// The pane names the way to select lines, counts the selection, and stages
// only the lines picked: the file's other change stays out of the index.
func TestStageSelectedLinesSendsOnlyTheirPatch(t *testing.T) {
	h, calls := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })
	if !h.app.lineSelectionAvailable() {
		t.Fatal("the unstaged diff does not offer line selection")
	}
	if !h.tester.HasText("Click line numbers to select lines") {
		t.Fatalf("the pane does not say how to select lines: %q", h.texts())
	}
	if h.tester.HasText("Unstaged changes") {
		t.Fatal("the selection row repeats the unstaged-side caption")
	}
	selectionButton, _ := h.tester.Find("Select multiple lines")
	hint, _ := h.tester.Find("Click line numbers to select lines")
	if selectionButton.X >= hint.X {
		t.Fatalf("the selection icon starts at %.0f, to the right of its hint at %.0f", selectionButton.X, hint.X)
	}

	// New-side line 1 is the change of the fake diff's "two" to "TWO"; the
	// fourth line's change is left alone.
	h.app.diffState.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1}, true)
	h.tester.Frame()
	if !h.tester.HasText("1 line selected") {
		t.Fatalf("the pane does not count the selected line: %q", h.texts())
	}
	if !h.tester.HasText("Stage selected lines") {
		t.Fatalf("the pane offers no way to stage the line: %q", h.texts())
	}
	if err := h.tester.Click("Stage selected lines"); err != nil {
		t.Fatalf("stage the selected line: %v", err)
	}
	h.waitFor(t, "the partial patch", func() bool {
		return findCall(calls, "apply-patch:false:") != ""
	})
	sent := findCall(calls, "apply-patch:false:")
	for _, want := range []string{"-two", "+TWO"} {
		if !strings.Contains(sent, want) {
			t.Errorf("the patch sent is missing %q: %q", want, sent)
		}
	}
	if strings.Contains(sent, "FOUR") {
		t.Errorf("the patch sent took the file's other change: %q", sent)
	}
}

// A multiple-line range sends both selected replacements in one partial
// patch, while the change in the unselected hunk stays out of that patch.
func TestStageSelectedLinesSendsEveryChangeInTheRange(t *testing.T) {
	calls := &callRecorder{}
	a := newApp(fakeOpener{
		result: openResult{snapshot: repository.Snapshot{
			Root:   "/work/project",
			Branch: "main",
			Files:  []repository.File{{Path: "app.go", Status: " M", Unstaged: true, Tracked: true}},
		}},
		calls: calls,
		patch: twoHunkPatch,
	})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })
	h.tester.Frame()
	if err := h.tester.Click("Select multiple lines"); err != nil {
		t.Fatal(err)
	}
	h.app.diffState.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 3}, true)
	h.tester.Frame()
	if !h.tester.HasText("3 lines selected") {
		t.Fatalf("the pane does not count the selected range: %q", h.texts())
	}
	if err := h.tester.Click("Stage selected lines"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "staging the range", func() bool {
		return findCall(calls, "apply-patch:false:") != ""
	})

	sent := findCall(calls, "apply-patch:false:")
	for _, want := range []string{"-two", "+TWO", "-four", "+FOUR"} {
		if !strings.Contains(sent, want) {
			t.Errorf("the staged patch is missing %q:\n%s", want, sent)
		}
	}
	if strings.Contains(sent, "ten and a half") {
		t.Fatalf("the staged patch took the unselected hunk:\n%s", sent)
	}
	if calls.has("stage:app.go") {
		t.Fatal("the range staged the whole file instead of its selected lines")
	}
}

// On the staged side the same selection goes the other way: it takes the
// lines out of the index.
func TestUnstageSelectedLinesTakesOnlyThemBack(t *testing.T) {
	h, calls := changesTestApp(t, []repository.File{
		{Path: "changed.txt", Status: "M ", Staged: true, Tracked: true},
	})
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })
	h.app.diffState.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1}, true)
	h.tester.Frame()
	if !h.tester.HasText("Unstage selected lines") {
		t.Fatalf("the staged side offers no way to unstage the line: %q", h.texts())
	}
	if err := h.tester.Click("Unstage selected lines"); err != nil {
		t.Fatalf("unstage the selected line: %v", err)
	}
	h.waitFor(t, "the reversed patch", func() bool {
		return findCall(calls, "apply-patch:true:") != ""
	})
	sent := findCall(calls, "apply-patch:true:")
	for _, want := range []string{"-two", "+TWO"} {
		if !strings.Contains(sent, want) {
			t.Errorf("the patch sent is missing %q: %q", want, sent)
		}
	}
	if strings.Contains(sent, "+FOUR") {
		t.Errorf("the patch sent took the file's other change: %q", sent)
	}
}

// An untracked file is shown as the patch that adds it, so its lines can be
// picked too: only the picked line goes into the index, and the file stays
// otherwise untracked.
func TestUntrackedFileStagesOnlyThePickedLine(t *testing.T) {
	h, calls := changesTestApp(t, []repository.File{
		{Path: "new.txt", Status: "??", Unstaged: true, Added: true},
	})
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })
	if !h.app.lineSelectionAvailable() {
		t.Fatal("a new file does not offer line selection")
	}
	if !h.tester.HasText("Click line numbers to select lines") {
		t.Fatalf("the pane does not say how to select lines: %q", h.texts())
	}

	// New-side line 1 is the file's second line; the first stays out.
	h.app.diffState.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1}, true)
	h.tester.Frame()
	if err := h.tester.Click("Stage selected lines"); err != nil {
		t.Fatalf("stage the selected line: %v", err)
	}
	h.waitFor(t, "the partial patch", func() bool {
		return findCall(calls, "apply-patch:false:") != ""
	})
	sent := findCall(calls, "apply-patch:false:")
	if !strings.Contains(sent, "+second") {
		t.Errorf("the patch sent is missing the picked line: %q", sent)
	}
	if strings.Contains(sent, "+first") {
		t.Errorf("the patch sent took the line beside it: %q", sent)
	}
	if calls.has("stage:new.txt") {
		t.Fatal("the whole file was staged")
	}
}

// Picking every line of a new file is adding the file, which is done as a
// whole rather than line by line.
func TestPickingEveryLineOfANewFileStagesTheFile(t *testing.T) {
	h, calls := changesTestApp(t, []repository.File{
		{Path: "new.txt", Status: "??", Unstaged: true, Added: true},
	})
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })
	h.app.diffState.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffNew, Start: 0, End: 1}, true)
	h.tester.Frame()
	if err := h.tester.Click("Stage selected lines"); err != nil {
		t.Fatalf("stage the selected lines: %v", err)
	}
	h.waitFor(t, "the file staged", func() bool { return calls.has("stage:new.txt") })
	if findCall(calls, "apply-patch:") != "" {
		t.Fatal("the file was applied line by line instead of added")
	}
}

// Space acts on the lines picked in the diff, as the pane's button does,
// rather than on the whole file.
func TestSpaceStagesTheSelectedLines(t *testing.T) {
	h, calls := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })
	h.app.diffState.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1}, true)
	h.press(t, ui.KeySpace)
	h.waitFor(t, "the partial patch", func() bool {
		return findCall(calls, "apply-patch:false:") != ""
	})
	if calls.has("stage:changed.txt") {
		t.Fatal("Space staged the whole file instead of the selected lines")
	}
}

// clickDiffLine does what the editor does when a line number is clicked: it
// makes the line the selection, then reports the click to the app.
func clickDiffLine(t *testing.T, h *harness, line editor.DiffLineRange) {
	t.Helper()
	h.app.diffState.SetSelectedLines(line, true)
	if h.app.diffState.OnLineSelection == nil {
		t.Fatal("the diff has no line-selection callback")
	}
	h.app.diffState.OnLineSelection(line)
	h.tester.Frame()
}

// The first click enters multiple-line mode and retains its line; subsequent
// plain clicks add independent lines without selecting the gap between them.
func TestPlainLineClickStartsMultipleSelection(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })
	clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1})
	if got, ok := h.app.diffState.SelectedLines(); !ok || got != (editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1}) {
		t.Fatalf("a click picked %+v, want one line", got)
	}
	if !h.app.lineSelectMode || !h.tester.HasText("Selecting multiple lines") {
		t.Fatalf("the first click did not enter multiple-line mode: %q", h.texts())
	}
	clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 3, End: 3})
	if count, ok := h.app.selectedLineCount(); !ok || count != 2 {
		t.Fatalf("two clicks selected %d lines, want 2", count)
	}
	h.tester.Frame()
	if !h.tester.HasText("2 lines selected") {
		t.Fatalf("the selection did not survive another frame: %q", h.texts())
	}
}

// Adjacent plain clicks coalesce into a range; an explicit Shift-style range
// keeps its unchanged interior.
func TestSelectMultipleLinesCoalescesAdjacentClicks(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })
	if err := h.tester.Click("Select multiple lines"); err != nil {
		t.Fatalf("enter the multi-line mode: %v", err)
	}
	h.tester.Frame()
	if !h.app.lineSelectMode {
		t.Fatal("the button did not enter the multi-line mode")
	}
	for _, want := range []string{"Selecting multiple lines",
		"Click line numbers to choose changes; Shift-click to choose a range.", "Cancel"} {
		if !h.tester.HasText(want) {
			t.Fatalf("the mode is missing %q: %q", want, h.texts())
		}
	}

	clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1})
	if !h.tester.HasText("1 line selected") {
		t.Fatalf("one click did not select its line: %q", h.texts())
	}
	clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 2, End: 2})
	got, ok := h.app.diffState.SelectedLines()
	if !ok || got != (editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 2}) {
		t.Fatalf("the second click selected %+v, want lines 1 to 2", got)
	}
	if !h.tester.HasText("2 lines selected") {
		t.Fatalf("the pane does not count the range: %q", h.texts())
	}

	// Shift-click explicitly chooses a contiguous range, including its middle.
	clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 3})
	got, _ = h.app.diffState.SelectedLines()
	if got != (editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 3}) {
		t.Fatalf("a range click left %+v, want lines 1 to 3", got)
	}
}

func TestMultipleLineModeSelectsOnlyTheChangedLinesClicked(t *testing.T) {
	h, _ := twoHunkChangesApp(t, unstagedTrackedFile())
	if err := h.tester.Click("Select multiple lines"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1})
	clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 5, End: 5})
	if !h.tester.HasText("2 lines selected") {
		t.Fatalf("two clicked changes include the unchanged lines between them: %q", h.texts())
	}
	if h.tester.HasText("5 lines selected") {
		t.Fatal("the selection became the whole range between the two clicks")
	}
	if got, _ := h.app.diffState.SelectedLines(); got != (editor.DiffLineRange{Side: editor.DiffNew, Start: 5, End: 5}) {
		t.Fatalf("the native highlight spans the gap: %+v", got)
	}
	if h.tester.HasText("Selected line") || len(h.app.diffState.Annotations) != 0 {
		t.Fatal("selecting separate lines added bottom borders to the diff")
	}
}

// A range picked in the mode is staged as one patch: only its changes move,
// and the change outside it stays in the working tree.
func TestSelectingARangeStagesThoseLines(t *testing.T) {
	h, calls := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })
	if err := h.tester.Click("Select multiple lines"); err != nil {
		t.Fatalf("enter the multi-line mode: %v", err)
	}
	h.tester.Frame()
	// New-side lines 1 to 2 hold the first change; the fourth line's change
	// is outside the range.
	clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1})
	clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 2, End: 2})
	if err := h.tester.Click("Stage selected lines"); err != nil {
		t.Fatalf("stage the range: %v", err)
	}
	h.waitFor(t, "the partial patch", func() bool {
		return findCall(calls, "apply-patch:false:") != ""
	})
	sent := findCall(calls, "apply-patch:false:")
	if !strings.Contains(sent, "+TWO") {
		t.Errorf("the patch missed the range's change: %q", sent)
	}
	if strings.Contains(sent, "+FOUR") {
		t.Errorf("the patch took the change outside the range: %q", sent)
	}
}

// Cancel leaves the mode and drops the selection; Escape does the same.
func TestCancelLeavesTheLineSelectionMode(t *testing.T) {
	for _, leave := range []struct {
		name string
		act  func(t *testing.T, h *harness)
	}{
		{"Cancel", func(t *testing.T, h *harness) {
			if err := h.tester.Click("Cancel"); err != nil {
				t.Fatalf("Cancel: %v", err)
			}
		}},
		{"Escape", func(t *testing.T, h *harness) { h.press(t, ui.KeyEscape) }},
	} {
		t.Run(leave.name, func(t *testing.T) {
			h, _ := changesTestApp(t, unstagedTrackedFile())
			h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })
			clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1})
			leave.act(t, h)
			h.tester.Frame()
			if h.app.lineSelectMode {
				t.Fatal("the mode is still on")
			}
			if _, ok := h.app.diffState.SelectedLines(); ok {
				t.Fatal("the selection was left behind")
			}
			if !h.tester.HasText("Click line numbers to select lines") {
				t.Fatalf("the pane does not return to its plain hint: %q", h.texts())
			}
		})
	}
}

// A new diff starts with the mode off: the lines of the file before must not
// be taken for the lines of the file after.
func TestANewDiffLeavesTheLineSelectionMode(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })
	if err := h.tester.Click("Select multiple lines"); err != nil {
		t.Fatalf("enter the multi-line mode: %v", err)
	}
	h.tester.Frame()
	h.app.resetFileDiff()
	if h.app.lineSelectMode || h.app.lineSelectHas {
		t.Fatal("the mode outlived the diff it was picking lines of")
	}
}

// Clicking line numbers enters the mode through the editor's input callbacks,
// and the next click extends the selection without using the toolbar button.
func TestClickingLineNumbersAutomaticallyEntersMultipleSelection(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })
	h.tester.Frame()
	caption, ok := h.tester.Find("Click line numbers to select lines")
	if !ok {
		t.Fatalf("the line-selection hint is not on screen: %q", h.texts())
	}
	button, _ := h.tester.Find("Select multiple lines")
	// The pane's rows: the column's gap, a row for the hunk's header, then a
	// row a line, at the default theme's 20 points. The line numbers align
	// with the start of the selection toolbar.
	row := func(i int) float32 { return caption.Y + caption.H + 10 + float32(i)*20 + 10 }
	h.tester.ClickAt(button.X+20, row(4)) // new-side line 1
	h.tester.Frame()
	if !h.app.lineSelectMode || !h.tester.HasText("Selecting multiple lines") {
		t.Fatalf("clicking a line number did not enter multiple-line mode: %q", h.texts())
	}
	diff, ok := h.tester.Find("Diff")
	if !ok {
		t.Fatal("the diff disappeared after selecting a line")
	}
	h.tester.ClickAt(diff.X+20, diff.Y+5*20+10) // new-side line 2
	h.tester.Frame()
	if got, ok := h.app.diffState.SelectedLines(); !ok ||
		got != (editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 2}) {
		t.Fatalf("two clicks picked %+v, want new-side lines 1 to 2", got)
	}
}
