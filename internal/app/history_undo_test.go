package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func TestUndoRedoShortcutsRequireConfirmationAndRun(t *testing.T) {
	tests := []struct {
		name      string
		mods      ui.Modifiers
		confirm   string
		wantCall  string
		formTitle string
	}{
		{
			name:      "undo",
			mods:      ui.Ctrl,
			confirm:   "Undo",
			wantCall:  "undo",
			formTitle: "Undo Git Operation",
		},
		{
			name:      "redo",
			mods:      ui.Ctrl | ui.Shift,
			confirm:   "Redo",
			wantCall:  "redo",
			formTitle: "Redo Git Operation",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h, calls := historyHarness(t)
			var formPrompt string
			if test.wantCall == "undo" {
				formPrompt = trf("Undo %s?", "checkout: moving from feature to main")
			} else {
				h.app.snapshot.Reflog = []repository.Commit{
					{Hash: "old", Subject: "[lazygit undo]: moving to new"},
					{Hash: "new", Subject: "checkout: moving from feature to main"},
				}
				formPrompt = trf("Redo %s?", "checkout: moving from feature to main")
			}
			h.tester.Key(test.mods, ui.KeyZ)
			h.waitFor(t, "the confirmation", func() bool { return h.app.confirm != nil })
			if h.app.confirm == nil {
				t.Fatal("the shortcut did not open a confirmation")
			}
			if !h.tester.HasText(test.formTitle) || !h.tester.HasText(formPrompt) {
				t.Fatalf("unexpected confirmation texts %q", h.tester.Texts())
			}
			if calls.has(test.wantCall) {
				t.Fatal("reflog replay ran before confirmation")
			}
			if err := h.tester.Click(test.confirm); err != nil {
				t.Fatal(err)
			}
			h.waitFor(t, "the reflog replay", func() bool { return calls.has(test.wantCall) })
		})
	}
}

func TestUndoConfirmationNamesJournaledSquash(t *testing.T) {
	h, _ := historyHarness(t)
	h.app.snapshot.Reflog = []repository.Commit{
		{Hash: "new", Subject: "rebase (finish): returning to refs/heads/main"},
		{Hash: "combined", Subject: "rebase (pick): Combine"},
		{Hash: "start", Subject: "rebase (start): checkout old"},
		{Hash: "old", Subject: "commit: Older"},
	}
	h.app.snapshot.Operations = []repository.OperationRecord{{
		Kind: repository.OperationRebase, Summary: "Squash 2 commits", Status: repository.OperationSucceeded,
		HeadBefore: "old", HeadAfter: "new",
	}}
	h.tester.Key(ui.Ctrl, ui.KeyZ)
	h.waitFor(t, "the confirmation", func() bool { return h.app.confirm != nil })
	if h.app.confirm == nil || h.app.confirm.message != trf("Undo %s?", "Squash 2 commits") {
		t.Fatalf("squash confirmation = %q, want its journal summary", h.app.confirm.message)
	}
}

// Redo keeps its generic question. Undo no longer asks an open-ended one: with
// no recorded change and no eligible reflog action it says there is nothing to
// undo, so Ctrl+Z never opens a dialog that cannot name what it would reverse.
func TestUndoSaysNothingToUndoAndRedoKeepsGenericFallback(t *testing.T) {
	h, _ := historyHarness(t)
	h.app.snapshot.Reflog = []repository.Commit{{Hash: "new", Subject: "merge topic: Merge made by the 'ort' strategy"}}
	h.tester.Key(ui.Ctrl, ui.KeyZ)
	h.waitFor(t, "the Nothing to undo notice", func() bool {
		h.tester.Frame()
		return slices.Contains(h.texts(), tr("Nothing to undo"))
	})
	if h.app.confirm != nil {
		t.Fatalf("undo with nothing to reverse asked %q", h.app.confirm.message)
	}
	if !slices.Contains(h.texts(), tr("Nothing to undo")) {
		t.Fatalf("Ctrl+Z did not say there is nothing to undo: %q", h.texts())
	}

	h, _ = historyHarness(t)
	h.app.snapshot.Reflog = []repository.Commit{{Hash: "new", Subject: "merge topic: Merge made by the 'ort' strategy"}}
	h.tester.Key(ui.Ctrl|ui.Shift, ui.KeyZ)
	if want := tr("Redo the latest undone Git operation?"); h.app.confirm == nil || h.app.confirm.message != want {
		t.Fatalf("redo fallback message = %v, want %q", h.app.confirm, want)
	}
}

func TestUndoConfirmationCanBeCancelled(t *testing.T) {
	h, calls := historyHarness(t)
	h.tester.Key(ui.Ctrl, ui.KeyZ)
	h.waitFor(t, "the confirmation", func() bool { return h.app.confirm != nil })
	h.press(t, ui.KeyEscape)
	if h.app.confirm != nil {
		t.Fatal("Escape left the undo confirmation open")
	}
	if calls.has("undo") {
		t.Fatal("cancelling the confirmation ran Undo")
	}
}
