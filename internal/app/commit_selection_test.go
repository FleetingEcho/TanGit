package app

import (
	"image/color"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
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
	h.app.commitSelectionMode = true
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
	if h.app.commitSelectionMode || len(h.app.selectedCommitsForBatch()) != 0 {
		t.Fatal("cancel did not clear the selection and exit batch mode")
	}
	if _, found := h.tester.Find("Select commit for batch actions"); found {
		t.Fatal("cancel left batch checkboxes visible")
	}
}

func TestBatchSelectionBarUsesOneCompactRow(t *testing.T) {
	h, _ := historyHarness(t)
	h.app.commitSelectionMode = true
	h.app.commitSelection = map[string]bool{"c2c2c2c2": true}
	h.tester.Frame()

	count, countOK := h.tester.Find("1 selected")
	batch, batchOK := h.tester.Find("Batch actions")
	clear, clearOK := h.tester.Find("Clear selection")
	if !countOK || !batchOK || !clearOK {
		t.Fatalf("the compact selection bar is incomplete: %q", h.texts())
	}
	if count.X >= batch.X || batch.X+batch.W > clear.X {
		t.Fatalf("selection controls are not ordered on one row: count=%+v batch=%+v clear=%+v", count, batch, clear)
	}
	countCenter := count.Y + count.H/2
	if countCenter != batch.Y+batch.H/2 || countCenter != clear.Y+clear.H/2 {
		t.Fatalf("selection controls are not centered on one row: count=%+v batch=%+v clear=%+v", count, batch, clear)
	}
	if clear.W != 34 || clear.H != 26 {
		t.Fatalf("clear selection is not a compact icon: %+v", clear)
	}
}

func TestBatchSelectionBarStaysVisibleWhenEmpty(t *testing.T) {
	h, _ := historyHarness(t)
	h.app.commitSelectionMode = true
	h.tester.Frame()

	if !h.tester.HasText("0 selected") {
		t.Fatalf("empty batch mode hid its selection count: %q", h.texts())
	}
	batch, found := h.tester.Access("Batch actions")
	if !found || !batch.Disabled {
		t.Fatalf("batch actions are enabled without selected commits: %+v, found=%v", batch, found)
	}
	clear, found := h.tester.Access("Clear selection")
	if !found || clear.Disabled {
		t.Fatalf("clear selection is unavailable without selected commits: %+v, found=%v", clear, found)
	}
	h.app.selectedCommit = 1
	if err := h.tester.Click("Clear selection"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.commitSelectionMode || len(h.app.commitSelection) != 0 {
		t.Fatal("cancel at zero did not exit batch mode and discard its selection state")
	}
	if h.app.selectedCommit != 1 {
		t.Fatal("cancel moved the commit-detail selection")
	}
	if h.tester.HasText("0 selected") {
		t.Fatal("cancel left the batch-selection bar visible")
	}
}

func TestBatchSelectionHighlightsCheckedCommitsNotDetailCursor(t *testing.T) {
	for _, section := range []int{sectionHistory, sectionGraph} {
		t.Run(workspaceSectionIDs[section], func(t *testing.T) {
			h, _ := historyHarness(t)
			h.app.workspaceTab = section
			h.app.selectedCommit = 1
			h.tester.Move(0, 0)
			h.tester.Frame()
			topSubject := h.app.visibleCommits()[0].Subject
			accent := commitRowBackground(t, h, "Initial commit")
			unselected := commitRowBackground(t, h, topSubject)

			h.app.selectedCommit = 0
			h.app.commitSelectionMode = true
			h.app.commitSelection = map[string]bool{"c1c1c1c1": true}
			h.tester.Move(0, 0)
			h.tester.Frame()
			if h.app.selectedCommit != 0 {
				t.Fatal("batch highlighting moved the commit-detail cursor")
			}
			if got := commitRowBackground(t, h, topSubject); got != unselected {
				t.Fatalf("the unchecked detail-selected row background = %v, want transparent-list background %v (accent %v)", got, unselected, accent)
			}
			top, topOK := h.tester.AccessByRole(ui.RoleListItem, topSubject)
			initial, initialOK := h.tester.AccessByRole(ui.RoleListItem, "Initial commit")
			if !topOK || !initialOK || initial.Bounds.Y-top.Bounds.Y-top.Bounds.H != 4 {
				t.Fatalf("batch rows do not have a 4 DIP gap: top=%+v initial=%+v", top, initial)
			}
			if got := commitRowBackground(t, h, "Initial commit"); got != accent {
				t.Fatalf("checked commit has background %v, want selected background %v", got, accent)
			}

			if err := h.tester.Click("Select multiple commits"); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			if got := commitRowBackground(t, h, topSubject); got != accent {
				t.Fatal("leaving batch mode did not restore the detail-selected row background")
			}
		})
	}
}

func commitRowBackground(t *testing.T, h *harness, subject string) color.RGBA {
	t.Helper()
	var shortHash string
	for _, commit := range h.app.visibleCommits() {
		if commit.Subject == subject {
			shortHash = commit.ShortHash
			break
		}
	}
	r, ok := h.tester.Find(shortHash)
	if !ok {
		t.Fatalf("commit row %q is not visible", subject)
	}
	return color.RGBAModel.Convert(h.tester.Image().At(int(r.X+r.W+4), int(r.Y+r.H/2))).(color.RGBA)
}

func TestForcePushWithLeaseConfirmAndCancel(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.workspaceTab = sectionBranches
	h.tester.Frame()
	h.rightClick(t, "main (local)")
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
	if _, found := h.tester.Find("Push options"); found {
		t.Fatal("the header still shows the rare push-options menu")
	}
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
	if got := h.app.selectedCommitsForBatch(); len(got) != 0 {
		t.Fatalf("disabled selection mode returned hidden commits: %v", got)
	}
	h.app.commitSelectionMode = true
	got := h.app.selectedCommitsForBatch()
	if len(got) != 1 || got[0].Hash != "c2c2c2c2" {
		t.Fatalf("selection=%v", got)
	}
}

func TestBatchSelectionModeHidesAndClearsSelection(t *testing.T) {
	for _, section := range []int{sectionHistory, sectionGraph} {
		t.Run(workspaceSectionIDs[section], func(t *testing.T) {
			h, _ := historyHarness(t)
			h.app.workspaceTab = section
			h.tester.Frame()
			if _, found := h.tester.Find("Select commit for batch actions"); found {
				t.Fatal("commit checkboxes are visible before multi-select is enabled")
			}
			if err := h.tester.Click("Select multiple commits"); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			if _, found := h.tester.Find("Select commit for batch actions"); !found {
				t.Fatal("multi-select did not reveal commit checkboxes")
			}
			h.app.commitSelection = map[string]bool{"c2c2c2c2": true}
			if err := h.tester.Click("Select multiple commits"); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			if h.app.commitSelectionMode || len(h.app.commitSelection) != 0 || len(h.app.selectedCommitsForBatch()) != 0 {
				t.Fatal("leaving multi-select retained a hidden selection")
			}
			if _, found := h.tester.Find("Select commit for batch actions"); found {
				t.Fatal("commit checkboxes stayed visible after multi-select was disabled")
			}
		})
	}
}

func TestBatchSelectionClickingCommitTextTogglesCheckmark(t *testing.T) {
	for _, section := range []int{sectionHistory, sectionGraph} {
		t.Run(workspaceSectionIDs[section], func(t *testing.T) {
			h, _ := historyHarness(t)
			h.app.workspaceTab = section
			h.app.commitSelectionMode = true
			h.tester.Frame()
			clicked, found := h.tester.AccessByRole(ui.RoleListItem, "Initial commit")
			if !found {
				t.Fatal("missing commit row")
			}
			x, y := clicked.Bounds.X+clicked.Bounds.W/2, clicked.Bounds.Y+clicked.Bounds.H/2
			h.tester.ClickAt(x, y)
			h.tester.Frame()
			if !h.app.commitSelection["c1c1c1c1"] {
				t.Fatal("clicking commit text did not check it for batch actions")
			}
			h.tester.Advance(time.Second)
			h.tester.ClickAt(x, y)
			h.tester.Frame()
			if h.app.commitSelection["c1c1c1c1"] {
				t.Fatal("clicking checked commit text did not uncheck it")
			}
			checkbox, found := h.tester.Find("Select commit for batch actions")
			if !found {
				t.Fatal("missing selection checkbox")
			}
			h.tester.ClickAt(checkbox.X+checkbox.W/2, checkbox.Y+checkbox.H/2)
			h.tester.Frame()
			if len(h.app.selectedCommitsForBatch()) != 1 {
				t.Fatal("a checkbox click toggled more than once")
			}
			h.app.cancelCommitSelection()
			h.tester.Frame()
			clicked, found = h.tester.AccessByRole(ui.RoleListItem, "Initial commit")
			if !found {
				t.Fatal("missing ordinary commit row")
			}
			h.tester.Advance(time.Second)
			h.tester.ClickAt(clicked.Bounds.X+clicked.Bounds.W/2, clicked.Bounds.Y+clicked.Bounds.H/2)
			h.tester.Frame()
			if h.app.commitSelectionMode || len(h.app.commitSelection) != 0 {
				t.Fatal("ordinary commit text click entered or changed batch selection")
			}
		})
	}
}

func TestBatchSelectionCheckboxAndKeyboard(t *testing.T) {
	h, _ := historyHarness(t)
	if err := h.tester.Click("Select multiple commits"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
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
	if err := h.tester.Click("Select multiple commits"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Key(ui.Ctrl, ui.KeySpace)
	h.tester.Frame()
	if !h.app.commitSelectionMode || len(h.app.selectedCommitsForBatch()) != 1 {
		t.Fatal("keyboard selection did not enter multi-select mode")
	}
	if _, found := h.tester.Find("Select commit for batch actions"); !found {
		t.Fatal("keyboard selection did not reveal its checkbox")
	}
}
