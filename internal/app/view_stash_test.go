package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// stashTestApp opens a repository holding one saved stash, with a recorder
// the test reads to see what the pane asked the session to read.
func stashTestApp(t *testing.T) (*harness, *callRecorder) {
	t.Helper()
	calls := &callRecorder{}
	a := newApp(fakeOpener{result: openResult{snapshot: repository.Snapshot{
		Root:   "/work/project",
		Branch: "main",
		Stashes: []repository.Stash{
			{Index: 0, Name: "WIP: native stash", Hash: "stashhash", Recency: "2m"},
		},
	}}, calls: calls})
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	return h, calls
}

// The Saved work page reads like History and Branches: the stashes on the
// left, and the files the chosen stash saved on the right. Each file opens
// its own diff rather than the whole stash as one stitched patch.
func TestStashPageListsFilesAndShowsTheChosenDiff(t *testing.T) {
	h, calls := stashTestApp(t)
	if err := h.tester.Click("Stash"); err != nil {
		t.Fatalf("open the Saved work section: %v", err)
	}
	h.waitFor(t, "the stash files", func() bool { return calls.has("stash-files:0") })
	h.waitFor(t, "the first file's diff", func() bool {
		return calls.has("stash-file-diff:0:one.txt") && h.app.stashDiffState != nil
	})
	for _, want := range []string{"Saved work", "WIP: native stash", "Changed files", "one.txt", "two.txt"} {
		if !h.tester.HasText(want) {
			t.Errorf("the stash page is missing %q: %q", want, h.texts())
		}
	}
	if len(h.app.stashFiles) != 2 {
		t.Fatalf("the page lists %d files, want 2", len(h.app.stashFiles))
	}
	if h.app.selectedStashFile != 0 {
		t.Fatalf("the chosen file is %d, want the first", h.app.selectedStashFile)
	}

	if err := h.tester.Click("two.txt"); err != nil {
		t.Fatalf("choose the second file: %v", err)
	}
	h.waitFor(t, "the second file's diff", func() bool {
		return calls.has("stash-file-diff:0:two.txt") && h.app.stashDiffPath == "two.txt"
	})
	if h.app.selectedStashFile != 1 {
		t.Fatalf("the chosen file is %d, want the second", h.app.selectedStashFile)
	}
}

// Saved files and their diff share a draggable horizontal divider, so a long
// path list can take more room without permanently crowding out the diff.
func TestStashFilesAndDiffShareTheirHeight(t *testing.T) {
	h, calls := stashTestApp(t)
	if err := h.tester.Click("Stash"); err != nil {
		t.Fatalf("open the Saved work section: %v", err)
	}
	h.waitFor(t, "the stash files", func() bool { return calls.has("stash-files:0") })
	h.waitFor(t, "the first file's diff", func() bool { return h.app.stashDiffState != nil })

	row, ok := h.tester.Find("one.txt")
	if !ok {
		t.Fatalf("the pane lists no file: %q", h.texts())
	}
	header, ok := h.tester.Find("Changed files")
	if !ok {
		t.Fatalf("the pane has no files header: %q", h.texts())
	}
	lineY := func() float32 { return header.Y + h.app.stashFilesHeight }
	x := row.X + 200
	before := h.app.stashFilesHeight
	dragTo(t, h.tester, x, lineY(), 0, -50)
	if h.app.stashFilesHeight > before-30 {
		t.Fatalf("dragging the line up left the files %v high, was %v",
			h.app.stashFilesHeight, before)
	}
}

// The saved-work pane reads the files of the chosen stash only: choosing
// another stash starts a new read rather than leaving the first stash's files
// beside the second stash's name.
func TestChoosingAnotherStashReadsItsFiles(t *testing.T) {
	h, calls := stashTestApp(t)
	h.app.snapshot.Stashes = append(h.app.snapshot.Stashes,
		repository.Stash{Index: 1, Name: "Older saved work", Hash: "olderhash", Recency: "1h"})
	h.tester.Frame()
	if err := h.tester.Click("Stash"); err != nil {
		t.Fatalf("open the Saved work section: %v", err)
	}
	h.waitFor(t, "the first stash's files", func() bool { return calls.has("stash-files:0") })
	if err := h.tester.Click("Older saved work"); err != nil {
		t.Fatalf("choose the older stash: %v", err)
	}
	h.waitFor(t, "the older stash's files", func() bool { return calls.has("stash-files:1") })
}

// The stash row wears apply and discard on the pointer's row, the way the
// commit rows of the Branches page wear cherry-pick and checkout. Applying
// saved work is routine and runs at once; discarding it is destructive and
// asks first.
func TestStashRowsOfferApplyAndDiscard(t *testing.T) {
	h, calls := stashTestApp(t)
	if err := h.tester.Click("Stash"); err != nil {
		t.Fatalf("open the Saved work section: %v", err)
	}
	h.waitFor(t, "the stash files", func() bool { return calls.has("stash-files:0") })

	row, ok := h.tester.Find("WIP: native stash")
	if !ok {
		t.Fatalf("the stash row is missing: %q", h.texts())
	}
	h.tester.Move(row.X+row.W/2, row.Y+row.H/2)
	h.tester.Frame()
	if err := h.tester.Click("Apply WIP: native stash"); err != nil {
		t.Fatalf("apply from the row icon: %v", err)
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the stash apply", func() bool { return calls.has("stash-apply:0:false") })
	h.waitFor(t, "the apply to settle", func() bool { return !h.app.actionBusy })

	// Discard is destructive: the row icon asks before it drops anything,
	// and Escape leaves the saved work alone.
	h.tester.Move(row.X+row.W/2, row.Y+row.H/2)
	h.tester.Frame()
	if err := h.tester.Click("Discard WIP: native stash"); err != nil {
		t.Fatalf("discard from the row icon: %v", err)
	}
	h.tester.Frame()
	if h.app.confirm == nil {
		t.Fatal("the row icon discarded the saved work without asking")
	}
	if calls.has("stash-drop:0") {
		t.Fatal("the row icon dropped the stash before the dialog was answered")
	}
	h.press(t, ui.KeyEscape)
	if calls.has("stash-drop:0") {
		t.Fatal("cancelling the confirmation still dropped the stash")
	}

	// The dialog's own button is the only thing that runs the drop.
	h.tester.Move(row.X+row.W/2, row.Y+row.H/2)
	h.tester.Frame()
	if err := h.tester.Click("Discard WIP: native stash"); err != nil {
		t.Fatalf("discard from the row icon: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Drop"); err != nil {
		t.Fatalf("confirm the drop: %v", err)
	}
	h.waitFor(t, "the stash drop", func() bool { return calls.has("stash-drop:0") })
}

// The file rows wear the same pair. Applying a file lands its saved version
// in the working tree and leaves the saved work saved; discarding a file
// rewrites the saved work without it — destructive, so it asks first and
// names the file, and only its button runs the rewrite.
func TestStashFileRowsOfferApplyAndDiscard(t *testing.T) {
	h, calls := stashTestApp(t)
	if err := h.tester.Click("Stash"); err != nil {
		t.Fatalf("open the Saved work section: %v", err)
	}
	h.waitFor(t, "the stash files", func() bool { return calls.has("stash-files:0") })
	h.waitFor(t, "the file rows", func() bool {
		_, ok := h.tester.Find("one.txt")
		return ok
	})

	one, ok := h.tester.Find("one.txt")
	if !ok {
		t.Fatalf("the file row is missing: %q", h.texts())
	}
	h.tester.Move(one.X+one.W/2, one.Y+one.H/2)
	h.tester.Frame()
	if err := h.tester.Click("Apply one.txt"); err != nil {
		t.Fatalf("apply from the row icon: %v", err)
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the file apply", func() bool { return calls.has("stash-apply-file:0:one.txt") && !h.app.actionBusy })
	h.waitFor(t, "the apply to settle", func() bool { return !h.app.actionBusy })

	// The action re-reads the saved work; wait for the rows to come back
	// before looking the file up again.
	h.waitFor(t, "the file rows", func() bool {
		_, ok := h.tester.Find("one.txt")
		return ok
	})
	one, ok = h.tester.Find("one.txt")
	if !ok {
		t.Fatalf("the file row is missing: %q", h.texts())
	}
	h.tester.Move(one.X+one.W/2, one.Y+one.H/2)
	h.tester.Frame()
	if err := h.tester.Click("Discard one.txt"); err != nil {
		t.Fatalf("discard from the row icon: %v", err)
	}
	h.tester.Frame()
	if h.app.confirm == nil {
		t.Fatal("the file row discarded the saved work without asking")
	}
	if !slices.Contains(h.texts(), "Discard one.txt from WIP: native stash?") {
		t.Fatalf("the confirmation does not name the file: %q", h.texts())
	}
	if calls.has("stash-discard-file:0:one.txt:WIP: native stash") {
		t.Fatal("the file row discarded before the dialog was answered")
	}
	h.press(t, ui.KeyEscape)
	if calls.has("stash-discard-file:0:one.txt:WIP: native stash") {
		t.Fatal("cancelling the confirmation still discarded the file")
	}

	// The dialog's button runs the rewrite, passing the stash's name so the
	// rewritten entry keeps it.
	h.tester.Move(one.X+one.W/2, one.Y+one.H/2)
	h.tester.Frame()
	if err := h.tester.Click("Discard one.txt"); err != nil {
		t.Fatalf("discard from the row icon: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Discard"); err != nil {
		t.Fatalf("confirm the file discard: %v", err)
	}
	h.waitFor(t, "the file discard", func() bool {
		return calls.has("stash-discard-file:0:one.txt:WIP: native stash")
	})
}

// The file rows' right-click menu carries the two row actions as well:
// applying and discarding both require confirmation.
func TestStashFileMenuOffersApplyAndDiscard(t *testing.T) {
	h, calls := stashTestApp(t)
	if err := h.tester.Click("Stash"); err != nil {
		t.Fatalf("open the Saved work section: %v", err)
	}
	h.waitFor(t, "the file rows", func() bool {
		_, ok := h.tester.Find("one.txt")
		return ok
	})

	h.rightClick(t, "one.txt")
	h.requireMenuLabels(t, "Apply this file", "Discard this file…")
	h.chooseMenuItem(t, "Apply this file")
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the file apply", func() bool { return calls.has("stash-apply-file:0:one.txt") && !h.app.actionBusy })
	if h.app.confirm != nil {
		t.Fatal("the confirmed apply left its modal open")
	}

	// The discard asks first and names the file, and Escape leaves the
	// saved work whole.
	h.waitFor(t, "the file rows", func() bool {
		_, ok := h.tester.Find("one.txt")
		return ok
	})
	h.rightClick(t, "one.txt")
	h.chooseMenuItem(t, "Discard this file…")
	h.tester.Frame()
	if h.app.confirm == nil {
		t.Fatal("the menu discarded the saved work without asking")
	}
	if !slices.Contains(h.texts(), "Discard one.txt from WIP: native stash?") {
		t.Fatalf("the confirmation does not name the file: %q", h.texts())
	}
	if calls.has("stash-discard-file:0:one.txt:WIP: native stash") {
		t.Fatal("the menu discarded before the dialog was answered")
	}
	h.press(t, ui.KeyEscape)
	if calls.has("stash-discard-file:0:one.txt:WIP: native stash") {
		t.Fatal("cancelling the confirmation still discarded the file")
	}

	// The dialog's button is what runs the rewrite.
	h.waitFor(t, "the file rows", func() bool {
		_, ok := h.tester.Find("one.txt")
		return ok
	})
	h.rightClick(t, "one.txt")
	h.chooseMenuItem(t, "Discard this file…")
	h.tester.Frame()
	if err := h.tester.Click("Discard"); err != nil {
		t.Fatalf("confirm the file discard: %v", err)
	}
	h.waitFor(t, "the file discard", func() bool {
		return calls.has("stash-discard-file:0:one.txt:WIP: native stash")
	})
}
