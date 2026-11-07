package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The plus beside Local branches opens one modal that names the new branch
// and the local branch it starts from; Escape closes it, and Create makes it
// from the chosen branch.
func TestBranchCreateModalPicksTheBase(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.press(t, sectionKeys[sectionBranches])
	if err := h.tester.Click("New branch…"); err != nil {
		t.Fatalf("open the branch form: %v", err)
	}
	h.tester.Frame()
	if !h.app.showBranchForm {
		t.Fatal("the New branch button opened no modal")
	}
	if h.app.newBranchBase != "main" {
		t.Fatalf("the form starts from %q, want the checked out branch", h.app.newBranchBase)
	}
	for _, want := range []string{"Create Branch", "Branch name", "From branch", "Create branch"} {
		if !slices.Contains(h.texts(), want) {
			t.Fatalf("the branch modal is missing %q: %q", want, h.texts())
		}
	}

	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if h.app.showBranchForm {
		t.Fatal("Escape did not close the branch modal")
	}

	// Reopen, type the name into the focused field, and choose the base
	// branch from the select with the keyboard.
	if err := h.tester.Click("New branch…"); err != nil {
		t.Fatalf("reopen the branch form: %v", err)
	}
	h.tester.Frame()
	h.tester.Type("topic")
	h.tester.Frame()
	if err := h.tester.Click("Base branch"); err != nil {
		t.Fatalf("open the base branch list: %v", err)
	}
	h.tester.Frame()
	h.tester.Key(0, ui.KeyDown)
	h.tester.Key(0, ui.KeyEnter)
	h.tester.Frame()
	if h.app.newBranchBase != "feature" {
		t.Fatalf("the base branch picker chose %q, want feature", h.app.newBranchBase)
	}
	if err := h.tester.Click("Create branch"); err != nil {
		t.Fatalf("create the branch: %v", err)
	}
	h.waitFor(t, "the branch to be created", func() bool {
		return calls.has("branch:topic:feature")
	})
}
