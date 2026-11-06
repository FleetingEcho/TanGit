package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func TestRecoveryCardUsesTheStandardIslandGapAboveSavedWork(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.app.setWorkspaceTab(sectionStash)
	h.tester.Frame()

	recovery, found := h.tester.AccessByRole(ui.RoleRegion, "Recovery & checkpoints")
	if !found {
		t.Fatal("the recovery card is missing its region")
	}
	savedWork, found := h.tester.AccessByRole(ui.RoleRegion, "Saved work")
	if !found {
		t.Fatal("the saved-work card is missing its region")
	}
	gap := savedWork.Bounds.Y - (recovery.Bounds.Y + recovery.Bounds.H)
	if gap != islandGap {
		t.Fatalf("gap below the recovery card = %v, want the standard island gap %v", gap, islandGap)
	}
}

func TestRecoveryAndCheckpointActionsAskBeforeChangingSavedWork(t *testing.T) {
	h, calls := changesTestApp(t, unstagedTrackedFile())
	h.app.snapshot.WorkSnapshots = []repository.WorkSnapshot{{
		ID: "abc123", Hash: "abc123", Kind: repository.RecoverySnapshotKind,
		Name: "Discard changed.txt", CreatedAt: 1_700_000_000,
	}}
	h.app.setWorkspaceTab(sectionStash)
	h.tester.Frame()
	if !h.tester.HasText("Discard changed.txt") {
		t.Fatalf("recovery row is missing: %q", h.texts())
	}
	if err := h.tester.Click("Restore abc123"); err != nil {
		t.Fatal(err)
	}
	if h.app.confirm == nil || calls.has("restore-snapshot:abc123") {
		t.Fatal("restore did not wait for confirmation")
	}
	h.tester.Frame()
	if err := h.tester.Click("Restore"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "restored recovery", func() bool { return calls.has("restore-snapshot:abc123") })
	h.settle(t)

	// Action refresh replaces the fixture snapshot, so put the saved row back
	// to exercise its independent destructive delete route.
	h.app.snapshot.WorkSnapshots = []repository.WorkSnapshot{{ID: "abc123", Kind: repository.RecoverySnapshotKind, Name: "Discard changed.txt"}}
	h.tester.Frame()
	if err := h.tester.Click("Delete abc123"); err != nil {
		t.Fatal(err)
	}
	if h.app.confirm == nil || calls.has("delete-snapshot:abc123") {
		t.Fatal("delete did not wait for confirmation")
	}
	h.tester.Frame()
	if err := h.tester.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "deleted recovery", func() bool { return calls.has("delete-snapshot:abc123") })
	h.settle(t)

	if err := h.tester.Click("Checkpoint name"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("Before experiment")
	if err := h.tester.Click("Create checkpoint"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "created checkpoint", func() bool { return calls.has("checkpoint:Before experiment") })
}
