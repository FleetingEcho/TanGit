package app

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"lazygitapp/internal/repository"
)

// stubWorkingFile makes the pane read content and record what it writes back,
// so a test edits a file that never touches the disk.
func stubWorkingFile(t *testing.T, content string) *[]byte {
	t.Helper()
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() {
		readWorkingFile, writeWorkingFile = originalRead, originalWrite
	})
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
	written := new([]byte)
	writeWorkingFile = func(_ string, data []byte) error {
		*written = append([]byte(nil), data...)
		return nil
	}
	return written
}

// clickEditor focuses the pane's editor by clicking past the header its button
// sits in. The editor fills the rest of the pane.
func clickEditor(t *testing.T, h *harness, anchor string) {
	t.Helper()
	rect, ok := h.tester.Find(anchor)
	if !ok {
		t.Fatalf("no element shows %q: %q", anchor, h.texts())
	}
	h.tester.ClickAt(rect.X+rect.W/2, rect.Y+rect.H+120)
}

// An untracked file's text can be edited in the pane, and each keystroke is
// written back without a save action; closing the editor returns to its diff.
func TestFilePaneEditsTheWorkingFile(t *testing.T) {
	content := "{\n  \"a\": 1\n}\n"
	written := stubWorkingFile(t, content)

	h, _ := changesTestApp(t, []repository.File{
		{Path: "data.json", Status: "??", Unstaged: true, Added: true},
	})
	h.tester.Frame()
	if err := h.tester.Click("Edit"); err != nil {
		t.Fatalf("open the file editor: %v", err)
	}
	h.waitFor(t, "the file read", func() bool {
		return !h.app.fileEditLoading && h.app.fileEditState != nil
	})
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Done") {
		t.Fatalf("the pane does not offer a way out of the editor: %q", h.texts())
	}
	// The editor owns the file whole: the header keeps only the way back, so
	// the file cannot be staged or discarded while it is half edited.
	for _, gone := range []string{"File actions", "Blame", "Stage", "Unstage", "Discard changes…", "Open file in editor"} {
		if slices.Contains(h.texts(), gone) {
			t.Fatalf("%q still shows while the editor is open: %q", gone, h.texts())
		}
	}

	clickEditor(t, h, "Done")
	h.tester.Type("X")
	h.tester.Frame()
	h.tester.Frame()

	if got := string(*written); !strings.Contains(got, "X") {
		t.Fatalf("typed text was not written back: %q", got)
	}
	if got := h.app.fileEditState.Buffer.String(); !strings.Contains(got, "X") {
		t.Fatalf("the editor does not hold the typed text: %q", got)
	}

	if err := h.tester.Click("Done"); err != nil {
		t.Fatalf("close the file editor: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	if h.app.fileEditing {
		t.Fatal("closing the editor left it open")
	}
	if !slices.Contains(h.texts(), "Edit") {
		t.Fatalf("the diff does not offer the editor again: %q", h.texts())
	}
}

// A binary file is refused with a message rather than opened in an editor that
// would corrupt it.
func TestFilePaneRefusesABinaryFile(t *testing.T) {
	stubWorkingFile(t, "before\x00after")

	h, _ := changesTestApp(t, []repository.File{
		{Path: "blob.bin", Status: "??", Unstaged: true, Added: true},
	})
	h.tester.Frame()
	if err := h.tester.Click("Edit"); err != nil {
		t.Fatalf("open the file editor: %v", err)
	}
	h.waitFor(t, "the binary check", func() bool {
		return !h.app.fileEditLoading && h.app.fileEditBinary
	})
	h.tester.Frame()
	if !slices.Contains(h.texts(), "This file is binary and cannot be edited here.") {
		t.Fatalf("the pane does not explain the refusal: %q", h.texts())
	}
	if h.app.fileEditState != nil {
		t.Fatal("a binary file was opened in the editor")
	}
}

// A file that cannot be read says so, and offers to read it again.
func TestFilePaneReportsAnUnreadableFile(t *testing.T) {
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return nil, errors.New("permission denied") }

	h, _ := changesTestApp(t, []repository.File{
		{Path: "notes.txt", Status: "??", Unstaged: true, Added: true},
	})
	h.tester.Frame()
	if err := h.tester.Click("Edit"); err != nil {
		t.Fatalf("open the file editor: %v", err)
	}
	h.waitFor(t, "the failed read", func() bool {
		return !h.app.fileEditLoading && h.app.fileEditError != ""
	})
	h.tester.Frame()
	if !slices.Contains(h.texts(), "The file could not be read.") {
		t.Fatalf("the pane does not explain the read: %q", h.texts())
	}
	if !slices.Contains(h.texts(), "permission denied") {
		t.Fatalf("the pane does not show the error: %q", h.texts())
	}
	if !slices.Contains(h.texts(), "Try again") {
		t.Fatalf("the pane does not offer to read the file again: %q", h.texts())
	}
}

// The pane edits the working tree, so only a file with unstaged changes offers
// its editor: a file that is only staged shows what the index holds, and has
// no Edit icon.
func TestOnlyAWorkingTreeChangeOffersTheEditor(t *testing.T) {
	h, _ := changesTestApp(t, []repository.File{
		{Path: "staged.txt", Status: "M ", Staged: true, Tracked: true},
		{Path: "work.txt", Status: " M", Unstaged: true, Tracked: true},
	})
	h.tester.Frame()
	if !slices.Contains(h.texts(), "staged.txt") {
		t.Fatalf("the fixture's staged file does not show: %q", h.texts())
	}
	if slices.Contains(h.texts(), "Edit") {
		t.Fatalf("a staged file offers the working-tree editor: %q", h.texts())
	}

	if err := h.tester.Click("work.txt"); err != nil {
		t.Fatalf("choose the working-tree change: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	if !slices.Contains(h.texts(), "Edit") {
		t.Fatalf("a working-tree change offers no editor: %q", h.texts())
	}
}

// A file with both a staged and a working-tree change shows the editor only
// on its working-tree row: its staged row is the index's side.
func TestBothSidesOfAFileOfferTheEditorOnlyInTheChangesSection(t *testing.T) {
	h, _ := changesTestApp(t, []repository.File{
		{Path: "both.txt", Status: "MM", Staged: true, Unstaged: true, Tracked: true},
	})
	h.tester.Frame()
	if err := h.tester.Click("both.txt"); err != nil {
		t.Fatalf("choose the file's staged row: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	if h.app.selectedFile != 1 || !h.app.changeRows[h.app.selectedFile].staged {
		t.Fatalf("the click chose row %d, not the file's staged row: %+v", h.app.selectedFile, h.app.changeRows)
	}
	if slices.Contains(h.texts(), "Edit") {
		t.Fatalf("the staged side offers the working-tree editor: %q", h.texts())
	}

	// Choose the file's working-tree row, the other of the two it has.
	for i, row := range h.app.changeRows {
		if row.file.Path == "both.txt" && !row.staged {
			h.app.selectedFile = i
		}
	}
	h.tester.Frame()
	h.settle(t)
	if h.app.changeRows[h.app.selectedFile].staged {
		t.Fatalf("the chosen row is not the file's working-tree one: %+v", h.app.changeRows)
	}
	if !slices.Contains(h.texts(), "Edit") {
		t.Fatalf("the working-tree side offers no editor: %q", h.texts())
	}
}

// A read-only diff can receive focus for keyboard review without editing text.
func TestClickingTheDiffFocusesReadOnlyKeyboardReview(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })
	caption, ok := h.tester.Find("Unstaged changes")
	if !ok {
		t.Fatalf("the diff's caption is not on screen: %q", h.texts())
	}
	// Below the caption and its hunk header, in the text of the change.
	h.tester.ClickAt(caption.X+120, caption.Y+caption.H+70)
	h.tester.Frame()
	if !h.tester.Focused("New text") {
		t.Fatal("clicking the diff did not focus keyboard review")
	}
	before := h.app.diffState.New.String()
	h.tester.Type("should not edit")
	h.tester.Frame()
	if h.app.diffState.New.String() != before {
		t.Fatal("keyboard review edited a read-only diff")
	}
}

// A conflicted file is editable too: typing is written back, and the markers
// the edit leaves are found again.
func TestConflictPaneTakesHandEdits(t *testing.T) {
	content := "first\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\nlast\n"
	written := stubWorkingFile(t, content)

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflict read", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()

	clickEditor(t, h, "Conflict 1 of 1")
	h.tester.Type("Z")
	h.tester.Frame()
	h.tester.Frame()
	if got := string(*written); !strings.Contains(got, "Z") {
		t.Fatalf("typed text was not written back: %q", got)
	}

	// Typing the markers away is an edit like any other: the toolbar finds no
	// conflict left and the file is written without them.
	h.app.conflictView.Buffer.Set("resolved\n")
	h.tester.Frame()
	h.tester.Frame()
	if len(h.app.conflictState.regions) != 0 {
		t.Fatalf("the markers left after the edit were not found: %#v", h.app.conflictState.regions)
	}
	if got, want := string(*written), "resolved\n"; got != want {
		t.Fatalf("written = %q, want %q", got, want)
	}
}
