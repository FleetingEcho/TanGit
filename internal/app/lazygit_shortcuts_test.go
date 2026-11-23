package app

import (
	"testing"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
	"github.com/egoist/mygo/ui"
)

func selectFeatureForShortcut(t *testing.T, h *harness) {
	t.Helper()
	if err := h.tester.Click("Branches"); err != nil {
		t.Fatalf("open Branches: %v", err)
	}
	if err := h.tester.Click("feature"); err != nil {
		t.Fatalf("select feature: %v", err)
	}
	h.tester.Frame()
}

func selectSecondCommitInHistory(t *testing.T, h *harness) {
	t.Helper()
	if err := h.tester.Click("History"); err != nil {
		t.Fatalf("open History: %v", err)
	}
	h.waitFor(t, "the selected commit in History", func() bool {
		return h.tester.HasText("Second commit")
	})
	if err := h.tester.Click("Second commit"); err != nil {
		t.Fatalf("select commit from History: %v; texts=%q", err, h.tester.Texts())
	}
	h.tester.Frame()
}

func pressShortcutWithModifiers(t *testing.T, h *harness, mods ui.Modifiers, key ui.Key) {
	t.Helper()
	h.tester.Frame()
	h.tester.Key(mods, key)
	h.tester.Frame()
	h.settle(t)
}

func TestLazyGitStyleWorkflowShortcuts(t *testing.T) {
	t.Run("tag from selected commit", func(t *testing.T) {
		h, _ := menuTestAppWithCalls(t)
		selectSecondCommitInHistory(t, h)
		pressShortcutWithModifiers(t, h, ui.Shift, ui.KeyT)
		h.tester.Frame()
		if !h.app.showCommitTagForm {
			t.Fatal("T did not open a tag form for the selected commit")
		}
	})

	t.Run("branch from selected commit", func(t *testing.T) {
		h, _ := menuTestAppWithCalls(t)
		selectSecondCommitInHistory(t, h)
		h.press(t, ui.KeyN)
		if !h.app.showCommitBranchForm {
			t.Fatal("n did not create a branch from the selected commit")
		}
	})

	t.Run("fast-forward selected branch", func(t *testing.T) {
		h, calls := menuTestAppWithCalls(t)
		selectFeatureForShortcut(t, h)
		h.press(t, ui.KeyF)
		if h.app.confirm == nil {
			t.Fatal("fast-forward did not request the expected confirmation")
		}
		h.confirmAttentionForTest(t)
		h.waitFor(t, "branch fast-forward", func() bool { return calls.has("fast-forward:feature") })
	})

	t.Run("force checkout still confirms", func(t *testing.T) {
		h, calls := menuTestAppWithCalls(t)
		selectFeatureForShortcut(t, h)
		pressShortcutWithModifiers(t, h, ui.Shift, ui.KeyF)
		if h.app.confirm == nil || !h.app.confirm.destructive {
			t.Fatal("force checkout did not retain a destructive confirmation")
		}
		if err := h.tester.Click("Force checkout"); err != nil {
			t.Fatalf("approve force checkout: %v", err)
		}
		h.waitFor(t, "force checkout", func() bool { return calls.has("force-checkout:feature") })
	})

	t.Run("rename selected branch", func(t *testing.T) {
		h, _ := menuTestAppWithCalls(t)
		selectFeatureForShortcut(t, h)
		pressShortcutWithModifiers(t, h, ui.Shift, ui.KeyR)
		if !h.app.showRenameForm || h.app.renameBranch != "feature" {
			t.Fatalf("R did not target rename at feature: form=%v branch=%q selectedRow=%d", h.app.showRenameForm, h.app.renameBranch, h.app.selectedBranchRow)
		}
	})

	t.Run("stage all toggles", func(t *testing.T) {
		h, calls := menuTestAppWithCalls(t)
		h.press(t, ui.KeyA)
		h.waitFor(t, "unstaging all staged files", func() bool {
			return calls.has("unstage-all") && !h.app.actionBusy && !h.app.refreshBusy
		})
		for i := range h.app.snapshot.Files {
			h.app.snapshot.Files[i].Staged = false
		}
		h.tester.Frame()
		h.press(t, ui.KeyA)
		h.waitFor(t, "staging all after the index is clear", func() bool {
			return calls.has("stage-all") && !h.app.actionBusy && !h.app.refreshBusy
		})
	})

	t.Run("rename selected stash", func(t *testing.T) {
		h, _ := menuTestAppWithCalls(t)
		h.app.snapshot.Stashes = []repository.Stash{{Index: 0, Name: "WIP: verify bindings", Hash: "stashhash"}}
		if err := h.tester.Click("Stash"); err != nil {
			t.Fatal(err)
		}
		if err := h.tester.Click("WIP: verify bindings"); err != nil {
			t.Fatalf("select the stash: %v", err)
		}
		h.press(t, ui.KeyR)
		if !h.app.showStashRenameForm {
			t.Fatal("r did not open the selected stash's rename form")
		}
	})
}
