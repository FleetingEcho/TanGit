package app

import (
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func (h *harness) confirmAttentionForTest(t *testing.T) {
	t.Helper()
	h.waitFor(t, "operation confirmation", func() bool {
		return h.app.confirm != nil && !h.app.actionBusy && !h.app.confirm.disabled
	})
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

func TestPushShowsConfirmationBeforeDiscoveryWithoutStartingWork(t *testing.T) {
	h, calls := historyHarness(t)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	h.app.session = &blockingStackDiscoverySession{
		fakeSession: fakeSession{snapshot: h.app.snapshot, calls: calls}, release: release,
	}

	h.app.confirmPush()

	if h.app.confirm == nil {
		t.Fatalf("push confirmation is delayed behind stack discovery; working=%v", h.app.actionBusy)
	}
	if h.app.actionBusy {
		t.Fatal("stack discovery marks push as working before the confirmed action")
	}
	if !h.app.confirm.disabled {
		t.Fatal("push can be accepted before its stack preview is ready")
	}
	if calls.has("push") {
		t.Fatal("push ran before confirmation")
	}
	releaseOnce.Do(func() { close(release) })
	h.waitFor(t, "the stack preview to become ready", func() bool {
		return h.app.confirm != nil && !h.app.confirm.disabled
	})
	h.app.acceptConfirm()
	h.waitFor(t, "the accepted stacked push", func() bool {
		return calls.has("push-stack:base") && !h.app.actionBusy
	})
}

func TestCancelingPushDuringDiscoveryDoesNotReopenConfirmation(t *testing.T) {
	h, _ := historyHarness(t)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	h.app.session = &blockingStackDiscoverySession{fakeSession: fakeSession{snapshot: h.app.snapshot}, release: release}

	h.app.confirmPush()
	if h.app.confirm == nil {
		t.Fatal("push did not show its immediate confirmation")
	}
	h.app.cancelConfirm()
	releaseOnce.Do(func() { close(release) })
	h.updates.waitForUpdate(t)
	if h.app.confirm != nil {
		t.Fatal("a canceled push reopened its confirmation after stack discovery")
	}
}

func TestOperationTextButtonsLiveInChangesCard(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.snapshot.Operation = "merge"
	h.app.snapshot.OperationState = &repository.OperationState{
		Type: repository.OperationTypeMerge,
		Actions: []repository.OperationAction{
			repository.OperationActionContinue,
			repository.OperationActionAbort,
		},
	}
	h.app.workspaceTab = sectionChanges
	h.tester.Frame()
	header := requireRect(t, h.tester, "Repository header")
	card := requireRect(t, h.tester, "Operation context")
	for _, label := range []string{"Continue", "Abort"} {
		button := requireRect(t, h.tester, label)
		requireRectInside(t, label, button, card)
		if button.Y < header.Y+header.H {
			t.Errorf("operation action %q remained in the repository header: button=%+v header=%+v", label, button, header)
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
			if tc.name == "push" {
				// Await the selected commit's background data, not repository
				// mutations: only later calls count toward this guarantee.
				h.waitFor(t, "commit details", func() bool { return h.app.commitDiffState != nil })
			}
			before := len(calls.all())
			tc.run(h.app)
			if tc.name == "push" {
				h.waitFor(t, "push confirmation", func() bool { return h.app.confirm != nil && !h.app.confirm.disabled })
			}
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
