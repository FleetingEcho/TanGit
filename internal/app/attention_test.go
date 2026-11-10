package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func (h *harness) confirmAttentionForTest(t *testing.T) {
	t.Helper()
	if h.app.confirm == nil {
		t.Fatal("operation did not ask for confirmation")
	}
	if h.app.actionBusy {
		t.Fatal("operation started before confirmation")
	}
	h.tester.Frame()
	if err := h.tester.Click("Continue"); err != nil {
		t.Fatal(err)
	}
}

func TestHeaderTextButtonsShareTheIconButtonHeight(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.snapshot.Operation = "merge"
	h.tester.Frame()
	icon, found := h.tester.Find("Fetch")
	if !found {
		t.Fatal("header icon button is missing")
	}
	for _, label := range []string{"Continue", "Abort…"} {
		button, found := h.tester.Find(label)
		if !found {
			t.Fatalf("header action %q is missing", label)
		}
		if button.H != icon.H {
			t.Errorf("%s height = %.1f, icon height = %.1f", label, button.H, icon.H)
		}
		if button.X+button.W > icon.X {
			t.Errorf("%s ends at %.1f, after the icon group starts at %.1f", label, button.X+button.W, icon.X)
		}
	}
}

func TestAttentionOperationsRequireConfirmationAndCancel(t *testing.T) {
	cases := []struct {
		name string
		run  func(*app)
	}{
		{"push", func(a *app) { a.confirmPush() }},
		{"pull", func(a *app) { a.confirmPull() }},
		{"checkout", func(a *app) { a.checkoutBranch("feature") }},
		{"branch pull", func(a *app) { a.beginPullBranch(a.snapshot.Branches[0]) }},
		{"fast forward", func(a *app) { a.beginFastForwardBranch(a.snapshot.Branches[0]) }},
		{"unset upstream", func(a *app) { a.beginUnsetUpstream(a.snapshot.Branches[0]) }},
		{"set upstream", func(a *app) { a.beginSetUpstream(a.snapshot.Remotes[0], a.snapshot.Remotes[0].Branches[0]) }},
		{"stash", func(a *app) { a.beginStashChanges() }},
		{"stash staged", func(a *app) { a.stashStagedChanges() }},
		{"apply stash", func(a *app) { a.applyStash(repository.Stash{Index: 0, Name: "saved work"}) }},
		{"apply stash file", func(a *app) {
			a.applyStashFile(repository.Stash{Index: 0, Name: "saved work"}, repository.CommitFile{Path: "one.txt"})
		}},
		{"amend", func(a *app) { a.commitAmend = true; a.commitSummary = "amended"; a.beginCommit() }},
		{"remote edit", func(a *app) {
			a.remoteOriginalName = "origin"
			a.remoteName = "backup"
			a.remoteURL = "https://example.com/new.git"
			a.beginUpdateRemote()
		}},
		{"tag push", func(a *app) { a.selectedTag = 0; a.tagRemote = "origin"; a.beginPushTag() }},
		{"continue", func(a *app) { a.snapshot.Operation = "rebase"; a.confirmContinueOperation() }},
		{"whole file resolution", func(a *app) { a.resolveConflictFile(repository.File{Path: "one.txt"}, true) }},
		{"submodule update", func(a *app) {
			a.snapshot.Submodules = []repository.Submodule{{Name: "lib", Path: "lib"}}
			a.selectedSubmodule = 0
			a.updateSelectedSubmodule()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, calls := historyHarness(t)
			before := len(calls.all())
			tc.run(h.app)
			if h.app.confirm == nil {
				t.Fatal("operation did not ask for confirmation")
			}
			if h.app.confirm.detail == "" {
				t.Fatal("confirmation does not explain consequences")
			}
			h.tester.Frame()
			h.tester.Frame()
			if h.app.actionBusy || len(calls.all()) != before {
				t.Fatal("operation ran before confirmation")
			}
			h.tester.Key(0, ui.KeyEscape)
			h.tester.Frame()
			h.tester.Frame()
			if h.app.confirm != nil || h.app.actionBusy || len(calls.all()) != before {
				t.Fatal("cancel did not leave repository untouched")
			}
		})
	}
}

func TestConfirmationRejectsChangedRepositoryOrBranch(t *testing.T) {
	for _, change := range []string{"repository", "branch"} {
		t.Run(change, func(t *testing.T) {
			h, _ := historyHarness(t)
			ran := false
			h.app.askConfirm(confirmRequest{title: "Review", run: func() { ran = true }})
			if change == "repository" {
				h.app.generation++
			} else {
				h.app.snapshot.Branch = "other"
			}
			h.app.confirm.run()
			if ran {
				t.Fatal("stale confirmation ran against a different target")
			}
		})
	}
}
