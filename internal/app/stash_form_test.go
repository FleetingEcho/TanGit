package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The plus beside Saved work opens one modal that names the stash and says
// whether untracked files come with it; Escape closes it.
func TestStashChangesModalNamesTheStash(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.setWorkspaceTab(sectionStash)
	h.tester.Frame()
	if err := h.tester.Click("Stash changes…"); err != nil {
		t.Fatalf("open the stash form: %v", err)
	}
	h.tester.Frame()
	if !h.app.showStashForm {
		t.Fatal("the Stash changes button opened no modal")
	}
	for _, want := range []string{"Stash Changes", "Stash message", "Include untracked", "Stash changes"} {
		if !slices.Contains(h.texts(), want) {
			t.Fatalf("the stash modal is missing %q: %q", want, h.texts())
		}
	}

	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if h.app.showStashForm {
		t.Fatal("Escape did not close the stash modal")
	}
	if h.app.stashMessage != "" || h.app.stashIncludeUntracked {
		t.Fatalf("the closed form kept %q / %v", h.app.stashMessage, h.app.stashIncludeUntracked)
	}

	// Reopen and fill it in: the message field is focused, and the
	// checkbox comes with the untracked files.
	if err := h.tester.Click("Stash changes…"); err != nil {
		t.Fatalf("reopen the stash form: %v", err)
	}
	h.tester.Frame()
	h.tester.Type("work in progress")
	if err := h.tester.Click("Include untracked"); err != nil {
		t.Fatalf("include untracked: %v", err)
	}
	h.tester.Frame()
	if h.app.stashMessage != "work in progress" || !h.app.stashIncludeUntracked {
		t.Fatalf("the form holds %q / %v", h.app.stashMessage, h.app.stashIncludeUntracked)
	}
	if err := h.tester.Click("Cancel"); err != nil {
		t.Fatalf("cancel the stash form: %v", err)
	}
	h.tester.Frame()
	if h.app.showStashForm {
		t.Fatal("Cancel did not close the stash modal")
	}
}

// The Changes header's stash button opens the same modal, and a message typed
// there is the name the stash is saved under.
func TestChangesHeaderStashesWithAMessage(t *testing.T) {
	h, calls := changesTestApp(t, sectionedFiles())
	if err := h.tester.Click("Stash changes…"); err != nil {
		t.Fatalf("open the stash form from Changes: %v", err)
	}
	h.tester.Frame()
	if !h.app.showStashForm {
		t.Fatal("the Changes stash button opened no modal")
	}
	h.tester.Type("keep my work")
	if err := h.tester.Click("Include untracked"); err != nil {
		t.Fatalf("include untracked: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Stash changes"); err != nil {
		t.Fatalf("stash the changes: %v", err)
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the stash", func() bool {
		return calls.has("stash:keep my work") && calls.has("stash-untracked")
	})
	h.settle(t)
	if h.app.showStashForm {
		t.Fatal("the modal stayed open after stashing")
	}
}

// Leaving the message empty stashes the work under a timestamped name, so a
// quick stash never asks for a name it does not need.
func TestChangesHeaderStashesWithADefaultMessage(t *testing.T) {
	h, calls := changesTestApp(t, sectionedFiles())
	if err := h.tester.Click("Stash changes…"); err != nil {
		t.Fatalf("open the stash form from Changes: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Stash changes"); err != nil {
		t.Fatalf("stash the changes: %v", err)
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the default-named stash", func() bool {
		for _, call := range calls.all() {
			if strings.HasPrefix(call, "stash:WIP on main at ") {
				return true
			}
		}
		return false
	})
	if h.app.stashMessage != "" {
		t.Fatalf("the form kept the generated message: %q", h.app.stashMessage)
	}
}
