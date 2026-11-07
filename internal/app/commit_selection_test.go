package app

import (
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestBatchContextMenuConfirmsAndRunsSelection(t *testing.T) {
	for _, section := range []int{sectionHistory, sectionGraph} {
		t.Run(workspaceSectionIDs[section], func(t *testing.T) {
			h, calls := historyHarness(t)
			h.app.workspaceTab = section
			h.tester.Frame()
			for _, subject := range []string{"Second commit", "Initial commit"} {
				h.rightClick(t, subject)
				h.chooseMenuItem(t, "Toggle batch selection")
			}
			if len(h.app.selectedCommitsForBatch()) != 2 {
				t.Fatal("selection was not retained")
			}
			h.rightClick(t, "Second commit")
			h.requireMenuLabels(t, "Cherry-pick selected commits…", "Squash selected commits…", "Fixup selected commits…", "Drop selected commits…", "Move selected commits toward HEAD…", "Move selected commits toward older history…")
			h.chooseMenuItem(t, "Squash selected commits…")
			if h.app.confirm == nil || calls.has("batch-edit:squash") {
				t.Fatal("batch action bypassed confirmation")
			}
			if err := h.tester.Click("Apply"); err != nil {
				t.Fatal(err)
			}
			h.waitFor(t, "batch rewrite", func() bool { return calls.has("batch-edit:squash") && !h.app.actionBusy })
			if len(h.app.commitSelection) != 0 {
				t.Fatal("completed rewrite left stale selection")
			}
			h.tester.Frame()
			h.tester.Frame()
		})
	}
}

func TestBatchMenuButtonAndClearSelection(t *testing.T) {
	h, _ := historyHarness(t)
	h.app.commitSelection = map[string]bool{"c2c2c2c2": true, "c1c1c1c1": true}
	h.tester.Frame()
	if err := h.tester.Click("Batch actions"); err != nil {
		t.Fatal(err)
	}
	h.requireMenuLabels(t, "Cherry-pick selected commits…", "Squash selected commits…")
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if err := h.tester.Click("Clear selection"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if len(h.app.selectedCommitsForBatch()) != 0 {
		t.Fatal("selection not cleared")
	}
}

func TestForcePushOptionsConfirmAndCancel(t *testing.T) {
	h, calls := historyHarness(t)
	if err := h.tester.Click("Push options"); err != nil {
		t.Fatal(err)
	}
	h.chooseMenuItem(t, "Force push with lease…")
	if h.app.confirm == nil || calls.has("force-push-with-lease") {
		t.Fatal("force push bypassed confirmation")
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if calls.has("force-push-with-lease") {
		t.Fatal("cancelled force push ran")
	}
	h.app.confirmForcePushWithLease()
	h.tester.Frame()
	if err := h.tester.Click("Force push with lease"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "force push", func() bool { return calls.has("force-push-with-lease") })
}

func TestForcePushOnCurrentBranchContextMenu(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.workspaceTab = sectionBranches
	h.tester.Frame()
	h.rightClick(t, "main (local)")
	h.requireMenuLabels(t, "Force push with lease…")
	h.chooseMenuItem(t, "Force push with lease…")
	if h.app.confirm == nil {
		t.Fatal("current branch menu did not ask for confirmation")
	}
}

func TestBatchSelectionIgnoresHiddenCommits(t *testing.T) {
	h, _ := historyHarness(t)
	h.app.commitSelection = map[string]bool{"missing": true, "c2c2c2c2": true}
	got := h.app.selectedCommitsForBatch()
	if len(got) != 1 || got[0].Hash != "c2c2c2c2" {
		t.Fatalf("selection=%v", got)
	}
}

func TestBatchSelectionCheckboxAndKeyboard(t *testing.T) {
	h, _ := historyHarness(t)
	// The named checkbox is repeated per row; a direct coordinate targets the
	// first row's checkbox without relying on its shared accessibility label.
	r, ok := h.tester.Find("Select commit for batch actions")
	if !ok {
		t.Fatal("missing selection checkbox")
	}
	h.tester.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	h.tester.Frame()
	if len(h.app.selectedCommitsForBatch()) != 1 {
		t.Fatal("checkbox did not select commit")
	}
	h.app.selectedCommit = 0
	h.tester.Key(ui.Ctrl, ui.KeySpace)
	h.tester.Frame()
	if len(h.app.selectedCommitsForBatch()) != 0 {
		t.Fatal("keyboard did not toggle selection off")
	}
	h.tester.Key(ui.Ctrl, ui.KeySpace)
	h.tester.Frame()
	if len(h.app.selectedCommitsForBatch()) != 1 {
		t.Fatal("keyboard did not toggle selection on")
	}
}
