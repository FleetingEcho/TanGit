package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

func twoHunkChangesApp(t *testing.T, files []repository.File) (*harness, *callRecorder) {
	t.Helper()
	calls := &callRecorder{}
	a := newApp(fakeOpener{
		result: openResult{snapshot: repository.Snapshot{Root: "/work/project", Branch: "main", Files: files}},
		calls:  calls,
		patch:  twoHunkPatch,
	})
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	h.waitFor(t, "the two-hunk diff", func() bool { return h.app.diffState != nil })
	return h, calls
}

func TestChangesUseSelectedLineActionsInsteadOfHunkActions(t *testing.T) {
	h, _ := twoHunkChangesApp(t, unstagedTrackedFile())
	for _, obsolete := range []string{"Stage hunk", "Unstage hunk", "Stash hunk…", "Discard hunk…"} {
		if h.tester.HasText(obsolete) {
			t.Errorf("obsolete hunk action %q is still offered", obsolete)
		}
	}
	clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1})
	for _, action := range []string{"Stage selected lines", "Stash selected lines…"} {
		if !h.tester.HasText(action) {
			t.Errorf("selected-line action %q is missing", action)
		}
	}
}

func TestChangedFileNavigationButtonsAndShortcuts(t *testing.T) {
	h, _ := twoHunkChangesApp(t, []repository.File{
		{Path: "a.txt", Status: " M", Unstaged: true, Tracked: true},
		{Path: "b.txt", Status: " M", Unstaged: true, Tracked: true},
	})
	h.tester.Key(ui.Alt, ui.KeyK)
	h.tester.Frame()
	if change, _ := h.app.selectedChange(); change.file.Path != "b.txt" {
		t.Fatalf("previous shortcut selected %q", change.file.Path)
	}
	if err := h.tester.Click("Next changed file"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if change, _ := h.app.selectedChange(); change.file.Path != "a.txt" {
		t.Fatalf("next selected %q", change.file.Path)
	}
	if err := h.tester.Click("Previous changed file"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if change, _ := h.app.selectedChange(); change.file.Path != "b.txt" {
		t.Fatalf("previous button did not wrap to %q", change.file.Path)
	}
}
