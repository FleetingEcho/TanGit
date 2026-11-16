package app

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func openRecoveryForTest(t *testing.T, h *harness) {
	t.Helper()
	if err := h.tester.Click("Recovery & checkpoints"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if _, found := h.tester.AccessByRole(ui.RoleDialog, "Recovery & checkpoints"); !found {
		t.Fatal("recovery modal did not open")
	}
}

func TestRecoveryLivesInSavedWorkModal(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.app.setWorkspaceTab(sectionStash)
	h.tester.Frame()
	if _, found := h.tester.AccessByRole(ui.RoleRegion, "Recovery & checkpoints"); found {
		t.Fatal("recovery card still occupies the workspace")
	}
	if _, found := h.tester.AccessByRole(ui.RoleRegion, "Saved work"); !found {
		t.Fatal("saved-work pane missing")
	}
	openRecoveryForTest(t, h)
	if !h.app.anyFormOpen() {
		t.Fatal("modal does not block workspace shortcuts")
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "recovery-modal", h.tester)
	}
	h.tester.SetSize(640, 480)
	h.tester.Frame()
	dialog, found := h.tester.AccessByRole(ui.RoleDialog, "Recovery & checkpoints")
	if !found || dialog.Bounds.X < 0 || dialog.Bounds.Y < 0 || dialog.Bounds.X+dialog.Bounds.W > 640 || dialog.Bounds.Y+dialog.Bounds.H > 480 {
		t.Fatalf("recovery modal exceeds the small window: %+v", dialog.Bounds)
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "recovery-modal-small", h.tester)
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if h.app.showRecoveryModal {
		t.Fatal("Escape did not close recovery")
	}
	openRecoveryForTest(t, h)
	if err := h.tester.Click("Close"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.showRecoveryModal {
		t.Fatal("Close did not close recovery")
	}
	openRecoveryForTest(t, h)
	h.app.closeAllForms()
	h.tester.Frame()
	if h.app.showRecoveryModal {
		t.Fatal("repository cleanup left recovery open")
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
	openRecoveryForTest(t, h)
	if !h.tester.HasText("Discard changed.txt") {
		t.Fatalf("recovery row is missing: %q", h.texts())
	}
	if err := h.tester.Click("Restore abc123"); err != nil {
		t.Fatal(err)
	}
	if h.app.confirm == nil || calls.has("restore-snapshot:abc123") {
		t.Fatal("restore did not wait for confirmation")
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	h.tester.Frame()
	if h.app.confirm != nil || !h.app.showRecoveryModal {
		t.Fatal("cancelling restore also closed the recovery modal")
	}
	if err := h.tester.Click("Restore abc123"); err != nil {
		t.Fatal(err)
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
