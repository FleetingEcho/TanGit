package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func TestStashSelectedLinesUsesOnlyTheirIndependentChanges(t *testing.T) {
	h, calls := twoHunkChangesApp(t, unstagedTrackedFile())
	if err := h.tester.Click("Select multiple lines"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1})
	clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 5, End: 5})
	if err := h.tester.Click("Stash selected lines…"); err != nil {
		t.Fatal(err)
	}
	if !h.app.showStashForm || h.app.stashPatch == "" {
		t.Fatal("selected-line stash did not open its scoped form")
	}
	if !h.tester.HasText("Save only the selected lines to restore them later.") {
		t.Fatalf("form does not identify selected-line scope: %q", h.texts())
	}
	if err := h.tester.Click("Stash message"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("two changes")
	if err := h.tester.Click("Stash changes"); err != nil {
		t.Fatal(err)
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "selected lines stashed", func() bool { return findCall(calls, "stash-patch:two changes:") != "" })
	call := findCall(calls, "stash-patch:two changes:")
	if !strings.Contains(call, "+TWO") || !strings.Contains(call, "+ten and a half") || strings.Contains(call, "+FOUR") {
		t.Fatalf("stash patch has more than the two selected changes:\n%s", call)
	}
	h.settle(t)
	if h.app.showStashForm || h.app.stashPatch != "" {
		t.Fatal("successful selected-line stash left stale form scope")
	}
}

func TestStashSelectedFileKeepsScopeThroughConfirmation(t *testing.T) {
	h, calls := twoHunkChangesApp(t, unstagedTrackedFile())
	change, ok := h.app.selectedChange()
	if !ok {
		t.Fatal("no changed file")
	}
	h.app.openSelectedFileStash(change.file)
	h.tester.Frame()
	if !h.tester.HasText("Save only the selected file to restore it later.") {
		t.Fatalf("form does not identify selected-file scope: %q", h.texts())
	}
	if err := h.tester.Click("Stash message"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("one file")
	if err := h.tester.Click("Stash changes"); err != nil {
		t.Fatal(err)
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "selected file stashed", func() bool {
		return calls.has("stash-files:one file:changed.txt:false")
	})
	if len(calls.all()) == 0 || calls.has("stash") {
		t.Fatalf("selected-file stash used the whole-tree action: %#v", calls.all())
	}
}
