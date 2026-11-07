package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestUndoRedoShortcutsRequireConfirmationAndRun(t *testing.T) {
	tests := []struct {
		name       string
		mods       ui.Modifiers
		confirm    string
		wantCall   string
		formTitle  string
		formPrompt string
	}{
		{
			name:       "undo",
			mods:       ui.Ctrl,
			confirm:    "Undo",
			wantCall:   "undo",
			formTitle:  "Undo Git Operation",
			formPrompt: "Undo the latest eligible Git operation?",
		},
		{
			name:       "redo",
			mods:       ui.Ctrl | ui.Shift,
			confirm:    "Redo",
			wantCall:   "redo",
			formTitle:  "Redo Git Operation",
			formPrompt: "Redo the latest undone Git operation?",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h, calls := historyHarness(t)
			h.tester.Key(test.mods, ui.KeyZ)
			if h.app.confirm == nil {
				t.Fatal("the shortcut did not open a confirmation")
			}
			if !h.tester.HasText(test.formTitle) || !h.tester.HasText(test.formPrompt) {
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

func TestUndoConfirmationCanBeCancelled(t *testing.T) {
	h, calls := historyHarness(t)
	h.tester.Key(ui.Ctrl, ui.KeyZ)
	h.press(t, ui.KeyEscape)
	if h.app.confirm != nil {
		t.Fatal("Escape left the undo confirmation open")
	}
	if calls.has("undo") {
		t.Fatal("cancelling the confirmation ran Undo")
	}
}
