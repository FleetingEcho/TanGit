package app

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// A two-way conflict is found from its four markers, and each side splices
// over the whole conflict: markers included, nothing else touched.
func TestConflictStatePicksASide(t *testing.T) {
	content := "first\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\nlast\n"
	state := newConflictState("file.txt", content, 0)
	if len(state.regions) != 1 {
		t.Fatalf("regions = %#v, want one", state.regions)
	}
	region := state.regions[0]
	if region.start != 1 || region.split != 3 || region.end != 5 || region.hasBase() {
		t.Fatalf("region = %#v", region)
	}

	if !state.resolve(conflictCurrent) {
		t.Fatal("picking the current side changed nothing")
	}
	if got, want := state.text(), "first\nours\nlast\n"; got != want {
		t.Fatalf("current side = %q, want %q", got, want)
	}
	if len(state.regions) != 0 {
		t.Fatalf("the conflict survived its resolution: %#v", state.regions)
	}

	if !state.undoResolution() {
		t.Fatal("undo changed nothing")
	}
	if state.text() != content {
		t.Fatalf("undo = %q, want the original", state.text())
	}

	if !state.resolve(conflictIncoming) {
		t.Fatal("picking the incoming side changed nothing")
	}
	if got, want := state.text(), "first\ntheirs\nlast\n"; got != want {
		t.Fatalf("incoming side = %q, want %q", got, want)
	}
}

// The diff3 style adds the common ancestor between the sides; both drops it,
// and picking the ancestor is only offered while it is there.
func TestConflictStateDiff3(t *testing.T) {
	content := "<<<<<<< HEAD\nours\n||||||| base\ncommon\n=======\ntheirs\n>>>>>>> topic\n"
	state := newConflictState("file.txt", content, 0)
	if len(state.regions) != 1 {
		t.Fatalf("regions = %#v, want one", state.regions)
	}
	if !state.regions[0].hasBase() {
		t.Fatal("the ancestor marker was not found")
	}

	if !state.resolve(conflictBoth) {
		t.Fatal("picking both changed nothing")
	}
	if got, want := state.text(), "ours\ntheirs\n"; got != want {
		t.Fatalf("both = %q, want %q", got, want)
	}

	state = newConflictState("file.txt", content, 0)
	if !state.resolve(conflictBase) {
		t.Fatal("picking the ancestor changed nothing")
	}
	if got, want := state.text(), "common\n"; got != want {
		t.Fatalf("base = %q, want %q", got, want)
	}
}

// A file without markers has no conflicts, and an ordinary line of equals
// signs is text, not a separator.
func TestConflictStateIgnoresNonMarkers(t *testing.T) {
	for _, content := range []string{
		"",
		"plain\ntext\n",
		"=======\nnot a marker run\n",
		"<<<<<<< HEAD\nours\n=======\n",
	} {
		state := newConflictState("file.txt", content, 0)
		if len(state.regions) != 0 {
			t.Fatalf("%q gave regions %#v", content, state.regions)
		}
		if state.resolve(conflictCurrent) {
			t.Fatalf("%q resolved something", content)
		}
	}
}

// A resolved file keeps the line ending and the final newline it had.
func TestConflictStateKeepsLineEndings(t *testing.T) {
	content := "first\r\n<<<<<<< HEAD\r\nours\r\n=======\r\ntheirs\r\n>>>>>>> topic"
	state := newConflictState("file.txt", content, 0)
	if !state.resolve(conflictCurrent) {
		t.Fatal("picking the current side changed nothing")
	}
	if got, want := state.text(), "first\r\nours"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

// Choosing another conflict moves through the file and stops at its ends.
func TestConflictStateMovesBetweenConflicts(t *testing.T) {
	content := "a\n<<<<<<< HEAD\n1\n=======\n2\n>>>>>>> t\nb\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> t\n"
	state := newConflictState("file.txt", content, 0)
	if len(state.regions) != 2 {
		t.Fatalf("regions = %#v, want two", state.regions)
	}
	if !state.moveSelection(1) {
		t.Fatal("moving down did nothing")
	}
	if state.selected != 1 {
		t.Fatalf("selected = %d, want 1", state.selected)
	}
	if state.moveSelection(1) {
		t.Fatal("moving past the last conflict worked")
	}
	if state.moveSelection(-2) {
		t.Fatal("moving before the first conflict worked")
	}
	if state.selected != 1 {
		t.Fatalf("a refused move changed the selection to %d", state.selected)
	}
	if !state.moveSelection(-1) || state.selected != 0 {
		t.Fatalf("moving up did not reach the first conflict")
	}
}

// The pane resolves the working-tree file: a pick writes the file and undo
// puts the original content back.
func TestConflictPaneResolvesTheFile(t *testing.T) {
	content := "first\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\nlast\n"
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() {
		readWorkingFile, writeWorkingFile = originalRead, originalWrite
	})
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
	var written []byte
	writeWorkingFile = func(_ string, data []byte) error {
		written = append([]byte(nil), data...)
		return nil
	}

	h, calls := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflict read", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()

	for _, text := range []string{
		"Conflict 1 of 1", "Use current", "Use incoming", "Use both",
		"Undo resolution", "Open file in editor", "Mark resolved",
	} {
		if !slices.Contains(h.texts(), text) {
			t.Fatalf("conflict control %q missing from %q", text, h.texts())
		}
	}

	if err := h.tester.Click("Use incoming"); err != nil {
		t.Fatalf("pick the incoming side: %v", err)
	}
	h.tester.Frame()
	if got, want := string(written), "first\ntheirs\nlast\n"; got != want {
		t.Fatalf("written = %q, want %q", got, want)
	}
	if len(h.app.conflictState.regions) != 0 {
		t.Fatalf("the resolved file still has conflicts: %#v", h.app.conflictState.regions)
	}
	if !slices.Contains(h.texts(), "No conflict markers") {
		t.Fatalf("the toolbar does not say the conflicts are gone: %q", h.texts())
	}

	if err := h.tester.Click("Undo resolution"); err != nil {
		t.Fatalf("undo the pick: %v", err)
	}
	h.tester.Frame()
	if string(written) != content {
		t.Fatalf("undo wrote %q, want the original", written)
	}
	if len(h.app.conflictState.regions) != 1 {
		t.Fatalf("undo restored no conflict: %#v", h.app.conflictState.regions)
	}

	// Mark resolved stages the file, which is what tells Git the conflict
	// is settled.
	if err := h.tester.Click("Mark resolved"); err != nil {
		t.Fatalf("mark the file resolved: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	if !calls.has("stage:conflict.txt") {
		t.Fatalf("mark resolved did not stage the file: %#v", calls.all())
	}
}

// The toolbar walks the file's conflicts, and each pick moves the pane on to
// the next one that is left.
func TestConflictPaneMovesBetweenConflicts(t *testing.T) {
	content := "a\n<<<<<<< HEAD\n1\n=======\n2\n>>>>>>> t\nb\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> t\n"
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() {
		readWorkingFile, writeWorkingFile = originalRead, originalWrite
	})
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
	writeWorkingFile = func(string, []byte) error { return nil }

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflict read", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Conflict 1 of 2") {
		t.Fatalf("the toolbar starts on the wrong conflict: %q", h.texts())
	}

	if err := h.tester.Click("Next conflict"); err != nil {
		t.Fatalf("move to the next conflict: %v", err)
	}
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Conflict 2 of 2") {
		t.Fatalf("moving down did not select the second conflict: %q", h.texts())
	}
	if h.app.conflictState.selected != 1 {
		t.Fatalf("selected = %d, want 1", h.app.conflictState.selected)
	}

	if err := h.tester.Click("Previous conflict"); err != nil {
		t.Fatalf("move to the previous conflict: %v", err)
	}
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Conflict 1 of 2") {
		t.Fatalf("moving up did not select the first conflict: %q", h.texts())
	}
}

// The complete hunk workflow is available without leaving the marker editor:
// navigate, pick both/incoming/current, and undo the last pick. Modified
// choices leave ordinary letters and digits available to the editor.
func TestConflictKeyboardResolvesHunks(t *testing.T) {
	content := "a\n<<<<<<< HEAD\n1\n=======\n2\n>>>>>>> t\nb\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> t\n"
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() {
		readWorkingFile, writeWorkingFile = originalRead, originalWrite
	})
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
	var written string
	writeWorkingFile = func(_ string, data []byte) error {
		written = string(data)
		return nil
	}

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflict read", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()

	h.tester.Key(0, ui.KeyF7)
	h.tester.Frame()
	if h.app.conflictState.selected != 1 {
		t.Fatalf("F7 selected conflict %d, want the second", h.app.conflictState.selected)
	}
	h.tester.Key(ui.Shift, ui.KeyF7)
	h.tester.Frame()
	if h.app.conflictState.selected != 0 {
		t.Fatalf("Shift+F7 selected conflict %d, want the first", h.app.conflictState.selected)
	}

	h.tester.Key(ui.Alt, ui.Key4)
	h.tester.Frame()
	if got, want := written, "a\n1\n2\nb\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> t\n"; got != want {
		t.Fatalf("Alt+4 wrote %q, want both sides of the first hunk %q", got, want)
	}
	h.tester.Key(ui.Alt, ui.Key3)
	h.tester.Frame()
	if got, want := written, "a\n1\n2\nb\n4\n"; got != want {
		t.Fatalf("Alt+3 wrote %q, want the incoming second hunk %q", got, want)
	}
	if len(h.app.conflictState.regions) != 0 {
		t.Fatalf("keyboard picks left conflicts: %#v", h.app.conflictState.regions)
	}

	h.tester.Key(ui.Alt, ui.KeyZ)
	h.tester.Frame()
	if got, want := written, "a\n1\n2\nb\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> t\n"; got != want {
		t.Fatalf("Alt+Z wrote %q, want the incoming pick undone %q", got, want)
	}
	h.tester.Key(ui.Alt, ui.Key1)
	h.tester.Frame()
	if got, want := written, "a\n1\n2\nb\n3\n"; got != want {
		t.Fatalf("Alt+1 wrote %q, want the current second hunk %q", got, want)
	}
}

// An interrupted merge cannot be continued while Git still reports a
// conflict. The disabled header action must not call the repository at all.
func TestContinueStaysDisabledWithUnresolvedConflicts(t *testing.T) {
	h, calls := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.app.snapshot.Operation = "merge"
	h.tester.Frame()

	if err := h.tester.Click("Continue"); err != nil {
		t.Fatalf("click disabled Continue: %v", err)
	}
	h.tester.Frame()
	if calls.has("continue") {
		t.Fatal("Continue ran with an unresolved conflict")
	}
}

// A file that cannot be read says so in the pane instead of pretending it
// has no conflicts.
func TestConflictPaneReportsAnUnreadableFile(t *testing.T) {
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) {
		return nil, errors.New("permission denied")
	}

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the failed read", func() bool {
		return !h.app.diffLoading && h.app.diffError != ""
	})
	h.tester.Frame()
	if !slices.Contains(h.texts(), "The conflicted file could not be read.") {
		t.Fatalf("the pane does not explain the read: %q", h.texts())
	}
	if !slices.Contains(h.texts(), "permission denied") {
		t.Fatalf("the pane does not show the error: %q", h.texts())
	}
}

// The repository can write markers of its own size through the
// conflict-marker-size attribute; the parser takes the file's size, and the
// default does not mistake a wider marker for text.
func TestConflictStateUsesTheFilesMarkerSize(t *testing.T) {
	content := strings.Repeat("<", 12) + " HEAD\nours\n" + strings.Repeat("=", 12) + "\ntheirs\n" + strings.Repeat(">", 12) + " topic\n"
	state := newConflictState("file.txt", content, 12)
	if len(state.regions) != 1 {
		t.Fatalf("regions = %#v, want one", state.regions)
	}
	if region := state.regions[0]; region.start != 0 || region.split != 2 || region.end != 4 {
		t.Fatalf("region = %#v", region)
	}
	if !state.resolve(conflictIncoming) {
		t.Fatal("picking the incoming side changed nothing")
	}
	if got, want := state.text(), "theirs\n"; got != want {
		t.Fatalf("incoming side = %q, want %q", got, want)
	}
	if regions := newConflictState("file.txt", content, 0).regions; len(regions) != 0 {
		t.Fatalf("the default size found the 12-character markers: %#v", regions)
	}
}

// Git leaves no markers when one side deleted the file: it keeps the other
// side in the tree, and the pane shows that side rather than an empty merge.
func TestConflictSidesPaneShowsTheKeptSide(t *testing.T) {
	const content = "line1\nline2 changed\nline3\n"
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }

	h, calls := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "DU", Staged: true, Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the kept side", func() bool {
		return !h.app.diffLoading && h.app.conflictSides != nil
	})
	h.tester.Frame()

	if h.app.conflictState != nil {
		t.Fatal("a conflict without markers opened the marker editor")
	}
	sides := h.app.conflictSides
	if sides.content != content || sides.current || !sides.incoming {
		t.Fatalf("sides = %+v, want the incoming version alone", sides)
	}
	for _, text := range []string{
		"Deleted by us",
		"The current side deleted this file. The incoming version Git kept is below.",
		"Use current",
		"Use incoming",
		"This file was deleted in this branch and changed in the incoming branch. Git kept the incoming version.",
	} {
		if !slices.Contains(h.texts(), text) {
			t.Fatalf("the pane is missing %q: %q", text, h.texts())
		}
	}
	if slices.Contains(h.texts(), "Git left conflict markers in this file. Choose a side, or edit them away and mark it resolved.") {
		t.Fatalf("the pane claims markers that do not exist: %q", h.texts())
	}

	if err := h.tester.Click("Use current"); err != nil {
		t.Fatalf("resolve with the current side: %v", err)
	}
	h.waitFor(t, "the resolution", func() bool { return calls.has("resolve:conflict.txt:current") })
}

// A conflict both sides deleted has no text at all: the pane names the state
// and still offers both resolutions.
func TestConflictSidesPaneForABothDeletedFile(t *testing.T) {
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return nil, fs.ErrNotExist }

	h, _ := changesTestApp(t, []repository.File{
		{Path: "gone.txt", Status: "DD", Staged: true, Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the missing file", func() bool {
		return !h.app.diffLoading && h.app.conflictSides != nil
	})
	h.tester.Frame()

	if h.app.conflictSides.content != "" {
		t.Fatalf("a both-deleted conflict carries %q", h.app.conflictSides.content)
	}
	for _, text := range []string{"Deleted by both", "Both sides deleted this file.", "Use current", "Use incoming"} {
		if !slices.Contains(h.texts(), text) {
			t.Fatalf("the pane is missing %q: %q", text, h.texts())
		}
	}
}

// The review of a conflict compares the file as each side would have it:
// the current side's hunks and the incoming side's, with the ancestor
// dropped and the text outside the conflicts unchanged.
func TestConflictSidesTexts(t *testing.T) {
	content := "first\n<<<<<<< HEAD\nours\n||||||| base\ncommon\n=======\ntheirs\n>>>>>>> topic\nlast\n"
	state := newConflictState("file.txt", content, 0)
	current, incoming := state.sidesTexts()
	if got, want := current, "first\nours\nlast\n"; got != want {
		t.Fatalf("current side = %q, want %q", got, want)
	}
	if got, want := incoming, "first\ntheirs\nlast\n"; got != want {
		t.Fatalf("incoming side = %q, want %q", got, want)
	}

	// A resolved conflict leaves both sides at the resolved text.
	state.resolve(conflictCurrent)
	current, incoming = state.sidesTexts()
	if current != "first\nours\nlast\n" || current != incoming {
		t.Fatalf("resolved sides = %q and %q", current, incoming)
	}
}

// A conflicted file can be reviewed like any other change: the toggle shows
// the two sides as a diff, with the layout, wrapping, saving and expanding
// the other diffs offer, and the way back to editing the markers.
func TestConflictReviewShowsBothSidesAsADiff(t *testing.T) {
	const content = "one\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nlast\n"
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }

	h, calls := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflicted file", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Review both sides") {
		t.Fatalf("the pane offers no review: %q", h.texts())
	}

	if err := h.tester.Click("Review both sides"); err != nil {
		t.Fatalf("open the review: %v", err)
	}
	h.tester.Frame()

	if !h.app.conflictReview {
		t.Fatal("the review did not open")
	}
	// The way back to the markers, and the Edit icon that opens the whole
	// file, are both there: the review is a diff, the markers are the file.
	for _, label := range []string{"Show conflict markers", "Edit"} {
		if !slices.Contains(h.texts(), label) {
			t.Fatalf("the review is missing %q: %q", label, h.texts())
		}
	}
	for _, label := range []string{
		"Toggle diff layout",
		"Toggle diff line wrapping",
		"Save diff",
		"Expand diff",
	} {
		if !slices.Contains(h.texts(), label) {
			t.Fatalf("the review is missing %q: %q", label, h.texts())
		}
	}
	review := h.app.conflictDiffState
	if review == nil {
		t.Fatal("the review has no diff state")
	}
	if got, want := review.Old.String(), "one\nours\nlast\n"; got != want {
		t.Fatalf("current side = %q, want %q", got, want)
	}
	if got, want := review.New.String(), "one\ntheirs\nlast\n"; got != want {
		t.Fatalf("incoming side = %q, want %q", got, want)
	}
	shown, ok := h.app.activeDiff()
	if !ok || shown.state != review {
		t.Fatalf("the review is not the active diff: %+v, %v", shown, ok)
	}
	if shown.path != "conflict.txt" {
		t.Fatalf("the active diff names %q, want conflict.txt", shown.path)
	}
	if !calls.has("diff:conflict.txt:unstaged") {
		t.Fatalf("the combined diff was never read: %v", calls.all())
	}

	if err := h.tester.Click("Show conflict markers"); err != nil {
		t.Fatalf("return to the editor: %v", err)
	}
	h.tester.Frame()
	if h.app.conflictReview {
		t.Fatal("the editor did not come back")
	}
	if !slices.Contains(h.texts(), "Review both sides") {
		t.Fatalf("the pane does not offer the review again: %q", h.texts())
	}
}

// A conflicted file's header carries what a changed file's does: the way to
// the review of its sides, then the controls of a diff, since the review is
// one, and its own menu last.
func TestAConflictedFileHeaderCarriesTheDiffControls(t *testing.T) {
	const content = "one\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nlast\n"
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflicted file", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()

	for _, label := range []string{
		"Edit",
		"Open file in editor",
		"Review both sides",
		"Toggle diff line wrapping",
		"Save diff",
		"Expand diff",
		"File actions",
	} {
		if !slices.Contains(h.texts(), label) {
			t.Fatalf("the conflicted file is missing %q: %q", label, h.texts())
		}
	}
	// The layout toggle belongs to the review: the markers are one buffer,
	// with no two sides to lay out until the review shows them.
	if slices.Contains(h.texts(), "Toggle diff layout") {
		t.Fatalf("the markers offer a layout to switch: %q", h.texts())
	}
	// The menu comes last, after the controls that change only the view.
	more, _ := h.tester.Find("File actions")
	expand, _ := h.tester.Find("Expand diff")
	if more.X <= expand.X {
		t.Fatalf("the overflow menu sits at %.0f, before Expand diff at %.0f", more.X, expand.X)
	}
}

// Wrapping the pane wraps the conflict's markers too, so a long conflicted
// line reads without scrolling sideways, as a diff's long line does.
func TestWrappingThePaneWrapsTheConflictMarkers(t *testing.T) {
	const content = "one\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nlast\n"
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflicted file", func() bool {
		return !h.app.diffLoading && h.app.conflictView != nil
	})
	h.tester.Frame()
	if h.app.conflictView.SoftWrap {
		t.Fatal("the markers wrap before the toggle was used")
	}
	if err := h.tester.Click("Toggle diff line wrapping"); err != nil {
		t.Fatalf("wrap the pane: %v", err)
	}
	h.tester.Frame()
	if !h.app.conflictView.SoftWrap {
		t.Fatal("the toggle left the markers unwrapped")
	}
}

// The conflicted file's menu holds the resolution actions and the two ways to
// copy its path, and none of the staging, blaming or discarding that would act
// on a file whose sides are not decided yet.
func TestAConflictedFileMenuHoldsTheResolutionActions(t *testing.T) {
	const content = "one\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nlast\n"
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflicted file", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()
	if err := h.tester.Click("File actions"); err != nil {
		t.Fatalf("open the conflicted file's menu: %v", err)
	}
	h.tester.Frame()

	for _, label := range []string{
		"Use current",
		"Use incoming",
		"Mark resolved (stage as is)",
		"Copy relative path",
		"Copy absolute path",
	} {
		if !slices.Contains(h.texts(), label) {
			t.Fatalf("the menu is missing %q: %q", label, h.texts())
		}
	}
	for _, label := range []string{"Stage", "Unstage", "Blame", "Discard changes…"} {
		if slices.Contains(h.texts(), label) {
			t.Fatalf("the conflicted file's menu offers %q: %q", label, h.texts())
		}
	}
}

// The Edit icon opens the conflicted file whole in the pane's editor, as it
// does for any changed file, and Done reads the conflict again so the markers
// show what was typed.
func TestEditingAConflictedFileWholeReturnsToItsMarkers(t *testing.T) {
	const content = "one\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nlast\n"
	// The pane reads and writes the same text, as it does on disk, so the
	// read after Done shows what was typed.
	var mu sync.Mutex
	text := content
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() { readWorkingFile, writeWorkingFile = originalRead, originalWrite })
	readWorkingFile = func(string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		return []byte(text), nil
	}
	writeWorkingFile = func(_ string, data []byte) error {
		mu.Lock()
		defer mu.Unlock()
		text = string(data)
		return nil
	}

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflicted file", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()
	if err := h.tester.Click("Edit"); err != nil {
		t.Fatalf("open the file editor: %v", err)
	}
	h.waitFor(t, "the file read", func() bool {
		return !h.app.fileEditLoading && h.app.fileEditState != nil
	})
	h.tester.Frame()
	if !h.app.fileEditing {
		t.Fatal("the Edit icon did not open the editor")
	}
	// The editor owns the file whole: the conflict actions step aside while
	// it shows, so a click meant for the file cannot mark it resolved half
	// edited.
	for _, gone := range []string{"Mark resolved", "Use current", "File actions", "Review both sides"} {
		if slices.Contains(h.texts(), gone) {
			t.Fatalf("%q still shows while the editor is open: %q", gone, h.texts())
		}
	}

	clickEditor(t, h, "Done")
	h.tester.Type("X")
	h.tester.Frame()
	h.tester.Frame()
	mu.Lock()
	wrote := text
	mu.Unlock()
	if !strings.Contains(wrote, "X") {
		t.Fatalf("typed text was not written back: %q", wrote)
	}

	if err := h.tester.Click("Done"); err != nil {
		t.Fatalf("leave the editor: %v", err)
	}
	h.waitFor(t, "the conflict read again", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil && !h.app.fileEditing
	})
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Review both sides") {
		t.Fatalf("the markers did not come back: %q", h.texts())
	}
	if !strings.Contains(h.app.conflictState.text(), "X") {
		t.Fatalf("the markers show the file as it was before the edit: %q", h.app.conflictState.text())
	}
}

// A conflicted file's header is a changed file's bar, with the way to review
// its sides where a changed file keeps its Edit icon, and the way to mark it
// resolved on the conflict toolbar under it rather than beside the name. At
// the window's default width every control must still show: none may be laid
// out past the pane's edge with no width, and the name must keep room to read.
func TestAConflictedFileFitsItsControlsInTheDefaultWindow(t *testing.T) {
	const content = "one\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nlast\n"
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }

	const path = "internal/repository/session.go"
	calls := &callRecorder{}
	a := newApp(fakeOpener{result: openResult{snapshot: repository.Snapshot{
		Root:   "/work/project",
		Branch: "main",
		Files: []repository.File{
			{Path: path, Status: "UU", Unstaged: true, Tracked: true, HasMergeConflicts: true},
		},
	}}, calls: calls})
	h := &harness{app: a, updates: newUpdates()}
	opts := appWindowOptions(nil)
	h.tester = ui.NewTester(a.view, opts.Width, opts.Height)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	h.waitFor(t, "the conflicted file", func() bool {
		return !a.diffLoading && a.conflictState != nil
	})
	h.tester.Frame()

	for _, label := range []string{
		"Edit",
		"Open file in editor",
		"Review both sides",
		"Toggle diff line wrapping",
		"Save diff",
		"Expand diff",
		"File actions",
		"Mark resolved",
		"Previous conflict",
		"Next conflict",
		"Undo resolution",
		"Use current",
		"Use incoming",
		"Use both",
	} {
		r, ok := h.tester.Find(label)
		if !ok {
			t.Fatalf("the conflicted file is missing %q: %q", label, h.texts())
		}
		if r.W <= 0 {
			t.Fatalf("%q is laid out past the pane's edge, with no width", label)
		}
	}
	name, ok := h.tester.Find(path)
	if !ok || name.W < 120 {
		t.Fatalf("the file's name is squeezed to %0.f wide", name.W)
	}
	// Mark resolved ends the resolution, so it shares the conflict toolbar's
	// row rather than the header beside the name: the header is then the bar
	// a changed file has, on one row.
	edit, _ := h.tester.Find("Edit")
	mark, _ := h.tester.Find("Mark resolved")
	if mark.Y <= edit.Y {
		t.Fatalf("Mark resolved sits on the header row at %.0f, as Edit at %.0f", mark.Y, edit.Y)
	}
}
